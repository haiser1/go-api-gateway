package middleware

import (
	"net/http"
	"sync"
	"time"

	"github.com/haiser1/go-api-gateway/internal/gateway"
)

func init() {
	gateway.RegisterPlugin("rate-limiting", func(config map[string]interface{}) gateway.PluginMiddleware {
		reqLimitFloat, _ := config["requests_per_minute"].(float64) // JSON angka biasanya float64
		reqLimit := int(reqLimitFloat)
		if reqLimit <= 0 {
			reqLimit = 60
		}

		var mu sync.Mutex
		requests := make(map[string]int)

		// Goroutine pembersih
		go func() {
			ticker := time.NewTicker(1 * time.Minute)
			for range ticker.C {
				mu.Lock()
				// Reset map sepenuhnya untuk mencegah memory leak
				// (Garbage collector akan membersihkan map lama)
				requests = make(map[string]int)
				mu.Unlock()
			}
		}()

		return gateway.PluginFunc{
			NameStr: "rate-limiting",
			Handler: func(w http.ResponseWriter, r *http.Request, next http.Handler) bool {
				ip := r.RemoteAddr
				// Note: Di production sebaiknya pakai X-Forwarded-For jika di belakang LB,
				// tapi r.RemoteAddr cukup untuk skripsi docker network.

				mu.Lock()
				requests[ip]++
				count := requests[ip]
				mu.Unlock()

				if count > reqLimit {
					http.Error(w, `{"error": "Rate limit exceeded"}`, http.StatusTooManyRequests)
					return false
				}

				next.ServeHTTP(w, r)
				return true
			},
		}
	})
}
