package middleware

import (
	"crypto/subtle"
	"fmt"
	"net/http"
	"strings"

	"github.com/golang-jwt/jwt/v5"
	"github.com/haiser1/go-api-gateway/internal/gateway"
	"github.com/haiser1/go-api-gateway/internal/secret"
	"github.com/rs/zerolog/log"
)

// NewAuthPlugin creates a pre-compiled auth middleware from config.
//
// The plugin only executes on the hot path when PluginConfig.Enabled is true
// (gated in buildMiddlewareChain — that is the auth ON/OFF switch). When the
// chain compiles the plugin (auth ON), the .env file is provisioned if missing
// and its secrets are read ONCE at build time, never per request:
//
//   - type: jwt     -> JWT_SECRET     (default signing alg: HS256)
//   - type: api_key -> API_KEY_SECRET (checked against the API key header)
//
// Config (all optional):
//   - type:            "jwt" (default) or "api_key"
//   - alg:             JWT signing algorithm, default "HS256"; only HS256,
//                      HS384 and HS512 are accepted
//   - api_key_header:  header carrying the API key, default "X-API-Key"
//   - jwt_key/api_key: rarely-used in-config fallback when the .env file is
//                      unavailable — secrets should normally live in .env
func NewAuthPlugin(config map[string]interface{}) gateway.PluginMiddleware {
	authType, _ := config["type"].(string)
	if authType == "" {
		authType = "jwt"
	}
	authType = strings.ToLower(strings.TrimSpace(authType))
	switch authType {
	case "jwt", "api_key", "apikey", "api-key":
	default:
		log.Warn().Str("type", authType).
			Msg("Unknown auth plugin type, falling back to 'jwt'")
		authType = "jwt"
	}

	// JWT signing algorithm (default HS256).
	jwtAlg := "HS256"
	if a, ok := config["alg"].(string); ok && a != "" {
		jwtAlg = strings.ToUpper(strings.TrimSpace(a))
	}
	switch jwtAlg {
	case "HS256", "HS384", "HS512":
	default:
		log.Warn().Str("alg", jwtAlg).
			Msg("Unsupported JWT algorithm, falling back to 'HS256'")
		jwtAlg = "HS256"
	}

	apiKeyHeader := "X-API-Key"
	if h, ok := config["api_key_header"].(string); ok && h != "" {
		apiKeyHeader = h
	}

	// Secrets are resolved ONCE at chain-build time (config load/reload),
	// keeping the per-request hot path allocation-free. Explicit in-config
	// keys act as a fallback so existing deployments keep working when the
	// .env file cannot be provisioned (e.g. read-only filesystem).
	resolvedSecret := ""
	envPath := secret.DefaultPath()
	if created, err := secret.EnsureEnvFile(envPath); err != nil {
		log.Warn().Err(err).Str("path", envPath).
			Msg("Failed to provision .env secret file for auth plugin")
	} else {
		if created {
			log.Info().Str("path", envPath).
				Msg("Auth plugin compiled: .env created with generated API_KEY_SECRET and JWT_SECRET")
		}
		if secrets, lErr := secret.LoadFile(envPath); lErr != nil {
			log.Warn().Err(lErr).Str("path", envPath).
				Msg("Failed to read .env secret file for auth plugin")
		} else if authType == "jwt" {
			resolvedSecret = secrets[secret.JWTSecret]
		} else {
			resolvedSecret = secrets[secret.APIKeySecret]
		}
	}
	if resolvedSecret == "" && authType == "jwt" {
		resolvedSecret, _ = config["jwt_key"].(string)
	}
	if resolvedSecret == "" && authType != "jwt" {
		resolvedSecret, _ = config["api_key"].(string)
	}

	// Fail-safe: without a secret the plugin cannot enforce anything, so it
	// short-circuits to pass-through (same behavior as the legacy plugin).
	if resolvedSecret == "" {
		log.Warn().Str("type", authType).
			Msg("authorization plugin disabled: no auth secret available (.env empty and no in-config key)")
		return gateway.PluginFunc{
			NameStr: "auth-disabled",
			Handler: func(w http.ResponseWriter, r *http.Request, next http.Handler) bool {
				next.ServeHTTP(w, r)
				return true
			},
		}
	}

	if authType == "jwt" {
		return gateway.PluginFunc{
			NameStr: "auth",
			Handler: func(w http.ResponseWriter, r *http.Request, next http.Handler) bool {
				authHeader := r.Header.Get("Authorization")
				if authHeader == "" {
					http.Error(w, "Authorization header required", http.StatusUnauthorized)
					return false
				}

				parts := strings.Split(authHeader, " ")
				if len(parts) != 2 || strings.ToLower(parts[0]) != "bearer" {
					http.Error(w, "Invalid Authorization header format", http.StatusUnauthorized)
					return false
				}

				// WithValidMethods hard-restricts the accepted signature
				// algorithm to the configured one (HS256 by default).
				token, err := jwt.Parse(parts[1], func(t *jwt.Token) (interface{}, error) {
					if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
						return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
					}
					return []byte(resolvedSecret), nil
				}, jwt.WithValidMethods([]string{jwtAlg}))

				if err != nil {
					http.Error(w, fmt.Sprintf("Unauthorized: %v", err), http.StatusUnauthorized)
					return false
				}
				if !token.Valid {
					http.Error(w, "Unauthorized: Invalid token", http.StatusUnauthorized)
					return false
				}

				next.ServeHTTP(w, r)
				return true
			},
		}
	}

	// authType == api_key
	return gateway.PluginFunc{
		NameStr: "auth",
		Handler: func(w http.ResponseWriter, r *http.Request, next http.Handler) bool {
			provided := r.Header.Get(apiKeyHeader)
			if provided == "" {
				http.Error(w, fmt.Sprintf("%s header required", apiKeyHeader), http.StatusUnauthorized)
				return false
			}
			// Constant-time comparison to avoid timing side channels.
			if subtle.ConstantTimeCompare([]byte(provided), []byte(resolvedSecret)) != 1 {
				http.Error(w, "Unauthorized: Invalid API key", http.StatusUnauthorized)
				return false
			}
			next.ServeHTTP(w, r)
			return true
		},
	}
}

func init() {
	gateway.RegisterPlugin("auth", NewAuthPlugin)
	gateway.RegisterPlugin("authorization", NewAuthPlugin)
}
