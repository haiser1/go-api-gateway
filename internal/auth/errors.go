package auth

import "errors"

var (
	// ErrAPIKeyRequired indicates the API-key header is missing.
	ErrAPIKeyRequired = errors.New("api key required")
	// ErrInvalidAPIKey indicates the supplied API key is not valid.
	ErrInvalidAPIKey = errors.New("invalid api key")
	// ErrAuthorizationHeaderRequired indicates the Authorization header is missing.
	ErrAuthorizationHeaderRequired = errors.New("authorization header required")
	// ErrInvalidAuthorizationHeader indicates the Authorization header is malformed.
	ErrInvalidAuthorizationHeader = errors.New("invalid authorization header format")
	// ErrInvalidToken indicates the bearer token is not valid.
	ErrInvalidToken = errors.New("invalid token")
)
