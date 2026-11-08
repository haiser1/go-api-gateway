package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/haiser1/go-api-gateway/internal/gateway"
)

// Phase 7: plugin-generated client-facing errors must speak the same JSON
// error contract as the proxy ({\"error\": ...} + Content-Type application/json).

func chainThrough(plugin gateway.PluginMiddleware, body string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !plugin.Execute(w, r, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(body))
		})) {
			return
		}
	})
}

func TestRateLimitPlugin_JSONContentTypeOn429(t *testing.T) {
	plugin := NewRateLimitPlugin(map[string]interface{}{"requests_per_minute": float64(1)})

	hit := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
		req.RemoteAddr = "203.0.113.9:1111"
		rec := httptest.NewRecorder()
		chainThrough(plugin, "upstream").ServeHTTP(rec, req)
		return rec
	}

	if rec := hit(); rec.Code != http.StatusOK {
		t.Fatalf("first request must pass, got %d", rec.Code)
	}

	rec := hit()
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("second request must be limited, got %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", ct)
	}
	if got := rec.Body.String(); got != `{"error":"Rate limit exceeded"}` && got != `{"error":"Rate limit exceeded"}`+"\n" {
		t.Fatalf("body = %q, want {\"error\":\"Rate limit exceeded\"}", got)
	}
}
