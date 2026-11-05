package middleware

import (
	"fmt"
	"net"
	"net/http"
	"regexp"
	"strings"

	"github.com/haiser1/go-api-gateway/internal/gateway"
)

type ipMatcher struct {
	exactIPs []net.IP
	cidrs    []*net.IPNet
}

func parseIPMatcher(entries []string) ipMatcher {
	var m ipMatcher
	for _, raw := range entries {
		entry := strings.TrimSpace(raw)
		if entry == "" {
			continue
		}
		if strings.Contains(entry, "/") {
			if _, ipNet, err := net.ParseCIDR(entry); err == nil && ipNet != nil {
				m.cidrs = append(m.cidrs, ipNet)
				continue
			}
		}
		if ip := net.ParseIP(entry); ip != nil {
			m.exactIPs = append(m.exactIPs, ip)
		}
	}
	return m
}

func (m *ipMatcher) isEmpty() bool {
	return len(m.exactIPs) == 0 && len(m.cidrs) == 0
}

func (m *ipMatcher) matches(clientIP net.IP) bool {
	if clientIP == nil {
		return false
	}
	for _, ip := range m.exactIPs {
		if ip.Equal(clientIP) {
			return true
		}
	}
	for _, cidr := range m.cidrs {
		if cidr.Contains(clientIP) {
			return true
		}
	}
	return false
}

func extractClientIP(r *http.Request, trustForwardedFor bool) net.IP {
	if trustForwardedFor {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			first := strings.TrimSpace(strings.Split(xff, ",")[0])
			if ip := net.ParseIP(first); ip != nil {
				return ip
			}
		}
		if xri := strings.TrimSpace(r.Header.Get("X-Real-IP")); xri != "" {
			if ip := net.ParseIP(xri); ip != nil {
				return ip
			}
		}
	}

	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	return net.ParseIP(strings.TrimSpace(host))
}

// NewIPRestrictionPlugin creates a pre-compiled IP allowlist/denylist and bot-detection middleware.
//
// Supported config keys:
//   - allowed_ips / whitelist ([]string): Exact IPs or CIDR ranges allowed through
//   - denied_ips / blacklist ([]string): Exact IPs or CIDR ranges blocked
//   - denied_user_agents / blocked_bots ([]string): Case-insensitive regex/substring patterns for User-Agent blocking
//   - trust_forwarded_for (bool): Read client IP from X-Forwarded-For / X-Real-IP (default: false)
//   - status_code (int): HTTP status code when blocked (default: 403)
//   - message (string): Error message returned in JSON body when blocked
func NewIPRestrictionPlugin(config map[string]interface{}) gateway.PluginMiddleware {
	allowedRaw := parseStringSlice(config["allowed_ips"])
	if len(allowedRaw) == 0 {
		allowedRaw = parseStringSlice(config["whitelist"])
	}

	deniedRaw := parseStringSlice(config["denied_ips"])
	if len(deniedRaw) == 0 {
		deniedRaw = parseStringSlice(config["blacklist"])
	}

	botPatternsRaw := parseStringSlice(config["denied_user_agents"])
	if len(botPatternsRaw) == 0 {
		botPatternsRaw = parseStringSlice(config["blocked_bots"])
	}

	allowMatcher := parseIPMatcher(allowedRaw)
	denyMatcher := parseIPMatcher(deniedRaw)

	var botRegexes []*regexp.Regexp
	for _, pat := range botPatternsRaw {
		if re, err := regexp.Compile("(?i)" + pat); err == nil {
			botRegexes = append(botRegexes, re)
		}
	}

	trustForwardedFor := false
	if v, ok := config["trust_forwarded_for"].(bool); ok {
		trustForwardedFor = v
	}

	statusCode := http.StatusForbidden
	if v, ok := config["status_code"].(float64); ok && v >= 400 && v < 600 {
		statusCode = int(v)
	} else if v, ok := config["status_code"].(int); ok && v >= 400 && v < 600 {
		statusCode = v
	}

	errMessage := "Forbidden: Client IP or User-Agent is restricted"
	if v, ok := config["message"].(string); ok && v != "" {
		errMessage = v
	}
	jsonErrorPayload := fmt.Sprintf(`{"error": %q}`, errMessage)

	return gateway.PluginFunc{
		NameStr: "ip-restriction",
		Handler: func(w http.ResponseWriter, r *http.Request, next http.Handler) bool {
			// 1. Check User-Agent / Bot patterns if configured
			if len(botRegexes) > 0 {
				ua := r.UserAgent()
				for _, re := range botRegexes {
					if re.MatchString(ua) {
						w.Header().Set("Content-Type", "application/json")
						w.WriteHeader(statusCode)
						_, _ = w.Write([]byte(jsonErrorPayload))
						return false
					}
				}
			}

			// 2. Check IP denylist and allowlist if configured
			if !denyMatcher.isEmpty() || !allowMatcher.isEmpty() {
				clientIP := extractClientIP(r, trustForwardedFor)

				// Denylist takes precedence
				if !denyMatcher.isEmpty() && denyMatcher.matches(clientIP) {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(statusCode)
					_, _ = w.Write([]byte(jsonErrorPayload))
					return false
				}

				// If allowlist is non-empty, client IP must match it
				if !allowMatcher.isEmpty() && !allowMatcher.matches(clientIP) {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(statusCode)
					_, _ = w.Write([]byte(jsonErrorPayload))
					return false
				}
			}

			next.ServeHTTP(w, r)
			return true
		},
	}
}

func init() {
	gateway.RegisterPlugin("ip-restriction", NewIPRestrictionPlugin)
	gateway.RegisterPlugin("bot-detection", NewIPRestrictionPlugin)
}
