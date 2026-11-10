package middleware

import (
	"math"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/haiser1/go-api-gateway/internal/auth"
	"github.com/haiser1/go-api-gateway/internal/gateway"
)

const (
	rateLimitKeyByIP   = "ip"
	rateLimitKeyByUser = "user"

	defaultRateLimitRequests = 60
	defaultRateLimitWindow   = time.Minute
)

type rateLimitAlgorithm int

const (
	rateLimitFixedWindow rateLimitAlgorithm = iota
	rateLimitSlidingWindow
	rateLimitTokenBucket
)

type rateLimitConfig struct {
	algorithm         rateLimitAlgorithm
	limit             int
	window            time.Duration
	burst             float64
	keyBy             string
	userClaim         string
	trustForwardedFor bool
}

type rateLimitDecision struct {
	allowed    bool
	limit      int
	remaining  int
	resetAfter time.Duration
	retryAfter time.Duration
}

type fixedWindowEntry struct {
	windowID int64
	count    int
}

type slidingWindowEntry struct {
	windowID int64
	current  int
	previous int
}

type tokenBucketEntry struct {
	tokens float64
	last   time.Time
}

type rateLimiter struct {
	cfg       rateLimitConfig
	mu        sync.Mutex
	fixed     map[string]*fixedWindowEntry
	sliding   map[string]*slidingWindowEntry
	buckets   map[string]*tokenBucketEntry
	nextSweep time.Time
}

func init() {
	gateway.RegisterPlugin("rate-limiting", NewRateLimitPlugin)
}

// NewRateLimitPlugin creates a configurable in-memory rate limiter.
//
// Supported config keys:
//   - algorithm: fixed_window (default), sliding_window, token_bucket
//   - requests_per_window / requests_per_minute: requests allowed per window
//   - window_seconds: window length (default 60)
//   - burst: token bucket capacity (default = request limit)
//   - key_by: ip (default) or user
//   - user_claim: JWT claim used by key_by=user (default: sub, then user_id/uid)
//   - trust_forwarded_for: use X-Forwarded-For / X-Real-IP for IP keys
func NewRateLimitPlugin(config map[string]interface{}) gateway.PluginMiddleware {
	cfg := parseRateLimitConfig(config)
	limiter := newRateLimiter(cfg)

	return gateway.PluginFunc{
		NameStr: "rate-limiting",
		Handler: func(w http.ResponseWriter, r *http.Request, next http.Handler) bool {
			decision := limiter.allow(limiter.requestKey(r), time.Now())
			setRateLimitHeaders(w, decision)
			if !decision.allowed {
				gateway.WriteJSONError(w, http.StatusTooManyRequests, "Rate limit exceeded")
				return false
			}

			next.ServeHTTP(w, r)
			return true
		},
	}
}

func parseRateLimitConfig(config map[string]interface{}) rateLimitConfig {
	cfg := rateLimitConfig{
		algorithm: rateLimitFixedWindow,
		limit:     defaultRateLimitRequests,
		window:    defaultRateLimitWindow,
		keyBy:     rateLimitKeyByIP,
	}

	if name, ok := config["algorithm"].(string); ok {
		if algorithm, ok := normalizeRateLimitAlgorithm(name); ok {
			cfg.algorithm = algorithm
		}
	}

	if limit, ok := positiveConfigNumber(config["requests_per_window"]); ok {
		cfg.limit = int(limit)
	} else if limit, ok := positiveConfigNumber(config["requests_per_minute"]); ok {
		cfg.limit = int(limit)
	}
	if cfg.limit <= 0 {
		cfg.limit = defaultRateLimitRequests
	}

	if seconds, ok := positiveConfigNumber(config["window_seconds"]); ok {
		cfg.window = time.Duration(seconds * float64(time.Second))
	}
	if cfg.window <= 0 {
		cfg.window = defaultRateLimitWindow
	}

	cfg.burst = float64(cfg.limit)
	if burst, ok := positiveConfigNumber(config["burst"]); ok && burst >= 1 {
		cfg.burst = burst
	}

	if keyBy, ok := config["key_by"].(string); ok {
		cfg.keyBy = normalizeRateLimitKeyBy(keyBy)
	}
	if claim, ok := config["user_claim"].(string); ok {
		cfg.userClaim = strings.TrimSpace(claim)
	}
	if trust, ok := config["trust_forwarded_for"].(bool); ok {
		cfg.trustForwardedFor = trust
	}

	return cfg
}

func normalizeRateLimitAlgorithm(value string) (rateLimitAlgorithm, bool) {
	switch strings.ReplaceAll(strings.ToLower(strings.TrimSpace(value)), "-", "_") {
	case "fixed", "fixed_window":
		return rateLimitFixedWindow, true
	case "sliding", "sliding_window":
		return rateLimitSlidingWindow, true
	case "token_bucket", "tokenbucket":
		return rateLimitTokenBucket, true
	default:
		return rateLimitFixedWindow, false
	}
}

func normalizeRateLimitKeyBy(value string) string {
	switch strings.ReplaceAll(strings.ToLower(strings.TrimSpace(value)), "-", "_") {
	case "user", "user_id", "uid":
		return rateLimitKeyByUser
	default:
		return rateLimitKeyByIP
	}
}

func positiveConfigNumber(value interface{}) (float64, bool) {
	number, ok := configNumber(value)
	return number, ok && number > 0
}

func configNumber(value interface{}) (float64, bool) {
	switch v := value.(type) {
	case int:
		return float64(v), true
	case int32:
		return float64(v), true
	case int64:
		return float64(v), true
	case uint:
		return float64(v), true
	case uint32:
		return float64(v), true
	case uint64:
		return float64(v), true
	case float32:
		return float64(v), true
	case float64:
		return v, true
	case string:
		number, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
		return number, err == nil
	default:
		return 0, false
	}
}

func newRateLimiter(cfg rateLimitConfig) *rateLimiter {
	return &rateLimiter{
		cfg:     cfg,
		fixed:   make(map[string]*fixedWindowEntry),
		sliding: make(map[string]*slidingWindowEntry),
		buckets: make(map[string]*tokenBucketEntry),
	}
}

func (l *rateLimiter) allow(key string, now time.Time) rateLimitDecision {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.sweepLocked(now)

	switch l.cfg.algorithm {
	case rateLimitSlidingWindow:
		return l.allowSlidingWindowLocked(key, now)
	case rateLimitTokenBucket:
		return l.allowTokenBucketLocked(key, now)
	default:
		return l.allowFixedWindowLocked(key, now)
	}
}

func (l *rateLimiter) allowFixedWindowLocked(key string, now time.Time) rateLimitDecision {
	windowNanos := int64(l.cfg.window)
	windowID := now.UnixNano() / windowNanos

	entry, ok := l.fixed[key]
	if !ok {
		entry = &fixedWindowEntry{windowID: windowID}
		l.fixed[key] = entry
	}
	if entry.windowID != windowID {
		entry.windowID = windowID
		entry.count = 0
	}

	resetAfter := l.cfg.window - time.Duration(now.UnixNano()%windowNanos)
	if entry.count >= l.cfg.limit {
		return rateLimitDecision{
			allowed:    false,
			limit:      l.cfg.limit,
			remaining:  0,
			resetAfter: resetAfter,
			retryAfter: resetAfter,
		}
	}

	entry.count++
	return rateLimitDecision{
		allowed:    true,
		limit:      l.cfg.limit,
		remaining:  l.cfg.limit - entry.count,
		resetAfter: resetAfter,
	}
}

func (l *rateLimiter) allowSlidingWindowLocked(key string, now time.Time) rateLimitDecision {
	windowNanos := int64(l.cfg.window)
	windowID := now.UnixNano() / windowNanos

	entry, ok := l.sliding[key]
	if !ok {
		entry = &slidingWindowEntry{windowID: windowID}
		l.sliding[key] = entry
	}
	if entry.windowID != windowID {
		if entry.windowID == windowID-1 {
			entry.previous = entry.current
		} else {
			entry.previous = 0
		}
		entry.current = 0
		entry.windowID = windowID
	}

	elapsed := time.Duration(now.UnixNano() % windowNanos)
	weight := float64(l.cfg.window-elapsed) / float64(l.cfg.window)
	estimated := int(float64(entry.previous)*weight) + entry.current
	resetAfter := l.cfg.window - elapsed

	if estimated >= l.cfg.limit {
		return rateLimitDecision{
			allowed:    false,
			limit:      l.cfg.limit,
			remaining:  0,
			resetAfter: resetAfter,
			retryAfter: resetAfter,
		}
	}

	entry.current++
	return rateLimitDecision{
		allowed:    true,
		limit:      l.cfg.limit,
		remaining:  l.cfg.limit - estimated - 1,
		resetAfter: resetAfter,
	}
}

func (l *rateLimiter) allowTokenBucketLocked(key string, now time.Time) rateLimitDecision {
	refillRate := float64(l.cfg.limit) / l.cfg.window.Seconds()
	capacity := l.cfg.burst

	entry, ok := l.buckets[key]
	if !ok {
		entry = &tokenBucketEntry{tokens: capacity, last: now}
		l.buckets[key] = entry
	}
	if elapsed := now.Sub(entry.last).Seconds(); elapsed > 0 {
		entry.tokens = math.Min(capacity, entry.tokens+elapsed*refillRate)
		entry.last = now
	}

	if entry.tokens < 1 {
		retryAfter := time.Duration((1 - entry.tokens) / refillRate * float64(time.Second))
		return rateLimitDecision{
			allowed:    false,
			limit:      l.cfg.limit,
			remaining:  0,
			resetAfter: retryAfter,
			retryAfter: retryAfter,
		}
	}

	entry.tokens--
	remaining := int(math.Floor(entry.tokens + 1e-9))
	resetAfter := time.Duration(0)
	if entry.tokens < 1 {
		resetAfter = time.Duration((1 - entry.tokens) / refillRate * float64(time.Second))
	}
	return rateLimitDecision{
		allowed:    true,
		limit:      l.cfg.limit,
		remaining:  remaining,
		resetAfter: resetAfter,
	}
}

func (l *rateLimiter) sweepLocked(now time.Time) {
	if !l.nextSweep.IsZero() && now.Before(l.nextSweep) {
		return
	}
	l.nextSweep = now.Add(l.cfg.window)

	currentWindow := now.UnixNano() / int64(l.cfg.window)
	for key, entry := range l.fixed {
		if entry.windowID < currentWindow-1 {
			delete(l.fixed, key)
		}
	}
	for key, entry := range l.sliding {
		if entry.windowID < currentWindow-1 {
			delete(l.sliding, key)
		}
	}

	idleTTL := l.cfg.window
	if refillRate := float64(l.cfg.limit) / l.cfg.window.Seconds(); refillRate > 0 {
		if fullRefill := time.Duration(l.cfg.burst / refillRate * float64(time.Second)); fullRefill > idleTTL {
			idleTTL = fullRefill
		}
	}
	cutoff := now.Add(-idleTTL)
	for key, entry := range l.buckets {
		if entry.last.Before(cutoff) {
			delete(l.buckets, key)
		}
	}
}

func (l *rateLimiter) requestKey(r *http.Request) string {
	if l.cfg.keyBy == rateLimitKeyByUser {
		if userID := rateLimitUserID(r, l.cfg.userClaim); userID != "" {
			return "user:" + userID
		}
	}
	return "ip:" + rateLimitClientIP(r, l.cfg.trustForwardedFor)
}

func rateLimitUserID(r *http.Request, claim string) string {
	authCtx, ok := auth.FromContext(r.Context())
	if !ok || authCtx == nil || authCtx.User == nil {
		return ""
	}
	if claim != "" {
		return claimString(authCtx.User.Claims[claim])
	}
	if authCtx.User.Subject != "" {
		return authCtx.User.Subject
	}
	for _, name := range []string{"sub", "user_id", "uid"} {
		if value := claimString(authCtx.User.Claims[name]); value != "" {
			return value
		}
	}
	return ""
}

func claimString(value interface{}) string {
	switch v := value.(type) {
	case string:
		return strings.TrimSpace(v)
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	case float32:
		return strconv.FormatFloat(float64(v), 'f', -1, 32)
	case int:
		return strconv.Itoa(v)
	case int64:
		return strconv.FormatInt(v, 10)
	default:
		return ""
	}
}

func rateLimitClientIP(r *http.Request, trustForwardedFor bool) string {
	if trustForwardedFor {
		if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
			for _, part := range strings.Split(forwarded, ",") {
				if ip := hostOnly(strings.TrimSpace(part)); ip != "" {
					return ip
				}
			}
		}
		if realIP := hostOnly(strings.TrimSpace(r.Header.Get("X-Real-IP"))); realIP != "" {
			return realIP
		}
	}
	return hostOnly(strings.TrimSpace(r.RemoteAddr))
}

func hostOnly(value string) string {
	if value == "" {
		return ""
	}
	if host, _, err := net.SplitHostPort(value); err == nil && host != "" {
		return host
	}
	return value
}

func setRateLimitHeaders(w http.ResponseWriter, decision rateLimitDecision) {
	header := w.Header()
	header.Set("X-RateLimit-Limit", strconv.Itoa(decision.limit))
	header.Set("X-RateLimit-Remaining", strconv.Itoa(max(decision.remaining, 0)))
	header.Set("X-RateLimit-Reset", strconv.Itoa(durationSeconds(decision.resetAfter)))

	if !decision.allowed {
		header.Set("Retry-After", strconv.Itoa(max(durationSeconds(decision.retryAfter), 1)))
	}
}

func durationSeconds(duration time.Duration) int {
	if duration <= 0 {
		return 0
	}
	return int(math.Ceil(duration.Seconds()))
}
