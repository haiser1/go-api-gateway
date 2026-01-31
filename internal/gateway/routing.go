package gateway

import (
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"regexp"
	"sort"
	"strings"

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

		// Get cached transport for this service (global instance per service ID)
		baseTransport := GetServiceTransport(service)

		// Wrap with circuit breaker (uses service ID for breaker lookup)
		cbTransport := NewCircuitBreakerTransport(baseTransport, service.Id)

		rp := httputil.NewSingleHostReverseProxy(targetURL)
		rp.Transport = cbTransport

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

				if strings.Contains(path, ":") {
					rr.IsRegex = true
					reStr := regexp.MustCompile(`:[^/]+`).ReplaceAllString(path, `[^/]+`)
					rr.Regex = regexp.MustCompile("^" + reStr + "$")
				}

				routes = append(routes, rr)
			}
		}
	}

	sort.Slice(routes, func(i, j int) bool {
		if !routes[i].IsRegex && routes[j].IsRegex {
			return true
		}
		if routes[i].IsRegex && !routes[j].IsRegex {
			return false
		}

		return len(routes[i].PathPattern) > len(routes[j].PathPattern)
	})

	return routes, cfg.GlobalPlugins
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
