package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestIPRestrictionAllowlistAndCIDR(t *testing.T) {
	cfg := map[string]interface{}{
		"allowed_ips": []interface{}{
			"192.168.1.50",
			"10.10.0.0/16",
		},
	}
	plugin := NewIPRestrictionPlugin(cfg)

	// Allowed exact IP
	req1 := httptest.NewRequest(http.MethodGet, "/api/v1/data", nil)
	req1.RemoteAddr = "192.168.1.50:54321"
	rr1 := httptest.NewRecorder()
	called1 := false
	cont1 := plugin.Execute(rr1, req1, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called1 = true
		w.WriteHeader(http.StatusOK)
	}))
	if !cont1 || !called1 || rr1.Code != http.StatusOK {
		t.Errorf("expected 192.168.1.50 to be allowed, got continued=%v status=%d", cont1, rr1.Code)
	}

	// Allowed CIDR IP
	req2 := httptest.NewRequest(http.MethodGet, "/api/v1/data", nil)
	req2.RemoteAddr = "10.10.42.99:12345"
	rr2 := httptest.NewRecorder()
	called2 := false
	cont2 := plugin.Execute(rr2, req2, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called2 = true
		w.WriteHeader(http.StatusOK)
	}))
	if !cont2 || !called2 || rr2.Code != http.StatusOK {
		t.Errorf("expected 10.10.42.99 (in 10.10.0.0/16) to be allowed, got continued=%v status=%d", cont2, rr2.Code)
	}

	// Blocked IP outside allowlist
	req3 := httptest.NewRequest(http.MethodGet, "/api/v1/data", nil)
	req3.RemoteAddr = "203.0.113.5:54321"
	rr3 := httptest.NewRecorder()
	called3 := false
	cont3 := plugin.Execute(rr3, req3, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called3 = true
	}))
	if cont3 || called3 || rr3.Code != http.StatusForbidden {
		t.Errorf("expected 203.0.113.5 to be blocked with 403, got continued=%v status=%d", cont3, rr3.Code)
	}
}

func TestIPRestrictionDenylistAndForwardedFor(t *testing.T) {
	cfg := map[string]interface{}{
		"denied_ips":          []string{"172.16.0.0/12"},
		"trust_forwarded_for": true,
		"message":             "Access denied by IP policy",
	}
	plugin := NewIPRestrictionPlugin(cfg)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/data", nil)
	req.RemoteAddr = "127.0.0.1:8080"
	req.Header.Set("X-Forwarded-For", "172.20.10.5, 10.0.0.1")
	rr := httptest.NewRecorder()

	cont := plugin.Execute(rr, req, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	if cont {
		t.Fatal("expected request from denied X-Forwarded-For CIDR to be blocked")
	}
	if rr.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "Access denied by IP policy") {
		t.Errorf("expected custom message in body, got %s", rr.Body.String())
	}
}

func TestBotDetectionUserAgent(t *testing.T) {
	cfg := map[string]interface{}{
		"blocked_bots": []string{"sqlmap", "nikto", "badscraper/.*"},
	}
	plugin := NewIPRestrictionPlugin(cfg)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/data", nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; BadScraper/1.2)")
	rr := httptest.NewRecorder()

	cont := plugin.Execute(rr, req, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	if cont || rr.Code != http.StatusForbidden {
		t.Errorf("expected bot User-Agent to be blocked with 403, got continued=%v status=%d", cont, rr.Code)
	}
}
