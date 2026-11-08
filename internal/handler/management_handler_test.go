package handler_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/haiser1/go-api-gateway/internal/config"
	"github.com/haiser1/go-api-gateway/internal/domain"
	"github.com/haiser1/go-api-gateway/internal/gateway"
	"github.com/julienschmidt/httprouter"
)

func setupTestRouter(t *testing.T) (*httprouter.Router, *config.Manager, string) {
	tmpDir, err := os.MkdirTemp("", "handler-test")
	if err != nil {
		t.Fatal(err)
	}

	configPath := filepath.Join(tmpDir, "config.yaml")
	initialConfig := config.Config{
		Server: config.ServerConfig{ProxyPort: 8080, AdminPort: 8081},
		GlobalPlugins: []config.PluginConfig{
			{
				Name:    "logger",
				Enabled: true,
				Config:  map[string]interface{}{"format": "json"},
			},
		},
	}

	data, err := json.Marshal(initialConfig)
	if err != nil {
		t.Fatal(err)
	}

	err = os.WriteFile(configPath, data, 0644)
	if err != nil {
		t.Fatal(err)
	}

	manager, err := config.NewManager(tmpDir)
	if err != nil {
		t.Fatal(err)
	}

	router := httprouter.New()
	gateway.RegisterManagementRoutes(router, manager, nil)

	return router, manager, tmpDir
}

func TestCORSConfigHandlers(t *testing.T) {
	router, manager, tmpDir := setupTestRouter(t)
	defer os.RemoveAll(tmpDir)
	defer manager.Close()

	// 1. GET CORS config (should be 404 since it's not in global plugins initially)
	req := httptest.NewRequest("GET", "/api/config/cors", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Errorf("Expected GET /api/config/cors to return 404, got %d", rr.Code)
	}

	// 2. POST to create CORS config
	corsReq := domain.CORSConfig{
		Enabled:          true,
		AllowedOrigins:   []string{"http://test.com"},
		AllowedMethods:   []string{"GET", "POST"},
		AllowedHeaders:   []string{"X-Header"},
		ExposedHeaders:   []string{"X-Exposed"},
		AllowCredentials: true,
		MaxAge:           3600,
	}
	body, _ := json.Marshal(corsReq)
	req = httptest.NewRequest("POST", "/api/config/cors", bytes.NewBuffer(body))
	rr = httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusCreated {
		t.Errorf("Expected POST /api/config/cors to return 201, got %d. Body: %s", rr.Code, rr.Body.String())
	}

	// 3. GET CORS config again (should now return 200 with the saved configuration)
	req = httptest.NewRequest("GET", "/api/config/cors", nil)
	rr = httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected GET /api/config/cors to return 200, got %d", rr.Code)
	}

	var getResponse struct {
		Success bool              `json:"success"`
		Message string            `json:"message"`
		Data    domain.CORSConfig `json:"data"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &getResponse); err != nil {
		t.Fatal(err)
	}

	if !getResponse.Success || getResponse.Data.MaxAge != 3600 || getResponse.Data.AllowedOrigins[0] != "http://test.com" {
		t.Errorf("Unexpected CORS configuration retrieved: %+v", getResponse)
	}

	// 4. POST duplicate (should return 409 conflict)
	req = httptest.NewRequest("POST", "/api/config/cors", bytes.NewBuffer(body))
	rr = httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusConflict {
		t.Errorf("Expected duplicate POST to return 409, got %d", rr.Code)
	}

	// 5. PUT to update CORS config
	corsReq.MaxAge = 7200
	body, _ = json.Marshal(corsReq)
	req = httptest.NewRequest("PUT", "/api/config/cors", bytes.NewBuffer(body))
	rr = httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected PUT to return 200, got %d", rr.Code)
	}

	// Verify update via GET
	req = httptest.NewRequest("GET", "/api/config/cors", nil)
	rr = httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	_ = json.Unmarshal(rr.Body.Bytes(), &getResponse)
	if getResponse.Data.MaxAge != 7200 {
		t.Errorf("Expected updated max_age to be 7200, got %d", getResponse.Data.MaxAge)
	}

	// 6. DELETE CORS config
	req = httptest.NewRequest("DELETE", "/api/config/cors", nil)
	rr = httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Errorf("Expected DELETE to return 204, got %d", rr.Code)
	}

	// Verify DELETE via GET (should be 404 again)
	req = httptest.NewRequest("GET", "/api/config/cors", nil)
	rr = httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Errorf("Expected GET after delete to return 404, got %d", rr.Code)
	}
}

func TestServiceAndRouteCORSConfigHandlers(t *testing.T) {
	router, manager, tmpDir := setupTestRouter(t)
	defer os.RemoveAll(tmpDir)
	defer manager.Close()

	// Seed upstream, service, and route
	_ = manager.AddUpstream(config.Upstream{
		Id:        "up-1",
		Name:      "upstream-1",
		Algorithm: "round-robin",
		Targets:   []config.UpstreamTarget{{Host: "localhost", Port: 9090, Weight: 100}},
	})
	_ = manager.AddService(config.Service{
		Id:         "svc-1",
		Name:       "service-1",
		UpstreamId: "up-1",
		Protocol:   "http",
	})
	_ = manager.AddRoute(config.Route{
		Id:        "rt-1",
		Name:      "route-1",
		Methods:   []string{"GET"},
		Paths:     []string{"/api/v1/test"},
		ServiceId: "svc-1",
	})

	corsPayload := domain.CORSConfig{
		Enabled:          true,
		AllowedOrigins:   []string{"https://app.example.com"},
		AllowedMethods:   []string{"GET", "POST", "OPTIONS"},
		AllowedHeaders:   []string{"Authorization", "Content-Type"},
		AllowCredentials: true,
		MaxAge:           600,
	}
	body, _ := json.Marshal(corsPayload)

	// Service CORS CRUD
	req := httptest.NewRequest("POST", "/api/services/svc-1/cors", bytes.NewBuffer(body))
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201 for POST /api/services/svc-1/cors, got %d: %s", rr.Code, rr.Body.String())
	}

	req = httptest.NewRequest("GET", "/api/services/svc-1/cors", nil)
	rr = httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 for GET /api/services/svc-1/cors, got %d", rr.Code)
	}

	corsPayload.MaxAge = 1200
	updatedBody, _ := json.Marshal(corsPayload)
	req = httptest.NewRequest("PUT", "/api/services/svc-1/cors", bytes.NewBuffer(updatedBody))
	rr = httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 for PUT /api/services/svc-1/cors, got %d", rr.Code)
	}

	req = httptest.NewRequest("DELETE", "/api/services/svc-1/cors", nil)
	rr = httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("expected 204 for DELETE /api/services/svc-1/cors, got %d", rr.Code)
	}

	// Route CORS CRUD
	req = httptest.NewRequest("POST", "/api/routes/rt-1/cors", bytes.NewBuffer(body))
	rr = httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201 for POST /api/routes/rt-1/cors, got %d: %s", rr.Code, rr.Body.String())
	}

	req = httptest.NewRequest("GET", "/api/routes/rt-1/cors", nil)
	rr = httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 for GET /api/routes/rt-1/cors, got %d", rr.Code)
	}

	req = httptest.NewRequest("PUT", "/api/routes/rt-1/cors", bytes.NewBuffer(updatedBody))
	rr = httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 for PUT /api/routes/rt-1/cors, got %d", rr.Code)
	}

	req = httptest.NewRequest("DELETE", "/api/routes/rt-1/cors", nil)
	rr = httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("expected 204 for DELETE /api/routes/rt-1/cors, got %d", rr.Code)
	}
}

func TestIPRestrictionAndRequestTransformConfigHandlers(t *testing.T) {
	router, manager, tmpDir := setupTestRouter(t)
	defer os.RemoveAll(tmpDir)
	defer manager.Close()

	_ = manager.AddUpstream(config.Upstream{
		Id:   "up-1",
		Name: "upstream-1",
		Targets: []config.UpstreamTarget{
			{Host: "localhost", Port: 9090, Weight: 100},
		},
	})
	_ = manager.AddService(config.Service{
		Id:         "svc-1",
		Name:       "service-1",
		UpstreamId: "up-1",
	})
	_ = manager.AddRoute(config.Route{
		Id:        "rt-1",
		Name:      "route-1",
		Methods:   []string{"GET"},
		Paths:     []string{"/api/v1/items"},
		ServiceId: "svc-1",
	})

	// 1. Global, Service, and Route IP Restriction CRUD
	ipCfg := domain.IPRestrictionConfig{
		Enabled:           true,
		AllowedIPs:        []string{"10.0.0.0/8"},
		DeniedIPs:         []string{"10.0.0.99"},
		BlockedBots:       []string{"sqlmap"},
		TrustForwardedFor: true,
	}
	ipBody, _ := json.Marshal(ipCfg)

	for _, endpoint := range []string{
		"/api/config/ip-restriction",
		"/api/services/svc-1/ip-restriction",
		"/api/routes/rt-1/ip-restriction",
	} {
		req := httptest.NewRequest("POST", endpoint, bytes.NewBuffer(ipBody))
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)
		if rr.Code != http.StatusCreated {
			t.Fatalf("POST %s: expected 201, got %d: %s", endpoint, rr.Code, rr.Body.String())
		}

		req = httptest.NewRequest("GET", endpoint, nil)
		rr = httptest.NewRecorder()
		router.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("GET %s: expected 200, got %d", endpoint, rr.Code)
		}

		ipCfg.Message = "Custom blocked message"
		updBody, _ := json.Marshal(ipCfg)
		req = httptest.NewRequest("PUT", endpoint, bytes.NewBuffer(updBody))
		rr = httptest.NewRecorder()
		router.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("PUT %s: expected 200, got %d", endpoint, rr.Code)
		}

		req = httptest.NewRequest("DELETE", endpoint, nil)
		rr = httptest.NewRecorder()
		router.ServeHTTP(rr, req)
		if rr.Code != http.StatusNoContent {
			t.Fatalf("DELETE %s: expected 204, got %d", endpoint, rr.Code)
		}
	}

	// 2. Global, Service, and Route Request Transform CRUD
	rtCfg := domain.RequestTransformConfig{
		Enabled:               true,
		AddHeaders:            map[string]string{"X-Gateway": "true"},
		RemoveHeaders:         []string{"X-Internal"},
		AddQueryParams:        map[string]string{"v": "2"},
		AddResponseHeaders:    map[string]string{"X-Served-By": "gw"},
		RemoveResponseHeaders: []string{"X-Powered-By"},
	}
	rtBody, _ := json.Marshal(rtCfg)

	for _, endpoint := range []string{
		"/api/config/request-transform",
		"/api/services/svc-1/request-transform",
		"/api/routes/rt-1/request-transform",
	} {
		req := httptest.NewRequest("POST", endpoint, bytes.NewBuffer(rtBody))
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)
		if rr.Code != http.StatusCreated {
			t.Fatalf("POST %s: expected 201, got %d: %s", endpoint, rr.Code, rr.Body.String())
		}

		req = httptest.NewRequest("GET", endpoint, nil)
		rr = httptest.NewRecorder()
		router.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("GET %s: expected 200, got %d", endpoint, rr.Code)
		}

		req = httptest.NewRequest("PUT", endpoint, bytes.NewBuffer(rtBody))
		rr = httptest.NewRecorder()
		router.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("PUT %s: expected 200, got %d", endpoint, rr.Code)
		}

		req = httptest.NewRequest("DELETE", endpoint, nil)
		rr = httptest.NewRecorder()
		router.ServeHTTP(rr, req)
		if rr.Code != http.StatusNoContent {
			t.Fatalf("DELETE %s: expected 204, got %d", endpoint, rr.Code)
		}
	}
}

func TestStatusCircuitBreakerConfigAndMetricsResetHandlers(t *testing.T) {
	router, manager, tmpDir := setupTestRouter(t)
	defer os.RemoveAll(tmpDir)
	defer manager.Close()

	_ = manager.AddUpstream(config.Upstream{
		Id:      "up-cb",
		Name:    "upstream-cb",
		Targets: []config.UpstreamTarget{{Host: "localhost", Port: 9090, Weight: 100}},
	})
	_ = manager.AddService(config.Service{
		Id:         "svc-cb",
		Name:       "service-cb",
		UpstreamId: "up-cb",
	})

	// Trip the circuit breaker for svc-cb (5 failures -> OPEN)
	cb := gateway.GetCircuitBreaker("svc-cb")
	for i := 0; i < 5; i++ {
		cb.RecordFailure()
	}
	if cb.GetState() != "OPEN" {
		t.Fatalf("expected circuit breaker to be OPEN, got %s", cb.GetState())
	}

	// 1. Check GET /api/services/svc-cb/circuit-breaker
	req := httptest.NewRequest("GET", "/api/services/svc-cb/circuit-breaker", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 from GET circuit-breaker, got %d", rr.Code)
	}
	var cbResp struct {
		Success bool                               `json:"success"`
		Data    domain.ServiceCircuitBreakerStatus `json:"data"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &cbResp)
	if cbResp.Data.State != "OPEN" || cbResp.Data.FailureCount != 5 {
		t.Errorf("expected state=OPEN and failures=5, got %+v", cbResp.Data)
	}

	// 2. Reset via POST /api/services/svc-cb/circuit-breaker/reset
	req = httptest.NewRequest("POST", "/api/services/svc-cb/circuit-breaker/reset", nil)
	rr = httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 from POST circuit-breaker/reset, got %d", rr.Code)
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &cbResp)
	if cbResp.Data.State != "CLOSED" || cbResp.Data.FailureCount != 0 {
		t.Errorf("expected state=CLOSED and failures=0 after reset, got %+v", cbResp.Data)
	}

	// 3. Check GET /api/status
	req = httptest.NewRequest("GET", "/api/status", nil)
	rr = httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 from GET /api/status, got %d", rr.Code)
	}
	var statusResp struct {
		Success bool                         `json:"success"`
		Data    domain.GatewayStatusResponse `json:"data"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &statusResp)
	if statusResp.Data.Status != "healthy" || statusResp.Data.ServicesCount != 1 {
		t.Errorf("unexpected status response: %+v", statusResp.Data)
	}

	// 4. Check GET /api/config
	req = httptest.NewRequest("GET", "/api/config", nil)
	rr = httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 from GET /api/config, got %d", rr.Code)
	}

	// 5. Check POST /api/metrics/reset
	gateway.DefaultMetrics.Record("GET", "/test", 200, 0)
	if gateway.DefaultMetrics.Snapshot().TotalRequests == 0 {
		t.Fatal("expected non-zero metrics before reset")
	}
	req = httptest.NewRequest("POST", "/api/metrics/reset", nil)
	rr = httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 from POST /api/metrics/reset, got %d", rr.Code)
	}
	if gateway.DefaultMetrics.Snapshot().TotalRequests != 0 {
		t.Errorf("expected 0 total requests after reset, got %d", gateway.DefaultMetrics.Snapshot().TotalRequests)
	}
}
