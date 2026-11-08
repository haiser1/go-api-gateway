package auth

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/haiser1/go-api-gateway/internal/config"
	"github.com/haiser1/go-api-gateway/internal/secrets"
)

// Runtime is an immutable compiled authentication policy.
type Runtime struct {
	mode   string
	apiKey *APIKeyVerifier
	jwt    *JWTVerifier
}

// BuildRuntime resolves secret references and compiles the authentication
// policy. A nil config means authentication is disabled.
func BuildRuntime(cfg *config.AuthConfig, provider secrets.Provider) (*Runtime, error) {
	if cfg == nil {
		return nil, nil
	}
	if provider == nil {
		return nil, errors.New("secret provider is required when auth is configured")
	}

	resolved := cfg.DeepCopy()
	resolved.ApplyDefaults()
	if err := resolved.Validate(); err != nil {
		return nil, err
	}

	runtime := &Runtime{mode: resolved.Mode}

	if resolved.Mode == config.AuthModeAPIKey || resolved.Mode == config.AuthModeBoth {
		value, err := resolveSecret(provider, resolved.APIKey.SecretRef)
		if err != nil {
			return nil, err
		}
		verifier, err := NewAPIKeyVerifier(value, resolved.APIKey.Header)
		if err != nil {
			return nil, fmt.Errorf("build api key verifier: %w", err)
		}
		runtime.apiKey = verifier
	}

	if resolved.Mode == config.AuthModeJWT || resolved.Mode == config.AuthModeBoth {
		opts := JWTOptions{
			Algorithm: resolved.JWT.Alg,
			Header:    resolved.JWT.AuthorizationHeader,
			Issuer:    resolved.JWT.Issuer,
			Audience:  resolved.JWT.Audience,
			KeyID:     resolved.JWT.KeyID,
		}
		leeway, err := resolved.JWT.LeewayDuration()
		if err != nil {
			return nil, err
		}
		opts.Leeway = leeway

		switch resolved.JWT.Alg {
		case config.JWTAlgHS256:
			value, err := resolveSecret(provider, resolved.JWT.HS256SecretRef)
			if err != nil {
				return nil, err
			}
			verifier, err := NewHS256Verifier([]byte(value), opts)
			if err != nil {
				return nil, err
			}
			runtime.jwt = verifier
		case config.JWTAlgRS256:
			value, err := resolveSecret(provider, resolved.JWT.RS256PublicKeyRef)
			if err != nil {
				return nil, err
			}
			publicKey, err := ParseRSAPublicKey(value)
			if err != nil {
				return nil, fmt.Errorf("build RS256 verifier: %w", err)
			}
			verifier, err := NewRS256Verifier(publicKey, opts)
			if err != nil {
				return nil, err
			}
			runtime.jwt = verifier
		default:
			return nil, fmt.Errorf("unsupported JWT algorithm %q", resolved.JWT.Alg)
		}
	}

	return runtime, nil
}

// Mode returns the normalized authentication mode.
func (r *Runtime) Mode() string {
	if r == nil {
		return ""
	}
	return r.mode
}

// Enabled reports whether authentication is active.
func (r *Runtime) Enabled() bool {
	return r != nil && (r.apiKey != nil || r.jwt != nil)
}

// HasAPIKey reports whether API-key authentication is active.
func (r *Runtime) HasAPIKey() bool {
	return r != nil && r.apiKey != nil
}

// HasJWT reports whether JWT authentication is active.
func (r *Runtime) HasJWT() bool {
	return r != nil && r.jwt != nil
}

// Authenticate validates the request credentials. A disabled runtime returns a
// nil Context and no error.
func (r *Runtime) Authenticate(req *http.Request) (*Context, error) {
	if !r.Enabled() {
		return nil, nil
	}

	result := &Context{}

	if r.apiKey != nil {
		raw := req.Header.Get(r.apiKey.Header())
		if raw == "" {
			return nil, ErrAPIKeyRequired
		}
		if !r.apiKey.Verify(raw) {
			return nil, ErrInvalidAPIKey
		}
		result.Machine = &MachineIdentity{APIKeyID: "api-key"}
	}

	if r.jwt != nil {
		header := req.Header.Get(r.jwt.Header())
		if header == "" {
			return nil, ErrAuthorizationHeaderRequired
		}
		token, ok := bearerToken(header)
		if !ok {
			return nil, ErrInvalidAuthorizationHeader
		}
		user, err := r.jwt.Verify(token)
		if err != nil {
			return nil, ErrInvalidToken
		}
		result.User = user
	}

	return result, nil
}

func resolveSecret(provider secrets.Provider, ref string) (string, error) {
	value, err := provider.Get(ref)
	if err != nil {
		if errors.Is(err, secrets.ErrNotFound) {
			return "", fmt.Errorf("secret %q is not configured", ref)
		}
		return "", fmt.Errorf("read secret %q: %w", ref, err)
	}
	if strings.TrimSpace(value) == "" {
		return "", fmt.Errorf("secret %q is empty", ref)
	}
	return value, nil
}

func bearerToken(header string) (string, bool) {
	parts := strings.SplitN(strings.TrimSpace(header), " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return "", false
	}
	token := strings.TrimSpace(parts[1])
	if token == "" {
		return "", false
	}
	return token, true
}
