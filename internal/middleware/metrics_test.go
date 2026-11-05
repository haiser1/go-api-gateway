package middleware

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/haiser1/go-api-gateway/internal/gateway"
)

func TestMetricsPluginInMemoryRecording(t *testing.T) {
	collector := gateway.NewMetricsCollector()
	plugin := NewMetricsPluginWithCollector(map[string]interface{}{}, collector)

	// 1. 200 OK request
	req1 := httptest.NewRequest(http.MethodGet, "/api/v1/users", nil)
	rr1 := httptest.NewRecorder()
	plugin.Execute(rr1, req1, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// 2. 404 Not Found request
	req2 := httptest.NewRequest(http.MethodGet, "/api/v1/users", nil)
	rr2 := httptest.NewRecorder()
	plugin.Execute(rr2, req2, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))

	// 3. 503 Service Unavailable request on another route
	req3 := httptest.NewRequest(http.MethodPost, "/api/v1/orders", nil)
	rr3 := httptest.NewRecorder()
	plugin.Execute(rr3, req3, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))

	snap := collector.Snapshot()
	if snap.TotalRequests != 3 {
		t.Fatalf("expected 3 total requests, got %d", snap.TotalRequests)
	}
	if snap.ActiveRequests != 0 {
		t.Errorf("expected 0 active requests after completion, got %d", snap.ActiveRequests)
	}
	if snap.Status2xx != 1 || snap.Status4xx != 1 || snap.Status5xx != 1 {
		t.Errorf("unexpected status counts: 2xx=%d, 4xx=%d, 5xx=%d", snap.Status2xx, snap.Status4xx, snap.Status5xx)
	}
	if len(snap.Routes) != 2 {
		t.Fatalf("expected 2 route entries, got %d", len(snap.Routes))
	}
}

func TestMetricsExpositionFormats(t *testing.T) {
	collector := gateway.NewMetricsCollector()
	plugin := NewMetricsPluginWithCollector(map[string]interface{}{}, collector)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	rr := httptest.NewRecorder()
	plugin.Execute(rr, req, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	handler := collector.Handler()

	// Prometheus text format
	promReq := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	promRR := httptest.NewRecorder()
	handler.ServeHTTP(promRR, promReq)

	if promRR.Code != http.StatusOK {
		t.Fatalf("expected 200 from /metrics, got %d", promRR.Code)
	}
	body := promRR.Body.String()
	if !strings.Contains(body, "gateway_requests_total 1") {
		t.Errorf("expected Prometheus output to contain 'gateway_requests_total 1', got:\n%s", body)
	}
	if !strings.Contains(body, `gateway_route_requests_total{method="GET",path="/api/v1/health"} 1`) {
		t.Errorf("expected Prometheus output to contain route counter, got:\n%s", body)
	}

	// JSON format via ?format=json
	jsonReq := httptest.NewRequest(http.MethodGet, "/metrics?format=json", nil)
	jsonRR := httptest.NewRecorder()
	handler.ServeHTTP(jsonRR, jsonReq)

	if jsonRR.Code != http.StatusOK {
		t.Fatalf("expected 200 from /metrics?format=json, got %d", jsonRR.Code)
	}
	var snap gateway.MetricsSnapshot
	if err := json.Unmarshal(jsonRR.Body.Bytes(), &snap); err != nil {
		t.Fatalf("failed to unmarshal JSON metrics snapshot: %v", err)
	}
	if snap.TotalRequests != 1 {
		t.Errorf("expected TotalRequests=1 in JSON snapshot, got %d", snap.TotalRequests)
	}
}
