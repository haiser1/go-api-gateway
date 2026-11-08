package auth

import (
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// JWTOptions configures claim validation shared by HS256 and RS256 verifiers.
type JWTOptions struct {
	Algorithm string
	Header    string
	Issuer    string
	Audience  string
	KeyID     string
	Leeway    time.Duration
}

// JWTVerifier validates signed JWTs using a fixed algorithm and key.
type JWTVerifier struct {
	opts    JWTOptions
	hsKey   []byte
	rsaKey  *rsa.PublicKey
	keyFunc jwt.Keyfunc
}

// NewHS256Verifier builds an HS256 verifier. The secret must be at least 32
// bytes.
func NewHS256Verifier(secret []byte, opts JWTOptions) (*JWTVerifier, error) {
	if len(secret) < 32 {
		return nil, fmt.Errorf("HS256 secret must be at least 32 bytes, got %d", len(secret))
	}
	opts.Algorithm = "HS256"
	v := &JWTVerifier{opts: opts, hsKey: append([]byte(nil), secret...)}
	v.keyFunc = func(token *jwt.Token) (interface{}, error) {
		if token.Method.Alg() != "HS256" {
			return nil, ErrInvalidToken
		}
		if opts.KeyID != "" && token.Header["kid"] != opts.KeyID {
			return nil, ErrInvalidToken
		}
		return v.hsKey, nil
	}
	return v, nil
}

// NewRS256Verifier builds an RS256 verifier from an RSA public key.
func NewRS256Verifier(publicKey *rsa.PublicKey, opts JWTOptions) (*JWTVerifier, error) {
	if publicKey == nil {
		return nil, errors.New("RSA public key is required")
	}
	if publicKey.N == nil || publicKey.N.BitLen() < 2048 {
		return nil, errors.New("RSA public key must be at least 2048 bits")
	}
	opts.Algorithm = "RS256"
	v := &JWTVerifier{opts: opts, rsaKey: publicKey}
	v.keyFunc = func(token *jwt.Token) (interface{}, error) {
		if token.Method.Alg() != "RS256" {
			return nil, ErrInvalidToken
		}
		if opts.KeyID != "" && token.Header["kid"] != opts.KeyID {
			return nil, ErrInvalidToken
		}
		return v.rsaKey, nil
	}
	return v, nil
}

// Header returns the configured authorization header name.
func (v *JWTVerifier) Header() string {
	if v == nil {
		return ""
	}
	return v.opts.Header
}

// Verify validates tokenString and returns the user identity.
func (v *JWTVerifier) Verify(tokenString string) (*UserIdentity, error) {
	if v == nil || strings.TrimSpace(tokenString) == "" {
		return nil, ErrInvalidToken
	}

	claims := jwt.MapClaims{}
	options := []jwt.ParserOption{
		jwt.WithValidMethods([]string{v.opts.Algorithm}),
	}
	if v.opts.Leeway > 0 {
		options = append(options, jwt.WithLeeway(v.opts.Leeway))
	}
	if v.opts.Issuer != "" {
		options = append(options, jwt.WithIssuer(v.opts.Issuer))
	}
	if v.opts.Audience != "" {
		options = append(options, jwt.WithAudience(v.opts.Audience))
	}

	token, err := jwt.ParseWithClaims(tokenString, claims, v.keyFunc, options...)
	if err != nil || !token.Valid {
		return nil, ErrInvalidToken
	}

	subject, _ := claims.GetSubject()
	issuer, _ := claims.GetIssuer()
	audiences, _ := claims.GetAudience()
	expiresAt, _ := claims.GetExpirationTime()
	notBefore, _ := claims.GetNotBefore()

	user := &UserIdentity{
		Subject:   subject,
		Issuer:    issuer,
		Audiences: append([]string(nil), audiences...),
		Claims:    map[string]interface{}(claims),
	}
	if expiresAt != nil {
		user.ExpiresAt = expiresAt.Time
	}
	if notBefore != nil {
		user.NotBefore = notBefore.Time
	}
	return user, nil
}

// ParseRSAPublicKey accepts PEM bytes directly, PEM with escaped newlines, or
// base64-encoded PEM. Both PKIX and PKCS#1 public keys are supported.
func ParseRSAPublicKey(value string) (*rsa.PublicKey, error) {
	text := strings.TrimSpace(value)
	if text == "" {
		return nil, errors.New("RSA public key is empty")
	}
	if strings.Contains(text, `\n`) {
		text = strings.ReplaceAll(text, `\n`, "\n")
	}
	if !strings.Contains(text, "BEGIN") {
		decoded, err := base64.StdEncoding.DecodeString(text)
		if err != nil {
			return nil, fmt.Errorf("RSA public key is neither PEM nor base64-encoded PEM: %w", err)
		}
		text = string(decoded)
	}

	block, _ := pem.Decode([]byte(text))
	if block == nil {
		return nil, errors.New("RSA public key PEM block not found")
	}

	var publicKey *rsa.PublicKey
	switch block.Type {
	case "PUBLIC KEY":
		parsed, err := x509.ParsePKIXPublicKey(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("parse PKIX public key: %w", err)
		}
		var ok bool
		publicKey, ok = parsed.(*rsa.PublicKey)
		if !ok {
			return nil, errors.New("public key is not RSA")
		}
	case "RSA PUBLIC KEY":
		parsed, err := x509.ParsePKCS1PublicKey(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("parse PKCS#1 public key: %w", err)
		}
		publicKey = parsed
	default:
		return nil, fmt.Errorf("unsupported public key PEM block %q", block.Type)
	}

	if publicKey.N == nil || publicKey.N.BitLen() < 2048 {
		return nil, errors.New("RSA public key must be at least 2048 bits")
	}
	return publicKey, nil
}
