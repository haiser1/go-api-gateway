package gateway

import (
	"context"
	"net/http"
	"sync/atomic"

	"github.com/haiser1/go-api-gateway/internal/config"
)

// contextKey is a custom type for context keys to avoid collisions
type contextKey string

const pathParamsKey contextKey = "pathParams"

// routeSnapshot holds immutable routing data for atomic swap
type routeSnapshot struct {
	tree          *RadixTree
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
	newTree, newGlobalPlugins := p.buildRoutingTable(cfg)

	snap := &routeSnapshot{
		tree:          newTree,
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

	// Radix Tree Lookup — O(k) where k = number of path segments
	matchedRoute, params := snap.tree.Search(r.Method, r.URL.Path)

	if matchedRoute == nil {
		http.Error(w, `{"error": "Not Found"}`, http.StatusNotFound)
		return
	}

	// Store path params in context for downstream use
	if len(params) > 0 {
		ctx := context.WithValue(r.Context(), pathParamsKey, params)
		r = r.WithContext(ctx)
	}

	// Execute pre-compiled middleware chain (built at config load time)
	matchedRoute.CompiledHandler.ServeHTTP(w, r)
}

// GetPathParams extracts path parameters from request context.
// Usage: params := gateway.GetPathParams(r)
// Then: userId := params["id"]
func GetPathParams(r *http.Request) map[string]string {
	if params, ok := r.Context().Value(pathParamsKey).(map[string]string); ok {
		return params
	}
	return nil
}
