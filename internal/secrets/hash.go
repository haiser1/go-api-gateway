package secrets

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"strings"
)

// HashPrefix identifies the canonical SHA-256 hash representation stored for
// API keys and administrator tokens.
const HashPrefix = "sha256:"

// HashSecret returns the canonical hash representation of a raw secret.
func HashSecret(value string) string {
	sum := sha256.Sum256([]byte(value))
	return HashPrefix + hex.EncodeToString(sum[:])
}

// IsHash reports whether value uses the canonical hash representation.
func IsHash(value string) bool {
	return strings.HasPrefix(value, HashPrefix)
}

// ParseHash decodes a canonical SHA-256 hash value.
func ParseHash(value string) ([32]byte, error) {
	var out [32]byte
	if !IsHash(value) {
		return out, fmt.Errorf("value is not a %s hash", HashPrefix)
	}
	raw, err := hex.DecodeString(strings.TrimPrefix(value, HashPrefix))
	if err != nil {
		return out, fmt.Errorf("invalid secret hash encoding: %w", err)
	}
	if len(raw) != sha256.Size {
		return out, fmt.Errorf("invalid secret hash length: got %d bytes, want %d", len(raw), sha256.Size)
	}
	copy(out[:], raw)
	return out, nil
}

// HashMatchesDigest compares a raw secret against an expected SHA-256 digest
// in constant time.
func HashMatchesDigest(raw string, expected [32]byte) bool {
	got := sha256.Sum256([]byte(raw))
	return subtle.ConstantTimeCompare(got[:], expected[:]) == 1
}

// VerifyHash compares a raw secret against a stored value that may be either
// a canonical hash or a legacy plaintext secret.
func VerifyHash(raw, stored string) bool {
	if IsHash(stored) {
		expected, err := ParseHash(stored)
		if err != nil {
			return false
		}
		return HashMatchesDigest(raw, expected)
	}
	expected := sha256.Sum256([]byte(stored))
	return HashMatchesDigest(raw, expected)
}

// ValidateHash validates a canonical hash value.
func ValidateHash(value string) error {
	_, err := ParseHash(value)
	return err
}
