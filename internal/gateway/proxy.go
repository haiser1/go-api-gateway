package gateway

import (
	"context"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/haiser1/go-api-gateway/internal/config"
	"github.com/haiser1/go-api-gateway/internal/secrets"
	"github.com/rs/zerolog/log"
)

// contextKey is a custom type for context keys to avoid collisions
type contextKey string

const pathParamsKey contextKey = "pathParams"

// Proxy serves traffic from an immutable RuntimeSnapshot that is published
// via an atomic pointer swap. In-flight requests keep completing against the
// snapshot they started with, even when a reload publishes a newer version.
type Proxy struct {
	snapshot atomic.Pointer[RuntimeSnapshot]
	version  atomic.Uint64
	secrets  secrets.Provider
}

// NewProxy builds and publishes the initial runtime snapshot. It fails when
// the runtime cannot be built, instead of starting with a partial routing
// table.
func NewProxy(cfg *config.Config) (*Proxy, error) {
	return NewProxyWithSecrets(cfg, nil)
}

// NewProxyWithSecrets is NewProxy with an explicit secret provider used to
// compile authentication policies.
func NewProxyWithSecrets(cfg *config.Config, provider secrets.Provider) (*Proxy, error) {
	p := &Proxy{secrets: provider}
	if err := p.UpdateRoutes(cfg); err != nil {
		return nil, err
	}
	return p, nil
}

// UpdateRoutes builds a new runtime snapshot and, on success, publishes it
// atomically. On a build failure the previously published runtime stays
// active — failed reloads never leave the proxy without a working snapshot.
// The superseded runtime is retired once it is no longer used by in-flight
// requests.
func (p *Proxy) UpdateRoutes(cfg *config.Config) error {
	snap, err := buildRuntimeSnapshotWithProvider(cfg, p.secrets)
	if err != nil {
		return err
	}
	snap.Version = p.version.Add(1)
	snap.CreatedAt = time.Now()
	snap.drainFn = func() { p.drainRuntime(snap) }

	prev := p.snapshot.Swap(snap)
	p.retireSnapshot(prev)
	return nil
}

// retireSnapshot schedules resource retirement for a superseded runtime. It
// runs immediately when no request is executing on it; otherwise the last
// in-flight releaseRequest triggers the drain. Idempotent — repeat calls are
// no-ops, so rapid reloads cannot double-retire a snapshot.
func (p *Proxy) retireSnapshot(old *RuntimeSnapshot) {
	if old == nil || !old.draining.CompareAndSwap(false, true) {
		return
	}
	if old.inflight.Load() == 0 {
		p.drainRuntime(old)
	}
}

// drainRuntime releases the resources owned by a retired runtime. Transports
// shared with the currently published runtime are left intact so the active
// runtime keeps its connection pools; everything else gets its idle
// connections closed and its global cache entry evicted (only when the cache
// still maps to the retired transport). Safe to run repeatedly.
func (p *Proxy) drainRuntime(retired *RuntimeSnapshot) {
	current := p.snapshot.Load()
	for serviceId, tr := range retired.Transports {
		if current != nil && current.Transports[serviceId] == tr {
			// Still owned by the active runtime.
			continue
		}
		tr.CloseIdleConnections()
		DropTransport(serviceId, tr)
		log.Info().Str("service", serviceId).Msg("Retired transport of previous runtime")
	}
}

// ServeHTTP adalah handler utama untuk proxy
func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	snap := p.snapshot.Load()
	if snap == nil {
		WriteJSONError(w, http.StatusServiceUnavailable, "Service not ready")
		return
	}

	// Track the request against this snapshot so the runtime cannot be
	// retired while it is still being served.
	snap.trackRequest()
	defer snap.releaseRequest()

	// Radix Tree Lookup — O(k) where k = number of path segments.
	// SearchWithAllowed also collects the methods registered for the matched
	// path when the requested method is not registered on it, enabling HTTP
	// 405 responses with an Allow header (collection is skipped entirely
	// when the route matches — the hot path pays nothing).
	matchedRoute, params, allowed := snap.Tree.SearchWithAllowed(r.Method, r.URL.Path)

	if matchedRoute == nil {
		if len(allowed) > 0 {
			// Path exists but not for this method — 405 with the exact
			// set of supported methods per RFC 9110.
			w.Header().Set("Allow", strings.Join(allowed, ", "))
			WriteJSONError(w, http.StatusMethodNotAllowed, "Method Not Allowed")
			return
		}
		WriteJSONError(w, http.StatusNotFound, "Not Found")
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

// CurrentSnapshot returns the currently published runtime snapshot, or nil
// before the first successful publication.
func (p *Proxy) CurrentSnapshot() *RuntimeSnapshot {
	return p.snapshot.Load()
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
