package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// BenchmarkMiddlewareChain measures the per-request overhead of a realistic
// plugin chain (cors → rate-limiting → request-transform) around a terminal
// handler — no upstream network noise. Wrapping follows the same nesting the
// gateway uses for compiled chains.
func BenchmarkMiddlewareChain(b *testing.B) {
	noopConfig := map[string]interface{}{}
	limited := NewRateLimitPlugin(map[string]interface{}{"requests_per_minute": float64(1 << 30)})
	transform := NewRequestTransformPlugin(noopConfig)
	cors := NewCORSPlugin(noopConfig)

	terminal := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})
	throughTransform := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if transform.Execute(w, r, terminal) {
			return
		}
	})
	throughLimit := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if limited.Execute(w, r, throughTransform) {
			return
		}
	})
	chain := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if cors.Execute(w, r, throughLimit) {
			return
		}
	})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest(http.MethodGet, "/bench", nil)
		rec := httptest.NewRecorder()
		chain.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			b.Fatalf("chain returned %d", rec.Code)
		}
	}
}
