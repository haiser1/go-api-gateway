package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSimpleCORSAllowedOrigin(t *testing.T) {
	config := map[string]interface{}{
		"allowed_origins": []string{"http://example.com"},
	}
	cors := NewCORSPlugin(config)

	req := httptest.NewRequest("GET", "/api/v1/resource", nil)
	req.Header.Set("Origin", "http://example.com")
	rr := httptest.NewRecorder()

	nextCalled := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	continued := cors.Execute(rr, req, next)

	if !continued {
		t.Error("Expected execution to continue to the next handler")
	}
	if !nextCalled {
		t.Error("Expected next handler to be called")
	}
	if rr.Header().Get("Access-Control-Allow-Origin") != "http://example.com" {
		t.Errorf("Expected Access-Control-Allow-Origin to be 'http://example.com', got '%s'", rr.Header().Get("Access-Control-Allow-Origin"))
	}
}

func TestSimpleCORSBlockedOrigin(t *testing.T) {
	config := map[string]interface{}{
		"allowed_origins": []string{"http://example.com"},
	}
	cors := NewCORSPlugin(config)

	req := httptest.NewRequest("GET", "/api/v1/resource", nil)
	req.Header.Set("Origin", "http://malicious.com")
	rr := httptest.NewRecorder()

	nextCalled := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusOK)
	})

	continued := cors.Execute(rr, req, next)

	if !continued {
		t.Error("Expected execution to continue even with blocked origin (browsers block based on lack of header)")
	}
	if !nextCalled {
		t.Error("Expected next handler to be called")
	}
	if rr.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Errorf("Expected Access-Control-Allow-Origin to be empty, got '%s'", rr.Header().Get("Access-Control-Allow-Origin"))
	}
}

func TestPreflightCORSRequest(t *testing.T) {
	config := map[string]interface{}{
		"allowed_origins":   []string{"http://example.com"},
		"allowed_methods":   []string{"GET", "POST"},
		"allowed_headers":   []string{"X-Custom-Header"},
		"allow_credentials": true,
		"max_age":           3600,
	}
	cors := NewCORSPlugin(config)

	req := httptest.NewRequest("OPTIONS", "/api/v1/resource", nil)
	req.Header.Set("Origin", "http://example.com")
	req.Header.Set("Access-Control-Request-Method", "POST")
	req.Header.Set("Access-Control-Request-Headers", "X-Custom-Header")
	rr := httptest.NewRecorder()

	nextCalled := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
	})

	continued := cors.Execute(rr, req, next)

	if continued {
		t.Error("Expected execution to halt on preflight OPTIONS request")
	}
	if nextCalled {
		t.Error("Expected next handler NOT to be called")
	}
	if rr.Code != http.StatusNoContent {
		t.Errorf("Expected status code 204, got %d", rr.Code)
	}
	if rr.Header().Get("Access-Control-Allow-Origin") != "http://example.com" {
		t.Errorf("Expected Access-Control-Allow-Origin to be 'http://example.com', got '%s'", rr.Header().Get("Access-Control-Allow-Origin"))
	}
	if rr.Header().Get("Access-Control-Allow-Methods") != "GET, POST" {
		t.Errorf("Expected Access-Control-Allow-Methods to be 'GET, POST', got '%s'", rr.Header().Get("Access-Control-Allow-Methods"))
	}
	if rr.Header().Get("Access-Control-Allow-Headers") != "X-Custom-Header" {
		t.Errorf("Expected Access-Control-Allow-Headers to be 'X-Custom-Header', got '%s'", rr.Header().Get("Access-Control-Allow-Headers"))
	}
	if rr.Header().Get("Access-Control-Allow-Credentials") != "true" {
		t.Errorf("Expected Access-Control-Allow-Credentials to be 'true', got '%s'", rr.Header().Get("Access-Control-Allow-Credentials"))
	}
	if rr.Header().Get("Access-Control-Max-Age") != "3600" {
		t.Errorf("Expected Access-Control-Max-Age to be '3600', got '%s'", rr.Header().Get("Access-Control-Max-Age"))
	}
}

func TestAllowCredentialsWildcardOrigin(t *testing.T) {
	config := map[string]interface{}{
		"allowed_origins":   []string{"*"},
		"allow_credentials": true,
	}
	cors := NewCORSPlugin(config)

	req := httptest.NewRequest("GET", "/api/v1/resource", nil)
	req.Header.Set("Origin", "http://example.com")
	rr := httptest.NewRecorder()

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	cors.Execute(rr, req, next)

	// Since allow_credentials is true, '*' must be replaced with the request's origin
	if rr.Header().Get("Access-Control-Allow-Origin") != "http://example.com" {
		t.Errorf("Expected Access-Control-Allow-Origin to be 'http://example.com', got '%s'", rr.Header().Get("Access-Control-Allow-Origin"))
	}
	if rr.Header().Get("Access-Control-Allow-Credentials") != "true" {
		t.Errorf("Expected Access-Control-Allow-Credentials to be 'true', got '%s'", rr.Header().Get("Access-Control-Allow-Credentials"))
	}
}
