package auth

import (
	"encoding/json"
	"errors"
	"net/http"
)

// Middleware enforces the compiled authentication policy. A nil or disabled
// runtime is a no-op.
func Middleware(runtime *Runtime) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if runtime == nil || !runtime.Enabled() {
			return next
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authCtx, err := runtime.Authenticate(r)
			if err != nil {
				writeAuthError(w, err)
				return
			}
			if authCtx != nil {
				r = r.WithContext(WithContext(r.Context(), authCtx))
			}
			next.ServeHTTP(w, r)
		})
	}
}

func writeAuthError(w http.ResponseWriter, err error) {
	message := "Unauthorized"
	switch {
	case errors.Is(err, ErrAPIKeyRequired):
		message = "API key required"
	case errors.Is(err, ErrInvalidAPIKey):
		message = "Unauthorized: Invalid API key"
	case errors.Is(err, ErrAuthorizationHeaderRequired):
		message = "Authorization header required"
	case errors.Is(err, ErrInvalidAuthorizationHeader):
		message = "Invalid Authorization header format"
	case errors.Is(err, ErrInvalidToken):
		message = "Unauthorized: Invalid token"
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
}
