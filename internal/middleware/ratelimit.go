package middleware

import (
	"net/http"
	"sync"
	"time"

	"github.com/haiser1/go-api-gateway/internal/gateway"
)

func init() {
	gateway.RegisterPlugin("rate-limiting", func(config map[string]interface{}) gateway.PluginMiddleware {
		reqLimit, _ := config["requests_per_minute"].(float64)
		if reqLimit == 0 {
			reqLimit = 60
		}

		mu := sync.Mutex{}
		requests := make(map[string]int)
		resetTicker := time.NewTicker(time.Minute)

		// Reset counter setiap 1 menit
		go func() {
			for range resetTicker.C {
				mu.Lock()
				requests = make(map[string]int)
				mu.Unlock()
			}
		}()

		return gateway.PluginFunc{
			NameStr: "rate-limiting",
			Handler: func(w http.ResponseWriter, r *http.Request, next http.Handler) bool {
				ip := r.RemoteAddr
				mu.Lock()
				requests[ip]++
				count := requests[ip]
				mu.Unlock()

				if count > int(reqLimit) {
					http.Error(w, "Rate limit exceeded", http.StatusTooManyRequests)
					return false
				}

				next.ServeHTTP(w, r)
				return true
			},
		}
	})
}
