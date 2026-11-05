package middleware

import (
	"net/http"
	"time"

	"github.com/haiser1/go-api-gateway/internal/gateway"
)

// metricsResponseWriter captures the HTTP status code written by downstream handlers.
type metricsResponseWriter struct {
	http.ResponseWriter
	statusCode  int
	wroteHeader bool
}

func (rw *metricsResponseWriter) WriteHeader(code int) {
	if !rw.wroteHeader {
		rw.statusCode = code
		rw.wroteHeader = true
	}
	rw.ResponseWriter.WriteHeader(code)
}

func (rw *metricsResponseWriter) Write(b []byte) (int, error) {
	if !rw.wroteHeader {
		rw.statusCode = http.StatusOK
		rw.wroteHeader = true
	}
	return rw.ResponseWriter.Write(b)
}

// NewMetricsPlugin creates an in-memory metrics middleware plugin using gateway.DefaultMetrics.
func NewMetricsPlugin(config map[string]interface{}) gateway.PluginMiddleware {
	return NewMetricsPluginWithCollector(config, gateway.DefaultMetrics)
}

// NewMetricsPluginWithCollector creates a metrics middleware plugin bound to a specific collector.
//
// Supported config keys:
//   - record_query (bool): Whether to include the query string in the recorded path label (default: false)
func NewMetricsPluginWithCollector(config map[string]interface{}, collector *gateway.MetricsCollector) gateway.PluginMiddleware {
	if collector == nil {
		collector = gateway.DefaultMetrics
	}

	recordQuery := false
	if v, ok := config["record_query"].(bool); ok {
		recordQuery = v
	}

	return gateway.PluginFunc{
		NameStr: "metrics",
		Handler: func(w http.ResponseWriter, r *http.Request, next http.Handler) bool {
			start := time.Now()
			collector.IncActive()
			defer collector.DecActive()

			mrw := &metricsResponseWriter{
				ResponseWriter: w,
				statusCode:     http.StatusOK,
			}

			next.ServeHTTP(mrw, r)

			path := r.URL.Path
			if recordQuery && r.URL.RawQuery != "" {
				path = path + "?" + r.URL.RawQuery
			}

			collector.Record(r.Method, path, mrw.statusCode, time.Since(start))
			return true
		},
	}
}

func init() {
	gateway.RegisterPlugin("metrics", NewMetricsPlugin)
	gateway.RegisterPlugin("prometheus", NewMetricsPlugin)
}
