// Package secret provisions and loads auth secrets from an .env file.
//
// The file is created automatically the first time an auth plugin is added
// (via the Management API or when an enabled auth plugin is compiled into
// a route) and holds the generated API_KEY_SECRET / JWT_SECRET values used
// by the authorization middleware. Adding a secret file in configurations
// avoids storing credentials inside config.yaml, which is user-editable and
// served over the Management API.
package secret

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Keys provisioned in the .env file for the auth plugin.
const (
	// APIKeySecret validates the X-API-Key header (auth type "api_key").
	APIKeySecret = "API_KEY_SECRET"

	// JWTSecret is the HMAC signing key for JWT verification.
	// Default signing algorithm: HS256 (HMAC-SHA256).
	JWTSecret = "JWT_SECRET"
)

// requiredKeys lists the keys EnsureEnvFile guarantees to exist.
var requiredKeys = []string{APIKeySecret, JWTSecret}

// EnvFileEnvVar allows overriding the default .env location.
const EnvFileEnvVar = "GATEWAY_ENV_FILE"

// DefaultPath resolves the .env file location used by the auth middleware:
//
//  1. $GATEWAY_ENV_FILE, if set;
//  2. configs/.env, when a configs directory exists relative to the working
//     directory (same directory that holds config.yaml);
//  3. .env in the working directory as a fallback.
func DefaultPath() string {
	if p := os.Getenv(EnvFileEnvVar); p != "" {
		return p
	}
	if info, err := os.Stat("configs"); err == nil && info.IsDir() {
		return filepath.Join("configs", ".env")
	}
	return ".env"
}

// GenerateKey returns 32 bytes of random data encoded as base64 (URL-safe),
// suitable as an HMAC (e.g. HS256) or API-key secret.
func GenerateKey() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("failed to generate random secret: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// LoadFile parses a dotenv-style file. Comments (full-line or inline after a
// value), blank lines, optional single/double quotes and an optional "export"
// prefix are supported. Values referencing other keys are NOT expanded.
func LoadFile(path string) (map[string]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return Parse(string(data)), nil
}

// Parse parses dotenv-style content into a map.
func Parse(content string) map[string]string {
	out := make(map[string]string)
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		trimmed = strings.TrimPrefix(trimmed, "export ")
		idx := strings.Index(trimmed, "=")
		if idx <= 0 {
			continue
		}
		key := strings.TrimSpace(trimmed[:idx])
		val := strings.TrimSpace(trimmed[idx+1:])

		// Strip inline comments: JWT_SECRET=abc123 # rotated on ...
		if i := strings.Index(val, " #"); i >= 0 {
			val = strings.TrimSpace(val[:i])
		}
		out[key] = unquote(val)
	}
	return out
}

// EnsureEnvFile provisions the .env file at the given path:
//
//   - if the file does not exist, it is created (0600) with freshly generated
//     values for all required keys;
//   - if the file exists, only MISSING or EMPTY required keys get generated
//     values appended — existing values are never modified.
//
// It returns true when the file was freshly created.
func EnsureEnvFile(path string) (created bool, err error) {
	if dir := filepath.Dir(path); dir != "" {
		if mErr := os.MkdirAll(dir, 0o700); mErr != nil {
			return false, fmt.Errorf("failed to create env directory: %w", mErr)
		}
	}

	existing, err := LoadFile(path)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			return false, fmt.Errorf("failed to read env file: %w", err)
		}

		// Fast path: exclusive creation avoids clobbering content when the
		// file shows up concurrently between the read and the create.
		var b strings.Builder
		b.WriteString("# Auto-provisioned secrets for the auth plugin - do NOT commit this file\n\n")
		for _, key := range requiredKeys {
			val, gErr := GenerateKey()
			if gErr != nil {
				return false, gErr
			}
			fmt.Fprintf(&b, "%s=%s\n", key, val)
		}

		f, cErr := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if cErr != nil {
			if !errors.Is(cErr, fs.ErrExist) {
				return false, fmt.Errorf("failed to create env file: %w", cErr)
			}
			// Raced with another provisioner — reload and merge below.
			existing, err = LoadFile(path)
			if err != nil {
				return false, fmt.Errorf("failed to read env file: %w", err)
			}
		} else {
			_, wErr := f.WriteString(b.String())
			closeErr := f.Close()
			if wErr != nil {
				return false, fmt.Errorf("failed to write env file: %w", wErr)
			}
			if closeErr != nil {
				return false, fmt.Errorf("failed to close env file: %w", closeErr)
			}
			return true, nil
		}
	}

	// Merge path: append values for missing/empty keys only.
	var missing []string
	for _, key := range requiredKeys {
		if v, ok := existing[key]; !ok || strings.TrimSpace(v) == "" {
			missing = append(missing, key)
		}
	}
	if len(missing) == 0 {
		return false, nil
	}

	// Merge path: append values for missing/empty keys to the ORIGINAL
	// content — existing lines are never rewritten or dropped.
	orig, rErr := os.ReadFile(path)
	if rErr != nil {
		return false, fmt.Errorf("failed to read env file: %w", rErr)
	}
	var content strings.Builder
	content.Write(orig)
	if len(orig) > 0 && orig[len(orig)-1] != '\n' {
		content.WriteByte('\n')
	}
	for _, key := range missing {
		val, gErr := GenerateKey()
		if gErr != nil {
			return false, gErr
		}
		fmt.Fprintln(&content, key+"="+val)
	}

	// Atomic replace: write a temp file in the same directory, then rename.
	tmp := path + ".tmp"
	mode := fs.FileMode(0o600)
	if info, sErr := os.Stat(path); sErr == nil {
		mode = info.Mode().Perm()
	}
	if wErr := os.WriteFile(tmp, []byte(content.String()), mode); wErr != nil {
		return false, fmt.Errorf("failed to write env file: %w", wErr)
	}
	if rErr := os.Rename(tmp, path); rErr != nil {
		_ = os.Remove(tmp)
		return false, fmt.Errorf("failed to replace env file: %w", rErr)
	}
	_ = os.Chmod(path, mode)
	return false, nil
}

// unquote strips a matching pair of surrounding single or double quotes.
func unquote(v string) string {
	if len(v) >= 2 {
		if (v[0] == '"' && v[len(v)-1] == '"') || (v[0] == '\'' && v[len(v)-1] == '\'') {
			return v[1 : len(v)-1]
		}
	}
	return v
}
