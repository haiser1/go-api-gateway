package gateway

import (
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/haiser1/go-api-gateway/internal/config"
	"github.com/rs/zerolog/log"
)

// ResolvedRoute menyimpan semua yang dibutuhkan untuk memproses request
// tanpa perlu alokasi memori lagi saat runtime.
type ResolvedRoute struct {
	Method      string
	PathPattern string // Path asli dari config (misal: /api/users/:id)
	TargetURL   *url.URL
	Plugins     []config.PluginConfig

	// Pre-built Reverse Proxy (OPTIMASI UTAMA)
	ProxyHandler *httputil.ReverseProxy

	// Regex Optimizations
	IsRegex bool
	Regex   *regexp.Regexp

	// Retry settings (from service config)
	Retries      int
	RetryBackoff float64
}

func (p *Proxy) buildRoutingTable(cfg *config.Config) ([]ResolvedRoute, []config.PluginConfig) {
	var routes []ResolvedRoute

	// Helper Map Service
	serviceMap := make(map[string]config.Service)
	for _, s := range cfg.Services {
		svc := s
		svc.ApplyDefaults() // Apply defaults for each service
		serviceMap[svc.Id] = svc
	}

	for _, route := range cfg.Routes {
		service, ok := serviceMap[route.ServiceId]
		if !ok {
			log.Warn().Str("route", route.Name).Msg("Service ID not found, skipping")
			continue
		}

		// Build URL Target (defaults already applied)
		targetURLStr := fmt.Sprintf("%s://%s:%d", service.Protocol, service.Host, service.Port)
		targetURL, err := url.Parse(targetURLStr)
		if err != nil {
			log.Error().Err(err).Msg("Invalid target URL")
			continue
		}

		// Resolve Plugin Chain
		resolvedPlugins := p.resolvePluginChain(route, service, cfg.GlobalPlugins)

		// Create per-service transport with timeout settings
		serviceTransport := &http.Transport{
			DialContext: (&net.Dialer{
				Timeout: time.Duration(service.ConnectTimeout) * time.Second,
			}).DialContext,
			ResponseHeaderTimeout: time.Duration(service.ReadTimeout) * time.Second,
			MaxIdleConns:          100,
			MaxIdleConnsPerHost:   10,
			IdleConnTimeout:       90 * time.Second,
		}

		rp := httputil.NewSingleHostReverseProxy(targetURL)
		rp.Transport = &latencyTransport{wrapped: serviceTransport}

		// Custom Error Handler agar JSON response-nya rapi
		rp.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
			log.Error().Err(err).Str("upstream", targetURL.Host).Msg("Proxy error")
			if strings.Contains(err.Error(), "circuit breaker") {
				http.Error(w, `{"error": "Service temporarily unavailable"}`, http.StatusServiceUnavailable)
			} else {
				http.Error(w, `{"error": "Bad Gateway"}`, http.StatusBadGateway)
			}
		}

		// Expand rute untuk setiap Method & Path
		for _, path := range route.Paths {
			for _, method := range route.Methods {
				rr := ResolvedRoute{
					Method:       method,
					PathPattern:  path,
					TargetURL:    targetURL,
					Plugins:      resolvedPlugins,
					ProxyHandler: rp,
				}

				// Compile Regex jika ada parameter (:)
				if strings.Contains(path, ":") {
					rr.IsRegex = true
					// Ubah /api/:id menjadi regex /api/[^/]+
					// Gunakan ^ dan $ agar match-nya presisi
					reStr := regexp.MustCompile(`:[^/]+`).ReplaceAllString(path, `[^/]+`)
					rr.Regex = regexp.MustCompile("^" + reStr + "$")
				}

				routes = append(routes, rr)
			}
		}
	}

	// --- SORTING LOGIC (PENTING) ---
	// Kita urutkan agar:
	// 1. Route statis (bukan regex) dicek duluan (karena lebih spesifik).
	// 2. Jika sama-sama statis/regex, path yang lebih panjang dicek duluan.
	sort.Slice(routes, func(i, j int) bool {
		// Rule 1: Non-Regex menang lawan Regex
		if !routes[i].IsRegex && routes[j].IsRegex {
			return true
		}
		if routes[i].IsRegex && !routes[j].IsRegex {
			return false
		}
		// Rule 2: Path lebih panjang menang (Longest Prefix Match)
		return len(routes[i].PathPattern) > len(routes[j].PathPattern)
	})

	return routes, cfg.GlobalPlugins
}

func (p *Proxy) resolvePluginChain(route config.Route, service config.Service, globalPlugins []config.PluginConfig) []config.PluginConfig {
	// Logic pewarisan plugin tetap sama (Override map)
	pluginMap := make(map[string]config.PluginConfig)

	for _, p := range globalPlugins {
		pluginMap[p.Name] = p
	}
	for _, p := range service.Plugins {
		pluginMap[p.Name] = p
	}
	for _, p := range route.Plugins {
		pluginMap[p.Name] = p
	}

	var chain []config.PluginConfig
	for _, p := range pluginMap {
		chain = append(chain, p)
	}
	return chain
}
