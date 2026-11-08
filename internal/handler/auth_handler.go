package handler

import (
	"encoding/base64"
	"errors"
	"net/http"
	"strings"

	"github.com/haiser1/go-api-gateway/internal/auth"
	"github.com/haiser1/go-api-gateway/internal/config"
	"github.com/haiser1/go-api-gateway/internal/domain"
	"github.com/haiser1/go-api-gateway/internal/helper"
	"github.com/haiser1/go-api-gateway/internal/secrets"
	"github.com/julienschmidt/httprouter"
)

// GetAuthConfig returns the effective authentication policy. Only secret
// references are returned; credential values are never exposed.
func (h *ManagementHandler) GetAuthConfig(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	cfg := h.Manager.GetConfig()
	effective, err := config.ResolveAuthConfig(cfg)
	if err != nil {
		helper.RespondError(w, http.StatusConflict, "Auth configuration conflict", err)
		return
	}
	if effective != nil {
		effective = effective.DeepCopy()
		effective.ApplyDefaults()
	}
	helper.RespondSuccess(w, http.StatusOK, "Auth configuration fetched successfully", effective)
}

// UpdateAPIKeyAuth stores a hashed API key and enables API-key authentication.
func (h *ManagementHandler) UpdateAPIKeyAuth(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	var req domain.UpdateAPIKeyAuthRequest
	if !decodeAndValidate(w, r, &req) {
		return
	}
	if len([]byte(req.Secret)) < 16 {
		helper.RespondError(w, http.StatusBadRequest, "API key must be at least 16 bytes", nil)
		return
	}
	if strings.ContainsAny(req.Secret, "\x00\r\n") {
		helper.RespondError(w, http.StatusBadRequest, "API key must not contain newlines or NUL bytes", nil)
		return
	}

	h.updateAuth(w, config.AuthModeAPIKey, secrets.APIKeyHashKey, secrets.HashSecret(req.Secret), func(cfg *config.AuthConfig) {
		if cfg.APIKey == nil {
			cfg.APIKey = &config.APIKeyAuthConfig{}
		}
		enabled := true
		cfg.APIKey.Enabled = &enabled
		cfg.APIKey.SecretRef = secrets.APIKeyHashKey
		if strings.TrimSpace(req.Header) != "" {
			cfg.APIKey.Header = strings.TrimSpace(req.Header)
		}
	})
}

// UpdateHS256Auth stores an HS256 signing secret and enables JWT auth.
func (h *ManagementHandler) UpdateHS256Auth(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	var req domain.UpdateHS256AuthRequest
	if !decodeAndValidate(w, r, &req) {
		return
	}
	if len([]byte(req.Secret)) < 32 {
		helper.RespondError(w, http.StatusBadRequest, "HS256 secret must be at least 32 bytes", nil)
		return
	}
	if strings.ContainsAny(req.Secret, "\x00\r\n") {
		helper.RespondError(w, http.StatusBadRequest, "HS256 secret must not contain newlines or NUL bytes", nil)
		return
	}

	h.updateAuth(w, config.AuthModeJWT, secrets.JWTHS256SecretKey, req.Secret, func(cfg *config.AuthConfig) {
		if cfg.JWT == nil {
			cfg.JWT = &config.JWTAuthConfig{}
		}
		enabled := true
		cfg.JWT.Enabled = &enabled
		cfg.JWT.Alg = config.JWTAlgHS256
		cfg.JWT.HS256SecretRef = secrets.JWTHS256SecretKey
		cfg.JWT.RS256PublicKeyRef = ""
		applyJWTOptions(cfg.JWT, req.Header, req.Issuer, req.Audience, req.KeyID, req.Leeway)
	})
}

// UpdateRS256Auth stores an RS256 public key and enables JWT auth.
func (h *ManagementHandler) UpdateRS256Auth(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	var req domain.UpdateRS256AuthRequest
	if !decodeAndValidate(w, r, &req) {
		return
	}
	if _, err := auth.ParseRSAPublicKey(req.PublicKey); err != nil {
		helper.RespondError(w, http.StatusBadRequest, "Invalid RSA public key", err)
		return
	}
	stored, err := encodePublicKeyForStorage(req.PublicKey)
	if err != nil {
		helper.RespondError(w, http.StatusBadRequest, "Invalid RSA public key", err)
		return
	}

	h.updateAuth(w, config.AuthModeJWT, secrets.JWTRS256PublicKeyKey, stored, func(cfg *config.AuthConfig) {
		if cfg.JWT == nil {
			cfg.JWT = &config.JWTAuthConfig{}
		}
		enabled := true
		cfg.JWT.Enabled = &enabled
		cfg.JWT.Alg = config.JWTAlgRS256
		cfg.JWT.RS256PublicKeyRef = secrets.JWTRS256PublicKeyKey
		cfg.JWT.HS256SecretRef = ""
		applyJWTOptions(cfg.JWT, req.Header, req.Issuer, req.Audience, req.KeyID, req.Leeway)
	})
}

func (h *ManagementHandler) updateAuth(w http.ResponseWriter, newMode, secretKey, storedValue string, mutate func(*config.AuthConfig)) {
	if h.SecretProvider == nil {
		helper.RespondError(w, http.StatusServiceUnavailable, "Secret provider is not configured", nil)
		return
	}

	h.authMu.Lock()
	defer h.authMu.Unlock()

	cfg := h.Manager.GetConfig()
	if cfg == nil {
		helper.RespondError(w, http.StatusInternalServerError, "Configuration is unavailable", nil)
		return
	}

	candidate := cfg.Auth.DeepCopy()
	if candidate == nil {
		candidate = &config.AuthConfig{Mode: newMode}
	} else {
		switch currentMode := config.NormalizedMode(candidate.Mode); currentMode {
		case "", newMode:
			candidate.Mode = newMode
		case config.AuthModeBoth:
			// Keep both: adding a credential must not disable the other one.
		default:
			candidate.Mode = config.AuthModeBoth
		}
	}
	mutate(candidate)
	candidate.ApplyDefaults()
	if err := candidate.Validate(); err != nil {
		helper.RespondError(w, http.StatusBadRequest, "Invalid auth configuration", err)
		return
	}

	oldValue, oldErr := h.SecretProvider.Get(secretKey)
	if err := h.SecretProvider.SetMany(map[string]string{secretKey: storedValue}); err != nil {
		helper.RespondError(w, http.StatusInternalServerError, "Failed to store credential", err)
		return
	}

	if err := h.Manager.UpdateAuthConfig(candidate); err != nil {
		if oldErr == nil {
			_ = h.SecretProvider.SetMany(map[string]string{secretKey: oldValue})
		} else {
			_ = h.SecretProvider.DeleteMany(secretKey)
		}
		helper.RespondError(w, http.StatusConflict, "Failed to update auth configuration", err)
		return
	}

	helper.RespondSuccess(w, http.StatusOK, "Auth configuration updated successfully", candidate)
}

func applyJWTOptions(cfg *config.JWTAuthConfig, header, issuer, audience, keyID, leeway string) {
	if strings.TrimSpace(header) != "" {
		cfg.AuthorizationHeader = strings.TrimSpace(header)
	}
	if strings.TrimSpace(issuer) != "" {
		cfg.Issuer = strings.TrimSpace(issuer)
	}
	if strings.TrimSpace(audience) != "" {
		cfg.Audience = strings.TrimSpace(audience)
	}
	if strings.TrimSpace(keyID) != "" {
		cfg.KeyID = strings.TrimSpace(keyID)
	}
	if strings.TrimSpace(leeway) != "" {
		cfg.Leeway = strings.TrimSpace(leeway)
	}
}

func encodePublicKeyForStorage(value string) (string, error) {
	text := strings.ReplaceAll(value, "\r\n", "\n")
	if strings.Contains(text, "BEGIN") {
		return base64.StdEncoding.EncodeToString([]byte(text)), nil
	}
	if strings.ContainsAny(text, "\x00\r\n") {
		return "", errors.New("public key must be PEM or base64-encoded PEM")
	}
	return strings.TrimSpace(text), nil
}
