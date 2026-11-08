package secrets

// Canonical secret references managed by the gateway.
const (
	APIKeyHashKey        = "API_KEY_HASH"
	JWTHS256SecretKey    = "JWT_HS256_SECRET"
	JWTRS256PublicKeyKey = "JWT_RS256_PUBLIC_KEY"
	AdminTokenKey        = "ADMIN_TOKEN"
	AdminTokenHashKey    = "ADMIN_TOKEN_HASH"
)
