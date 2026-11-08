package gateway

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/haiser1/go-api-gateway/internal/config"
)

// --- Unit tests: selector -------------------------------------------------

func lbRuntime(algorithm string, targetNames ...string) *UpstreamRuntime {
	up := config.Upstream{Id: "u-lb", Name: "lb", Algorithm: algorithm}
	for _, n := range targetNames {
		host, port := n, 9000
		for i := len(n) - 1; i >= 0; i-- {
			if n[i] == ':' && i > 0 {
				host, port = n[:i], portFromString(n[i+1:])
				break
			}
		}
		up.Targets = append(up.Targets, config.UpstreamTarget{Host: host, Port: port, Weight: 1})
	}
	up.ApplyDefaults()
	return NewUpstreamRuntime(up)
}

func portFromString(s string) int {
	n := 0
	for i := 0; i < len(s); i++ {
		n = n*10 + int(s[i]-'0')
	}
	return n
}

func targetName(t *testing.T, rt *UpstreamRuntime, idx int) string {
	t.Helper()
	if idx < 0 || idx >= len(rt.targets) {
		t.Fatalf("pick returned invalid index %d", idx)
	}
	return rt.targets[idx].name
}

func TestUpstreamRuntime_RoundRobinDistribution(t *testing.T) {
	rt := lbRuntime("round-robin", "a:1", "b:2", "c:3")
	if rt.hasWeights {
		t.Fatal("uniform upstream must keep the round-robin fast path")
	}

	counts := map[string]int{}
	for i := 0; i < 9; i++ {
		idx, ok := rt.pick(nil)
		if !ok {
			t.Fatal("pick must succeed for healthy targets")
		}
		counts[targetName(t, rt, idx)]++
	}
	for _, tc := range []string{"a:1", "b:2", "c:3"} {
		if counts[tc] != 3 {
			t.Fatalf("target %q picked %d times, want 3", tc, counts[tc])
		}
	}
}

func TestUpstreamRuntime_WeightedDistribution(t *testing.T) {
	up := config.Upstream{
		Id: "u-lbw", Name: "lbw", Algorithm: "weighted-round-robin",
		Targets: []config.UpstreamTarget{
			{Host: "a", Port: 1, Weight: 3},
			{Host: "b", Port: 2, Weight: 1},
		},
	}
	up.ApplyDefaults()
	rt := NewUpstreamRuntime(up)
	if !rt.hasWeights {
		t.Fatal("differing weights must enable the weighted path")
	}

	// Smooth weighted round robin for 3:1 interleaves — over any 4-pick
	// window each target must receive exactly its weight share.
	seq := ""
	for i := 0; i < 8; i++ {
		idx, ok := rt.pick(nil)
		if !ok {
			t.Fatal("weighted pick failed")
		}
		seq += targetName(t, rt, idx) + ","
	}
	want := "a:1,a:1,b:2,a:1,a:1,a:1,b:2,a:1,"
	if seq != want {
		t.Fatalf("weighted sequence = %q, want %q (smooth, no burst of heavy target)", seq, want)
	}
}

func TestUpstreamRuntime_EjectionSkipsUnhealthy(t *testing.T) {
	rt := lbRuntime("round-robin", "a:1", "b:2", "c:3")
	for i := 0; i < lbTargetEjectThreshold; i++ {
		rt.MarkFailure(1) // target "b:2"
	}
	if !rt.targets[1].ejected.Load() {
		t.Fatal("target must be ejected after the threshold")
	}

	counts := map[string]int{}
	for i := 0; i < 6; i++ {
		idx, ok := rt.pick(nil)
		if !ok {
			t.Fatal("remaining healthy targets must serve")
		}
		name := targetName(t, rt, idx)
		if name == "b:2" {
			t.Fatal("ejected target must not receive new requests")
		}
		counts[name]++
	}
	if counts["a:1"] != 3 || counts["c:3"] != 3 {
		t.Fatalf("split must be even between a and c: %v", counts)
	}
	if got := rt.healthyCount(); got != 2 {
		t.Fatalf("healthyCount = %d, want 2", got)
	}
}

func TestUpstreamRuntime_RecoveryOnSuccess(t *testing.T) {
	rt := lbRuntime("round-robin", "a:1", "b:2")
	for i := 0; i < lbTargetEjectThreshold; i++ {
		rt.MarkFailure(0)
	}
	if got := rt.healthyCount(); got != 1 {
		t.Fatalf("pre-recovery healthyCount = %d, want 1", got)
	}

	rt.MarkSuccess(0)
	if rt.targets[0].ejected.Load() {
		t.Fatal("successful attempt must readmit the ejected target")
	}
	idx, ok := rt.pick(nil)
	if !ok {
		t.Fatal("recovered upstream must serve again")
	}
	targetName(t, rt, idx) // valid index
}

func TestUpstreamRuntime_RetryExcludesTargets(t *testing.T) {
	rt := lbRuntime("round-robin", "a:1", "b:2", "c:3")

	idx, ok := rt.pick(map[int]struct{}{0: {}, 1: {}})
	if !ok || targetName(t, rt, idx) != "c:3" {
		t.Fatalf("pick excluding 0,1 = (%q,%v), want c:3", targetName(t, rt, idx), ok)
	}

	// Everything excluded → no candidate: the retry loop interprets this as
	// "all candidates failed" and surfaces Bad Gateway.
	idx, ok = rt.pick(map[int]struct{}{0: {}, 1: {}, 2: {}})
	if ok {
		t.Fatalf("fully excluded upstream must not pick anything, got index %d", idx)
	}
}

func TestUpstreamRuntime_ForceProbeLeastRecentlyFailed(t *testing.T) {
	rt := lbRuntime("round-robin", "a:1", "b:2", "c:3")
	for i := 0; i < len(rt.targets); i++ {
		for j := 0; j < lbTargetEjectThreshold; j++ {
			rt.MarkFailure(i)
		}
	}
	if rt.healthyCount() != 0 {
		t.Fatal("precondition: every target is ejected")
	}

	// Simulate drifting failure ages: c failed last, a failed first.
	rt.targets[0].lastFailed.Store(time.Now().Add(-10 * time.Second).UnixNano())
	rt.targets[1].lastFailed.Store(time.Now().Add(-5 * time.Second).UnixNano())
	rt.targets[2].lastFailed.Store(time.Now().UnixNano())

	idx, ok := rt.pick(nil)
	if !ok || targetName(t, rt, idx) != "a:1" {
		t.Fatalf("force probe must pick the least-recently-failed target, got %q ok=%v", targetName(t, rt, idx), ok)
	}
}

func TestUpstreamRuntime_CooldownReprobe(t *testing.T) {
	rt := lbRuntime("round-robin", "a:1", "b:2")
	for i := 0; i < len(rt.targets); i++ {
		for j := 0; j < lbTargetEjectThreshold; j++ {
			rt.MarkFailure(i)
		}
	}

	// a's failure is older than the cooldown → eligible for re-probe.
	rt.targets[0].lastFailed.Store(time.Now().Add(-lbTargetCooldown).UnixNano())
	// b failed just now.
	rt.targets[1].lastFailed.Store(time.Now().UnixNano())

	idx, ok := rt.pick(nil)
	if !ok || targetName(t, rt, idx) != "a:1" {
		t.Fatalf("cooldown re-probe must pick target past cooldown, got %q ok=%v", targetName(t, rt, idx), ok)
	}
}

func TestUpstreamRuntime_ConcurrentPicksAndMarks(t *testing.T) {
	rt := lbRuntime("round-robin", "a:1", "b:2", "c:3", "d:4")
	names := rt.targets

	var wg sync.WaitGroup
	for g := 0; g < 50; g++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				idx, ok := rt.pick(nil)
				if !ok || idx < 0 || idx >= len(names) {
					panic("pick returned invalid candidate")
				}
			}
		}()
		go func() {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				rt.MarkFailure(i % len(rt.targets))
				rt.MarkSuccess(i % len(rt.targets))
			}
		}()
	}
	wg.Wait()
}

// --- Integration: proxy-level load balancing ------------------------------

func newTaggedUpstream(t *testing.T, tag string, failWithStatus int) *httptest.Server {
	t.Helper()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if failWithStatus != 0 {
			w.WriteHeader(failWithStatus)
			_, _ = w.Write([]byte(tag))
			return
		}
		_, _ = w.Write([]byte(tag))
	}))
	t.Cleanup(ts.Close)
	return ts
}

func lbConfig(svcId, algorithm string, retries int, targets []config.UpstreamTarget, path string) *config.Config {
	up := config.Upstream{
		Id:        "u-" + svcId,
		Name:      "lb-" + svcId,
		Algorithm: algorithm,
		Targets:   targets,
	}
	up.ApplyDefaults()

	svc := config.Service{Id: svcId, Name: "svc-" + svcId, UpstreamId: up.Id, Protocol: "http", Retries: retries}
	svc.ApplyDefaults()

	return &config.Config{
		Server:    config.ServerConfig{ProxyPort: 8080, AdminPort: 8081},
		Upstreams: []config.Upstream{up},
		Services:  []config.Service{svc},
		Routes:    []config.Route{{Id: "r-" + svcId, Name: "route-" + svcId, ServiceId: svcId, Methods: []string{"GET"}, Paths: []string{path}}},
	}
}

func lbTargetFromServer(t *testing.T, ts *httptest.Server, weight int) config.UpstreamTarget {
	t.Helper()
	u, err := url.Parse(ts.URL)
	if err != nil {
		t.Fatal(err)
	}
	host := u.Hostname()
	port := portFromString(u.Port())
	return config.UpstreamTarget{Host: host, Port: port, Weight: weight}
}

func lbRequests(t *testing.T, p *Proxy, n int) (bodies []string, statuses []int) {
	t.Helper()
	for i := 0; i < n; i++ {
		req := httptest.NewRequest(http.MethodGet, "/lb", nil)
		rec := httptest.NewRecorder()
		p.ServeHTTP(rec, req)
		statuses = append(statuses, rec.Code)
		bodies = append(bodies, rec.Body.String())
	}
	return bodies, statuses
}

// Requests must be distributed across ALL targets in round-robin order —
// never freezing on targets[0] like the pre-Phase-8 build did.
func TestProxyLoadBalancing_RoundRobinOrder(t *testing.T) {
	a := newTaggedUpstream(t, "A", 0)
	b := newTaggedUpstream(t, "B", 0)
	c := newTaggedUpstream(t, "C", 0)

	cfg := lbConfig("svc-lb-rr", "round-robin", 0, []config.UpstreamTarget{
		lbTargetFromServer(t, a, 1), lbTargetFromServer(t, b, 1), lbTargetFromServer(t, c, 1),
	}, "/lb")

	p, err := NewProxy(cfg)
	if err != nil {
		t.Fatal(err)
	}

	bodies, statuses := lbRequests(t, p, 6)
	want := []string{"A", "B", "C", "A", "B", "C"}
	for i := 0; i < 6; i++ {
		if bodies[i] != want[i] {
			t.Fatalf("request %d landed on %q, want %q (bodies=%v)", i, bodies[i], want[i], bodies)
		}
		if statuses[i] != http.StatusOK {
			t.Fatalf("request %d got status %d", i, statuses[i])
		}
	}
}

func TestProxyLoadBalancing_Weighted(t *testing.T) {
	a := newTaggedUpstream(t, "A", 0)
	b := newTaggedUpstream(t, "B", 0)

	cfg := lbConfig("svc-lb-wrr", "weighted-round-robin", 0, []config.UpstreamTarget{
		lbTargetFromServer(t, a, 3), lbTargetFromServer(t, b, 1),
	}, "/lb")

	p, err := NewProxy(cfg)
	if err != nil {
		t.Fatal(err)
	}

	bodies, _ := lbRequests(t, p, 8)
	want := []string{"A", "A", "B", "A", "A", "A", "B", "A"}
	for i, got := range bodies {
		if got != want[i] {
			t.Fatalf("request %d landed on %q, want %q (bodies=%v)", i, got, want[i], bodies)
		}
	}
}

// A failing target must stop receiving new requests (health-aware selection)
// while the remaining healthy targets keep serving without the whole-service
// breaker tripping.
func TestProxyLoadBalancing_FailingTargetEjected(t *testing.T) {
	a := newTaggedUpstream(t, "A", 0)
	b := newTaggedUpstream(t, "B", http.StatusInternalServerError)
	c := newTaggedUpstream(t, "C", 0)

	cfg := lbConfig("svc-lb-eject", "round-robin", 0, []config.UpstreamTarget{
		lbTargetFromServer(t, a, 1), lbTargetFromServer(t, b, 1), lbTargetFromServer(t, c, 1),
	}, "/lb")

	p, err := NewProxy(cfg)
	if err != nil {
		t.Fatal(err)
	}

	bodies, _ := lbRequests(t, p, 14)

	// First full rotation: A,B,C,...
	wantPrefix := []string{"A", "B", "C", "A", "B", "C", "A", "B", "C"}
	for i := 0; i < 9; i++ {
		if bodies[i] != wantPrefix[i] {
			t.Fatalf("request %d landed on %q, want %q", i, bodies[i], wantPrefix[i])
		}
	}
	// After 3 B failures the target is ejected: no B ever again.
	for i := 9; i < 14; i++ {
		if bodies[i] == "B" {
			t.Fatalf("request %d still served by the ejected target (bodies=%v)", i, bodies)
		}
	}
}

// Retry integration: a dead first target is replaced by the next target on
// the retry attempt and the client sees a 200 from the surviving one.
func TestProxyRetry_FailsOverToNextTarget(t *testing.T) {
	// Dead target: a listener that is immediately closed.
	dead := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	deadURL, _ := url.Parse(dead.URL)
	deadPort := portFromString(deadURL.Port())
	dead.Close()

	alive := newTaggedUpstream(t, "ALIVE", 0)

	cfg := lbConfig("svc-retry-failover", "round-robin", 1, []config.UpstreamTarget{
		{Host: deadURL.Hostname(), Port: deadPort, Weight: 1},
		lbTargetFromServer(t, alive, 1),
	}, "/lb")

	p, err := NewProxy(cfg)
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/lb", nil)
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || rec.Body.String() != "ALIVE" {
		t.Fatalf("retry must fail over to the alive target, got %d %q", rec.Code, rec.Body.String())
	}
}

// Retries exhausted: every target dead → the client gets a structured 502.
func TestProxyRetry_ExhaustedReturnsBadGateway(t *testing.T) {
	dead1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	d1, _ := url.Parse(dead1.URL)
	d1Port := portFromString(d1.Port())
	dead1.Close()
	dead := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	d2, _ := url.Parse(dead.URL)
	d2Port := portFromString(d2.Port())
	dead.Close()

	cfg := lbConfig("svc-retry-dead", "round-robin", 1, []config.UpstreamTarget{
		{Host: d1.Hostname(), Port: d1Port, Weight: 1},
		{Host: d2.Hostname(), Port: d2Port, Weight: 1},
	}, "/lb")

	p, err := NewProxy(cfg)
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/lb", nil)
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("exhausted retries must return 502, got %d %q", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", ct)
	}
}

// A non-replayable streamed body must disable retries entirely: with the
// first target dead the request surfaces its 502 instead of being replayed.
func TestProxyRetry_NoReplayOfStreamedBody(t *testing.T) {
	dead := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	d, _ := url.Parse(dead.URL)
	deadPort := portFromString(d.Port())
	dead.Close()
	alive := newTaggedUpstream(t, "ALIVE", 0)

	cfg := lbConfig("svc-retry-streamed", "round-robin", 1, []config.UpstreamTarget{
		{Host: d.Hostname(), Port: deadPort, Weight: 1},
		lbTargetFromServer(t, alive, 1),
	}, "/lb")
	cfg.Routes[0].Methods = []string{"POST"}

	p, err := NewProxy(cfg)
	if err != nil {
		t.Fatal(err)
	}

	// A server-side request with a real streamed body: ContentLength > 0,
	// GetBody nil (the way net/http server always delivers bodies).
	req := httptest.NewRequest(http.MethodPost, "/lb", strings.NewReader("streamed-content"))
	req.ContentLength = int64(len("streamed-content"))
	req.GetBody = nil

	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("streamed body must not be replayed (want 502), got %d %q", rec.Code, rec.Body.String())
	}
}
