package gateway

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Standard latency histogram upper bounds in milliseconds.
var latencyBoundsMs = [9]int64{1, 5, 10, 25, 50, 100, 250, 500, 1000}

// routeMetricEntry holds lock-free atomic counters for a (method, path) pair.
type routeMetricEntry struct {
	method      string
	path        string
	total       atomic.Int64
	status2xx   atomic.Int64
	status3xx   atomic.Int64
	status4xx   atomic.Int64
	status5xx   atomic.Int64
	durationNs  atomic.Int64
	histBuckets [10]atomic.Int64 // 0..8 correspond to latencyBoundsMs, 9 is +Inf
}

// MetricsCollector stores in-memory gateway metrics using atomic counters.
type MetricsCollector struct {
	totalRequests  atomic.Int64
	activeRequests atomic.Int64
	status2xx      atomic.Int64
	status3xx      atomic.Int64
	status4xx      atomic.Int64
	status5xx      atomic.Int64
	totalDuration  atomic.Int64 // nanoseconds

	routes sync.Map // string ("METHOD:PATH") -> *routeMetricEntry
}

// DefaultMetrics is the shared process-wide in-memory metrics collector.
var DefaultMetrics = NewMetricsCollector()

// NewMetricsCollector creates a new in-memory MetricsCollector.
func NewMetricsCollector() *MetricsCollector {
	return &MetricsCollector{}
}

// IncActive increments the in-flight request gauge.
func (mc *MetricsCollector) IncActive() {
	mc.activeRequests.Add(1)
}

// DecActive decrements the in-flight request gauge.
func (mc *MetricsCollector) DecActive() {
	mc.activeRequests.Add(-1)
}

// Record records a completed HTTP request.
func (mc *MetricsCollector) Record(method, path string, statusCode int, duration time.Duration) {
	ns := duration.Nanoseconds()
	ms := duration.Milliseconds()

	mc.totalRequests.Add(1)
	mc.totalDuration.Add(ns)

	switch {
	case statusCode >= 200 && statusCode < 300:
		mc.status2xx.Add(1)
	case statusCode >= 300 && statusCode < 400:
		mc.status3xx.Add(1)
	case statusCode >= 400 && statusCode < 500:
		mc.status4xx.Add(1)
	case statusCode >= 500:
		mc.status5xx.Add(1)
	}

	key := method + ":" + path
	val, ok := mc.routes.Load(key)
	if !ok {
		actual, _ := mc.routes.LoadOrStore(key, &routeMetricEntry{
			method: method,
			path:   path,
		})
		val = actual
	}
	entry := val.(*routeMetricEntry)

	entry.total.Add(1)
	entry.durationNs.Add(ns)

	switch {
	case statusCode >= 200 && statusCode < 300:
		entry.status2xx.Add(1)
	case statusCode >= 300 && statusCode < 400:
		entry.status3xx.Add(1)
	case statusCode >= 400 && statusCode < 500:
		entry.status4xx.Add(1)
	case statusCode >= 500:
		entry.status5xx.Add(1)
	}

	// Cumulative histogram buckets: increment all buckets whose bound >= ms
	for i, bound := range latencyBoundsMs {
		if ms <= bound {
			entry.histBuckets[i].Add(1)
		}
	}
	entry.histBuckets[9].Add(1) // +Inf always incremented
}

// Reset clears all recorded metrics (useful for tests).
func (mc *MetricsCollector) Reset() {
	mc.totalRequests.Store(0)
	mc.activeRequests.Store(0)
	mc.status2xx.Store(0)
	mc.status3xx.Store(0)
	mc.status4xx.Store(0)
	mc.status5xx.Store(0)
	mc.totalDuration.Store(0)
	mc.routes.Range(func(key, _ interface{}) bool {
		mc.routes.Delete(key)
		return true
	})
}

// RouteMetricsSnapshot represents a JSON-serializable snapshot of a route's metrics.
type RouteMetricsSnapshot struct {
	Method        string  `json:"method"`
	Path          string  `json:"path"`
	TotalRequests int64   `json:"total_requests"`
	Status2xx     int64   `json:"status_2xx"`
	Status3xx     int64   `json:"status_3xx"`
	Status4xx     int64   `json:"status_4xx"`
	Status5xx     int64   `json:"status_5xx"`
	AvgLatencyMs  float64 `json:"avg_latency_ms"`
}

// MetricsSnapshot represents a JSON-serializable snapshot of all gateway metrics.
type MetricsSnapshot struct {
	TotalRequests  int64                  `json:"total_requests"`
	ActiveRequests int64                  `json:"active_requests"`
	Status2xx      int64                  `json:"status_2xx"`
	Status3xx      int64                  `json:"status_3xx"`
	Status4xx      int64                  `json:"status_4xx"`
	Status5xx      int64                  `json:"status_5xx"`
	AvgLatencyMs   float64                `json:"avg_latency_ms"`
	Routes         []RouteMetricsSnapshot `json:"routes"`
}

// Snapshot returns a point-in-time copy of the collected metrics.
func (mc *MetricsCollector) Snapshot() MetricsSnapshot {
	total := mc.totalRequests.Load()
	totalDurNs := mc.totalDuration.Load()
	var avgMs float64
	if total > 0 {
		avgMs = float64(totalDurNs) / float64(total) / 1e6
	}

	var routes []RouteMetricsSnapshot
	mc.routes.Range(func(_, value interface{}) bool {
		e := value.(*routeMetricEntry)
		rTotal := e.total.Load()
		rDurNs := e.durationNs.Load()
		var rAvgMs float64
		if rTotal > 0 {
			rAvgMs = float64(rDurNs) / float64(rTotal) / 1e6
		}
		routes = append(routes, RouteMetricsSnapshot{
			Method:        e.method,
			Path:          e.path,
			TotalRequests: rTotal,
			Status2xx:     e.status2xx.Load(),
			Status3xx:     e.status3xx.Load(),
			Status4xx:     e.status4xx.Load(),
			Status5xx:     e.status5xx.Load(),
			AvgLatencyMs:  rAvgMs,
		})
		return true
	})

	sort.Slice(routes, func(i, j int) bool {
		if routes[i].Path != routes[j].Path {
			return routes[i].Path < routes[j].Path
		}
		return routes[i].Method < routes[j].Method
	})

	return MetricsSnapshot{
		TotalRequests:  total,
		ActiveRequests: mc.activeRequests.Load(),
		Status2xx:      mc.status2xx.Load(),
		Status3xx:      mc.status3xx.Load(),
		Status4xx:      mc.status4xx.Load(),
		Status5xx:      mc.status5xx.Load(),
		AvgLatencyMs:   avgMs,
		Routes:         routes,
	}
}

// Handler returns an http.Handler that exposes metrics in Prometheus text format
// (or JSON if ?format=json or Accept: application/json is provided).
func (mc *MetricsCollector) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("format") == "json" || strings.Contains(r.Header.Get("Accept"), "application/json") {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(mc.Snapshot())
			return
		}

		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		w.WriteHeader(http.StatusOK)

		var sb strings.Builder
		sb.WriteString("# HELP gateway_requests_total Total number of HTTP requests processed by the gateway.\n")
		sb.WriteString("# TYPE gateway_requests_total counter\n")
		fmt.Fprintf(&sb, "gateway_requests_total %d\n", mc.totalRequests.Load())

		sb.WriteString("# HELP gateway_active_requests Current number of in-flight HTTP requests.\n")
		sb.WriteString("# TYPE gateway_active_requests gauge\n")
		fmt.Fprintf(&sb, "gateway_active_requests %d\n", mc.activeRequests.Load())

		sb.WriteString("# HELP gateway_responses_by_status_class_total Total HTTP responses partitioned by status class.\n")
		sb.WriteString("# TYPE gateway_responses_by_status_class_total counter\n")
		fmt.Fprintf(&sb, "gateway_responses_by_status_class_total{class=\"2xx\"} %d\n", mc.status2xx.Load())
		fmt.Fprintf(&sb, "gateway_responses_by_status_class_total{class=\"3xx\"} %d\n", mc.status3xx.Load())
		fmt.Fprintf(&sb, "gateway_responses_by_status_class_total{class=\"4xx\"} %d\n", mc.status4xx.Load())
		fmt.Fprintf(&sb, "gateway_responses_by_status_class_total{class=\"5xx\"} %d\n", mc.status5xx.Load())

		var entries []*routeMetricEntry
		mc.routes.Range(func(_, value interface{}) bool {
			entries = append(entries, value.(*routeMetricEntry))
			return true
		})
		sort.Slice(entries, func(i, j int) bool {
			if entries[i].path != entries[j].path {
				return entries[i].path < entries[j].path
			}
			return entries[i].method < entries[j].method
		})

		if len(entries) > 0 {
			sb.WriteString("# HELP gateway_route_requests_total Total HTTP requests per route and method.\n")
			sb.WriteString("# TYPE gateway_route_requests_total counter\n")
			for _, e := range entries {
				fmt.Fprintf(&sb, "gateway_route_requests_total{method=%q,path=%q} %d\n",
					e.method, e.path, e.total.Load())
			}

			sb.WriteString("# HELP gateway_request_duration_ms_bucket Request latency histogram in milliseconds.\n")
			sb.WriteString("# TYPE gateway_request_duration_ms_bucket histogram\n")
			for _, e := range entries {
				for i, bound := range latencyBoundsMs {
					fmt.Fprintf(&sb, "gateway_request_duration_ms_bucket{method=%q,path=%q,le=\"%d\"} %d\n",
						e.method, e.path, bound, e.histBuckets[i].Load())
				}
				fmt.Fprintf(&sb, "gateway_request_duration_ms_bucket{method=%q,path=%q,le=\"+Inf\"} %d\n",
					e.method, e.path, e.histBuckets[9].Load())
				fmt.Fprintf(&sb, "gateway_request_duration_ms_sum{method=%q,path=%q} %.3f\n",
					e.method, e.path, float64(e.durationNs.Load())/1e6)
				fmt.Fprintf(&sb, "gateway_request_duration_ms_count{method=%q,path=%q} %d\n",
					e.method, e.path, e.total.Load())
			}
		}

		_, _ = w.Write([]byte(sb.String()))
	})
}
