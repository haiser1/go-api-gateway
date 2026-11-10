// Command gen_tokens generates deterministic test fixtures for the thesis
// experiments: dummy API keys, an HS256 secret, an RSA-2048 key pair, and a
// catalogue of valid/invalid JWTs. All values are test-only dummy secrets.
package main

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const (
	issuer   = "thesis-test"
	audience = "go-api-gateway"
	subject  = "user-123"
)

type tokenEntry struct {
	Token     string `json:"token"`
	Kind      string `json:"kind"`
	Algorithm string `json:"algorithm"`
	ExpiresAt int64  `json:"exp,omitempty"`
	NotBefore int64  `json:"nbf,omitempty"`
	Note      string `json:"note,omitempty"`
}

type apiKeyEntry struct {
	Value string `json:"value"`
	Hash  string `json:"hash,omitempty"`
	Note  string `json:"note,omitempty"`
}

type fixtures struct {
	GeneratedAt  string                 `json:"generated_at"`
	Issuer       string                 `json:"issuer"`
	Audience     string                 `json:"audience"`
	HS256Secret  string                 `json:"hs256_secret"`
	RSAPublicPEM string                 `json:"rsa_public_pem"`
	APIKeys      map[string]apiKeyEntry `json:"api_keys"`
	Tokens       map[string]tokenEntry  `json:"tokens"`
}

func main() {
	outDir := "experiments/fixtures"
	if len(os.Args) > 1 {
		outDir = os.Args[1]
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		die(err)
	}

	hsSecret := randomURL(48)
	apiKeyValid := randomURL(32)
	apiKeyRevoked := randomURL(32)
	apiKeyUnknown := randomURL(32)

	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		die(err)
	}
	publicDER, err := x509.MarshalPKIXPublicKey(&privateKey.PublicKey)
	if err != nil {
		die(err)
	}
	publicPEM := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: publicDER})
	privateDER := x509.MarshalPKCS1PrivateKey(privateKey)
	privatePEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: privateDER})

	now := time.Now()
	longExp := now.Add(72 * time.Hour)

	hs := func(claims jwt.MapClaims) string {
		tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
		signed, err := tok.SignedString([]byte(hsSecret))
		if err != nil {
			die(err)
		}
		return signed
	}
	rs := func(claims jwt.MapClaims) string {
		tok := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
		signed, err := tok.SignedString(privateKey)
		if err != nil {
			die(err)
		}
		return signed
	}
	baseClaims := func() jwt.MapClaims {
		return jwt.MapClaims{
			"iss": issuer,
			"aud": audience,
			"sub": subject,
			"iat": now.Unix(),
			"nbf": now.Add(-10 * time.Second).Unix(),
			"exp": longExp.Unix(),
		}
	}

	tokens := map[string]tokenEntry{}

	valid := hs(baseClaims())
	tokens["valid_long"] = tokenEntry{Token: valid, Kind: "valid_long", Algorithm: "HS256", ExpiresAt: longExp.Unix(), Note: "valid for 72h"}
	tokens["valid_long_rs256"] = tokenEntry{Token: rs(baseClaims()), Kind: "valid_long_rs256", Algorithm: "RS256", ExpiresAt: longExp.Unix(), Note: "valid for 72h"}

	expiredClaims := baseClaims()
	expiredClaims["iat"] = now.Add(-2 * time.Minute).Unix()
	expiredClaims["nbf"] = now.Add(-2 * time.Minute).Unix()
	expiredClaims["exp"] = now.Add(-60 * time.Second).Unix()
	tokens["expired"] = tokenEntry{Token: hs(expiredClaims), Kind: "expired", Algorithm: "HS256", ExpiresAt: now.Add(-60 * time.Second).Unix()}

	// Tampered payload: sign a legitimate token, then modify the payload
	// while keeping the original signature.
	tamperedBase := hs(baseClaims())
	parts := strings.Split(tamperedBase, ".")
	payloadJSON, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		die(err)
	}
	var payload map[string]interface{}
	if err := json.Unmarshal(payloadJSON, &payload); err != nil {
		die(err)
	}
	payload["sub"] = "admin-999"
	payload["role"] = "superadmin"
	mutated, err := json.Marshal(payload)
	if err != nil {
		die(err)
	}
	parts[1] = base64.RawURLEncoding.EncodeToString(mutated)
	tokens["tampered_payload"] = tokenEntry{Token: strings.Join(parts, "."), Kind: "tampered_payload", Algorithm: "HS256", Note: "payload changed after signing"}

	// alg=none, both with and without the trailing dot.
	noneHeader := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none","typ":"JWT"}`))
	nonePayloadJSON, err := json.Marshal(baseClaims())
	if err != nil {
		die(err)
	}
	nonePayload := base64.RawURLEncoding.EncodeToString(nonePayloadJSON)
	tokens["alg_none_with_dot"] = tokenEntry{Token: noneHeader + "." + nonePayload + ".", Kind: "alg_none", Algorithm: "none", Note: "empty signature with trailing dot"}
	tokens["alg_none_no_dot"] = tokenEntry{Token: noneHeader + "." + nonePayload, Kind: "alg_none", Algorithm: "none", Note: "two segments, no signature"}

	wrongIss := baseClaims()
	wrongIss["iss"] = "evil-issuer"
	tokens["wrong_iss"] = tokenEntry{Token: hs(wrongIss), Kind: "wrong_iss", Algorithm: "HS256"}

	wrongAud := baseClaims()
	wrongAud["aud"] = "other-service"
	tokens["wrong_aud"] = tokenEntry{Token: hs(wrongAud), Kind: "wrong_aud", Algorithm: "HS256"}

	futureNbf := baseClaims()
	futureNbf["nbf"] = now.Add(24 * time.Hour).Unix()
	tokens["nbf_future"] = tokenEntry{Token: hs(futureNbf), Kind: "nbf_future", Algorithm: "HS256", NotBefore: futureNbf["nbf"].(int64)}

	// Algorithm confusion: HS256 token whose HMAC secret is the RSA public key.
	confusion := jwt.NewWithClaims(jwt.SigningMethodHS256, baseClaims())
	confusionSigned, err := confusion.SignedString(publicPEM)
	if err != nil {
		die(err)
	}
	tokens["alg_confusion"] = tokenEntry{Token: confusionSigned, Kind: "alg_confusion", Algorithm: "HS256", Note: "signed with RSA public key as HMAC secret"}

	// Malformed tokens.
	tokens["malformed_not_three_segments"] = tokenEntry{Token: "not-a-jwt", Kind: "malformed", Note: "one segment"}
	tokens["malformed_bad_base64"] = tokenEntry{Token: "aaa.@@@.bbb", Kind: "malformed", Note: "invalid base64 payload"}
	tokens["malformed_empty"] = tokenEntry{Token: "", Kind: "malformed", Note: "empty bearer token"}
	tokens["malformed_garbage"] = tokenEntry{Token: "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiJ4In0", Kind: "malformed", Note: "two segments"}

	// Oversized tokens: a valid signed token with a ~24 KB filler claim, and
	// a ~40 KB junk string that is not a JWT at all.
	oversizedClaims := baseClaims()
	oversizedClaims["filler"] = strings.Repeat("A", 24000)
	tokens["oversized_valid"] = tokenEntry{Token: hs(oversizedClaims), Kind: "oversized", Algorithm: "HS256", Note: "~24KB signed JWT"}
	tokens["oversized_garbage"] = tokenEntry{Token: strings.Repeat("A", 40000), Kind: "oversized", Note: "~40KB non-JWT"}

	fx := fixtures{
		GeneratedAt:  now.UTC().Format(time.RFC3339),
		Issuer:       issuer,
		Audience:     audience,
		HS256Secret:  hsSecret,
		RSAPublicPEM: string(publicPEM),
		APIKeys: map[string]apiKeyEntry{
			"valid":   {Value: apiKeyValid, Hash: hashSecret(apiKeyValid), Note: "accepted by the gateway"},
			"revoked": {Value: apiKeyRevoked, Hash: hashSecret(apiKeyRevoked), Note: "never active; used as a revoked-key sample"},
			"unknown": {Value: apiKeyUnknown, Hash: hashSecret(apiKeyUnknown), Note: "random, never registered"},
		},
		Tokens: tokens,
	}

	tokensJSON, err := json.MarshalIndent(fx, "", "  ")
	if err != nil {
		die(err)
	}
	write(filepath.Join(outDir, "tokens.json"), append(tokensJSON, '\n'))
	write(filepath.Join(outDir, "api_keys.json"), mustJSON(fx.APIKeys))
	write(filepath.Join(outDir, "rsa_private.pem"), privatePEM)
	write(filepath.Join(outDir, "rsa_public.pem"), publicPEM)

	publicB64 := base64.StdEncoding.EncodeToString(publicPEM)
	env := fmt.Sprintf(`# Auto-generated dummy secrets for thesis testing - NEVER use in production.
API_KEY_HASH=%s
JWT_HS256_SECRET=%s
JWT_RS256_PUBLIC_KEY=%s
`, hashSecret(apiKeyValid), hsSecret, publicB64)
	write(filepath.Join(outDir, ".env"), []byte(env))

	fmt.Printf("fixtures written to %s\n", outDir)
	fmt.Printf("  valid API key: %s\n", apiKeyValid)
	fmt.Printf("  HS256 secret length: %d bytes\n", len(hsSecret))
}

func randomURL(n int) string {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		die(err)
	}
	return base64.RawURLEncoding.EncodeToString(buf)
}

func hashSecret(value string) string {
	sum := sha256.Sum256([]byte(value))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func mustJSON(v interface{}) []byte {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		die(err)
	}
	return append(data, '\n')
}

func write(path string, data []byte) {
	if err := os.WriteFile(path, data, 0o600); err != nil {
		die(err)
	}
}

func die(err error) {
	fmt.Fprintf(os.Stderr, "gen_tokens: %v\n", err)
	os.Exit(1)
}
