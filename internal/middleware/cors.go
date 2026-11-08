package middleware

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/haiser1/go-api-gateway/internal/gateway"
)

// NewCORSPlugin creates a new CORS plugin middleware from the provided config.
func NewCORSPlugin(config map[string]interface{}) gateway.PluginMiddleware {
	// Parse allowed origins
	var allowedOrigins []string
	if origins, ok := config["allowed_origins"].([]interface{}); ok {
		for _, o := range origins {
			if s, ok := o.(string); ok {
				allowedOrigins = append(allowedOrigins, s)
			}
		}
	} else if origins, ok := config["allowed_origins"].([]string); ok {
		allowedOrigins = origins
	}
	if len(allowedOrigins) == 0 {
		allowedOrigins = []string{"*"}
	}

	// Parse allowed methods
	var allowedMethods []string
	if methods, ok := config["allowed_methods"].([]interface{}); ok {
		for _, m := range methods {
			if s, ok := m.(string); ok {
				allowedMethods = append(allowedMethods, s)
			}
		}
	} else if methods, ok := config["allowed_methods"].([]string); ok {
		allowedMethods = methods
	}
	if len(allowedMethods) == 0 {
		allowedMethods = []string{"GET", "POST", "PUT", "DELETE", "OPTIONS", "PATCH"}
	}

	// Parse allowed headers
	var allowedHeaders []string
	if headers, ok := config["allowed_headers"].([]interface{}); ok {
		for _, h := range headers {
			if s, ok := h.(string); ok {
				allowedHeaders = append(allowedHeaders, s)
			}
		}
	} else if headers, ok := config["allowed_headers"].([]string); ok {
		allowedHeaders = headers
	}
	if len(allowedHeaders) == 0 {
		allowedHeaders = []string{"Content-Type", "Authorization", "X-Requested-With"}
	}

	// Parse exposed headers
	var exposedHeaders []string
	if headers, ok := config["exposed_headers"].([]interface{}); ok {
		for _, h := range headers {
			if s, ok := h.(string); ok {
				exposedHeaders = append(exposedHeaders, s)
			}
		}
	} else if headers, ok := config["exposed_headers"].([]string); ok {
		exposedHeaders = headers
	}

	// Parse allow credentials
	allowCredentials := false
	if creds, ok := config["allow_credentials"].(bool); ok {
		allowCredentials = creds
	}

	// Parse max age
	maxAge := 0
	if ageFloat, ok := config["max_age"].(float64); ok {
		maxAge = int(ageFloat)
	} else if ageInt, ok := config["max_age"].(int); ok {
		maxAge = ageInt
	}

	allowedMethodsStr := strings.Join(allowedMethods, ", ")
	allowedHeadersStr := strings.Join(allowedHeaders, ", ")
	exposedHeadersStr := strings.Join(exposedHeaders, ", ")

	return gateway.PluginFunc{
		NameStr: "cors",
		Handler: func(w http.ResponseWriter, r *http.Request, next http.Handler) bool {
			origin := r.Header.Get("Origin")
			if origin == "" {
				// Request is not a CORS request, just proceed
				next.ServeHTTP(w, r)
				return true
			}

			// Check if origin is allowed
			originAllowed := false
			if len(allowedOrigins) == 1 && allowedOrigins[0] == "*" {
				originAllowed = true
				if allowCredentials {
					// Under CORS spec, Access-Control-Allow-Origin cannot be '*' if Allow-Credentials is true.
					// So we echo back the request's Origin instead of using wildcard.
					w.Header().Set("Access-Control-Allow-Origin", origin)
				} else {
					w.Header().Set("Access-Control-Allow-Origin", "*")
				}
			} else {
				for _, o := range allowedOrigins {
					if o == origin {
						originAllowed = true
						w.Header().Set("Access-Control-Allow-Origin", origin)
						break
					}
				}
			}

			if originAllowed {
				if allowCredentials {
					w.Header().Set("Access-Control-Allow-Credentials", "true")
				}
				if exposedHeadersStr != "" {
					w.Header().Set("Access-Control-Expose-Headers", exposedHeadersStr)
				}
			}

			// Preflight Request
			if r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") != "" {
				if originAllowed {
					w.Header().Set("Access-Control-Allow-Methods", allowedMethodsStr)
					w.Header().Set("Access-Control-Allow-Headers", allowedHeadersStr)
					if maxAge > 0 {
						w.Header().Set("Access-Control-Max-Age", strconv.Itoa(maxAge))
					}
				}
				w.WriteHeader(http.StatusNoContent) // 204 No Content is standard for OPTIONS preflight
				return false                        // Do not proceed to upstream
			}

			next.ServeHTTP(w, r)
			return true
		},
	}
}

func init() {
	gateway.RegisterPlugin("cors", NewCORSPlugin)
}
