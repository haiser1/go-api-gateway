package middleware

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/golang-jwt/jwt/v5"
	"github.com/haiser1/go-api-gateway/internal/gateway"
)

func init() {
	gateway.RegisterPlugin("authorization", func(config map[string]interface{}) gateway.PluginMiddleware {
		authType, _ := config["type"].(string)
		jwtKey, _ := config["jwt_key"].(string)

		// Validasi saat inisialisasi plugin
		if authType == "jwt" && jwtKey == "" {
			// Jika tidak ada jwt_key, plugin ini tidak akan melakukan apa-apa.
			// Ini adalah pendekatan "fail-safe".
			return gateway.PluginFunc{
				NameStr: "authorization-disabled",
				Handler: func(w http.ResponseWriter, r *http.Request, next http.Handler) bool {
					// Log a warning that the plugin is not configured
					// log.Println("[WARN] JWT authorization plugin is disabled due to missing 'jwt_key' in config")
					next.ServeHTTP(w, r)
					return true // Lanjutkan chain tanpa otorisasi
				},
			}
		}

		return gateway.PluginFunc{
			NameStr: "authorization",
			Handler: func(w http.ResponseWriter, r *http.Request, next http.Handler) bool {
				if authType != "jwt" {
					// Jika bukan JWT, langsung lanjutkan
					next.ServeHTTP(w, r)
					return true
				}

				// 1. Dapatkan token dari header
				authHeader := r.Header.Get("Authorization")
				if authHeader == "" {
					http.Error(w, "Authorization header required", http.StatusUnauthorized)
					return false
				}

				// 2. Cek format "Bearer <token>"
				parts := strings.Split(authHeader, " ")
				if len(parts) != 2 || strings.ToLower(parts[0]) != "bearer" {
					http.Error(w, "Invalid Authorization header format", http.StatusUnauthorized)
					return false
				}
				tokenString := parts[1]

				// 3. Parse dan validasi token
				token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
					// Pastikan algoritma signing adalah yang diharapkan (misal HMAC)
					if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
						return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
					}
					return []byte(jwtKey), nil
				})

				if err != nil {
					http.Error(w, fmt.Sprintf("Unauthorized: %v", err), http.StatusUnauthorized)
					return false
				}

				if !token.Valid {
					http.Error(w, "Unauthorized: Invalid token", http.StatusUnauthorized)
					return false
				}

				// Token valid, lanjutkan ke handler berikutnya
				next.ServeHTTP(w, r)
				return true
			},
		}
	})
}
