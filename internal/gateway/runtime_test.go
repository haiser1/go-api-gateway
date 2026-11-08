package gateway

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/haiser1/go-api-gateway/internal/config"
)

// recordedUpstream wraps an httptest server that answers with a fixed tag so
// tests can tell which upstream a request landed on.
type recordedUpstream struct {
	server *httptest.Server
	host   string
	port   int
}

// newEchoUpstream creates an upstream server replying with tag. When started
// is non-nil the handler signals it upon request arrival; when release is
// non-nil the handler blocks until it is closed — used to hold an in-flight
// request open across a reload.
func newEchoUpstream(t *testing.T, tag string, started chan<- struct{}, release <-chan struct{}) *recordedUpstream {
	t.Helper()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if started != nil {
			started <- struct{}{}
		}
		if release != nil {
			<-release
		}
		fmt.Fprint(w, tag)
	}))
	t.Cleanup(func() { ts.Close() })

	u, err := url.Parse(ts.URL)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil {
		t.Fatal(err)
	}
	return &recordedUpstream{server: ts, host: u.Hostname(), port: port}
}

// runtimeConfig returns a single upstream/service/route config pointing at
// the given target.
func runtimeConfig(host string, port int, paths ...string) *config.Config {
	up := config.Upstream{
		Id:        "u-runtime",
		Name:      "runtime-upstream",
		Algorithm: "round-robin",
		Targets:   []config.UpstreamTarget{{Host: host, Port: port, Weight: 1}},
	}
	up.ApplyDefaults()

	svc := config.Service{Id: "s-runtime", Name: "runtime-service", UpstreamId: "u-runtime", Protocol: "http"}
	svc.ApplyDefaults()

	return &config.Config{
		Server:    config.ServerConfig{ProxyPort: 8080, AdminPort: 8081},
		Upstreams: []config.Upstream{up},
		Services:  []config.Service{svc},
		Routes:    []config.Route{{Id: "r-runtime", Name: "runtime-route", ServiceId: "s-runtime", Methods: []string{"GET"}, Paths: paths}},
	}
}

func proxyGet(t *testing.T, p *Proxy, path string) string {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("proxy responded %d with body %q", rec.Code, rec.Body.String())
	}
	return rec.Body.String()
}

// Plan tests 1-5: build and publish runtime v1, serve a request through it,
// then publish v2 and verify new requests use v2 while the v1 snapshot
// remains fully functional.
func TestRuntimeSnapshot_PublishesAndReplacesRuntime(t *testing.T) {
	upA := newEchoUpstream(t, "upstreamA", nil, nil)
	cfgA := runtimeConfig(upA.host, upA.port, "/api/hello")

	p, err := NewProxy(cfgA)
	if err != nil {
		t.Fatalf("initial build failed: %v", err)
	}

	v1 := p.CurrentSnapshot()
	if v1 == nil {
		t.Fatal("expected v1 snapshot to be published")
	}
	if v1.Version != 1 {
		t.Fatalf("expected snapshot version 1, got %d", v1.Version)
	}
	if body := proxyGet(t, p, "/api/hello"); body != "upstreamA" {
		t.Fatalf("expected v1 runtime to route to upstreamA, got %q", body)
	}
	if len(v1.Routes) != 1 || len(v1.Transports) != 1 {
		t.Fatalf("snapshot incomplete: routes=%d transports=%d", len(v1.Routes), len(v1.Transports))
	}
	if _, ok := v1.Transports["s-runtime"]; !ok {
		t.Fatal("expected transport for s-runtime in snapshot")
	}

	// Build and publish runtime v2 pointing at a different upstream.
	upB := newEchoUpstream(t, "upstreamB", nil, nil)
	cfgB := runtimeConfig(upB.host, upB.port, "/api/hello")
	if err := p.UpdateRoutes(cfgB); err != nil {
		t.Fatalf("v2 build failed: %v", err)
	}

	v2 := p.CurrentSnapshot()
	if v2.Version != 2 {
		t.Fatalf("expected snapshot version 2 after reload, got %d", v2.Version)
	}
	if v2.Tree == v1.Tree {
		t.Fatal("expected a fresh routing tree after reload")
	}
	if body := proxyGet(t, p, "/api/hello"); body != "upstreamB" {
		t.Fatalf("expected new requests to use runtime v2 (upstreamB), got %q", body)
	}

	// v1 snapshot must still route correctly: serve through it directly.
	v1Route, _ := v1.Tree.Search("GET", "/api/hello")
	if v1Route == nil {
		t.Fatal("v1 snapshot lost its route after v2 was published")
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/hello", nil)
	v1Route.CompiledHandler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || rec.Body.String() != "upstreamA" {
		t.Fatalf("v1 handler served %d %q, want 200 %q", rec.Code, rec.Body.String(), "upstreamA")
	}
}

// Plan tests 3+6: a request that is already executing when a new runtime is
// published must complete using the runtime it started with — never get
// interrupted or rerouted mid-response.
func TestRuntimeSnapshot_InFlightRequestCompletesOnOldRuntime(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	upA := newEchoUpstream(t, "upstreamA", started, release)

	cfgA := runtimeConfig(upA.host, upA.port, "/api/slow")
	p, err := NewProxy(cfgA)
	if err != nil {
		t.Fatalf("initial build failed: %v", err)
	}

	type result struct {
		code int
		body string
	}
	done := make(chan result, 1)
	go func() {
		req := httptest.NewRequest(http.MethodGet, "/api/slow", nil)
		rec := httptest.NewRecorder()
		p.ServeHTTP(rec, req)
		done <- result{rec.Code, rec.Body.String()}
	}()

	// Wait until the request reached upstream A and is blocked in flight.
	<-started

	upB := newEchoUpstream(t, "upstreamB", nil, nil)
	cfgB := runtimeConfig(upB.host, upB.port, "/api/slow")
	if err := p.UpdateRoutes(cfgB); err != nil {
		t.Fatalf("v2 build failed: %v", err)
	}
	if p.CurrentSnapshot().Version != 2 {
		t.Fatalf("expected version 2 after reload, got %d", p.CurrentSnapshot().Version)
	}

	// New requests already use the new runtime.
	if body := proxyGet(t, p, "/api/slow"); body != "upstreamB" {
		t.Fatalf("expected new requests routed to upstreamB, got %q", body)
	}

	// Release the in-flight request and let it finish on runtime v1.
	close(release)
	select {
	case res := <-done:
		if res.code != http.StatusOK || res.body != "upstreamA" {
			t.Fatalf("in-flight request finished with %d %q, want 200 %q", res.code, res.body, "upstreamA")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("in-flight request never completed")
	}
}

// Plan test 7: a failed build must not replace the active runtime.
func TestRuntimeSnapshot_BuildFailureKeepsPreviousRuntime(t *testing.T) {
	upA := newEchoUpstream(t, "upstreamA", nil, nil)
	cfgA := runtimeConfig(upA.host, upA.port, "/api/ok")

	p, err := NewProxy(cfgA)
	if err != nil {
		t.Fatalf("initial build failed: %v", err)
	}

	// A control character in the host makes url.Parse reject the target URL.
	broken := runtimeConfig("bad\x7fhost", 9090, "/api/ok")
	if err := p.UpdateRoutes(broken); err == nil {
		t.Fatal("expected UpdateRoutes to fail on an invalid target")
	}

	if p.CurrentSnapshot().Version != 1 {
		t.Fatalf("version changed after failed build: %d", p.CurrentSnapshot().Version)
	}
	if body := proxyGet(t, p, "/api/ok"); body != "upstreamA" {
		t.Fatalf("previous runtime no longer serving after failed build: %q", body)
	}
}

// NewProxy must fail fast on an unusable config instead of publishing a
// partial runtime.
func TestNewProxy_FailsOnInvalidConfig(t *testing.T) {
	if _, err := NewProxy(runtimeConfig("bad\x7fhost", 80, "/api/ok")); err == nil {
		t.Fatal("expected NewProxy to fail on invalid target")
	}
	if _, err := NewProxy(nil); err == nil {
		t.Fatal("expected NewProxy to fail on nil config")
	}
}

// Stress variant (plan §4): concurrent ServeHTTP while the runtime is
// reloaded between two upstreams — every request must succeed (200) and
// always land on a valid upstream served by the runtime it started on.
func TestRuntimeSnapshot_ConcurrentRequestsDuringReload(t *testing.T) {
	upA := newEchoUpstream(t, "upstreamA", nil, nil)
	upB := newEchoUpstream(t, "upstreamB", nil, nil)
	cfgA := runtimeConfig(upA.host, upA.port, "/api/load")
	cfgB := runtimeConfig(upB.host, upB.port, "/api/load")

	p, err := NewProxy(cfgA)
	if err != nil {
		t.Fatalf("initial build failed: %v", err)
	}

	stop := make(chan struct{})
	var wg sync.WaitGroup
	const workers = 8
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				req := httptest.NewRequest(http.MethodGet, "/api/load", nil)
				rec := httptest.NewRecorder()
				p.ServeHTTP(rec, req)
				if rec.Code != http.StatusOK {
					t.Errorf("concurrent request got %d (%q)", rec.Code, rec.Body.String())
					return
				}
				if body := rec.Body.String(); body != "upstreamA" && body != "upstreamB" {
					t.Errorf("unexpected upstream body %q", body)
					return
				}
			}
		}()
	}

	for i := 0; i < 10; i++ {
		if err := p.UpdateRoutes(cfgB); err != nil {
			t.Errorf("reload to B failed: %v", err)
		}
		if err := p.UpdateRoutes(cfgA); err != nil {
			t.Errorf("reload to A failed: %v", err)
		}
	}

	close(stop)
	wg.Wait()
}

// Integration (Phase 9 plan §4): removing a service through reload must
// remove its routes from the routing table (structured 404) without
// disturbing the remaining services' routing.
func TestRuntimeReload_ServiceRemovalRemovesRoutesOnly(t *testing.T) {
	upA := newEchoUpstream(t, "upstreamA", nil, nil)
	upB := newEchoUpstream(t, "upstreamB", nil, nil)

	upCfgA := config.Upstream{
		Id: "u-a", Name: "upstream-a", Algorithm: "round-robin",
		Targets: []config.UpstreamTarget{{Host: upA.host, Port: upA.port, Weight: 1}},
	}
	upCfgA.ApplyDefaults()
	upCfgB := config.Upstream{
		Id: "u-b", Name: "upstream-b", Algorithm: "round-robin",
		Targets: []config.UpstreamTarget{{Host: upB.host, Port: upB.port, Weight: 1}},
	}
	upCfgB.ApplyDefaults()

	svcA := config.Service{Id: "s-a", Name: "service-a", UpstreamId: "u-a", Protocol: "http"}
	svcB := config.Service{Id: "s-b", Name: "service-b", UpstreamId: "u-b", Protocol: "http"}

	cfgBoth := &config.Config{
		Server:    config.ServerConfig{ProxyPort: 8080, AdminPort: 8081},
		Upstreams: []config.Upstream{upCfgA, upCfgB},
		Services:  []config.Service{svcA, svcB},
		Routes: []config.Route{
			{Id: "r-a", Name: "route-a", ServiceId: "s-a", Methods: []string{"GET"}, Paths: []string{"/a/api"}},
			{Id: "r-b", Name: "route-b", ServiceId: "s-b", Methods: []string{"GET"}, Paths: []string{"/b/api"}},
		},
	}

	p, err := NewProxy(cfgBoth)
	if err != nil {
		t.Fatal(err)
	}
	if body := proxyGet(t, p, "/a/api"); body != "upstreamA" {
		t.Fatalf("expected upstreamA, got %q", body)
	}
	if body := proxyGet(t, p, "/b/api"); body != "upstreamB" {
		t.Fatalf("expected upstreamB, got %q", body)
	}

	// Reload WITHOUT service B: its route must vanish, A must keep serving.
	cfgAOnly := *cfgBoth
	cfgAOnly.Services = []config.Service{svcA}
	cfgAOnly.Routes = []config.Route{cfgBoth.Routes[0]}
	cfgAOnly.Upstreams = []config.Upstream{upCfgA}
	if err := p.UpdateRoutes(&cfgAOnly); err != nil {
		t.Fatalf("reload without service-b failed: %v", err)
	}

	if body := proxyGet(t, p, "/a/api"); body != "upstreamA" {
		t.Fatalf("surviving service broken after removal: %q", body)
	}

	req := httptest.NewRequest(http.MethodGet, "/b/api", nil)
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("removed service must be a 404, got %d %q", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", ct)
	}
}
