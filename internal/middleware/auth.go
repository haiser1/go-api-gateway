package middleware

import (
	"net/http"

	"github.com/haiser1/go-api-gateway/internal/gateway"
)

// NewAuthPlugin is a compatibility shim for the retired map-based auth
// plugin. Authentication is now compiled once from the top-level auth block
// by the gateway runtime. If a legacy registration ever reaches this builder,
// it fails closed instead of silently passing traffic through.
func init() {
	// Declare the legacy names so configuration validation continues to
	// recognize them; the gateway compiles auth from the top-level block.
	gateway.RegisterPlugin("auth", NewAuthPlugin)
	gateway.RegisterPlugin("authorization", NewAuthPlugin)
}

func NewAuthPlugin(map[string]interface{}) gateway.PluginMiddleware {
	return gateway.PluginFunc{
		NameStr: "auth-legacy",
		Handler: func(w http.ResponseWriter, r *http.Request, next http.Handler) bool {
			gateway.WriteJSONError(w, http.StatusServiceUnavailable, "Authentication is not configured")
			return false
		},
	}
}
