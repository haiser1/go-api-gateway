package gateway

import (
	"fmt"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/haiser1/go-api-gateway/internal/config"
	// Anda perlu handler httprouter di sini
)

// ContextKey is a custom type for context keys to avoid collisions.
type ContextKey string

// UpstreamLatencyKey is the key for storing upstream latency in the request context.
const UpstreamLatencyKey ContextKey = "UpstreamLatency"

type BreakerState struct {
	mu          sync.RWMutex
	isDown      bool
	lastFailure time.Time
}

// Timeout sebelum mencoba service lagi
const breakerTimeout = 30 * time.Second

// IsDown (Tripped) memeriksa apakah sirkuit terbuka
func (b *BreakerState) IsDown() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	// Jika tidak 'down', sirkuit tertutup
	if !b.isDown {
		return false
	}
	// Jika 'down', cek apakah timeout sudah berlalu
	if time.Since(b.lastFailure) > breakerTimeout {
		// Timeout telah berlalu, kita masuk ke "Half-Open".
		// Kita akan membiarkan request ini lewat.
		return false
	}
	// 'Down' dan masih dalam periode timeout
	return true
}

// RecordFailure (Open Circuit) mencatat kegagalan dan membuka sirkuit
func (b *BreakerState) RecordFailure() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.isDown = true
	b.lastFailure = time.Now()
}

// RecordSuccess (Close Circuit) mencatat keberhasilan dan menutup sirkuit
func (b *BreakerState) RecordSuccess() {
	b.mu.Lock()
	defer b.mu.Unlock()
	// Hanya reset jika statusnya 'down' (terbuka)
	if b.isDown {
		b.isDown = false
		log.Printf("[INFO] Circuit Breaker: Sirkuit ditutup, service kembali online.")
	}
}

// Peta global untuk semua circuit breaker, satu per host
var serviceBreakers sync.Map // map[string]*BreakerState

// getBreaker mengambil (atau membuat) breaker untuk host tertentu
func getBreaker(host string) *BreakerState {
	iface, ok := serviceBreakers.Load(host)
	if !ok {
		// Buat breaker baru jika belum ada
		newState := &BreakerState{}
		iface, _ = serviceBreakers.LoadOrStore(host, newState)
	}
	return iface.(*BreakerState)
}

// --- AKHIR IMPLEMENTASI CIRCUIT BREAKER ---

// latencyTransport sekarang juga mengelola Circuit Breaker
type latencyTransport struct {
	wrapped http.RoundTripper
}

// RoundTrip menjalankan satu transaksi HTTP
func (t *latencyTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	host := r.Host
	breaker := getBreaker(host)

	// 1. Cek Circuit Breaker SEBELUM mengirim request
	if breaker.IsDown() {
		// Sirkuit terbuka, langsung gagalkan request
		return nil, fmt.Errorf("circuit breaker is open for %s", host)
	}

	// Sirkuit tertutup atau half-open, lanjutkan request
	start := time.Now()
	resp, err := t.wrapped.RoundTrip(r)
	duration := time.Since(start)

	// Update latensi (jika perlu)
	if latencyPtr, ok := r.Context().Value(UpstreamLatencyKey).(*time.Duration); ok && latencyPtr != nil {
		*latencyPtr = duration
	}

	// 2. Cek hasil dan update Circuit Breaker
	if err != nil || (resp != nil && resp.StatusCode >= 500) {
		// Kegagalan (network error ATAU 5xx), buka sirkuit
		breaker.RecordFailure()
		log.Printf("[ERROR] Circuit Breaker: Sirkuit terbuka untuk %s karena: %v", host, err)
	} else {
		// Request berhasil (non-5xx), tutup sirkuit
		breaker.RecordSuccess()
	}

	return resp, err
}

var sharedTransport = &http.Transport{
	MaxIdleConns:          100,
	MaxIdleConnsPerHost:   10,
	IdleConnTimeout:       90 * time.Second,
	DisableCompression:    false,
	ResponseHeaderTimeout: 5 * time.Second, // timeout header backend
	TLSHandshakeTimeout:   5 * time.Second,
	ExpectContinueTimeout: 1 * time.Second,
}

var pluginRegistry = make(map[string]func(map[string]interface{}) PluginMiddleware)

// RegisterPlugin digunakan saat init() untuk daftar plugin baru
func RegisterPlugin(name string, builder func(map[string]interface{}) PluginMiddleware) {
	pluginRegistry[name] = builder
}

// ResolvedRoute adalah rute yang sudah di-resolve, siap untuk di-proxy
type ResolvedRoute struct {
	TargetURL *url.URL              // URL lengkap microservice (cth: http://localhost:8081)
	Plugins   []config.PluginConfig // Rantai plugin yang SUDAH di-resolve
}

// Proxy adalah Data Plane. Dia memegang tabel routing di memori.
type Proxy struct {
	mu            sync.RWMutex
	routingTable  map[string]ResolvedRoute  // Kunci: "GET /api/v1/users"
	serviceMap    map[string]config.Service // Kunci: "service-id"
	globalPlugins []config.PluginConfig
}

// NewProxy membuat proxy baru dari config awal
func NewProxy(cfg *config.Config) *Proxy {
	p := &Proxy{}
	p.UpdateRoutes(cfg) // Membangun tabel routing awal
	return p
}

// UpdateRoutes adalah fungsi hot-reload. Dia membangun tabel routing baru
// dan menukarnya (swap) secara thread-safe.
func (p *Proxy) UpdateRoutes(cfg *config.Config) {
	newTable, newServiceMap, newGlobalPlugins := p.buildRoutingTable(cfg)

	p.mu.Lock() // Lock untuk menulis
	p.routingTable = newTable
	p.serviceMap = newServiceMap
	p.globalPlugins = newGlobalPlugins
	p.mu.Unlock()
}

// buildRoutingTable adalah "otak" dari proxy.
// Dia mengubah config menjadi map yang cepat untuk di-lookup.
func (p *Proxy) buildRoutingTable(cfg *config.Config) (map[string]ResolvedRoute, map[string]config.Service, []config.PluginConfig) {

	newTable := make(map[string]ResolvedRoute)

	// 1. Buat map service untuk lookup cepat (ID -> Service)
	newServiceMap := make(map[string]config.Service)
	for _, s := range cfg.Services {
		newServiceMap[s.Id] = s
	}

	// 2. Dapatkan plugin global
	newGlobalPlugins := cfg.GlobalPlugins

	// 3. Iterasi semua RUTE dan resolve
	for _, route := range cfg.Routes {
		service, serviceFound := newServiceMap[route.ServiceId]
		if !serviceFound {
			log.Printf("Warning: Route '%s' (ID: %s) menunjuk ke serviceId '%s' yang tidak ada. Rute ini dilewati.", route.Name, route.Id, route.ServiceId)
			continue
		}

		// Bangun URL target
		scheme := "http"
		if service.Protocol != "" {
			scheme = service.Protocol
		}

		targetURL := fmt.Sprintf("%s://%s:%d", scheme, service.Host, service.Port)
		target, err := url.Parse(targetURL)
		if err != nil {
			log.Printf("Invalid service URL for %s: %v", service.Name, err)
			continue
		}

		// Resolve rantai plugin
		resolvedPlugins := p.resolvePluginChain(route, service, newGlobalPlugins)

		resolvedRoute := ResolvedRoute{
			TargetURL: target,
			Plugins:   resolvedPlugins,
		}

		// Daftarkan rute ini untuk setiap path dan method
		for _, path := range route.Paths {
			for _, method := range route.Methods {
				key := method + " " + path // Cth: "GET /api/v1/users"
				newTable[key] = resolvedRoute
			}
		}
	}

	return newTable, newServiceMap, newGlobalPlugins
}

// resolvePluginChain menerapkan logika pewarisan: Global -> Service -> Route
func (p *Proxy) resolvePluginChain(route config.Route, service config.Service, globalPlugins []config.PluginConfig) []config.PluginConfig {
	// Gunakan map untuk menangani override (penimpaan) berdasarkan nama
	finalPlugins := make(map[string]config.PluginConfig)

	// 1. Terapkan Global Plugins
	for _, plugin := range globalPlugins {
		finalPlugins[plugin.Name] = plugin
	}

	// 2. Terapkan Service Plugins (akan menimpa global jika namanya sama)
	for _, plugin := range service.Plugins {
		finalPlugins[plugin.Name] = plugin
	}

	// 3. Terapkan Route Plugins (akan menimpa service/global jika namanya sama)
	for _, plugin := range route.Plugins {
		finalPlugins[plugin.Name] = plugin
	}

	// Ubah kembali map ke slice
	var chain []config.PluginConfig
	for _, plugin := range finalPlugins {
		chain = append(chain, plugin)
	}
	return chain
}

func matchPath(pattern, actual string) bool {
	// escape regex special char dulu, lalu ganti :param jadi grup
	reStr := regexp.MustCompile(`:[^/]+`).ReplaceAllString(pattern, `[^/]+`)
	re := regexp.MustCompile("^" + reStr + "$")
	return re.MatchString(actual)
}

func (p *Proxy) findRoute(method, path string) (*ResolvedRoute, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	// 1️⃣ Exact match dulu (lebih cepat)
	key := method + " " + path
	if route, ok := p.routingTable[key]; ok {
		return &route, true
	}

	// 2️⃣ Coba match pakai pattern `:param`
	for k, route := range p.routingTable {
		parts := strings.SplitN(k, " ", 2)
		if len(parts) != 2 {
			continue
		}

		methodKey, pattern := parts[0], parts[1]
		if methodKey != method {
			continue
		}

		if matchPath(pattern, path) {
			return &route, true
		}
	}

	// 3️⃣ (opsional) fallback prefix match
	for k, route := range p.routingTable {
		parts := strings.SplitN(k, " ", 2)
		if len(parts) != 2 {
			continue
		}
		if parts[0] == method && strings.HasPrefix(path, parts[1]) {
			return &route, true
		}
	}
	return nil, false
}

// ServeHTTP adalah handler utama untuk semua trafik publik
func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p.mu.RLock()
	route, found := p.findRoute(r.Method, r.URL.Path)
	p.mu.RUnlock()

	if !found {
		http.Error(w, "Not Found", http.StatusNotFound)
		return
	}

	// Build handler akhir → Reverse Proxy
	finalHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxy := httputil.NewSingleHostReverseProxy(route.TargetURL)
		proxy.Transport = &latencyTransport{wrapped: sharedTransport}

		// HAPUS cek 'isServiceDown' lama. Transport akan menanganinya.
		// if isServiceDown(route.TargetURL.Host) { ... }

		proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
			// HAPUS 'setServiceState'. Transport sudah menanganinya.
			// setServiceState(route.TargetURL.Host, true)

			log.Printf("[ERROR] Proxy error for %s: %v", route.TargetURL.Host, err)

			// Jika errornya adalah dari circuit breaker kita, kirim 503
			if strings.Contains(err.Error(), "circuit breaker is open") {
				http.Error(w, "Service temporarily unavailable", http.StatusServiceUnavailable)
				return
			}
			// Jika error lain (misal koneksi ditolak), kirim 502
			http.Error(w, "Upstream error", http.StatusBadGateway)
		}

		r.Header.Set("X-Forwarded-Host", r.Header.Get("Host"))
		r.URL.Host = route.TargetURL.Host
		r.URL.Scheme = route.TargetURL.Scheme

		proxy.ServeHTTP(w, r)
	})

	// Jalankan plugin chain dari global → service → route
	handler := finalHandler
	for i := len(route.Plugins) - 1; i >= 0; i-- {
		pluginCfg := route.Plugins[i]
		builder, ok := pluginRegistry[pluginCfg.Name]
		if !ok {
			log.Printf("[WARN] Plugin '%s' tidak terdaftar", pluginCfg.Name)
			continue
		}

		plugin := builder(pluginCfg.Config)
		next := handler
		handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !plugin.Execute(w, r, next) {
				// plugin mengembalikan false → stop chain
				return
			}
		})
	}

	// Jalankan chain
	handler.ServeHTTP(w, r)
}
