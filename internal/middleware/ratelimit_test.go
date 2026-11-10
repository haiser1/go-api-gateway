package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/haiser1/go-api-gateway/internal/auth"
)

func rateLimitRequest(remoteAddr, subject string, claims map[string]interface{}) *http.Request {
	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	req.RemoteAddr = remoteAddr
	if subject != "" || claims != nil {
		req = req.WithContext(auth.WithContext(req.Context(), &auth.Context{
			User: &auth.UserIdentity{Subject: subject, Claims: claims},
		}))
	}
	return req
}

func hitRateLimitPlugin(plugin interface {
	Execute(http.ResponseWriter, *http.Request, http.Handler) bool
}, req *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("upstream"))
	})
	plugin.Execute(rec, req, next)
	return rec
}

func TestRateLimitPlugin_IntegerConfigAndIPKey(t *testing.T) {
	plugin := NewRateLimitPlugin(map[string]interface{}{"requests_per_minute": 2})

	for i, remote := range []string{"203.0.113.9:1111", "203.0.113.9:2222"} {
		rec := hitRateLimitPlugin(plugin, rateLimitRequest(remote, "", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("request %d status = %d, want 200", i+1, rec.Code)
		}
		if got := rec.Header().Get("X-RateLimit-Limit"); got != "2" {
			t.Fatalf("X-RateLimit-Limit = %q, want 2", got)
		}
	}

	rec := hitRateLimitPlugin(plugin, rateLimitRequest("203.0.113.9:3333", "", nil))
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("third request from same IP status = %d, want 429", rec.Code)
	}
	if got := rec.Header().Get("Retry-After"); got == "" {
		t.Fatal("429 response missing Retry-After header")
	}
}

func TestRateLimitPlugin_UserKeyUsesVerifiedIdentity(t *testing.T) {
	plugin := NewRateLimitPlugin(map[string]interface{}{
		"requests_per_minute": 1,
		"key_by":              "user",
	})

	req := rateLimitRequest("203.0.113.10:1111", "alice", map[string]interface{}{"sub": "alice"})
	req.Header.Set("X-User-Id", "bob")
	if rec := hitRateLimitPlugin(plugin, req); rec.Code != http.StatusOK {
		t.Fatalf("alice first request status = %d, want 200", rec.Code)
	}

	req = rateLimitRequest("203.0.113.10:2222", "alice", map[string]interface{}{"sub": "alice"})
	if rec := hitRateLimitPlugin(plugin, req); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("alice second request status = %d, want 429 (spoofed header must not create a new key)", rec.Code)
	}

	req = rateLimitRequest("203.0.113.10:3333", "bob", map[string]interface{}{"sub": "bob"})
	if rec := hitRateLimitPlugin(plugin, req); rec.Code != http.StatusOK {
		t.Fatalf("bob first request status = %d, want 200", rec.Code)
	}
}

func TestRateLimitPlugin_UserKeySupportsUserIDClaimAndCustomClaim(t *testing.T) {
	plugin := NewRateLimitPlugin(map[string]interface{}{
		"requests_per_minute": 1,
		"key_by":              "user",
	})

	req := rateLimitRequest("203.0.113.11:1111", "", map[string]interface{}{"user_id": "u-42"})
	if rec := hitRateLimitPlugin(plugin, req); rec.Code != http.StatusOK {
		t.Fatalf("user_id claim first request status = %d, want 200", rec.Code)
	}
	req = rateLimitRequest("203.0.113.11:2222", "", map[string]interface{}{"user_id": "u-42"})
	if rec := hitRateLimitPlugin(plugin, req); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("user_id claim second request status = %d, want 429", rec.Code)
	}

	custom := NewRateLimitPlugin(map[string]interface{}{
		"requests_per_minute": 1,
		"key_by":              "user",
		"user_claim":          "tenant_id",
	})
	req = rateLimitRequest("203.0.113.12:1111", "alice", map[string]interface{}{"tenant_id": "tenant-1"})
	if rec := hitRateLimitPlugin(custom, req); rec.Code != http.StatusOK {
		t.Fatalf("custom claim first request status = %d, want 200", rec.Code)
	}
	req = rateLimitRequest("203.0.113.12:2222", "alice", map[string]interface{}{"tenant_id": "tenant-1"})
	if rec := hitRateLimitPlugin(custom, req); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("custom claim second request status = %d, want 429", rec.Code)
	}
}

func TestRateLimitPlugin_UserKeyFallsBackToIPWithoutAuth(t *testing.T) {
	plugin := NewRateLimitPlugin(map[string]interface{}{
		"requests_per_minute": 1,
		"key_by":              "user",
	})

	if rec := hitRateLimitPlugin(plugin, rateLimitRequest("203.0.113.13:1111", "", nil)); rec.Code != http.StatusOK {
		t.Fatalf("first unauthenticated request status = %d, want 200", rec.Code)
	}
	if rec := hitRateLimitPlugin(plugin, rateLimitRequest("203.0.113.13:2222", "", nil)); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("second unauthenticated request status = %d, want 429", rec.Code)
	}
}

func TestRateLimitPlugin_TrustForwardedFor(t *testing.T) {
	plugin := NewRateLimitPlugin(map[string]interface{}{
		"requests_per_minute": 1,
		"trust_forwarded_for": true,
	})

	first := rateLimitRequest("10.0.0.5:1111", "", nil)
	first.Header.Set("X-Forwarded-For", "198.51.100.7")
	if rec := hitRateLimitPlugin(plugin, first); rec.Code != http.StatusOK {
		t.Fatalf("first forwarded IP request status = %d, want 200", rec.Code)
	}

	second := rateLimitRequest("10.0.0.5:2222", "", nil)
	second.Header.Set("X-Forwarded-For", "198.51.100.8")
	if rec := hitRateLimitPlugin(plugin, second); rec.Code != http.StatusOK {
		t.Fatalf("different forwarded IP status = %d, want 200", rec.Code)
	}

	third := rateLimitRequest("10.0.0.5:3333", "", nil)
	third.Header.Set("X-Forwarded-For", "198.51.100.7")
	if rec := hitRateLimitPlugin(plugin, third); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("repeated forwarded IP status = %d, want 429", rec.Code)
	}
}

func TestRateLimiter_FixedWindow(t *testing.T) {
	limiter := newRateLimiter(rateLimitConfig{
		algorithm: rateLimitFixedWindow,
		limit:     2,
		window:    10 * time.Second,
		keyBy:     rateLimitKeyByIP,
	})
	now := time.Unix(1000, 0)

	if !limiter.allow("client", now).allowed {
		t.Fatal("first request must be allowed")
	}
	if !limiter.allow("client", now).allowed {
		t.Fatal("second request must be allowed")
	}
	if limiter.allow("client", now).allowed {
		t.Fatal("third request must be blocked")
	}
	if !limiter.allow("client", now.Add(10*time.Second)).allowed {
		t.Fatal("request in the next window must be allowed")
	}
}

func TestRateLimiter_SlidingWindow(t *testing.T) {
	limiter := newRateLimiter(rateLimitConfig{
		algorithm: rateLimitSlidingWindow,
		limit:     2,
		window:    10 * time.Second,
		keyBy:     rateLimitKeyByIP,
	})
	now := time.Unix(1000, 0)

	if !limiter.allow("client", now).allowed {
		t.Fatal("first request must be allowed")
	}
	if !limiter.allow("client", now).allowed {
		t.Fatal("second request must be allowed")
	}
	if limiter.allow("client", now).allowed {
		t.Fatal("third request must be blocked")
	}
	if limiter.allow("client", now.Add(10*time.Second)).allowed {
		t.Fatal("request at the next window boundary must still be blocked by the weighted previous window")
	}
	if !limiter.allow("client", now.Add(15*time.Second)).allowed {
		t.Fatal("request at half of the next window must be allowed")
	}
	if limiter.allow("client", now.Add(15*time.Second)).allowed {
		t.Fatal("second request at half of the next window must be blocked")
	}
}

func TestRateLimiter_TokenBucket(t *testing.T) {
	limiter := newRateLimiter(rateLimitConfig{
		algorithm: rateLimitTokenBucket,
		limit:     2,
		window:    10 * time.Second,
		burst:     2,
		keyBy:     rateLimitKeyByIP,
	})
	now := time.Unix(1000, 0)

	if !limiter.allow("client", now).allowed {
		t.Fatal("first request must be allowed")
	}
	if !limiter.allow("client", now).allowed {
		t.Fatal("second request must be allowed")
	}
	if limiter.allow("client", now).allowed {
		t.Fatal("third request must be blocked")
	}
	if !limiter.allow("client", now.Add(5*time.Second)).allowed {
		t.Fatal("request after partial refill must be allowed")
	}
	if limiter.allow("client", now.Add(5*time.Second)).allowed {
		t.Fatal("second request after partial refill must be blocked")
	}
	if !limiter.allow("client", now.Add(10*time.Second)).allowed {
		t.Fatal("request after another refill interval must be allowed")
	}
}

func TestRateLimitPlugin_ConfigAcceptsIntAndFloat(t *testing.T) {
	for _, value := range []interface{}{2, 2.0, int64(2), "2"} {
		cfg := parseRateLimitConfig(map[string]interface{}{"requests_per_minute": value})
		if cfg.limit != 2 {
			t.Fatalf("requests_per_minute %T(%v) parsed limit = %d, want 2", value, value, cfg.limit)
		}
	}
}
