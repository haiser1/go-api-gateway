package auth

import (
	"crypto/sha256"
	"fmt"
	"strings"

	"github.com/haiser1/go-api-gateway/internal/secrets"
)

// APIKeyVerifier validates API keys against a stored SHA-256 hash. Legacy
// plaintext values are hashed once during construction.
type APIKeyVerifier struct {
	header   string
	expected [32]byte
}

// NewAPIKeyVerifier builds a verifier from a canonical hash or legacy plaintext
// API key. The header is the request header carrying the raw API key.
func NewAPIKeyVerifier(stored, header string) (*APIKeyVerifier, error) {
	header = strings.TrimSpace(header)
	if header == "" {
		return nil, fmt.Errorf("api key header is required")
	}
	if strings.TrimSpace(stored) == "" {
		return nil, fmt.Errorf("api key secret is empty")
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

	return &APIKeyVerifier{header: header, expected: expected}, nil
}

// Header returns the configured API-key header name.
func (v *APIKeyVerifier) Header() string {
	return v.header
}

// Verify reports whether raw matches the stored API key in constant time.
func (v *APIKeyVerifier) Verify(raw string) bool {
	if v == nil || raw == "" {
		return false
	}
	return secrets.HashMatchesDigest(raw, v.expected)
}
