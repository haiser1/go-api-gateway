package config

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"
)

// Authentication modes.
const (
	AuthModeAPIKey = "api_key"
	AuthModeJWT    = "jwt"
	AuthModeBoth   = "both"
)

// Supported JWT algorithms.
const (
	JWTAlgHS256 = "HS256"
	JWTAlgRS256 = "RS256"
)

// Default secret references used by the legacy plugin-config adapter.
const (
	LegacyAPIKeySecretRef  = "API_KEY_SECRET"
	LegacyJWTSecretRef     = "JWT_SECRET"
	LegacyRSAPublicKeyRef  = "JWT_RSA_PUBLIC_KEY"
	DefaultAPIKeyHeader    = "X-API-Key"
	DefaultAuthorizationHd = "Authorization"
)

// AuthConfig is the gateway authentication policy. It contains only settings
// and secret references; credentials live in the secret provider.
type AuthConfig struct {
	Mode   string            `yaml:"mode" json:"mode" validate:"required"`
	APIKey *APIKeyAuthConfig `yaml:"api_key,omitempty" json:"api_key,omitempty"`
	JWT    *JWTAuthConfig    `yaml:"jwt,omitempty" json:"jwt,omitempty"`
}

// APIKeyAuthConfig configures machine authentication.
type APIKeyAuthConfig struct {
	Enabled   *bool  `yaml:"enabled,omitempty" json:"enabled,omitempty"`
	Header    string `yaml:"header,omitempty" json:"header,omitempty"`
	SecretRef string `yaml:"secret_ref,omitempty" json:"secret_ref,omitempty"`
}

// JWTAuthConfig configures user authentication.
type JWTAuthConfig struct {
	Enabled             *bool  `yaml:"enabled,omitempty" json:"enabled,omitempty"`
	Alg                 string `yaml:"alg,omitempty" json:"alg,omitempty"`
	AuthorizationHeader string `yaml:"authorization_header,omitempty" json:"authorization_header,omitempty"`
	HS256SecretRef      string `yaml:"hs256_secret_ref,omitempty" json:"hs256_secret_ref,omitempty"`
	RS256PublicKeyRef   string `yaml:"rs256_public_key_ref,omitempty" json:"rs256_public_key_ref,omitempty"`
	Issuer              string `yaml:"issuer,omitempty" json:"issuer,omitempty"`
	Audience            string `yaml:"audience,omitempty" json:"audience,omitempty"`
	KeyID               string `yaml:"kid,omitempty" json:"kid,omitempty"`
	Leeway              string `yaml:"leeway,omitempty" json:"leeway,omitempty"`
}

func boolOrDefault(v *bool) bool {
	if v == nil {
		return true
	}
	return *v
}

// IsEnabled reports whether the API-key block is enabled (default true).
func (c *APIKeyAuthConfig) IsEnabled() bool {
	return c != nil && boolOrDefault(c.Enabled)
}

// IsEnabled reports whether the JWT block is enabled (default true).
func (c *JWTAuthConfig) IsEnabled() bool {
	return c != nil && boolOrDefault(c.Enabled)
}

// NormalizedMode returns the canonical lowercase mode.
func NormalizedMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "apikey", "api-key":
		return AuthModeAPIKey
	default:
		return strings.ToLower(strings.TrimSpace(mode))
	}
}

// NormalizedAlg returns the canonical uppercase algorithm.
func NormalizedAlg(alg string) string {
	return strings.ToUpper(strings.TrimSpace(alg))
}

// ApplyDefaults fills optional defaults in place. It does not invent secret
// references: a missing reference remains missing and fails validation.
func (a *AuthConfig) ApplyDefaults() {
	if a == nil {
		return
	}
	a.Mode = NormalizedMode(a.Mode)
	if a.APIKey != nil {
		if strings.TrimSpace(a.APIKey.Header) == "" {
			a.APIKey.Header = DefaultAPIKeyHeader
		}
	}
	if a.JWT != nil {
		a.JWT.Alg = NormalizedAlg(a.JWT.Alg)
		if strings.TrimSpace(a.JWT.AuthorizationHeader) == "" {
			a.JWT.AuthorizationHeader = DefaultAuthorizationHd
		}
	}
}

// DeepCopy creates an independent copy of AuthConfig.
func (a *AuthConfig) DeepCopy() *AuthConfig {
	if a == nil {
		return nil
	}
	cp := &AuthConfig{Mode: a.Mode}
	if a.APIKey != nil {
		apiKey := *a.APIKey
		if a.APIKey.Enabled != nil {
			enabled := *a.APIKey.Enabled
			apiKey.Enabled = &enabled
		}
		cp.APIKey = &apiKey
	}
	if a.JWT != nil {
		jwtCfg := *a.JWT
		if a.JWT.Enabled != nil {
			enabled := *a.JWT.Enabled
			jwtCfg.Enabled = &enabled
		}
		cp.JWT = &jwtCfg
	}
	return cp
}

// LeewayDuration parses the configured clock-skew allowance.
func (c *JWTAuthConfig) LeewayDuration() (time.Duration, error) {
	if c == nil || strings.TrimSpace(c.Leeway) == "" {
		return 0, nil
	}
	d, err := time.ParseDuration(strings.TrimSpace(c.Leeway))
	if err != nil {
		return 0, fmt.Errorf("invalid jwt leeway %q: %w", c.Leeway, err)
	}
	if d < 0 {
		return 0, fmt.Errorf("jwt leeway %q must not be negative", c.Leeway)
	}
	return d, nil
}

// Validate checks the authentication policy's cross-field rules.
func (a *AuthConfig) Validate() error {
	if a == nil {
		return errors.New("auth configuration is required")
	}

	mode := NormalizedMode(a.Mode)
	switch mode {
	case AuthModeAPIKey, AuthModeJWT, AuthModeBoth:
	case "":
		return errors.New("auth.mode is required")
	default:
		return fmt.Errorf("auth.mode %q must be one of: %s, %s, %s", a.Mode, AuthModeAPIKey, AuthModeJWT, AuthModeBoth)
	}

	if mode == AuthModeAPIKey || mode == AuthModeBoth {
		if a.APIKey == nil {
			return fmt.Errorf("auth.mode %q requires an api_key configuration", mode)
		}
		if !a.APIKey.IsEnabled() {
			return fmt.Errorf("auth.mode %q requires api_key.enabled to be true", mode)
		}
		if strings.TrimSpace(a.APIKey.SecretRef) == "" {
			return errors.New("auth.api_key.secret_ref is required")
		}
	}

	if mode == AuthModeJWT || mode == AuthModeBoth {
		if a.JWT == nil {
			return fmt.Errorf("auth.mode %q requires a jwt configuration", mode)
		}
		if !a.JWT.IsEnabled() {
			return fmt.Errorf("auth.mode %q requires jwt.enabled to be true", mode)
		}
		switch NormalizedAlg(a.JWT.Alg) {
		case JWTAlgHS256:
			if strings.TrimSpace(a.JWT.HS256SecretRef) == "" {
				return errors.New("auth.jwt.hs256_secret_ref is required for HS256")
			}
		case JWTAlgRS256:
			if strings.TrimSpace(a.JWT.RS256PublicKeyRef) == "" {
				return errors.New("auth.jwt.rs256_public_key_ref is required for RS256")
			}
		case "":
			return errors.New("auth.jwt.alg is required (HS256 or RS256)")
		default:
			return fmt.Errorf("auth.jwt.alg %q is unsupported; must be %s or %s", a.JWT.Alg, JWTAlgHS256, JWTAlgRS256)
		}
		if _, err := a.JWT.LeewayDuration(); err != nil {
			return err
		}
	}

	return nil
}

// AuthConfigFromPluginConfig converts the legacy map-based auth plugin
// configuration into the typed policy. It accepts the old flat keys and the
// newer nested shape. Plaintext credentials are rejected.
func AuthConfigFromPluginConfig(m map[string]interface{}) (*AuthConfig, error) {
	if m == nil {
		return nil, errors.New("auth plugin config is empty; configure auth.mode and credentials references")
	}

	cfg := &AuthConfig{}

	mode := firstString(m, "mode", "type")
	if mode == "" {
		mode = AuthModeJWT // legacy default
	}
	cfg.Mode = NormalizedMode(mode)

	// API key settings: nested map preferred, flat legacy keys accepted.
	apiKey := &APIKeyAuthConfig{}
	apiKeyPresent := false
	if raw, ok := m["api_key"]; ok {
		switch v := raw.(type) {
		case string:
			if strings.TrimSpace(v) != "" {
				return nil, errors.New("plaintext api_key is not allowed in config.yaml; use api_key.secret_ref")
			}
		case map[string]interface{}:
			apiKeyPresent = true
			if enabled, ok := boolValue(v["enabled"]); ok {
				apiKey.Enabled = &enabled
			}
			apiKey.Header = stringValue(v["header"])
			apiKey.SecretRef = stringValue(v["secret_ref"])
		case map[interface{}]interface{}:
			apiKeyPresent = true
			converted := make(map[string]interface{}, len(v))
			for key, value := range v {
				converted[fmt.Sprint(key)] = value
			}
			if enabled, ok := boolValue(converted["enabled"]); ok {
				apiKey.Enabled = &enabled
			}
			apiKey.Header = stringValue(converted["header"])
			apiKey.SecretRef = stringValue(converted["secret_ref"])
		}
	}
	if _, ok := m["jwt_key"]; ok {
		return nil, errors.New("plaintext jwt_key is not allowed in config.yaml; use a secret reference")
	}
	if apiKey.Header == "" {
		apiKey.Header = stringValue(m["api_key_header"])
	}
	if apiKey.SecretRef == "" {
		apiKey.SecretRef = stringValue(m["api_key_secret_ref"])
	}
	if apiKey.SecretRef == "" {
		apiKey.SecretRef = LegacyAPIKeySecretRef
	}
	if apiKey.Header == "" {
		apiKey.Header = DefaultAPIKeyHeader
	}
	if apiKeyPresent || cfg.Mode == AuthModeAPIKey || cfg.Mode == AuthModeBoth {
		cfg.APIKey = apiKey
	}

	// JWT settings.
	jwtCfg := &JWTAuthConfig{}
	if raw, ok := m["jwt"]; ok {
		nested := map[string]interface{}{}
		switch v := raw.(type) {
		case map[string]interface{}:
			nested = v
		case map[interface{}]interface{}:
			for key, value := range v {
				nested[fmt.Sprint(key)] = value
			}
		}
		if _, plaintext := nested["jwt_key"]; plaintext {
			return nil, errors.New("plaintext jwt_key is not allowed in config.yaml; use a secret reference")
		}
		if enabled, ok := boolValue(nested["enabled"]); ok {
			jwtCfg.Enabled = &enabled
		}
		jwtCfg.Alg = stringValue(nested["alg"])
		jwtCfg.AuthorizationHeader = stringValue(nested["authorization_header"])
		jwtCfg.HS256SecretRef = stringValue(nested["hs256_secret_ref"])
		jwtCfg.RS256PublicKeyRef = stringValue(nested["rs256_public_key_ref"])
		jwtCfg.Issuer = stringValue(nested["issuer"])
		jwtCfg.Audience = stringValue(nested["audience"])
		jwtCfg.KeyID = stringValue(nested["kid"])
		jwtCfg.Leeway = stringValue(nested["leeway"])
	}
	if jwtCfg.Alg == "" {
		jwtCfg.Alg = stringValue(m["alg"])
	}
	if alg := NormalizedAlg(jwtCfg.Alg); alg != "" && alg != JWTAlgHS256 && alg != JWTAlgRS256 {
		return nil, fmt.Errorf("auth plugin alg %q is unsupported; must be %s or %s", jwtCfg.Alg, JWTAlgHS256, JWTAlgRS256)
	}
	if jwtCfg.AuthorizationHeader == "" {
		jwtCfg.AuthorizationHeader = stringValue(m["jwt_authorization_header"])
	}
	if jwtCfg.HS256SecretRef == "" {
		jwtCfg.HS256SecretRef = stringValue(m["jwt_secret_ref"])
	}
	if jwtCfg.RS256PublicKeyRef == "" {
		jwtCfg.RS256PublicKeyRef = stringValue(m["jwt_public_key_ref"])
	}
	if jwtCfg.Issuer == "" {
		jwtCfg.Issuer = stringValue(m["issuer"])
	}
	if jwtCfg.Audience == "" {
		jwtCfg.Audience = stringValue(m["audience"])
	}
	if jwtCfg.KeyID == "" {
		jwtCfg.KeyID = stringValue(m["kid"])
	}
	if jwtCfg.Leeway == "" {
		jwtCfg.Leeway = stringValue(m["leeway"])
	}
	if jwtCfg.AuthorizationHeader == "" {
		jwtCfg.AuthorizationHeader = DefaultAuthorizationHd
	}
	if jwtCfg.HS256SecretRef == "" {
		jwtCfg.HS256SecretRef = LegacyJWTSecretRef
	}
	if jwtCfg.RS256PublicKeyRef == "" {
		jwtCfg.RS256PublicKeyRef = LegacyRSAPublicKeyRef
	}
	if cfg.Mode == AuthModeJWT || cfg.Mode == AuthModeBoth || jwtCfg.Alg != "" {
		cfg.JWT = jwtCfg
	}

	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

func firstString(m map[string]interface{}, keys ...string) string {
	for _, key := range keys {
		if value := stringValue(m[key]); value != "" {
			return value
		}
	}
	return ""
}

func stringValue(v interface{}) string {
	if s, ok := v.(string); ok {
		return strings.TrimSpace(s)
	}
	return ""
}

func boolValue(v interface{}) (bool, bool) {
	if b, ok := v.(bool); ok {
		return b, true
	}
	return false, false
}

// ResolveAuthConfig returns the single authentication policy for cfg. The
// canonical source is the top-level auth block; legacy auth plugin
// configuration is accepted only as a migration adapter and must be uniform
// across every scope where it is enabled.
func ResolveAuthConfig(cfg *Config) (*AuthConfig, error) {
	if cfg == nil {
		return nil, nil
	}

	for _, plugin := range cfg.GlobalPlugins {
		if plugin.Name == "auth" || plugin.Name == "authorization" {
			return nil, errors.New("auth plugin is not allowed at global level; configure auth at service or route level")
		}
	}

	var legacy *AuthConfig
	collect := func(scope string, plugins []PluginConfig) error {
		for _, plugin := range plugins {
			if !plugin.Enabled || (plugin.Name != "auth" && plugin.Name != "authorization") {
				continue
			}
			if len(plugin.Config) == 0 {
				continue
			}
			parsed, err := AuthConfigFromPluginConfig(plugin.Config)
			if err != nil {
				return fmt.Errorf("%s auth plugin: %w", scope, err)
			}
			if legacy == nil {
				legacy = parsed
				continue
			}
			if !authConfigsEqual(legacy, parsed) {
				return fmt.Errorf("%s auth plugin conflicts with another auth plugin configuration; configure consistent credentials across services and routes", scope)
			}
		}
		return nil
	}

	for _, service := range cfg.Services {
		if err := collect(fmt.Sprintf("service %q", service.Name), service.Plugins); err != nil {
			return nil, err
		}
	}
	for _, route := range cfg.Routes {
		if err := collect(fmt.Sprintf("route %q", route.Name), route.Plugins); err != nil {
			return nil, err
		}
	}

	if cfg.Auth != nil {
		if legacy != nil && !authConfigsEqual(cfg.Auth, legacy) {
			return nil, errors.New("auth is configured both in the top-level auth block and as a plugin with conflicting settings")
		}
		return cfg.Auth, nil
	}
	return legacy, nil
}

func authConfigsEqual(a, b *AuthConfig) bool {
	return reflect.DeepEqual(canonicalAuthConfig(a), canonicalAuthConfig(b))
}

func canonicalAuthConfig(cfg *AuthConfig) *AuthConfig {
	cp := cfg.DeepCopy()
	if cp == nil {
		return nil
	}
	cp.ApplyDefaults()
	if cp.APIKey != nil {
		cp.APIKey.Enabled = nil
	}
	if cp.JWT != nil {
		cp.JWT.Enabled = nil
	}
	return cp
}
