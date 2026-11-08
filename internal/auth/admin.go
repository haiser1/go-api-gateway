package auth

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/haiser1/go-api-gateway/internal/secrets"
)

// AdminAuthenticator validates Management API bearer tokens.
type AdminAuthenticator struct {
	expected [32]byte
}

// NewAdminAuthenticator builds an authenticator from a canonical SHA-256 hash
// or a legacy plaintext token.
func NewAdminAuthenticator(stored string) (*AdminAuthenticator, error) {
	if strings.TrimSpace(stored) == "" {
		return nil, errors.New("admin token is empty")
	}
	var expected [32]byte
	if secrets.IsHash(stored) {
		parsed, err := secrets.ParseHash(stored)
		if err != nil {
			return nil, err
		}
		expected = parsed
	} else {
		expected = sha256.Sum256([]byte(stored))
	}
	return &AdminAuthenticator{expected: expected}, nil
}

// Verify reports whether raw matches the configured admin token.
func (a *AdminAuthenticator) Verify(raw string) bool {
	if a == nil || raw == "" {
		return false
	}
	return secrets.HashMatchesDigest(raw, a.expected)
}

// AdminMiddleware protects the Management API. A nil authenticator fails
// closed with 503 so an unconfigured gateway never exposes admin endpoints.
func AdminMiddleware(authenticator *AdminAuthenticator) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if authenticator == nil {
				writeAdminError(w, http.StatusServiceUnavailable, "Management API is disabled: admin token is not configured")
				return
			}

			token, ok := adminToken(r)
			if !ok {
				writeAdminError(w, http.StatusUnauthorized, "Admin token required")
				return
			}
			if !authenticator.Verify(token) {
				writeAdminError(w, http.StatusUnauthorized, "Unauthorized: Invalid admin token")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func adminToken(r *http.Request) (string, bool) {
	if token := strings.TrimSpace(r.Header.Get("X-Admin-Token")); token != "" {
		return token, true
	}
	return bearerToken(r.Header.Get("Authorization"))
}

func writeAdminError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
}
