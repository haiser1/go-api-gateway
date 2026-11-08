package config

import (
	"strings"
	"testing"
)

func boolPtr(v bool) *bool { return &v }

func TestAuthConfigValidate(t *testing.T) {
	valid := []*AuthConfig{
		{Mode: "api_key", APIKey: &APIKeyAuthConfig{SecretRef: "API_KEY_HASH"}},
		{Mode: "jwt", JWT: &JWTAuthConfig{Alg: "HS256", HS256SecretRef: "JWT_HS256_SECRET"}},
		{Mode: "jwt", JWT: &JWTAuthConfig{Alg: "rs256", RS256PublicKeyRef: "JWT_RS256_PUBLIC_KEY"}},
		{Mode: "both", APIKey: &APIKeyAuthConfig{SecretRef: "API_KEY_HASH"}, JWT: &JWTAuthConfig{Alg: "HS256", HS256SecretRef: "JWT_HS256_SECRET"}},
	}
	for i, cfg := range valid {
		if err := cfg.Validate(); err != nil {
			t.Errorf("valid[%d] rejected: %v", i, err)
		}
	}

	invalid := []*AuthConfig{
		nil,
		{},
		{Mode: "basic"},
		{Mode: "api_key"},
		{Mode: "api_key", APIKey: &APIKeyAuthConfig{Enabled: boolPtr(false), SecretRef: "API_KEY_HASH"}},
		{Mode: "api_key", APIKey: &APIKeyAuthConfig{}},
		{Mode: "jwt"},
		{Mode: "jwt", JWT: &JWTAuthConfig{Alg: "HS384", HS256SecretRef: "JWT_HS256_SECRET"}},
		{Mode: "jwt", JWT: &JWTAuthConfig{Alg: "none"}},
		{Mode: "jwt", JWT: &JWTAuthConfig{Alg: "HS256"}},
		{Mode: "jwt", JWT: &JWTAuthConfig{Alg: "RS256"}},
		{Mode: "jwt", JWT: &JWTAuthConfig{Alg: "HS256", HS256SecretRef: "JWT_HS256_SECRET", Leeway: "soon"}},
		{Mode: "both", APIKey: &APIKeyAuthConfig{SecretRef: "API_KEY_HASH"}},
	}
	for i, cfg := range invalid {
		if err := cfg.Validate(); err == nil {
			t.Errorf("invalid[%d] accepted: %+v", i, cfg)
		}
	}
}

func TestAuthConfigApplyDefaults(t *testing.T) {
	cfg := &AuthConfig{Mode: " JWT ", JWT: &JWTAuthConfig{Alg: "hs256", HS256SecretRef: "S"}}
	cfg.ApplyDefaults()
	if cfg.Mode != AuthModeJWT {
		t.Errorf("mode = %q", cfg.Mode)
	}
	if cfg.JWT.Alg != JWTAlgHS256 {
		t.Errorf("alg = %q", cfg.JWT.Alg)
	}
	if cfg.JWT.AuthorizationHeader != DefaultAuthorizationHd {
		t.Errorf("authorization header = %q", cfg.JWT.AuthorizationHeader)
	}

	apiCfg := &AuthConfig{Mode: "api_key", APIKey: &APIKeyAuthConfig{SecretRef: "S"}}
	apiCfg.ApplyDefaults()
	if apiCfg.APIKey.Header != DefaultAPIKeyHeader {
		t.Errorf("api key header = %q", apiCfg.APIKey.Header)
	}
}

func TestAuthConfigDeepCopy(t *testing.T) {
	orig := &AuthConfig{
		Mode:   "both",
		APIKey: &APIKeyAuthConfig{Enabled: boolPtr(true), Header: "X-Key", SecretRef: "A"},
		JWT:    &JWTAuthConfig{Enabled: boolPtr(true), Alg: "HS256", HS256SecretRef: "J"},
	}
	cp := orig.DeepCopy()
	cp.Mode = "jwt"
	cp.APIKey.Header = "changed"
	*cp.JWT.Enabled = false
	if orig.Mode != "both" || orig.APIKey.Header != "X-Key" || !*orig.JWT.Enabled {
		t.Fatal("DeepCopy mutated original")
	}
	if orig.DeepCopy() == orig || orig.DeepCopy().APIKey == orig.APIKey {
		t.Fatal("DeepCopy must allocate independent structs")
	}
}

func TestAuthConfigFromPluginConfig(t *testing.T) {
	legacyJWT, err := AuthConfigFromPluginConfig(map[string]interface{}{
		"type": "jwt", "alg": "HS256", "jwt_secret_ref": "JWT_HS256_SECRET",
	})
	if err != nil {
		t.Fatalf("legacy jwt: %v", err)
	}
	if legacyJWT.Mode != AuthModeJWT || legacyJWT.JWT.HS256SecretRef != "JWT_HS256_SECRET" {
		t.Fatalf("unexpected jwt config: %+v", legacyJWT)
	}

	legacyAPIKey, err := AuthConfigFromPluginConfig(map[string]interface{}{"type": "apikey"})
	if err != nil {
		t.Fatalf("legacy api key: %v", err)
	}
	if legacyAPIKey.Mode != AuthModeAPIKey || legacyAPIKey.APIKey.SecretRef != LegacyAPIKeySecretRef {
		t.Fatalf("unexpected api key config: %+v", legacyAPIKey)
	}

	both, err := AuthConfigFromPluginConfig(map[string]interface{}{
		"mode": "both", "alg": "RS256",
		"api_key_header":           "X-Custom-Key",
		"api_key_secret_ref":       "API_KEY_HASH",
		"jwt_public_key_ref":       "JWT_RS256_PUBLIC_KEY",
		"issuer":                   "issuer",
		"audience":                 "audience",
		"jwt_authorization_header": "X-Token",
	})
	if err != nil {
		t.Fatalf("both: %v", err)
	}
	if both.APIKey.Header != "X-Custom-Key" || both.JWT.AuthorizationHeader != "X-Token" {
		t.Fatalf("headers not mapped: %+v", both)
	}

	for name, cfg := range map[string]map[string]interface{}{
		"plaintext jwt":   {"type": "jwt", "alg": "HS256", "jwt_key": "secret"},
		"plaintext api":   {"type": "api_key", "api_key": "secret"},
		"missing alg":     {"type": "jwt"},
		"unsupported alg": {"type": "jwt", "alg": "HS512"},
	} {
		if _, err := AuthConfigFromPluginConfig(cfg); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}

func TestAuthConfigValidationErrorsAreActionable(t *testing.T) {
	err := (&AuthConfig{Mode: "jwt", JWT: &JWTAuthConfig{Alg: "HS512", HS256SecretRef: "S"}}).Validate()
	if err == nil || !strings.Contains(err.Error(), "unsupported") {
		t.Fatalf("expected unsupported algorithm error, got %v", err)
	}
}
