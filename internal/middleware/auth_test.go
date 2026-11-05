package middleware

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/haiser1/go-api-gateway/internal/secret"
)

// setupAuth test-fixture redirects .env to a temp path per test.
func setupAuth(t *testing.T) string {
	t.Helper()
	envPath := filepath.Join(t.TempDir(), ".env")
	t.Setenv(secret.EnvFileEnvVar, envPath)
	return envPath
}

type authNextResult struct {
	called bool
}

func newNext(t *testing.T) (http.Handler, *authNextResult) {
	t.Helper()
	res := &authNextResult{}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		res.called = true
		w.WriteHeader(http.StatusOK)
	}), res
}

func buildFromEnv(t *testing.T, config map[string]interface{}) (jwtSecret string, apiKey string) {
	t.Helper()
	// Trigger provisioning through the builder
	NewAuthPlugin(map[string]interface{}{})

	secrets, err := secret.LoadFile(secret.DefaultPath())
	if err != nil {
		t.Fatalf("failed to load provisioned .env: %v", err)
	}
	return secrets[secret.JWTSecret], secrets[secret.APIKeySecret]
}

func TestAuthPluginProvisionsEnvFile(t *testing.T) {
	path := setupAuth(t)
	config := map[string]interface{}{}

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal(".env should not exist before the auth plugin is built")
	}
	NewAuthPlugin(config)

	secrets, err := secret.LoadFile(path)
	if err != nil {
		t.Fatalf("expected .env created by auth plugin build: %v", err)
	}
	if secrets[secret.JWTSecret] == "" || secrets[secret.APIKeySecret] == "" {
		t.Errorf("expected both secrets generated: %v", secrets)
	}
}

func TestAuthJWTValidToken(t *testing.T) {
	setupAuth(t)
	jwtSecret, _ := buildFromEnv(t, nil)

	plugin := NewAuthPlugin(map[string]interface{}{}) // default: jwt + HS256

	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub": "user-1",
		"exp": time.Now().Add(time.Hour).Unix(),
	})
	tokenStr, err := tok.SignedString([]byte(jwtSecret))
	if err != nil {
		t.Fatalf("failed to sign token: %v", err)
	}

	req := httptest.NewRequest("GET", "/protected", nil)
	req.Header.Set("Authorization", "Bearer "+tokenStr)
	rr := httptest.NewRecorder()
	next, res := newNext(t)

	if continued := plugin.Execute(rr, req, next); !continued {
		t.Error("valid JWT must continue the chain")
	}
	if !res.called {
		t.Error("next handler must be called for a valid JWT")
	}
}

func TestAuthJWTRejectsMissingHeader(t *testing.T) {
	setupAuth(t)
	plugin := NewAuthPlugin(map[string]interface{}{})

	req := httptest.NewRequest("GET", "/protected", nil)
	rr := httptest.NewRecorder()
	next, res := newNext(t)

	if continued := plugin.Execute(rr, req, next); continued {
		t.Error("requests without Authorization must be rejected")
	}
	if res.called {
		t.Error("next handler must NOT be called")
	}
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rr.Code)
	}
}

func TestAuthJWTRejectsWrongKey(t *testing.T) {
	setupAuth(t)
	plugin := NewAuthPlugin(map[string]interface{}{})

	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{})
	tokenStr, _ := tok.SignedString([]byte("definitely-not-the-secret"))

	req := httptest.NewRequest("GET", "/protected", nil)
	req.Header.Set("Authorization", "Bearer "+tokenStr)
	rr := httptest.NewRecorder()
	next, res := newNext(t)

	if continued := plugin.Execute(rr, req, next); continued {
		t.Error("token signed with wrong key must be rejected")
	}
	if res.called || rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 and chain halted, got continued=%v code=%d", res.called, rr.Code)
	}
}

func TestAuthJWTDiscardsAlgByDefault(t *testing.T) {
	setupAuth(t)
	jwtSecret, _ := buildFromEnv(t, nil)
	plugin := NewAuthPlugin(map[string]interface{}{}) // default alg = HS256

	// Same secret but HS384 signing must NOT pass (default alg is HS256)
	tok := jwt.NewWithClaims(jwt.SigningMethodHS384, jwt.MapClaims{})
	tokenStr, _ := tok.SignedString([]byte(jwtSecret))

	req := httptest.NewRequest("GET", "/protected", nil)
	req.Header.Set("Authorization", "Bearer "+tokenStr)
	rr := httptest.NewRecorder()
	next, res := newNext(t)

	if continued := plugin.Execute(rr, req, next); continued {
		t.Error("HS384 token must be rejected when alg defaults to HS256")
	}
	if res.called {
		t.Error("next handler must NOT be called for alg mismatch")
	}
}

func TestAuthJWTConfigurableAlg(t *testing.T) {
	setupAuth(t)
	jwtSecret, _ := buildFromEnv(t, nil)
	plugin := NewAuthPlugin(map[string]interface{}{"alg": "HS384"})

	tok := jwt.NewWithClaims(jwt.SigningMethodHS384, jwt.MapClaims{})
	tokenStr, _ := tok.SignedString([]byte(jwtSecret))
	req := httptest.NewRequest("GET", "/protected", nil)
	req.Header.Set("Authorization", "Bearer "+tokenStr)
	rr := httptest.NewRecorder()
	next, res := newNext(t)
	if continued := plugin.Execute(rr, req, next); !continued {
		t.Error("HS384 token must pass when configured")
	}
	if !res.called {
		t.Error("next handler must be called")
	}
}

func TestAuthJWTKeyFallbackFromConfig(t *testing.T) {
	// .env must disappear: point it at a file so MkdirAll fails
	dir := t.TempDir()
	blocker := filepath.Join(dir, "notadir")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(secret.EnvFileEnvVar, filepath.Join(blocker, ".env"))

	plugin := NewAuthPlugin(map[string]interface{}{"jwt_key": "config-fallback-key"})

	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{})
	tokenStr, _ := tok.SignedString([]byte("config-fallback-key"))
	req := httptest.NewRequest("GET", "/protected", nil)
	req.Header.Set("Authorization", "Bearer "+tokenStr)
	rr := httptest.NewRecorder()
	next, res := newNext(t)

	if continued := plugin.Execute(rr, req, next); !continued {
		t.Error("in-config jwt_key fallback must keep JWT auth working")
	}
	if !res.called {
		t.Error("next handler must be called")
	}
}

func TestAuthPassThroughWhenNoSecretAvailable(t *testing.T) {
	dir := t.TempDir()
	blocker := filepath.Join(dir, "notadir")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(secret.EnvFileEnvVar, filepath.Join(blocker, ".env"))

	plugin := NewAuthPlugin(map[string]interface{}{}) // no secret anywhere

	req := httptest.NewRequest("GET", "/protected", nil)
	rr := httptest.NewRecorder()
	next, res := newNext(t)

	if continued := plugin.Execute(rr, req, next); !continued {
		t.Error("fail-safe plugin must continue the chain")
	}
	if !res.called {
		t.Error("fail-safe plugin must call next")
	}
	if rr.Code != http.StatusOK {
		t.Errorf("unauthenticated request must pass through, got %d", rr.Code)
	}
}

func TestAuthAPIKeyValid(t *testing.T) {
	setupAuth(t)
	_, apiKey := buildFromEnv(t, nil)
	plugin := NewAuthPlugin(map[string]interface{}{"type": "api_key"})

	req := httptest.NewRequest("GET", "/protected", nil)
	req.Header.Set("X-API-Key", apiKey)
	rr := httptest.NewRecorder()
	next, res := newNext(t)

	if continued := plugin.Execute(rr, req, next); !continued {
		t.Error("valid API key must continue the chain")
	}
	if !res.called {
		t.Error("next handler must be called for a valid API key")
	}
}

func TestAuthAPIKeyInvalidAndMissing(t *testing.T) {
	setupAuth(t)
	plugin := NewAuthPlugin(map[string]interface{}{"type": "api_key"})

	cases := []struct {
		name   string
		header bool
		value  string
	}{
		{name: "wrong key", header: true, value: "wrong-key"},
		{name: "missing header", header: false},
	}
	for _, tc := range cases {
		req := httptest.NewRequest("GET", "/protected", nil)
		if tc.header {
			req.Header.Set("X-API-Key", tc.value)
		}
		rr := httptest.NewRecorder()
		next, res := newNext(t)

		if continued := plugin.Execute(rr, req, next); continued {
			t.Errorf("%s must be rejected", tc.name)
		}
		if res.called {
			t.Errorf("%s: next handler must NOT be called", tc.name)
		}
		if rr.Code != http.StatusUnauthorized {
			t.Errorf("%s: expected 401, got %d", tc.name, rr.Code)
		}
	}
}

func TestAuthAPIKeyCustomHeader(t *testing.T) {
	setupAuth(t)
	_, apiKey := buildFromEnv(t, nil)
	plugin := NewAuthPlugin(map[string]interface{}{
		"type":           "api_key",
		"api_key_header": "X-Token",
	})

	req := httptest.NewRequest("GET", "/protected", nil)
	req.Header.Set("X-Token", apiKey)
	rr := httptest.NewRecorder()
	next, res := newNext(t)

	if continued := plugin.Execute(rr, req, next); !continued {
		t.Error("custom API key header must be honored")
	}
	if !res.called {
		t.Error("next handler must be called")
	}
}

func TestAuthAPIKeyFallbackFromConfig(t *testing.T) {
	dir := t.TempDir()
	blocker := filepath.Join(dir, "notadir")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(secret.EnvFileEnvVar, filepath.Join(blocker, ".env"))

	plugin := NewAuthPlugin(map[string]interface{}{"type": "api_key", "api_key": "cfg-key"})

	req := httptest.NewRequest("GET", "/protected", nil)
	req.Header.Set("X-API-Key", "cfg-key")
	rr := httptest.NewRecorder()
	next, res := newNext(t)

	if continued := plugin.Execute(rr, req, next); !continued {
		t.Error("in-config api_key fallback must keep api_key auth working")
	}
	if !res.called {
		t.Error("next handler must be called")
	}
}
