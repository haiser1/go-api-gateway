package middleware

import (
	"net/http"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/haiser1/go-api-gateway/internal/gateway"
)

// responseWriter is a wrapper around http.ResponseWriter to capture the status code
type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func newResponseWriter(w http.ResponseWriter) *responseWriter {
	return &responseWriter{w, http.StatusOK} // Default to 200
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

func init() {
	gateway.RegisterPlugin("logging", func(config map[string]interface{}) gateway.PluginMiddleware {
		// This plugin currently doesn't use any specific config, but the structure is here.
		return gateway.PluginFunc{
			NameStr: "logging",
			Handler: func(w http.ResponseWriter, r *http.Request, next http.Handler) bool {
				start := time.Now()

				// Wrap the original ResponseWriter to capture the status code
				rw := newResponseWriter(w)

				// Call the next handler in the chain
				next.ServeHTTP(rw, r)

				duration := time.Since(start)

				// Log the request details using Zerolog
				log.Info().
					Str("method", r.Method).
					Str("uri", r.RequestURI).
					Int("status", rw.statusCode).
					Dur("duration", duration).
					Str("protocol", r.Proto). // Added protocol field
					Msg("Request processed")

				return true // Always continue the chain (logging is passive)
			},
		}
	})
}
