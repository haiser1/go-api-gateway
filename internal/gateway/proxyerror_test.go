package gateway

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"
	"time"

	"github.com/haiser1/go-api-gateway/internal/config"
)

// --- Phase 7 harness ------------------------------------------------------

// newHeaderEchoUpstream replies with the headers it received, encoded as JSON,
// so tests can assert the forwarded header policy.
func newHeaderEchoUpstream(t *testing.T, delay time.Duration) *httptest.Server {
	t.Helper()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if delay > 0 {
			time.Sleep(delay)
		}
		b, _ := json.Marshal(r.Header)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(b)
	}))
	t.Cleanup(ts.Close)
	return ts
}

// proxyErrConfig builds a minimal valid config around a single route. Every
// caller gives the service a unique id so the shared transport cache and
// circuit breaker registry never leak state between tests.
func proxyErrConfig(u *url.URL, svcId, method, path string, readTimeoutSec int) *config.Config {
	up := config.Upstream{
		Id:        "u-p7",
		Name:      "p7-upstream",
		Algorithm: "round-robin",
		Targets:   []config.UpstreamTarget{{Host: u.Hostname(), Port: portOf(u), Weight: 1}},
	}
	up.ApplyDefaults()

	svc := config.Service{Id: svcId, Name: "p7-service", UpstreamId: "u-p7", Protocol: "http"}
	svc.ApplyDefaults()
	svc.ReadTimeout = readTimeoutSec

	return &config.Config{
		Server:    config.ServerConfig{ProxyPort: 8080, AdminPort: 8081},
		Upstreams: []config.Upstream{up},
		Services:  []config.Service{svc},
		Routes:    []config.Route{{Id: "r-p7", Name: "p7-route", ServiceId: svcId, Methods: []string{method}, Paths: []string{path}}},
	}
}

func portOf(u *url.URL) int {
	port, err := strconv.Atoi(u.Port())
	if err != nil {
		return 80
	}
	return port
}

type proxyResp struct {
	code    int
	body    string
	headers http.Header
}

func doProxyGet(t *testing.T, p *Proxy, method, path string) proxyResp {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	req.RemoteAddr = "192.0.2.77:55555"
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, req)
	return proxyResp{code: rec.Code, body: rec.Body.String(), headers: rec.Header()}
}

// --- Tests ----------------------------------------------------------------

// The whole proxy error surface must speak the JSON error contract (task 1-2,
// 7-8): Content-Type application/json on 404 / 405 / 502 / 503 / 504.
func TestProxyErrorResponses_JSONSurface(t *testing.T) {
	ts := newHeaderEchoUpstream(t, 0)

	tests := []struct {
		name        string
		svcId       string
		method      string
		reqMethod   string
		path        string
		reqPath     string
		readTimeout int
		openBreaker bool
		closeUp     bool
		wantCode    int
		wantError   string
	}{
		{
			name: "404 unknown path", svcId: "svc-p7-404",
			method: "GET", reqMethod: "GET", path: "/p7/api", reqPath: "/p7/nope",
			wantCode: http.StatusNotFound, wantError: "Not Found",
		},
		{
			name: "405 wrong method", svcId: "svc-p7-405",
			method: "GET", reqMethod: "POST", path: "/p7/only-get", reqPath: "/p7/only-get",
			wantCode: http.StatusMethodNotAllowed, wantError: "Method Not Allowed",
		},
		{
			name: "502 connection refused", svcId: "svc-p7-502",
			method: "GET", reqMethod: "GET", path: "/p7/refused", reqPath: "/p7/refused",
			closeUp: true, wantCode: http.StatusBadGateway, wantError: "Bad Gateway",
		},
		{
			name: "503 breaker open", svcId: "svc-p7-503",
			method: "GET", reqMethod: "GET", path: "/p7/open", reqPath: "/p7/open",
			openBreaker: true, wantCode: http.StatusServiceUnavailable,
			wantError: "Service temporarily unavailable",
		},
		{
			name: "504 upstream timeout", svcId: "svc-p7-504",
			method: "GET", reqMethod: "GET", path: "/p7/slow", reqPath: "/p7/slow",
			readTimeout: 1, wantCode: http.StatusGatewayTimeout, wantError: "Gateway Timeout",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var cfgURL *url.URL
			switch {
			case tt.closeUp:
				// A listener that is immediately closed guarantees a dial
				// failure without network flakiness.
				l, lErr := net.Listen("tcp", "127.0.0.1:0")
				if lErr != nil {
					t.Fatal(lErr)
				}
				cu, err2 := url.Parse("http://" + l.Addr().String())
				_ = l.Close()
				if err2 != nil {
					t.Fatal(err2)
				}
				cfgURL = cu
			case tt.svcId == "svc-p7-504":
				// A deliberately slow upstream behind the service's short
				// ReadTimeout produces ResponseHeaderTimeout → 504.
				slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					time.Sleep(3 * time.Second)
				}))
				t.Cleanup(slow.Close)
				su, err2 := url.Parse(slow.URL)
				if err2 != nil {
					t.Fatal(err2)
				}
				cfgURL = su
			default:
				tu, err2 := url.Parse(ts.URL)
				if err2 != nil {
					t.Fatal(err2)
				}
				cfgURL = tu
			}

			p, err := NewProxy(proxyErrConfig(cfgURL, tt.svcId, tt.method, tt.path, tt.readTimeout))
			if err != nil {
				t.Fatalf("initial build failed: %v", err)
			}
			if tt.openBreaker {
				for i := 0; i < failureThreshold; i++ {
					GetCircuitBreaker(tt.svcId).RecordFailure()
				}
			}

			resp := doProxyGet(t, p, tt.reqMethod, tt.reqPath)
			if resp.code != tt.wantCode {
				t.Fatalf("got status %d, want %d (body %q)", resp.code, tt.wantCode, resp.body)
			}
			if ct := resp.headers.Get("Content-Type"); ct != "application/json" {
				t.Fatalf("Content-Type = %q, want application/json", ct)
			}

			var payload struct {
				Error string `json:"error"`
			}
			if err := json.Unmarshal([]byte(resp.body), &payload); err != nil {
				t.Fatalf("body %q is not valid JSON error: %v", resp.body, err)
			}
			if payload.Error != tt.wantError {
				t.Fatalf("error message = %q, want %q", payload.Error, tt.wantError)
			}
		})
	}
}

// Task 3+4: 405 must carry the Allow header listing every method registered
// for the existing path.
func TestProxyErrorResponses_405AllowHeader(t *testing.T) {
	ts := newHeaderEchoUpstream(t, 0)
	u, err := url.Parse(ts.URL)
	if err != nil {
		t.Fatal(err)
	}

	cfg := proxyErrConfig(u, "svc-p7-allow", "GET", "/p7/allow", 0)
	cfg.Routes[0] = config.Route{
		Id: "r-p7-allow", Name: "p7-allow", ServiceId: "svc-p7-allow",
		Methods: []string{"GET", "POST", "DELETE"}, Paths: []string{"/p7/allow"},
	}
	p, err := NewProxy(cfg)
	if err != nil {
		t.Fatal(err)
	}

	resp := doProxyGet(t, p, "PUT", "/p7/allow")
	if resp.code != http.StatusMethodNotAllowed {
		t.Fatalf("got %d, want 405", resp.code)
	}
	allow := resp.headers.Get("Allow")
	if allow != "DELETE, GET, POST" {
		t.Fatalf("Allow = %q, want %q", allow, "DELETE, GET, POST")
	}

	// A path that exists nowhere must stay a plain 404 — no Allow header.
	resp = doProxyGet(t, p, "PUT", "/p7/nowhere")
	if resp.code != http.StatusNotFound {
		t.Fatalf("unknown path got %d, want 404", resp.code)
	}
	if ah := resp.headers.Get("Allow"); ah != "" {
		t.Fatalf("404 must not carry an Allow header, got %q", ah)
	}
}

// Task 5+6: forwarded headers must come from the parsed request state (r.Host
// / r.TLS), never from client-controllable header reads. X-Forwarded-For gets
// the real client address appended by ReverseProxy.
func TestProxyForwardedHeaders_FromRequestState(t *testing.T) {
	ts := newHeaderEchoUpstream(t, 0)
	u, err := url.Parse(ts.URL)
	if err != nil {
		t.Fatal(err)
	}

	// Spoof attempt: the client plants its own forwarded values. The gateway
	// must overwrite Host-derived ones and normalize Proto.
	cfg := proxyErrConfig(u, "svc-p7-fwd", "GET", "/p7/fwd", 0)
	p, err := NewProxy(cfg)
	if p == nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/p7/fwd", nil)
	req.RemoteAddr = "198.51.100.5:44444"
	req.Host = "gateway.example.com"
	req.Header.Set("X-Forwarded-Host", "spoofed.attacker.io")
	req.Header.Set("X-Forwarded-Proto", "spoofed-proto")
	req.Header.Set("X-Forwarded-For", "spoofed.entry")
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("proxy returned %d (%q)", rec.Code, rec.Body.String())
	}

	var hdr map[string][]string
	if err := json.Unmarshal(rec.Body.Bytes(), &hdr); err != nil {
		t.Fatalf("upstream echo is not valid JSON: %v (body %q)", err, rec.Body.String())
	}
	get := func(k string) string {
		if v, ok := hdr[http.CanonicalHeaderKey(k)]; ok && len(v) > 0 {
			return v[0]
		}
		if v, ok := hdr[k]; ok && len(v) > 0 {
			return v[0]
		}
		return ""
	}

	if got := get("X-Forwarded-Host"); got != "gateway.example.com" {
		t.Fatalf("X-Forwarded-Host = %q, want gateway.example.com (r.Host, not spoofed)", got)
	}
	if got := get("X-Forwarded-Proto"); got != "http" {
		t.Fatalf("X-Forwarded-Proto = %q, want http", got)
	}
	if got := get("X-Forwarded-For"); got != "spoofed.entry, 198.51.100.5" {
		t.Fatalf("X-Forwarded-For = %q, want spoofed.entry + appended real client 198.51.100.5", got)
	}
}

// Sanity: MapUpstreamError covers the full taxonomy without a live proxy.
func TestMapUpstreamError_Classification(t *testing.T) {
	cb := &CircuitBreakerOpenError{ServiceId: "svc-x"}
	if got := MapUpstreamError(cb); got != http.StatusServiceUnavailable {
		t.Fatalf("circuit open mapped to %d, want 503", got)
	}
	if got := MapUpstreamError(fmt.Errorf("wrapped: %w", cb)); got != http.StatusServiceUnavailable {
		t.Fatalf("wrapped circuit open mapped to %d, want 503", got)
	}
	timeout := &timeoutErrStub{}
	if got := MapUpstreamError(fmt.Errorf("dial: %w", timeout)); got != http.StatusGatewayTimeout {
		t.Fatalf("timeout mapped to %d, want 504", got)
	}
	if got := MapUpstreamError(fmt.Errorf("wrapped: %w", timeout)); got != http.StatusGatewayTimeout {
		t.Fatalf("wrapped timeout mapped to %d, want 504", got)
	}
	if got := MapUpstreamError(fmt.Errorf("boom")); got != http.StatusBadGateway {
		t.Fatalf("generic error mapped to %d, want 502", got)
	}
}

type timeoutErrStub struct{}

func (e *timeoutErrStub) Error() string   { return "i/o timeout" }
func (e *timeoutErrStub) Timeout() bool   { return true }
func (e *timeoutErrStub) Temporary() bool { return true }
