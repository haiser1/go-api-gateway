package gateway

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"

	"github.com/haiser1/go-api-gateway/internal/config"
	"github.com/rs/zerolog/log"
)

type ResolvedRoute struct {
	Method      string
	PathPattern string
	TargetURL   *url.URL
	Plugins     []config.PluginConfig

	// Pre-built Reverse Proxy
	ProxyHandler *httputil.ReverseProxy

	// Pre-compiled middleware chain (built once at config load, not per-request)
	CompiledHandler http.Handler

	// Retry settings (from service config)
	Retries      int
	RetryBackoff float64
}

func (p *Proxy) buildRoutingTable(cfg *config.Config) (*RadixTree, []config.PluginConfig) {
	tree := NewRadixTree()

	// Build upstream map for fast lookup
	upstreamMap := make(map[string]config.Upstream)
	for _, u := range cfg.Upstreams {
		upstream := u
		upstream.ApplyDefaults()
		upstreamMap[upstream.Id] = upstream
	}

	// Build service map
	serviceMap := make(map[string]config.Service)
	for _, s := range cfg.Services {
		svc := s
		svc.ApplyDefaults()
		serviceMap[svc.Id] = svc
	}

	for _, route := range cfg.Routes {
		service, ok := serviceMap[route.ServiceId]
		if !ok {
			log.Warn().Str("route", route.Name).Msg("Service ID not found, skipping")
			continue
		}

		// Resolve upstream from service
		upstream, ok := upstreamMap[service.UpstreamId]
		if !ok {
			log.Warn().Str("route", route.Name).Str("upstream_id", service.UpstreamId).Msg("Upstream not found, skipping")
			continue
		}

		if len(upstream.Targets) == 0 {
			log.Warn().Str("route", route.Name).Str("upstream", upstream.Name).Msg("Upstream has no targets, skipping")
			continue
		}

		// Use first target for now (future: implement load balancing across targets)
		target := upstream.Targets[0]

		// Build URL Target
		targetURLStr := fmt.Sprintf("%s://%s:%d", service.Protocol, target.Host, target.Port)
		targetURL, err := url.Parse(targetURLStr)
		if err != nil {
			log.Error().Err(err).Msg("Invalid target URL")
			continue
		}

		// Resolve Plugin Chain
		resolvedPlugins := p.resolvePluginChain(route, service, cfg.GlobalPlugins)

		// Get cached transport for this service
		baseTransport := GetServiceTransport(service)

		// Wrap with circuit breaker (uses service ID for breaker lookup)
		cbTransport := NewCircuitBreakerTransport(baseTransport, service.Id)

		rp := httputil.NewSingleHostReverseProxy(targetURL)
		rp.Transport = cbTransport

		// Custom Error Handler agar JSON response-nya rapi
		rp.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
			log.Error().Err(err).Str("upstream", targetURL.Host).Msg("Proxy error")
			var cbErr *CircuitBreakerOpenError
			if errors.As(err, &cbErr) {
				http.Error(w, `{"error": "Service temporarily unavailable"}`, http.StatusServiceUnavailable)
			} else {
				http.Error(w, `{"error": "Bad Gateway"}`, http.StatusBadGateway)
			}
		}

		// Insert setiap kombinasi Method & Path ke Radix Tree
		for _, path := range route.Paths {
			for _, method := range route.Methods {
				rr := ResolvedRoute{
					Method:       method,
					PathPattern:  path,
					TargetURL:    targetURL,
					Plugins:      resolvedPlugins,
					ProxyHandler: rp,
					Retries:      service.Retries,
					RetryBackoff: service.RetryBackoff,
				}

				// Pre-compile middleware chain (zero allocation per-request)
				rr.CompiledHandler = buildMiddlewareChain(&rr)

				if err := tree.Insert(method, path, &rr); err != nil {
					log.Warn().Err(err).Str("route", route.Name).Str("method", method).Str("path", path).Msg("Skipping duplicate route")
					continue
				}
				log.Debug().Str("method", method).Str("path", path).Str("target", targetURLStr).Msg("Route inserted into radix tree")
			}
		}
	}

	return tree, cfg.GlobalPlugins
}

func (p *Proxy) resolvePluginChain(route config.Route, service config.Service, globalPlugins []config.PluginConfig) []config.PluginConfig {
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

// buildMiddlewareChain pre-compiles the full handler chain for a route.
// This is called once during buildRoutingTable, NOT per-request.
func buildMiddlewareChain(rr *ResolvedRoute) http.Handler {
	// Final handler: proxy to upstream
	var handler http.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Header.Set("X-Forwarded-Host", r.Header.Get("Host"))
		r.URL.Host = rr.TargetURL.Host
		r.URL.Scheme = rr.TargetURL.Scheme
		rr.ProxyHandler.ServeHTTP(w, r)
	})

	// Wrap plugins in reverse order (last plugin runs first)
	for i := len(rr.Plugins) - 1; i >= 0; i-- {
		pluginCfg := rr.Plugins[i]
		if builder, ok := pluginRegistry[pluginCfg.Name]; ok {
			plugin := builder(pluginCfg.Config)
			next := handler
			handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if !plugin.Execute(w, r, next) {
					return
				}
			})
		}
	}

	return handler
}
