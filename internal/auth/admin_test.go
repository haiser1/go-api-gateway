package auth

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/haiser1/go-api-gateway/internal/secrets"
)

func TestAdminAuthenticator(t *testing.T) {
	for name, stored := range map[string]string{
		"plaintext": "admin-token",
		"hash":      secrets.HashSecret("admin-token"),
	} {
		authenticator, err := NewAdminAuthenticator(stored)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if !authenticator.Verify("admin-token") || authenticator.Verify("wrong") {
			t.Fatalf("%s: verification failed", name)
		}
	}
	if _, err := NewAdminAuthenticator(""); err == nil {
		t.Fatal("empty admin token must be rejected")
	}
	if _, err := NewAdminAuthenticator("sha256:bad"); err == nil {
		t.Fatal("malformed admin hash must be rejected")
	}
}

func TestAdminMiddleware(t *testing.T) {
	authenticator, err := NewAdminAuthenticator(secrets.HashSecret("admin-token"))
	if err != nil {
		t.Fatal(err)
	}
	handler := AdminMiddleware(authenticator)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	cases := []struct {
		name    string
		header  string
		value   string
		status  int
		message string
	}{
		{"missing", "", "", http.StatusUnauthorized, "Admin token required"},
		{"wrong bearer", "Authorization", "Bearer wrong", http.StatusUnauthorized, "Invalid admin token"},
		{"wrong header", "X-Admin-Token", "wrong", http.StatusUnauthorized, "Invalid admin token"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
			if tc.header != "" {
				req.Header.Set(tc.header, tc.value)
			}
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != tc.status || !strings.Contains(rec.Body.String(), tc.message) {
				t.Fatalf("status = %d body = %q", rec.Code, rec.Body.String())
			}
		})
	}

	for name, header := range map[string]string{
		"bearer":       "Bearer admin-token",
		"admin header": "admin-token",
	} {
		req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
		if name == "bearer" {
			req.Header.Set("Authorization", header)
		} else {
			req.Header.Set("X-Admin-Token", header)
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusNoContent {
			t.Fatalf("%s: status = %d body = %q", name, rec.Code, rec.Body.String())
		}
	}
}

func TestAdminMiddlewareFailsClosedWhenUnconfigured(t *testing.T) {
	handler := AdminMiddleware(nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler must not run")
	}))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/status", nil))
	if rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), "admin token is not configured") {
		t.Fatalf("status = %d body = %q", rec.Code, rec.Body.String())
	}
}
