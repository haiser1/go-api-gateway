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
	gateway.RegisterManagementRoutes(router, manager)

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
