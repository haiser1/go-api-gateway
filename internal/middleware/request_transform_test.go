package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRequestTransformHeadersAndQuery(t *testing.T) {
	cfg := map[string]interface{}{
		"add_headers": map[string]interface{}{
			"X-Gateway-Source": "go-api-gateway",
		},
		"set_headers": map[string]interface{}{
			"X-Overwrite": "new-value",
		},
		"remove_headers": []interface{}{
			"X-Secret-Internal",
		},
		"add_query_params": map[string]interface{}{
			"version": "2",
		},
		"remove_query_params": []interface{}{
			"debug",
		},
	}

	plugin := NewRequestTransformPlugin(cfg)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/items?debug=true&keep=1", nil)
	req.Header.Set("X-Overwrite", "old-value")
	req.Header.Set("X-Secret-Internal", "top-secret")
	rr := httptest.NewRecorder()

	var capturedReq *http.Request
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedReq = r
		w.WriteHeader(http.StatusOK)
	})

	continued := plugin.Execute(rr, req, next)
	if !continued {
		t.Fatal("expected request-transform plugin to continue")
	}
	if capturedReq == nil {
		t.Fatal("expected next handler to be called")
	}

	if got := capturedReq.Header.Get("X-Gateway-Source"); got != "go-api-gateway" {
		t.Errorf("expected X-Gateway-Source 'go-api-gateway', got '%s'", got)
	}
	if got := capturedReq.Header.Get("X-Overwrite"); got != "new-value" {
		t.Errorf("expected X-Overwrite 'new-value', got '%s'", got)
	}
	if got := capturedReq.Header.Get("X-Secret-Internal"); got != "" {
		t.Errorf("expected X-Secret-Internal to be removed, got '%s'", got)
	}

	q := capturedReq.URL.Query()
	if got := q.Get("version"); got != "2" {
		t.Errorf("expected query param version='2', got '%s'", got)
	}
	if got := q.Get("debug"); got != "" {
		t.Errorf("expected query param debug to be removed, got '%s'", got)
	}
	if got := q.Get("keep"); got != "1" {
		t.Errorf("expected query param keep='1', got '%s'", got)
	}
}

func TestRequestTransformResponseHeaders(t *testing.T) {
	cfg := map[string]interface{}{
		"add_response_headers": map[string]string{
			"X-Proxy-By": "go-api-gateway",
		},
		"set_response_headers": map[string]string{
			"X-Frame-Options": "DENY",
		},
		"remove_response_headers": []string{
			"X-Powered-By",
		},
	}

	plugin := NewRequestTransformPlugin(cfg)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/items", nil)
	rr := httptest.NewRecorder()

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Powered-By", "Express")
		w.Header().Set("X-Frame-Options", "SAMEORIGIN")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"ok":true}`))
	})

	continued := plugin.Execute(rr, req, next)
	if !continued {
		t.Fatal("expected request-transform plugin to continue")
	}
	if rr.Code != http.StatusCreated {
		t.Errorf("expected status 201, got %d", rr.Code)
	}
	if got := rr.Header().Get("X-Proxy-By"); got != "go-api-gateway" {
		t.Errorf("expected X-Proxy-By 'go-api-gateway', got '%s'", got)
	}
	if got := rr.Header().Get("X-Frame-Options"); got != "DENY" {
		t.Errorf("expected X-Frame-Options 'DENY', got '%s'", got)
	}
	if got := rr.Header().Get("X-Powered-By"); got != "" {
		t.Errorf("expected X-Powered-By to be removed, got '%s'", got)
	}
}
