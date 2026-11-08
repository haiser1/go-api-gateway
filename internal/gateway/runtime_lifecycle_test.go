package gateway

import (
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/haiser1/go-api-gateway/internal/config"
)

// connTracker observes when upstream connections are closed on the server
// side, so tests can verify that a retired transport's idle connections are
// actually closed instead of assuming it.
type connTracker struct {
	mu     sync.Mutex
	closed int
}

func (tr *connTracker) closedCount() int {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	return tr.closed
}

func closedSoon(t *testing.T, tracker *connTracker) bool {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if tracker.closedCount() > 0 {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return false
}

// newTrackedUpstream is like newEchoUpstream but reports connection-closure
// events on the server side via a connTracker. When park is non-nil the
// handler only blocks on started/release while park is true, so an earlier
// warm-up request can still complete normally.
func newTrackedUpstream(t *testing.T, tag string, park *atomic.Bool, started chan<- struct{}, release <-chan struct{}) (*recordedUpstream, *connTracker) {
	t.Helper()

	tracker := &connTracker{}
	srv := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if park != nil && park.Load() {
				if started != nil {
					started <- struct{}{}
				}
				if release != nil {
					<-release
				}
			}
			fmt.Fprint(w, tag)
		}),
		ConnState: func(_ net.Conn, cs http.ConnState) {
			if cs == http.StateClosed {
				tracker.mu.Lock()
				tracker.closed++
				tracker.mu.Unlock()
			}
		},
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = srv.Serve(listener) }()
	t.Cleanup(func() { _ = srv.Close() })

	addr := listener.Addr().(*net.TCPAddr)
	return &recordedUpstream{host: addr.IP.String(), port: addr.Port}, tracker
}

// Reload with an unchanged config must reuse the transport: the superseded
// snapshot's retirement must not close idle connections of the transport that
// the active runtime still uses.
func TestTransportLifecycle_SharedTransportSurvivesReload(t *testing.T) {
	upA, tracker := newTrackedUpstream(t, "upstreamA", nil, nil, nil)
	cfgA := runtimeConfig(upA.host, upA.port, "/api/ok")

	p, err := NewProxy(cfgA)
	if err != nil {
		t.Fatalf("initial build failed: %v", err)
	}

	if body := proxyGet(t, p, "/api/ok"); body != "upstreamA" {
		t.Fatalf("expected routing to upstreamA, got %q", body)
	}
	v1 := p.CurrentSnapshot()
	transport := v1.Transports["s-runtime"]
	if transport == nil {
		t.Fatal("expected transport registered on the snapshot")
	}

	// Reload with the identical config — same upstream, same route, same
	// timeouts — so the fingerprint matches and the transport is reused.
	if err := p.UpdateRoutes(runtimeConfig(upA.host, upA.port, "/api/ok")); err != nil {
		t.Fatalf("reload failed: %v", err)
	}
	current := p.CurrentSnapshot()
	if v1.Version != 1 || current.Version != 2 {
		t.Fatalf("unexpected versions %d -> %d", v1.Version, current.Version)
	}
	if current.Transports["s-runtime"] != transport {
		t.Fatal("expected transport reuse across reload with unchanged config")
	}

	transportMu.RLock()
	_, cached := serviceTransports["s-runtime"]
	transportMu.RUnlock()
	if !cached {
		t.Fatal("expected the transport cache entry to be retained for the active runtime")
	}

	// The pooled idle connection must stay open: v1's retirement must not
	// touch the transport the active runtime shares.
	time.Sleep(150 * time.Millisecond)
	if closed := tracker.closedCount(); closed != 0 {
		t.Fatalf("expected no closed connections, got %d", closed)
	}

	if body := proxyGet(t, p, "/api/ok"); body != "upstreamA" {
		t.Fatalf("active runtime broken after reload: %q", body)
	}
}

type blockedResult struct {
	code int
	body string
}

// Removing the service from the config must not strand its transport and must
// not interrupt the in-flight request that still uses it: the request
// completes against the old runtime, and only then the transport's idle
// connections are closed and its cache entry evicted.
func TestTransportLifecycle_RetiresAfterServiceRemovalAndDrain(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	park := &atomic.Bool{}
	upA, tracker := newTrackedUpstream(t, "upstreamA", park, started, release)

	cfgA := runtimeConfig(upA.host, upA.port, "/api/ok")
	p, err := NewProxy(cfgA)
	if err != nil {
		t.Fatalf("initial build failed: %v", err)
	}

	// Warm the connection pool so one connection sits idle.
	if body := proxyGet(t, p, "/api/ok"); body != "upstreamA" {
		t.Fatalf("expected routing to upstreamA, got %q", body)
	}
	v1 := p.CurrentSnapshot()
	retiredTransport := v1.Transports["s-runtime"]
	if retiredTransport == nil {
		t.Fatal("expected transport registered on the snapshot")
	}

	// An in-flight request is parked on upstream A while the service is
	// removed from the configuration.
	park.Store(true)
	blocked := make(chan blockedResult, 1)
	go func() {
		req := httptest.NewRequest(http.MethodGet, "/api/ok", nil)
		rec := httptest.NewRecorder()
		p.ServeHTTP(rec, req)
		blocked <- blockedResult{rec.Code, rec.Body.String()}
	}()
	<-started

	emptyCfg := &config.Config{Server: config.ServerConfig{ProxyPort: 8080, AdminPort: 8081}}
	if err := p.UpdateRoutes(emptyCfg); err != nil {
		t.Fatalf("reload without service failed: %v", err)
	}
	if p.CurrentSnapshot().Version != 2 {
		t.Fatalf("expected version 2 after reload, got %d", p.CurrentSnapshot().Version)
	}

	// Release the in-flight request: it completes via runtime v1, and the
	// last releaseRequest triggers the deferred drain synchronously.
	close(release)
	select {
	case res := <-blocked:
		if res.code != http.StatusOK || res.body != "upstreamA" {
			t.Fatalf("in-flight request finished with %d %q, want 200 %q", res.code, res.body, "upstreamA")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("in-flight request never completed")
	}

	// The retired transport must be evicted from the global cache.
	transportMu.RLock()
	_, cached := serviceTransports["s-runtime"]
	transportMu.RUnlock()
	if cached {
		t.Fatal("expected retired transport evicted from the global cache")
	}

	// ...and its idle connections must actually close.
	if !closedSoon(t, tracker) {
		t.Fatal("expected the retired transport's idle connections to close")
	}

	// Repeated cleanup must be side-effect free (no panics, no resurrection).
	p.retireSnapshot(v1)
	p.drainRuntime(v1)
	transportMu.RLock()
	_, cached = serviceTransports["s-runtime"]
	transportMu.RUnlock()
	if cached {
		t.Fatal("cache entry resurrected by repeated cleanup")
	}

	// A second reload without the service is also safe.
	if err := p.UpdateRoutes(emptyCfg); err != nil {
		t.Fatalf("second empty reload failed: %v", err)
	}
	if p.CurrentSnapshot().Version != 3 {
		t.Fatalf("expected version 3 after second reload, got %d", p.CurrentSnapshot().Version)
	}
}
