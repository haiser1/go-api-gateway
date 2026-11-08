package handler_test

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/haiser1/go-api-gateway/internal/auth"
	"github.com/haiser1/go-api-gateway/internal/config"
	"github.com/haiser1/go-api-gateway/internal/gateway"
	"github.com/haiser1/go-api-gateway/internal/secrets"
	"github.com/julienschmidt/httprouter"
)

func setupAuthTestRouter(t *testing.T) (*httprouter.Router, *config.Manager, *secrets.MapProvider, string) {
	t.Helper()
	tmpDir := t.TempDir()
	initialConfig := config.Config{Server: config.ServerConfig{ProxyPort: 8080, AdminPort: 8081}}
	data, err := json.Marshal(initialConfig)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "config.yaml"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	manager, err := config.NewManager(tmpDir)
	if err != nil {
		t.Fatal(err)
	}
	provider := secrets.NewMapProvider(nil)
	router := httprouter.New()
	gateway.RegisterManagementRoutes(router, manager, provider)
	return router, manager, provider, tmpDir
}

func authRequest(t *testing.T, router http.Handler, method, path string, body interface{}) *httptest.ResponseRecorder {
	t.Helper()
	var reader *bytes.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		reader = bytes.NewReader(data)
	} else {
		reader = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, reader)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func TestAuthHandlersAPIKey(t *testing.T) {
	router, manager, provider, _ := setupAuthTestRouter(t)
	defer manager.Close()

	raw := "0123456789abcdef"
	rec := authRequest(t, router, http.MethodPut, "/api/config/auth/api-key", map[string]string{"secret": raw})
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT api-key status = %d body = %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), raw) {
		t.Fatal("response leaked the raw API key")
	}

	stored, err := provider.Get(secrets.APIKeyHashKey)
	if err != nil || !secrets.VerifyHash(raw, stored) {
		t.Fatalf("stored API key hash invalid: %q, %v", stored, err)
	}
	if manager.GetConfig().Auth == nil || manager.GetConfig().Auth.Mode != config.AuthModeAPIKey {
		t.Fatalf("auth config not published: %+v", manager.GetConfig().Auth)
	}

	rec = authRequest(t, router, http.MethodGet, "/api/config/auth", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET auth status = %d body = %s", rec.Code, rec.Body.String())
	}
	var response struct {
		Success bool               `json:"success"`
		Data    *config.AuthConfig `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Data == nil || response.Data.Mode != config.AuthModeAPIKey {
		t.Fatalf("GET auth data = %+v", response.Data)
	}
}

func TestAuthHandlersJWTAndModeTransitions(t *testing.T) {
	router, manager, provider, _ := setupAuthTestRouter(t)
	defer manager.Close()

	apiKey := "0123456789abcdef"
	if rec := authRequest(t, router, http.MethodPut, "/api/config/auth/api-key", map[string]string{"secret": apiKey}); rec.Code != http.StatusOK {
		t.Fatalf("api key update failed: %d %s", rec.Code, rec.Body.String())
	}

	hsSecret := "0123456789abcdef0123456789abcdef"
	if rec := authRequest(t, router, http.MethodPut, "/api/config/auth/jwt/hs256", map[string]string{"secret": hsSecret, "issuer": "issuer"}); rec.Code != http.StatusOK {
		t.Fatalf("hs256 update failed: %d %s", rec.Code, rec.Body.String())
	}
	authCfg := manager.GetConfig().Auth
	if authCfg.Mode != config.AuthModeBoth || authCfg.JWT == nil || authCfg.JWT.Alg != config.JWTAlgHS256 {
		t.Fatalf("unexpected auth after hs256: %+v", authCfg)
	}
	if stored, _ := provider.Get(secrets.JWTHS256SecretKey); stored != hsSecret {
		t.Fatalf("HS256 secret not stored: %q", stored)
	}

	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKIXPublicKey(&privateKey.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	pemKey := string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}))
	if rec := authRequest(t, router, http.MethodPut, "/api/config/auth/jwt/rs256", map[string]string{"public_key": pemKey}); rec.Code != http.StatusOK {
		t.Fatalf("rs256 update failed: %d %s", rec.Code, rec.Body.String())
	}
	authCfg = manager.GetConfig().Auth
	if authCfg.JWT.Alg != config.JWTAlgRS256 || authCfg.JWT.RS256PublicKeyRef != secrets.JWTRS256PublicKeyKey {
		t.Fatalf("unexpected auth after rs256: %+v", authCfg)
	}
	stored, err := provider.Get(secrets.JWTRS256PublicKeyKey)
	if err != nil {
		t.Fatal(err)
	}
	if strings.ContainsAny(stored, "\r\n") {
		t.Fatal("stored public key must not contain raw newlines")
	}
	decoded, err := base64.StdEncoding.DecodeString(stored)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := auth.ParseRSAPublicKey(string(decoded)); err != nil {
		t.Fatalf("stored public key cannot be parsed: %v", err)
	}
}

func TestAuthHandlersRejectInvalidCredentials(t *testing.T) {
	router, manager, provider, _ := setupAuthTestRouter(t)
	defer manager.Close()

	cases := []struct {
		path string
		body map[string]string
	}{
		{"/api/config/auth/api-key", map[string]string{"secret": "short"}},
		{"/api/config/auth/jwt/hs256", map[string]string{"secret": "short"}},
		{"/api/config/auth/jwt/rs256", map[string]string{"public_key": "not a key"}},
	}
	for _, tc := range cases {
		rec := authRequest(t, router, http.MethodPut, tc.path, tc.body)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s status = %d body = %s", tc.path, rec.Code, rec.Body.String())
		}
	}
	if len(provider.Snapshot()) != 0 {
		t.Fatalf("invalid credentials must not be stored: %v", provider.Snapshot())
	}
	if manager.GetConfig().Auth != nil {
		t.Fatal("invalid credentials must not publish auth config")
	}
}

func TestAuthHandlerRollsBackSecretOnConfigFailure(t *testing.T) {
	router, manager, provider, _ := setupAuthTestRouter(t)
	defer manager.Close()

	config.SetRuntimeBuilder(func(cfg *config.Config) error {
		return errors.New("forced build failure")
	})
	defer config.SetRuntimeBuilder(nil)

	rec := authRequest(t, router, http.MethodPut, "/api/config/auth/api-key", map[string]string{"secret": "0123456789abcdef"})
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	if _, err := provider.Get(secrets.APIKeyHashKey); !errors.Is(err, secrets.ErrNotFound) {
		t.Fatalf("secret was not rolled back: %v", err)
	}
	if manager.GetConfig().Auth != nil {
		t.Fatal("auth config was published despite build failure")
	}
}

func TestGetFullConfigRedactsPluginSecrets(t *testing.T) {
	router, manager, _, _ := setupAuthTestRouter(t)
	defer manager.Close()

	if err := manager.AddGlobalPlugin(config.PluginConfig{
		Name: "logger", Enabled: true,
		Config: map[string]interface{}{
			"token":      "super-secret",
			"secret_ref": "TOKEN_REF",
			"nested": []interface{}{
				map[string]interface{}{"password": "nested-secret", "name": "keep"},
			},
		},
	}); err != nil {
		t.Fatal(err)
	}

	rec := authRequest(t, router, http.MethodGet, "/api/config", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	body := rec.Body.String()
	if strings.Contains(body, "super-secret") || strings.Contains(body, "nested-secret") {
		t.Fatalf("config response leaked a secret: %s", body)
	}
	if !strings.Contains(body, "TOKEN_REF") || !strings.Contains(body, "keep") {
		t.Fatalf("config response should keep non-secret values: %s", body)
	}
	live := manager.GetConfig().GlobalPlugins[0].Config
	if live["token"] != "super-secret" {
		t.Fatalf("redaction mutated the live config: %v", live)
	}
	nested := live["nested"].([]interface{})[0].(map[string]interface{})
	if nested["password"] != "nested-secret" {
		t.Fatalf("redaction mutated nested live config: %v", nested)
	}
}
