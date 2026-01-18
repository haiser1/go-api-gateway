package gateway

import (
	"net/http"
	"sync/atomic"

	"github.com/haiser1/go-api-gateway/internal/config"
)

// routeSnapshot holds immutable routing data for atomic swap
type routeSnapshot struct {
	routes        []ResolvedRoute
	globalPlugins []config.PluginConfig
}

type Proxy struct {
	snapshot atomic.Pointer[routeSnapshot]
}

func NewProxy(cfg *config.Config) *Proxy {
	p := &Proxy{}
	p.UpdateRoutes(cfg)
	return p
}

func (p *Proxy) UpdateRoutes(cfg *config.Config) {
	newRoutes, newGlobalPlugins := p.buildRoutingTable(cfg)

	snap := &routeSnapshot{
		routes:        newRoutes,
		globalPlugins: newGlobalPlugins,
	}
	p.snapshot.Store(snap)
}

// ServeHTTP adalah handler utama untuk proxy
func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	snap := p.snapshot.Load()
	if snap == nil {
		http.Error(w, `{"error": "Service not ready"}`, http.StatusServiceUnavailable)
		return
	}

	var matchedRoute *ResolvedRoute

	// Iterasi Slice yang sudah terurut (Priority Routing)
	for i := range snap.routes {
		rr := &snap.routes[i]

		// 1. Cek Method
		if rr.Method != r.Method {
			continue
		}

		// 2. Cek Path
		if rr.IsRegex {
			if rr.Regex.MatchString(r.URL.Path) {
				matchedRoute = rr
				break // Ketemu! Stop looping.
			}
		} else {
			if rr.PathPattern == r.URL.Path {
				matchedRoute = rr
				break // Ketemu! Stop looping.
			}
		}
	}

	if matchedRoute == nil {
		http.Error(w, `{"error": "Not Found"}`, http.StatusNotFound)
		return
	}

	// Siapkan Handler Akhir: Eksekusi Proxy yang sudah di-cache
	finalHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Update header agar upstream tahu host aslinya
		r.Header.Set("X-Forwarded-Host", r.Header.Get("Host"))
		r.URL.Host = matchedRoute.TargetURL.Host
		r.URL.Scheme = matchedRoute.TargetURL.Scheme

		// Panggil proxy object yang sudah ready (Super Cepat)
		matchedRoute.ProxyHandler.ServeHTTP(w, r)
	})

	// Eksekusi Middleware Chain
	handler := finalHandler
	for i := len(matchedRoute.Plugins) - 1; i >= 0; i-- {
		pluginCfg := matchedRoute.Plugins[i]
		if builder, ok := pluginRegistry[pluginCfg.Name]; ok {
			plugin := builder(pluginCfg.Config)
			next := handler
			handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if !plugin.Execute(w, r, next) {
					return // Middleware memutus rantai (misal: Auth gagal)
				}
			})
		}
	}

	handler.ServeHTTP(w, r)
}
