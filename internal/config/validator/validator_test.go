package validator

import (
	"testing"

	"github.com/haiser1/go-api-gateway/internal/config"
)

// fullConfig template used by tests; every field passes validation.
func fullConfig() *config.Config {
	return &config.Config{
		Server: config.ServerConfig{ProxyPort: 8080, AdminPort: 8081, LogLevel: "info"},
		Upstreams: []config.Upstream{{
			Id:        "up-1",
			Name:      "backend",
			Algorithm: "round-robin",
			Targets:   []config.UpstreamTarget{{Host: "127.0.0.1", Port: 9090, Weight: 100}},
		}},
		Services: []config.Service{{
			Id:         "svc-1",
			Name:       "api",
			UpstreamId: "up-1",
			Protocol:   "http",
		}},
		Routes: []config.Route{{
			Id:        "rt-1",
			Name:      "root",
			Methods:   []string{"GET"},
			Paths:     []string{"/api"},
			ServiceId: "svc-1",
		}},
	}
}

func assertNoErrors(t *testing.T, errs ValidationErrors) {
	t.Helper()
	if len(errs) != 0 {
		t.Fatalf("expected valid config, got %d errors: %v", len(errs), errs)
	}
}

func assertHave(t *testing.T, errs ValidationErrors, code, contains string) {
	t.Helper()
	for _, e := range errs {
		if e.Code == code && (contains == "" || stringIn(e.Message, contains)) {
			return
		}
	}
	t.Fatalf("expected error code %s (message containing %q), got: %v", code, contains, errs)
}

func stringIn(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

func TestValidate_AcceptsFullConfig(t *testing.T) {
	assertNoErrors(t, Validate(fullConfig()))
}

func TestValidate_NilConfig(t *testing.T) {
	assertHave(t, Validate(nil), CodeInvalidValue, "nil")
}

func TestValidate_StructuralErrors(t *testing.T) {
	cases := []struct {
		name  string
		mut   func(*config.Config)
		code  string
		field string
	}{
		{"missing upstream id", func(c *config.Config) { c.Upstreams[0].Id = "" }, CodeRequired, "Id"},
		{"missing route id", func(c *config.Config) { c.Routes[0].Id = "" }, CodeRequired, "Id"},
		{"bad log level", func(c *config.Config) { c.Server.LogLevel = "verbose" }, CodeInvalidValue, "LogLevel"},
		{"bad proxy port", func(c *config.Config) { c.Server.ProxyPort = 70000 }, CodeInvalidValue, "ProxyPort"},
		{"bad protocol", func(c *config.Config) { c.Services[0].Protocol = "grpc" }, CodeInvalidValue, "Protocol"},
		{"negative timeout", func(c *config.Config) { c.Services[0].Timeout = -1 }, CodeInvalidValue, "Timeout"},
		{"bad algorithm", func(c *config.Config) { c.Upstreams[0].Algorithm = "least-conn" }, CodeInvalidValue, "Algorithm"},
		{"negative weight", func(c *config.Config) { c.Upstreams[0].Targets[0].Weight = -5 }, CodeInvalidValue, "Weight"},
		{"bad port range", func(c *config.Config) { c.Upstreams[0].Targets[0].Port = 99999 }, CodeInvalidValue, "Port"},
		{"empty route methods", func(c *config.Config) { c.Routes[0].Methods = []string{} }, CodeInvalidValue, "Methods"},
		{"nil route methods", func(c *config.Config) { c.Routes[0].Methods = nil }, CodeRequired, "Methods"},
		{"duplicate method", func(c *config.Config) { c.Routes[0].Methods = []string{"GET", "GET"} }, CodeDuplicate, "Methods"},
		{"bad method value", func(c *config.Config) { c.Routes[0].Methods = []string{"brew"} }, CodeInvalidValue, "Methods"},
		{"lowercase method", func(c *config.Config) { c.Routes[0].Methods = []string{"get"} }, CodeInvalidValue, "Methods"},
		{"path not absolute", func(c *config.Config) { c.Routes[0].Paths = []string{"api"} }, CodeInvalidFormat, "Paths"},
		{"no targets", func(c *config.Config) { c.Upstreams[0].Targets = nil }, CodeInvalidValue, "Targets"},
		{"empty target host", func(c *config.Config) { c.Upstreams[0].Targets[0].Host = "" }, CodeRequired, "Host"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := fullConfig()
			tc.mut(cfg)
			assertHave(t, Validate(cfg), tc.code, "")
		})
	}
}

func TestValidate_SemanticErrors(t *testing.T) {
	cases := []struct {
		name string
		mut  func(*config.Config)
		code string
	}{
		{"duplicate upstream id", func(c *config.Config) {
			u := c.Upstreams[0]
			u.Id = "up-1"
			c.Upstreams = append(c.Upstreams, u)
		}, CodeDuplicate},
		{"duplicate upstream name", func(c *config.Config) {
			u := c.Upstreams[0]
			u.Id = "up-2"
			c.Upstreams = append(c.Upstreams, u)
		}, CodeDuplicate},
		{"unknown upstream ref", func(c *config.Config) { c.Services[0].UpstreamId = "missing" }, CodeUnknownReference},
		{"unknown service ref", func(c *config.Config) { c.Routes[0].ServiceId = "missing" }, CodeUnknownReference},
		{"host with control char", func(c *config.Config) { c.Upstreams[0].Targets[0].Host = "bad\x7fhost" }, CodeInvalidValue},
		{"route conflict", func(c *config.Config) {
			r := config.Route{Id: "rt-2", Name: "dup", Methods: []string{"GET"}, Paths: []string{"/api"}, ServiceId: "svc-1"}
			c.Routes = append(c.Routes, r)
		}, CodeDuplicate},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := fullConfig()
			tc.mut(cfg)
			assertHave(t, Validate(cfg), tc.code, "")
		})
	}
}

func TestValidate_DuplicatePluginWithinScope(t *testing.T) {
	cfg := fullConfig()
	cfg.GlobalPlugins = []config.PluginConfig{
		{Name: "logger", Enabled: true},
		{Name: "logger", Enabled: false},
	}
	assertHave(t, Validate(cfg), CodeDuplicate, "logger")
}

func TestValidate_PluginContracts(t *testing.T) {
	cases := []struct {
		name       string
		pluginName string
		cfgMap     map[string]interface{}
		code       string
	}{
		{"rate limit zero", "rate-limiting", map[string]interface{}{"requests_per_minute": 0}, CodeInvalidPluginConfig},
		{"rate limit string", "rate-limiting", map[string]interface{}{"requests_per_minute": "fast"}, CodeInvalidPluginConfig},
		{"auth unknown type", "auth", map[string]interface{}{"type": "basic"}, CodeInvalidPluginConfig},
		{"auth unknown alg", "auth", map[string]interface{}{"type": "jwt", "alg": "HS384"}, CodeInvalidPluginConfig},
		{"auth empty jwt key", "auth", map[string]interface{}{"jwt_key": ""}, CodeInvalidPluginConfig},
		{"cors origins not list", "cors", map[string]interface{}{"allowed_origins": "http://x.com"}, CodeInvalidPluginConfig},
		{"cors bad credentials", "cors", map[string]interface{}{"allow_credentials": "yes"}, CodeInvalidPluginConfig},
		{"cors bad max age", "cors", map[string]interface{}{"max_age": -3}, CodeInvalidPluginConfig},
		{"ip status out of range", "ip-restriction", map[string]interface{}{"status_code": 999}, CodeInvalidPluginConfig},
		{"ip whitelist not list", "ip-restriction", map[string]interface{}{"whitelist": "10.0.0.1"}, CodeInvalidPluginConfig},
		{"bot blocked bots not list", "bot-detection", map[string]interface{}{"blocked_bots": "sqlmap"}, CodeInvalidPluginConfig},
		{"transform headers not map", "request-transform", map[string]interface{}{"add_headers": "X-A"}, CodeInvalidPluginConfig},
		{"transform reveals not list", "request-transform", map[string]interface{}{"remove_headers": map[string]string{"a": "b"}}, CodeInvalidPluginConfig},
		{"metrics bad flag", "metrics", map[string]interface{}{"record_query": 1}, CodeInvalidPluginConfig},
		{"prometheus bad flag", "prometheus", map[string]interface{}{"record_query": "true"}, CodeInvalidPluginConfig},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := fullConfig()
			cfg.GlobalPlugins = []config.PluginConfig{{Name: tc.pluginName, Enabled: true, Config: tc.cfgMap}}
			assertHave(t, Validate(cfg), tc.code, "")
		})
	}

	// The same keys with correct types must pass.
	t.Run("correct plugin configs pass", func(t *testing.T) {
		cfg := fullConfig()
		cfg.GlobalPlugins = []config.PluginConfig{
			{Name: "rate-limiting", Enabled: true, Config: map[string]interface{}{"requests_per_minute": 60}},
			{Name: "auth", Enabled: true, Config: map[string]interface{}{"type": "jwt", "alg": "HS256", "jwt_secret_ref": "JWT_SECRET"}},
			{Name: "cors", Enabled: true, Config: map[string]interface{}{"allowed_origins": []interface{}{"http://x.com"}, "max_age": 100}},
			{Name: "ip-restriction", Enabled: true, Config: map[string]interface{}{"status_code": 403}},
			{Name: "bot-detection", Enabled: true, Config: map[string]interface{}{"blocked_bots": []string{"sqlmap"}}},
			{Name: "request-transform", Enabled: true, Config: map[string]interface{}{"add_headers": map[string]string{"X-A": "b"}}},
			{Name: "metrics", Enabled: true, Config: map[string]interface{}{"record_query": true}},
		}
		assertNoErrors(t, Validate(cfg))
	})
}

func TestValidate_UnknownPluginRejectedWhenRegistryKnown(t *testing.T) {
	RegisterKnownPlugin("test-exclusive-plugin")

	cfg := fullConfig()
	cfg.GlobalPlugins = []config.PluginConfig{{Name: "test-exclusive-plugin", Enabled: true}}
	assertNoErrors(t, Validate(cfg))

	cfg = fullConfig()
	cfg.GlobalPlugins = []config.PluginConfig{{Name: "ghost-plugin", Enabled: true}}
	assertHave(t, Validate(cfg), CodeUnknownPlugin, "ghost-plugin")
}

func TestValidateStruct_DTOs(t *testing.T) {
	t.Run("route dto aggregates", func(t *testing.T) {
		errs := ValidateStruct(&dtoRouteReq{
			Name:      "",
			Methods:   []string{"get", "get"},
			Paths:     []string{"nope", "nope"},
			ServiceId: "",
		})
		if len(errs) < 4 {
			t.Fatalf("expected >=4 aggregated errors, got %d: %v", len(errs), errs)
		}
	})

	t.Run("upstream dto dive", func(t *testing.T) {
		errs := ValidateStruct(&dtoUpstreamReq{
			Name:    "u",
			Targets: []dtoTarget{{Host: "", Port: 99999}},
		})
		if len(errs) < 2 {
			t.Fatalf("expected >=2 aggregated errors, got %d: %v", len(errs), errs)
		}
	})

	t.Run("valid dto passes", func(t *testing.T) {
		errs := ValidateStruct(&dtoUpstreamReq{
			Name: "u",
			Targets: []dtoTarget{
				{Host: "localhost", Port: 8080, Weight: 1},
			},
		})
		assertNoErrors(t, errs)
	})

	t.Run("nil payload", func(t *testing.T) {
		assertHave(t, ValidateStruct(nil), CodeInvalidValue, "nil")
	})
}

// local aliases (no dependency on internal/domain from this package's tests)
type dtoRouteReq struct {
	Name      string   `validate:"required"`
	Methods   []string `validate:"required,min=1,unique,dive,oneof=GET POST PUT PATCH DELETE HEAD OPTIONS TRACE CONNECT"`
	Paths     []string `validate:"required,min=1,unique,dive,startswith=/"`
	ServiceId string   `validate:"required"`
}

type dtoTarget struct {
	Host   string `validate:"required"`
	Port   int    `validate:"omitempty,min=1,max=65535"`
	Weight int    `validate:"omitempty,min=1"`
}

type dtoUpstreamReq struct {
	Name      string      `validate:"required"`
	Algorithm string      `validate:"omitempty,oneof=round-robin weighted-round-robin"`
	Targets   []dtoTarget `validate:"required,min=1,dive"`
}

func TestValidate_Aggregation(t *testing.T) {
	// Multiple independent problems must be reported together, not one-by-one.
	cfg := fullConfig()
	cfg.Server.LogLevel = "verbose"
	cfg.Upstreams[0].Targets = nil
	cfg.Routes[0].ServiceId = "missing"
	errs := Validate(cfg)
	if len(errs) < 3 {
		t.Fatalf("expected >=3 aggregated errors, got %d: %v", len(errs), errs)
	}
}

func TestValidate_TopLevelAuth(t *testing.T) {
	valid := []*config.AuthConfig{
		{Mode: "api_key", APIKey: &config.APIKeyAuthConfig{SecretRef: "API_KEY_HASH"}},
		{Mode: "jwt", JWT: &config.JWTAuthConfig{Alg: "HS256", HS256SecretRef: "JWT_HS256_SECRET"}},
		{Mode: "jwt", JWT: &config.JWTAuthConfig{Alg: "RS256", RS256PublicKeyRef: "JWT_RS256_PUBLIC_KEY"}},
		{Mode: "both", APIKey: &config.APIKeyAuthConfig{SecretRef: "API_KEY_HASH"}, JWT: &config.JWTAuthConfig{Alg: "HS256", HS256SecretRef: "JWT_HS256_SECRET"}},
	}
	for i, authCfg := range valid {
		cfg := fullConfig()
		cfg.Auth = authCfg
		assertNoErrors(t, Validate(cfg))
		_ = i
	}

	invalid := []*config.AuthConfig{
		{Mode: "basic"},
		{Mode: "api_key"},
		{Mode: "jwt", JWT: &config.JWTAuthConfig{Alg: "HS512", HS256SecretRef: "S"}},
		{Mode: "both", APIKey: &config.APIKeyAuthConfig{SecretRef: "API_KEY_HASH"}},
	}
	for _, authCfg := range invalid {
		cfg := fullConfig()
		cfg.Auth = authCfg
		assertHave(t, Validate(cfg), CodeInvalidValue, "")
	}
}
