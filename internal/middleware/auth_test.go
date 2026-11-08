package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLegacyAuthPluginFailsClosed(t *testing.T) {
	plugin := NewAuthPlugin(map[string]interface{}{"jwt_key": "ignored"})
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	if continued := plugin.Execute(rec, req, next); continued {
		t.Fatal("legacy auth shim must halt the chain")
	}
	if called {
		t.Fatal("legacy auth shim must not call next")
	}
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
}
