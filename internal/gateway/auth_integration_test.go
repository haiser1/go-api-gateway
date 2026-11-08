package gateway

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/haiser1/go-api-gateway/internal/config"
	"github.com/haiser1/go-api-gateway/internal/secrets"
	"gopkg.in/yaml.v3"
)

const gatewayTestHSSecret = "0123456789abcdef0123456789abcdef"

func gatewayTestToken(t *testing.T) string {
	t.Helper()
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub": "user-1",
		"exp": time.Now().Add(time.Hour).Unix(),
	})
	signed, err := token.SignedString([]byte(gatewayTestHSSecret))
	if err != nil {
		t.Fatal(err)
	}
	return signed
}

func TestProxy_TopLevelAPIKeyAuth(t *testing.T) {
	up := newEchoUpstream(t, "ok", nil, nil)
	cfg := runtimeConfig(up.host, up.port, "/api/secure")
	cfg.Auth = &config.AuthConfig{
		Mode:   config.AuthModeAPIKey,
		APIKey: &config.APIKeyAuthConfig{SecretRef: "API_KEY_HASH"},
	}
	provider := secrets.NewMapProvider(map[string]string{
		"API_KEY_HASH": secrets.HashSecret("key-1"),
	})
	p, err := NewProxyWithSecrets(cfg, provider)
	if err != nil {
		t.Fatal(err)
	}
	if p.CurrentSnapshot().Auth == nil || !p.CurrentSnapshot().Auth.Enabled() {
		t.Fatal("snapshot auth runtime not published")
	}

	req := httptest.NewRequest(http.MethodGet, "/api/secure", nil)
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("missing key status = %d, want 401", rec.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/secure", nil)
	req.Header.Set("X-API-Key", "key-1")
	rec = httptest.NewRecorder()
	p.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || rec.Body.String() != "ok" {
		t.Fatalf("valid key status = %d body = %q", rec.Code, rec.Body.String())
	}
}

func TestProxy_TopLevelJWTAuth(t *testing.T) {
	up := newEchoUpstream(t, "ok", nil, nil)
	cfg := runtimeConfig(up.host, up.port, "/api/secure")
	cfg.Auth = &config.AuthConfig{
		Mode: config.AuthModeJWT,
		JWT:  &config.JWTAuthConfig{Alg: "HS256", HS256SecretRef: "JWT_HS256_SECRET"},
	}
	provider := secrets.NewMapProvider(map[string]string{"JWT_HS256_SECRET": gatewayTestHSSecret})
	p, err := NewProxyWithSecrets(cfg, provider)
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/secure", nil)
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized || !strings.Contains(rec.Body.String(), "Authorization header required") {
		t.Fatalf("missing token status = %d body = %q", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/secure", nil)
	req.Header.Set("Authorization", "Bearer "+gatewayTestToken(t))
	rec = httptest.NewRecorder()
	p.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || rec.Body.String() != "ok" {
		t.Fatalf("valid token status = %d body = %q", rec.Code, rec.Body.String())
	}
}

func TestProxy_LegacyAuthPluginRemainsRouteScoped(t *testing.T) {
	up := newEchoUpstream(t, "ok", nil, nil)
	cfg := runtimeConfig(up.host, up.port, "/api/secure")
	cfg.Routes[0].Plugins = []config.PluginConfig{{
		Name:    "auth",
		Enabled: true,
		Config: map[string]interface{}{
			"type": "jwt", "alg": "HS256", "jwt_secret_ref": "JWT_HS256_SECRET",
		},
	}}
	cfg.Routes = append(cfg.Routes, config.Route{
		Id: "r-open", Name: "open-route", ServiceId: "s-runtime",
		Methods: []string{"GET"}, Paths: []string{"/api/open"},
	})

	provider := secrets.NewMapProvider(map[string]string{"JWT_HS256_SECRET": gatewayTestHSSecret})
	p, err := NewProxyWithSecrets(cfg, provider)
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/secure", nil)
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("secure route status = %d, want 401", rec.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/open", nil)
	rec = httptest.NewRecorder()
	p.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("open route status = %d, want 200", rec.Code)
	}
}

func TestProxy_AuthConfigurationConflictsFailBuild(t *testing.T) {
	up := newEchoUpstream(t, "ok", nil, nil)
	provider := secrets.NewMapProvider(map[string]string{
		"API_KEY_HASH":     secrets.HashSecret("key-1"),
		"JWT_HS256_SECRET": gatewayTestHSSecret,
	})

	base := runtimeConfig(up.host, up.port, "/api/secure")
	base.Auth = &config.AuthConfig{
		Mode:   config.AuthModeAPIKey,
		APIKey: &config.APIKeyAuthConfig{SecretRef: "API_KEY_HASH"},
	}
	base.Routes[0].Plugins = []config.PluginConfig{{
		Name: "auth", Enabled: true,
		Config: map[string]interface{}{"type": "jwt", "alg": "HS256", "jwt_secret_ref": "JWT_HS256_SECRET"},
	}}
	if _, err := NewProxyWithSecrets(base, provider); err == nil {
		t.Fatal("top-level auth plus auth plugin must fail")
	}

	conflicting := runtimeConfig(up.host, up.port, "/api/secure")
	conflicting.Routes[0].Plugins = []config.PluginConfig{{
		Name: "auth", Enabled: true,
		Config: map[string]interface{}{"type": "jwt", "alg": "HS256", "jwt_secret_ref": "JWT_HS256_SECRET"},
	}}
	conflicting.Routes = append(conflicting.Routes, config.Route{
		Id: "r-other", Name: "other-route", ServiceId: "s-runtime",
		Methods: []string{"GET"}, Paths: []string{"/api/other"},
		Plugins: []config.PluginConfig{{
			Name: "authorization", Enabled: true,
			Config: map[string]interface{}{"type": "api_key", "api_key_secret_ref": "API_KEY_HASH"},
		}},
	})
	if _, err := NewProxyWithSecrets(conflicting, provider); err == nil {
		t.Fatal("conflicting legacy auth plugins must fail")
	}
}

func TestProxy_AuthMissingSecretFailsBuild(t *testing.T) {
	up := newEchoUpstream(t, "ok", nil, nil)
	cfg := runtimeConfig(up.host, up.port, "/api/secure")
	cfg.Auth = &config.AuthConfig{
		Mode:   config.AuthModeAPIKey,
		APIKey: &config.APIKeyAuthConfig{SecretRef: "MISSING"},
	}
	if _, err := NewProxyWithSecrets(cfg, secrets.NewMapProvider(nil)); err == nil {
		t.Fatal("missing auth secret must fail the build")
	}
	if _, err := NewProxy(cfg); err == nil {
		t.Fatal("auth without a provider must fail the build")
	}
}

func TestProxy_AuthReloadPicksUpNewSecret(t *testing.T) {
	up := newEchoUpstream(t, "ok", nil, nil)
	cfg := runtimeConfig(up.host, up.port, "/api/secure")
	cfg.Auth = &config.AuthConfig{
		Mode:   config.AuthModeAPIKey,
		APIKey: &config.APIKeyAuthConfig{SecretRef: "API_KEY_HASH"},
	}
	provider := secrets.NewMapProvider(map[string]string{
		"API_KEY_HASH": secrets.HashSecret("key-1"),
	})
	p, err := NewProxyWithSecrets(cfg, provider)
	if err != nil {
		t.Fatal(err)
	}

	request := func(key string) int {
		req := httptest.NewRequest(http.MethodGet, "/api/secure", nil)
		req.Header.Set("X-API-Key", key)
		rec := httptest.NewRecorder()
		p.ServeHTTP(rec, req)
		return rec.Code
	}

	if code := request("key-1"); code != http.StatusOK {
		t.Fatalf("initial key status = %d", code)
	}
	if err := provider.SetMany(map[string]string{"API_KEY_HASH": secrets.HashSecret("key-2")}); err != nil {
		t.Fatal(err)
	}
	if err := p.UpdateRoutes(cfg); err != nil {
		t.Fatal(err)
	}
	if code := request("key-1"); code != http.StatusUnauthorized {
		t.Fatalf("old key status = %d, want 401", code)
	}
	if code := request("key-2"); code != http.StatusOK {
		t.Fatalf("new key status = %d, want 200", code)
	}
}

func TestProxy_AuthEnvFileReloadIsAtomic(t *testing.T) {
	up := newEchoUpstream(t, "ok", nil, nil)
	cfg := runtimeConfig(up.host, up.port, "/api/secure")
	cfg.Auth = &config.AuthConfig{
		Mode:   config.AuthModeAPIKey,
		APIKey: &config.APIKeyAuthConfig{SecretRef: "API_KEY_HASH"},
	}

	envPath := filepath.Join(t.TempDir(), ".env")
	writeEnv := func(content string) {
		t.Helper()
		if err := os.WriteFile(envPath, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	key1 := secrets.HashSecret("key-1")
	key2 := secrets.HashSecret("key-2")
	key3 := secrets.HashSecret("key-3")
	writeEnv("API_KEY_HASH=" + key1 + "\n")

	provider, err := secrets.OpenEnvFileProvider(envPath)
	if err != nil {
		t.Fatal(err)
	}
	p, err := NewProxyWithSecrets(cfg, provider)
	if err != nil {
		t.Fatal(err)
	}

	request := func(key string) int {
		req := httptest.NewRequest(http.MethodGet, "/api/secure", nil)
		req.Header.Set("X-API-Key", key)
		rec := httptest.NewRecorder()
		p.ServeHTTP(rec, req)
		return rec.Code
	}

	if code := request("key-1"); code != http.StatusOK {
		t.Fatalf("initial key status = %d", code)
	}

	// A valid new secret is staged, built, and published as one snapshot.
	writeEnv("API_KEY_HASH=" + key2 + "\n")
	if err := p.UpdateRoutes(cfg); err != nil {
		t.Fatal(err)
	}
	if code := request("key-1"); code != http.StatusUnauthorized {
		t.Fatalf("old key status = %d, want 401", code)
	}
	if code := request("key-2"); code != http.StatusOK {
		t.Fatalf("new key status = %d, want 200", code)
	}

	// An invalid secret file must not publish a new runtime or mutate the
	// provider's committed values.
	writeEnv("OTHER=1\n")
	if err := p.UpdateRoutes(cfg); err == nil {
		t.Fatal("missing referenced secret must fail the build")
	}
	if code := request("key-2"); code != http.StatusOK {
		t.Fatalf("previous runtime must stay active; key-2 status = %d", code)
	}
	if got, err := provider.Get("API_KEY_HASH"); err != nil || got != key2 {
		t.Fatalf("provider value = %q, %v; want committed key-2", got, err)
	}

	// A later valid write recovers normally.
	writeEnv("API_KEY_HASH=" + key3 + "\n")
	if err := p.UpdateRoutes(cfg); err != nil {
		t.Fatal(err)
	}
	if code := request("key-2"); code != http.StatusUnauthorized {
		t.Fatalf("key-2 status = %d, want 401", code)
	}
	if code := request("key-3"); code != http.StatusOK {
		t.Fatalf("key-3 status = %d, want 200", code)
	}
}

func TestServer_SecretWatcherReloadsEnvKeys(t *testing.T) {
	up := newEchoUpstream(t, "ok", nil, nil)
	cfg := runtimeConfig(up.host, up.port, "/api/secure")
	cfg.Auth = &config.AuthConfig{
		Mode:   config.AuthModeAPIKey,
		APIKey: &config.APIKeyAuthConfig{SecretRef: "API_KEY_HASH"},
	}

	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	data, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, data, 0o644); err != nil {
		t.Fatal(err)
	}
	manager, err := config.NewManager(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()

	envPath := filepath.Join(dir, ".env")
	key1 := secrets.HashSecret("watcher-key-1")
	key2 := secrets.HashSecret("watcher-key-2")
	if err := os.WriteFile(envPath, []byte("API_KEY_HASH="+key1+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	provider, err := secrets.OpenEnvFileProvider(envPath)
	if err != nil {
		t.Fatal(err)
	}
	proxy, err := NewProxyWithSecrets(manager.GetConfig(), provider)
	if err != nil {
		t.Fatal(err)
	}

	s := &Server{
		cfgManager: manager,
		proxy:      proxy,
		secretPath: envPath,
		stopCh:     make(chan struct{}),
	}
	go s.watchSecretChanges()
	defer s.stopOnce.Do(func() { close(s.stopCh) })

	request := func(key string) int {
		req := httptest.NewRequest(http.MethodGet, "/api/secure", nil)
		req.Header.Set("X-API-Key", key)
		rec := httptest.NewRecorder()
		proxy.ServeHTTP(rec, req)
		return rec.Code
	}

	if code := request("watcher-key-1"); code != http.StatusOK {
		t.Fatalf("initial key status = %d", code)
	}

	// Let the watcher goroutine register its directory watch before writing.
	time.Sleep(100 * time.Millisecond)
	if err := os.WriteFile(envPath, []byte("API_KEY_HASH="+key2+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(3 * time.Second)
	for proxy.CurrentSnapshot().Version < 2 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if version := proxy.CurrentSnapshot().Version; version < 2 {
		t.Fatalf("secret watcher did not publish a new runtime; version = %d", version)
	}
	if code := request("watcher-key-1"); code != http.StatusUnauthorized {
		t.Fatalf("old key status = %d, want 401", code)
	}
	if code := request("watcher-key-2"); code != http.StatusOK {
		t.Fatalf("new key status = %d, want 200", code)
	}
}
