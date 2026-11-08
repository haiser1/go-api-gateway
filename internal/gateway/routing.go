package gateway

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"sort"
	"time"

	"github.com/haiser1/go-api-gateway/internal/auth"
	"github.com/haiser1/go-api-gateway/internal/config"
	"github.com/haiser1/go-api-gateway/internal/config/validator"
	"github.com/haiser1/go-api-gateway/internal/secrets"
	"github.com/rs/zerolog/log"
)

type ResolvedRoute struct {
	Method      string
	PathPattern string

	// Upstream is the immutable upstream state this route was resolved
	// against (targets, algorithm) — request-time target selection reads it
	// from here rather than from live config.
	Upstream config.Upstream
	Plugins  []config.PluginConfig

	// Selector performs health-aware, request-time target selection over the
	// upstream's targets (Phase 8). It is shared by every route resolving
	// through the same upstream and is nil only for hand-built routes in
	// tests that never invoke the proxy handler.
	Selector *UpstreamRuntime

	// TargetProxies holds one pre-built ReverseProxy per target, index
	// aligned with Selector.targets (same order as the configured targets).
	TargetProxies []*httputil.ReverseProxy
	TargetURLs    []*url.URL

	// Pre-compiled middleware chain (built once at config load, not per-request)
	CompiledHandler http.Handler

	// Retry settings (from service config)
	Retries      int
	RetryBackoff float64
}

func buildRuntimeSnapshot(cfg *config.Config) (*RuntimeSnapshot, error) {
	return buildRuntimeSnapshotWithProvider(cfg, nil)
}

func buildRuntimeSnapshotWithProvider(cfg *config.Config, provider secrets.Provider) (*RuntimeSnapshot, error) {
	if cfg == nil {
		return nil, errors.New("cannot build runtime from nil config")
	}

	// Final safety boundary: the runtime builder independently runs the
	// centralized validator on the whole configuration (structural +
	// semantic). Even if a caller published an invalid config without
	// validation, a build through this function can never accept it —
	// delivering the same guarantees as reloads and admin commits.
	if err := validator.Validate(cfg); err != nil {
		return nil, fmt.Errorf("config validation failed: %w", err)
	}

	authCfg, err := config.ResolveAuthConfig(cfg)
	if err != nil {
		return nil, err
	}
	var (
		authRuntime    *auth.Runtime
		pendingSecrets *secrets.Reloaded
		secretReloader secrets.Reloadable
	)
	if authCfg != nil {
		if provider == nil {
			return nil, errors.New("auth is configured but no secret provider is available")
		}

		// Prefer a staged reload: compile the candidate auth runtime from a
		// snapshot of the secret store, leaving the published provider values
		// untouched until the whole runtime has built successfully.
		secretProvider := provider
		if reloader, ok := provider.(secrets.Reloadable); ok {
			snapshot, reloadErr := reloader.ReloadSnapshot()
			if reloadErr != nil {
				return nil, fmt.Errorf("failed to reload secrets: %w", reloadErr)
			}
			secretProvider = secrets.NewMapProvider(snapshot.Values)
			pendingSecrets = &snapshot
			secretReloader = reloader
		} else if reloadErr := provider.Reload(); reloadErr != nil {
			return nil, fmt.Errorf("failed to reload secrets: %w", reloadErr)
		}

		authRuntime, err = auth.BuildRuntime(authCfg, secretProvider)
		if err != nil {
			return nil, fmt.Errorf("failed to build auth runtime: %w", err)
		}
	}

	tree := NewRadixTree()
	// Load balancer runtimes keyed by upstream ID (Phase 8): created once per
	// snapshot, shared across all routes and services of the same upstream.
	upstreamRuntimes := make(map[string]*UpstreamRuntime)
	snap := &RuntimeSnapshot{
		Auth:       authRuntime,
		Tree:       tree,
		Transports: make(map[string]*http.Transport),
	}

	// Build upstream map for fast lookup
	upstreamMap := make(map[string]config.Upstream)
	// Immutable copies so snapshot-resident data never aliases live config.
	upstreamSnapshots := make(map[string]config.Upstream)
	for _, u := range cfg.Upstreams {
		upstream := u
		upstream.ApplyDefaults()
		upstreamMap[upstream.Id] = upstream
		upstreamSnapshots[upstream.Id] = upstream.DeepCopy()
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

		// Resolve Plugin Chain
		resolvedPlugins := resolvePluginChain(route, service, cfg.GlobalPlugins)
		if cfg.Auth != nil {
			// Top-level auth applies to every route. Insert a synthetic auth
			// plugin so it keeps its normal middleware priority position.
			resolvedPlugins = append(resolvedPlugins, config.PluginConfig{Name: "auth", Enabled: true})
			resolvedPlugins = sortPluginChain(resolvedPlugins)
		}

		// Get cached transport for this service and register it on the
		// snapshot so the runtime stays self-contained.
		baseTransport := GetServiceTransport(service)
		snap.Transports[service.Id] = baseTransport

		// Circuit breakers deliberately live in a shared registry keyed by
		// stable service ID (circuit_breaker.go) so breaker state survives
		// config reloads.
		cbTransport := NewCircuitBreakerTransport(baseTransport, service.Id)

		// Upstream runtime: request-time, health-aware target selection
		// (Phase 8). One runtime per upstream per snapshot, shared by every
		// route resolving through the same upstream.
		selector, ok := upstreamRuntimes[service.UpstreamId]
		if !ok {
			selector = NewUpstreamRuntime(upstreamSnapshots[service.UpstreamId])
			upstreamRuntimes[service.UpstreamId] = selector
			log.Info().Str("upstream", upstream.Name).Str("algorithm", selector.algorithm).
				Int("targets", len(upstreamSnapshots[service.UpstreamId].Targets)).
				Msg("Upstream runtime initialized for load balancing")
		}

		// Pre-build one ReverseProxy per target, index-aligned with the
		// selector's target list. The per-target transport wrapper records
		// every upstream attempt against the target's health state, so the
		// selector ejects failing targets on its own.
		targetProxies := make([]*httputil.ReverseProxy, len(upstream.Targets))
		targetURLs := make([]*url.URL, len(upstream.Targets))
		for i, t := range upstreamSnapshots[service.UpstreamId].Targets {
			targetURLStr := fmt.Sprintf("%s://%s:%d", service.Protocol, t.Host, t.Port)
			targetURL, perr := url.Parse(targetURLStr)
			if perr != nil {
				// A malformed target leaves the route unresolvable — fail the
				// whole build so the previous runtime snapshot stays published.
				return nil, fmt.Errorf("route %q: invalid target URL %q: %w", route.Name, targetURLStr, perr)
			}
			targetURLs[i] = targetURL

			rp := httputil.NewSingleHostReverseProxy(targetURL)
			rp.Transport = NewLbTargetTransport(cbTransport, selector, i)

			// Custom Error Handler: translate upstream/transport errors into
			// the gateway's structured JSON error mapping (httperror.go):
			//   circuit breaker open  → 503 (typed CircuitBreakerOpenError)
			//   upstream timeout      → 504
			//   everything else       → 502
			// Client cancellations are skipped entirely, and a retry in
			// flight (registered via the retrySinkKey context value) may
			// intercept the error instead of writing a response.
			rp.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
				if isClientCancellation(err) {
					log.Debug().Err(err).Str("upstream", targetURL.Host).
						Msg("Client canceled request before upstream response")
					return
				}
				if sinkFn, isSink := r.Context().Value(retrySinkKey{}).(func(error) bool); isSink && sinkFn(err) {
					// The retry loop intercepted this attempt failure and
					// will try the next target — do not write a response.
					return
				}
				status := MapUpstreamError(err)
				log.Error().Err(err).Int("status", status).Str("upstream", targetURL.Host).Msg("Proxy error")
				switch status {
				case http.StatusServiceUnavailable:
					WriteJSONError(w, status, "Service temporarily unavailable")
				case http.StatusGatewayTimeout:
					WriteJSONError(w, status, "Gateway Timeout")
				default:
					WriteJSONError(w, status, "Bad Gateway")
				}
			}

			targetProxies[i] = rp
		}

		// Check if CORS is enabled in the plugin chain
		hasCORS := false
		for _, pluginCfg := range resolvedPlugins {
			if pluginCfg.Name == "cors" && pluginCfg.Enabled {
				hasCORS = true
				break
			}
		}

		// Insert setiap kombinasi Method & Path ke Radix Tree
		for _, path := range route.Paths {
			methods := route.Methods
			if hasCORS {
				hasOptions := false
				for _, m := range methods {
					if m == "OPTIONS" {
						hasOptions = true
						break
					}
				}
				if !hasOptions {
					methods = append(append([]string{}, methods...), "OPTIONS")
				}
			}

			for _, method := range methods {
				rr := ResolvedRoute{
					Method:        method,
					PathPattern:   path,
					Upstream:      upstreamSnapshots[service.UpstreamId],
					Plugins:       resolvedPlugins,
					Selector:      selector,
					TargetProxies: targetProxies,
					TargetURLs:    targetURLs,
					Retries:       service.Retries,
					RetryBackoff:  service.RetryBackoff,
				}

				// Pre-compile middleware chain (zero allocation per-request)
				rr.CompiledHandler = buildMiddlewareChain(&rr, authRuntime)

				if err := tree.Insert(method, path, &rr); err != nil {
					log.Warn().Err(err).Str("route", route.Name).Str("method", method).Str("path", path).Msg("Skipping duplicate route")
					continue
				}
				snap.Routes = append(snap.Routes, &rr)
				log.Debug().Str("method", method).Str("path", path).Str("upstream", service.UpstreamId).
					Int("targets", len(targetURLs)).
					Msg("Route inserted into radix tree")
			}
		}
	}

	// The runtime is fully built. Publish the staged secret snapshot now;
	// CommitReload rejects it if the backing store changed while we were
	// building, in which case the previous runtime remains active and the
	// next watcher event retries from the newer contents.
	if pendingSecrets != nil && secretReloader != nil {
		if err := secretReloader.CommitReload(*pendingSecrets); err != nil {
			return nil, fmt.Errorf("failed to commit secrets: %w", err)
		}
	}

	return snap, nil
}

func resolvePluginChain(route config.Route, service config.Service, globalPlugins []config.PluginConfig) []config.PluginConfig {
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

	return sortPluginChain(chain)
}

func sortPluginChain(chain []config.PluginConfig) []config.PluginConfig {
	// Sort the chain by a predefined priority list to ensure deterministic execution order:
	// metrics/prometheus -> cors -> logger/logging -> ip-restriction/bot-detection -> rate-limiting -> authorization -> request-transform -> others
	pluginPriority := map[string]int{
		"metrics":                0,
		"prometheus":             0,
		"cors":                   1,
		"logger":                 2,
		"logging":                2,
		"ip-restriction":         3,
		"bot-detection":          3,
		"rate-limiting":          4,
		"auth":                   5,
		"authorization":          5,
		"request-transform":      6,
		"request-transformation": 6,
	}

	sort.Slice(chain, func(i, j int) bool {
		p1, ok1 := pluginPriority[chain[i].Name]
		p2, ok2 := pluginPriority[chain[j].Name]
		if !ok1 {
			p1 = 50
		}
		if !ok2 {
			p2 = 50
		}
		if p1 != p2 {
			return p1 < p2
		}
		return chain[i].Name < chain[j].Name
	})

	return chain
}

// retrySinkKey stores the per-attempt retry interceptor in the request
// context. The ReverseProxy ErrorHandler consults it before writing an error
// response: when the retry loop decides to try another target, the error is
// intercepted and no response is written (preventing double writes).
type retrySinkKey struct{}

// buildMiddlewareChain pre-compiles the full handler chain for a route.
// This is called once during buildRuntimeSnapshot, NOT per-request.
//
// Forwarded header policy (Phase 7 audit):
//   - X-Forwarded-Host is taken from r.Host (the parsed host of the
//     request line), NOT r.Header.Get("Host") — net/http moves the
//     request Host out of the header map, so Header.Get returns "" and
//     the upstream always received an empty value. This is a FIX.
//   - X-Forwarded-Proto is derived from the gateway's own connection
//     state and overwrites any client-supplied value, so upstreams can
//     never be spoofed into believing the request was https.
//   - X-Forwarded-For is left to httputil.ReverseProxy's default
//     behavior: it APPENDS the real client address (r.RemoteAddr) to
//     the inbound value. Trust note: entries BEFORE the appended one are
//     client-controlled and untrusted at the gateway; upstreams that
//     need a reliable client identity must use the LAST entry only
//     (or configure trust_forwarded_for-aware plugins accordingly).
//
// Target selection + retry (Phase 8):
//   - A target is selected AT REQUEST TIME by the upstream runtime
//     (rr.Selector) — never frozen at route-build time.
//   - When service.Retries > 0 and the request is replay-safe
//     (requestReplayable), failed attempts on 502/504-class errors try the
//     NEXT target(s); client-visible output is written exactly once (either
//     the successful stream or the final error response).
func buildMiddlewareChain(rr *ResolvedRoute, authRuntime *auth.Runtime) http.Handler {
	// Final handler: select a target, forward headers, proxy to upstream
	// with the configured retry policy.
	var handler http.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proto := "http"
		if r.TLS != nil {
			proto = "https"
		}
		r.Header.Set("X-Forwarded-Host", r.Host)
		r.Header.Set("X-Forwarded-Proto", proto)

		if rr.Selector == nil || len(rr.TargetProxies) == 0 {
			// Hand-built route without a selector (tests only) — nothing to
			// forward to. Belt-and-braces: never panic in production paths.
			WriteJSONError(w, http.StatusServiceUnavailable, "Service not ready")
			return
		}

		// Retry policy: streamed bodies are never replayed.
		retries := rr.Retries
		if !requestReplayable(r) {
			retries = 0
		}
		backoff := time.Duration(rr.RetryBackoff * float64(time.Second))

		var exclude map[int]struct{}
		for attempt := 0; ; attempt++ {
			idx, ok := rr.Selector.pick(exclude)
			if !ok {
				// Only possible while retrying: every target of this
				// upstream already failed within this request.
				log.Warn().Str("service", rr.Upstream.Id).Int("attempts", attempt+1).
					Msg("All retry candidates failed; giving up")
				WriteJSONError(w, http.StatusBadGateway, "Bad Gateway")
				return
			}

			r.URL.Scheme = rr.TargetURLs[idx].Scheme
			r.URL.Host = rr.TargetURLs[idx].Host

			if attempt >= retries {
				// Final attempt: let the ErrorHandler write the error
				// response on failure — writes exactly once.
				rr.TargetProxies[idx].ServeHTTP(w, r)
				return
			}

			// Intermediate attempt: intercept failures through the retry
			// sink so the ErrorHandler stays silent while we try the next
			// target.
			var retried bool
			sink := func(err error) bool {
				switch MapUpstreamError(err) {
				case http.StatusBadGateway, http.StatusGatewayTimeout:
					if exclude == nil {
						exclude = make(map[int]struct{})
					}
					exclude[idx] = struct{}{}
					retried = true
					return true
				}
				return false
			}
			attemptCtx := context.WithValue(r.Context(), retrySinkKey{}, sink)

			var attemptReq *http.Request
			if r.GetBody != nil {
				// Buffered body: refresh it so the transport of the previous
				// attempt (which closed it) does not poison the replay.
				attemptReq = r.Clone(attemptCtx)
				if body, berr := r.GetBody(); berr == nil {
					attemptReq.Body = body
				}
			} else {
				// No body / empty body: ReverseProxy nulls out req.Body for
				// empty ContentLength, so the original reader stays valid.
				attemptReq = r.WithContext(attemptCtx)
			}
			rr.TargetProxies[idx].ServeHTTP(w, attemptReq)

			if !retried {
				// Success (response already streamed) or a terminal error
				// (already written by the ErrorHandler).
				return
			}
			if backoff > 0 {
				time.Sleep(backoff)
			}
		}
	})

	// Wrap plugins in reverse order (last plugin runs first)
	for i := len(rr.Plugins) - 1; i >= 0; i-- {
		pluginCfg := rr.Plugins[i]
		if !pluginCfg.Enabled {
			continue // Skip disabled plugins
		}
		if pluginCfg.Name == "auth" || pluginCfg.Name == "authorization" {
			if authRuntime == nil {
				// Fail closed: an enabled auth plugin without a compiled
				// policy must never let requests through unauthenticated.
				handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					WriteJSONError(w, http.StatusServiceUnavailable, "Authentication is not configured")
				})
				continue
			}
			handler = auth.Middleware(authRuntime)(handler)
			continue
		}
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
