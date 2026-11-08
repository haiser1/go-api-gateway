package auth

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/haiser1/go-api-gateway/internal/config"
	"github.com/haiser1/go-api-gateway/internal/secrets"
)

const testHSSecret = "0123456789abcdef0123456789abcdef"

func signHS256(t *testing.T, claims jwt.MapClaims, kid string) string {
	t.Helper()
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	if kid != "" {
		token.Header["kid"] = kid
	}
	signed, err := token.SignedString([]byte(testHSSecret))
	if err != nil {
		t.Fatal(err)
	}
	return signed
}

func signRS256(t *testing.T, key *rsa.PrivateKey, claims jwt.MapClaims) string {
	t.Helper()
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	signed, err := token.SignedString(key)
	if err != nil {
		t.Fatal(err)
	}
	return signed
}

func rsaPublicPEM(t *testing.T, key *rsa.PrivateKey) string {
	t.Helper()
	der, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}))
}

func validClaims() jwt.MapClaims {
	now := time.Now()
	return jwt.MapClaims{
		"sub": "user-1",
		"iss": "issuer",
		"aud": "audience",
		"iat": now.Unix(),
		"exp": now.Add(time.Hour).Unix(),
	}
}

func TestAPIKeyVerifier(t *testing.T) {
	hashed, err := NewAPIKeyVerifier(secrets.HashSecret("key-1"), "X-API-Key")
	if err != nil {
		t.Fatal(err)
	}
	if !hashed.Verify("key-1") || hashed.Verify("wrong") {
		t.Fatal("hashed api key verification failed")
	}

	legacy, err := NewAPIKeyVerifier("legacy-key", "X-API-Key")
	if err != nil {
		t.Fatal(err)
	}
	if !legacy.Verify("legacy-key") || legacy.Verify("wrong") {
		t.Fatal("legacy api key verification failed")
	}

	if _, err := NewAPIKeyVerifier("", "X-API-Key"); err == nil {
		t.Fatal("empty api key must be rejected")
	}
	if _, err := NewAPIKeyVerifier("key", ""); err == nil {
		t.Fatal("empty header must be rejected")
	}
	if _, err := NewAPIKeyVerifier("sha256:not-hex", "X-API-Key"); err == nil {
		t.Fatal("malformed hash must be rejected")
	}
}

func TestBuildRuntimeAPIKey(t *testing.T) {
	provider := secrets.NewMapProvider(map[string]string{
		"API_KEY_HASH": secrets.HashSecret("key-1"),
	})
	runtime, err := BuildRuntime(&config.AuthConfig{
		Mode:   config.AuthModeAPIKey,
		APIKey: &config.APIKeyAuthConfig{SecretRef: "API_KEY_HASH"},
	}, provider)
	if err != nil {
		t.Fatal(err)
	}
	if !runtime.Enabled() || !runtime.HasAPIKey() || runtime.HasJWT() {
		t.Fatal("unexpected runtime capabilities")
	}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-API-Key", "key-1")
	ctx, err := runtime.Authenticate(req)
	if err != nil || ctx == nil || ctx.Machine == nil || ctx.User != nil {
		t.Fatalf("authenticate = %+v, %v", ctx, err)
	}

	req.Header.Set("X-API-Key", "wrong")
	if _, err := runtime.Authenticate(req); !errors.Is(err, ErrInvalidAPIKey) {
		t.Fatalf("expected invalid api key, got %v", err)
	}

	req.Header.Del("X-API-Key")
	if _, err := runtime.Authenticate(req); !errors.Is(err, ErrAPIKeyRequired) {
		t.Fatalf("expected api key required, got %v", err)
	}

	if _, err := BuildRuntime(&config.AuthConfig{
		Mode:   config.AuthModeAPIKey,
		APIKey: &config.APIKeyAuthConfig{SecretRef: "MISSING"},
	}, provider); err == nil {
		t.Fatal("missing secret must fail the build")
	}
}

func TestBuildRuntimeHS256(t *testing.T) {
	provider := secrets.NewMapProvider(map[string]string{"JWT_HS256_SECRET": testHSSecret})
	runtime, err := BuildRuntime(&config.AuthConfig{
		Mode: config.AuthModeJWT,
		JWT:  &config.JWTAuthConfig{Alg: "HS256", HS256SecretRef: "JWT_HS256_SECRET", Issuer: "issuer", Audience: "audience"},
	}, provider)
	if err != nil {
		t.Fatal(err)
	}

	token := signHS256(t, validClaims(), "")
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	ctx, err := runtime.Authenticate(req)
	if err != nil || ctx == nil || ctx.User == nil || ctx.User.Subject != "user-1" {
		t.Fatalf("authenticate = %+v, %v", ctx, err)
	}
	if ctx.User.Issuer != "issuer" || len(ctx.User.Audiences) != 1 || ctx.User.Audiences[0] != "audience" {
		t.Fatalf("unexpected user identity: %+v", ctx.User)
	}

	req.Header.Set("Authorization", "Bearer "+signHS256(t, jwt.MapClaims{
		"sub": "user-1", "iss": "wrong", "aud": "audience", "exp": time.Now().Add(time.Hour).Unix(),
	}, ""))
	if _, err := runtime.Authenticate(req); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("wrong issuer must fail, got %v", err)
	}

	req.Header.Set("Authorization", "Bearer "+signHS256(t, jwt.MapClaims{
		"sub": "user-1", "iss": "issuer", "aud": "audience",
		"exp": time.Now().Add(-time.Hour).Unix(),
	}, ""))
	if _, err := runtime.Authenticate(req); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("expired token must fail, got %v", err)
	}

	req.Header.Set("Authorization", "Token "+token)
	if _, err := runtime.Authenticate(req); !errors.Is(err, ErrInvalidAuthorizationHeader) {
		t.Fatalf("malformed header must fail, got %v", err)
	}
}

func TestBuildRuntimeHS256KeyIDAndLeeway(t *testing.T) {
	provider := secrets.NewMapProvider(map[string]string{"JWT_HS256_SECRET": testHSSecret})
	runtime, err := BuildRuntime(&config.AuthConfig{
		Mode: config.AuthModeJWT,
		JWT: &config.JWTAuthConfig{
			Alg: "HS256", HS256SecretRef: "JWT_HS256_SECRET",
			KeyID: "key-1", Leeway: "10s",
		},
	}, provider)
	if err != nil {
		t.Fatal(err)
	}

	token := signHS256(t, jwt.MapClaims{
		"sub": "user-1", "exp": time.Now().Add(-5 * time.Second).Unix(),
	}, "key-1")
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	if _, err := runtime.Authenticate(req); err != nil {
		t.Fatalf("leeway should allow recently expired token: %v", err)
	}

	token = signHS256(t, validClaims(), "wrong-kid")
	req.Header.Set("Authorization", "Bearer "+token)
	if _, err := runtime.Authenticate(req); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("wrong kid must fail, got %v", err)
	}
}

func TestBuildRuntimeRS256(t *testing.T) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	pemKey := rsaPublicPEM(t, privateKey)
	provider := secrets.NewMapProvider(map[string]string{"JWT_RS256_PUBLIC_KEY": pemKey})
	runtime, err := BuildRuntime(&config.AuthConfig{
		Mode: config.AuthModeJWT,
		JWT:  &config.JWTAuthConfig{Alg: "RS256", RS256PublicKeyRef: "JWT_RS256_PUBLIC_KEY"},
	}, provider)
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+signRS256(t, privateKey, validClaims()))
	if _, err := runtime.Authenticate(req); err != nil {
		t.Fatalf("RS256 token rejected: %v", err)
	}

	// An HS256 token must not be accepted by an RS256 verifier.
	req.Header.Set("Authorization", "Bearer "+signHS256(t, validClaims(), ""))
	if _, err := runtime.Authenticate(req); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("algorithm confusion must fail, got %v", err)
	}
}

func TestParseRSAPublicKeyEncodings(t *testing.T) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	pemKey := rsaPublicPEM(t, privateKey)

	escaped := strings.ReplaceAll(pemKey, "\n", `\n`)
	encoded := base64.StdEncoding.EncodeToString([]byte(pemKey))
	for name, value := range map[string]string{
		"pem":     pemKey,
		"escaped": escaped,
		"base64":  encoded,
	} {
		if _, err := ParseRSAPublicKey(value); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}

	smallKey, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ParseRSAPublicKey(rsaPublicPEM(t, smallKey)); err == nil {
		t.Fatal("1024-bit RSA key must be rejected")
	}
	if _, err := ParseRSAPublicKey("not a key"); err == nil {
		t.Fatal("garbage key must be rejected")
	}
}

func TestBuildRuntimeRejectsWeakHS256Secret(t *testing.T) {
	provider := secrets.NewMapProvider(map[string]string{"JWT_HS256_SECRET": "short"})
	_, err := BuildRuntime(&config.AuthConfig{
		Mode: config.AuthModeJWT,
		JWT:  &config.JWTAuthConfig{Alg: "HS256", HS256SecretRef: "JWT_HS256_SECRET"},
	}, provider)
	if err == nil {
		t.Fatal("short HS256 secret must fail")
	}
}

func TestBuildRuntimeBothRequiresBothCredentials(t *testing.T) {
	provider := secrets.NewMapProvider(map[string]string{
		"API_KEY_HASH":     secrets.HashSecret("key-1"),
		"JWT_HS256_SECRET": testHSSecret,
	})
	runtime, err := BuildRuntime(&config.AuthConfig{
		Mode:   config.AuthModeBoth,
		APIKey: &config.APIKeyAuthConfig{SecretRef: "API_KEY_HASH"},
		JWT:    &config.JWTAuthConfig{Alg: "HS256", HS256SecretRef: "JWT_HS256_SECRET"},
	}, provider)
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	if _, err := runtime.Authenticate(req); !errors.Is(err, ErrAPIKeyRequired) {
		t.Fatalf("expected api key required, got %v", err)
	}

	req.Header.Set("X-API-Key", "key-1")
	if _, err := runtime.Authenticate(req); !errors.Is(err, ErrAuthorizationHeaderRequired) {
		t.Fatalf("expected authorization required, got %v", err)
	}

	req.Header.Set("Authorization", "Bearer "+signHS256(t, validClaims(), ""))
	ctx, err := runtime.Authenticate(req)
	if err != nil || ctx == nil || ctx.Machine == nil || ctx.User == nil {
		t.Fatalf("both credentials should authenticate: %+v, %v", ctx, err)
	}
}

func TestMiddleware(t *testing.T) {
	provider := secrets.NewMapProvider(map[string]string{
		"API_KEY_HASH":     secrets.HashSecret("key-1"),
		"JWT_HS256_SECRET": testHSSecret,
	})
	runtime, err := BuildRuntime(&config.AuthConfig{
		Mode:   config.AuthModeBoth,
		APIKey: &config.APIKeyAuthConfig{SecretRef: "API_KEY_HASH"},
		JWT:    &config.JWTAuthConfig{Alg: "HS256", HS256SecretRef: "JWT_HS256_SECRET"},
	}, provider)
	if err != nil {
		t.Fatal(err)
	}

	var gotContext *Context
	handler := Middleware(runtime)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotContext, _ = FromContext(r.Context())
		w.WriteHeader(http.StatusNoContent)
	}))

	cases := []struct {
		name       string
		apiKey     string
		authHeader string
		status     int
		message    string
	}{
		{"missing api key", "", "", http.StatusUnauthorized, "API key required"},
		{"invalid api key", "wrong", "", http.StatusUnauthorized, "Unauthorized: Invalid API key"},
		{"missing authorization", "key-1", "", http.StatusUnauthorized, "Authorization header required"},
		{"malformed authorization", "key-1", "Token x", http.StatusUnauthorized, "Invalid Authorization header format"},
		{"invalid token", "key-1", "Bearer bad", http.StatusUnauthorized, "Unauthorized: Invalid token"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			if tc.apiKey != "" {
				req.Header.Set("X-API-Key", tc.apiKey)
			}
			if tc.authHeader != "" {
				req.Header.Set("Authorization", tc.authHeader)
			}
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != tc.status {
				t.Fatalf("status = %d, want %d", rec.Code, tc.status)
			}
			if !strings.Contains(rec.Body.String(), tc.message) {
				t.Fatalf("body = %q, want %q", rec.Body.String(), tc.message)
			}
		})
	}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-API-Key", "key-1")
	req.Header.Set("Authorization", "Bearer "+signHS256(t, validClaims(), ""))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("valid request status = %d", rec.Code)
	}
	if gotContext == nil || gotContext.User == nil || gotContext.Machine == nil {
		t.Fatalf("context not propagated: %+v", gotContext)
	}
}

func TestMiddlewareDisabledIsNoOp(t *testing.T) {
	handler := Middleware(nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("disabled middleware status = %d", rec.Code)
	}
}
