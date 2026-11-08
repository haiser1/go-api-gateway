package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"strconv"
)

// JSON gateway error contract: every proxy-side error response uses the
// `{"error": "..."}` shape with Content-Type application/json — including
// plugin errors (rate limiting, auth, ip-restriction) so clients can parse
// failures uniformly regardless of which stage produced them.

type gatewayErrorResponse struct {
	Error string `json:"error"`
}

// WriteJSONError writes a structured JSON error response exactly once:
// Content-Type and Content-Length are set BEFORE WriteHeader, and the body is
// always pre-marshaled so a failure to encode (impossible for this fixed
// shape, but defensive) cannot end up writing a plain-text body.
//
// Callers must not write anything before calling this helper — several of the
// Phase 7 tasks (405 + Allow, circuit breaker 503, upstream 502/504) funnel
// through it.
func WriteJSONError(w http.ResponseWriter, status int, message string) {
	var body []byte
	if marshaled, err := json.Marshal(gatewayErrorResponse{Error: message}); err == nil {
		body = marshaled
	} else {
		body = []byte(`{"error":"Internal Server Error"}`)
	}

	header := w.Header()
	header.Set("Content-Type", "application/json")
	header.Set("Content-Length", strconv.Itoa(len(body)))
	header.Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

// MapUpstreamError translates a proxy/upstream error into the HTTP status
// defined by the gateway error mapping policy (Phase 7):
//
//	circuit breaker open        → 503 Service Unavailable
//	upstream timeout            → 504 Gateway Timeout
//	any other upstream failure  → 502 Bad Gateway
//
// Client cancellations are NOT mapped — the ErrorHandler skips them entirely;
// writing an error response to a client that already left is meaningless.
func MapUpstreamError(err error) int {
	var cbErr *CircuitBreakerOpenError
	if errors.As(err, &cbErr) {
		return http.StatusServiceUnavailable
	}

	// context.DeadlineExceeded covers deadline propagation from the request
	// lifecycle; net.Error with Timeout() covers dial timeouts, TLS handshake
	// timeouts and ResponseHeaderTimeout (surfaced through *url.Error, whose
	// own net.Error methods delegate to the wrapped error, so errors.As
	// resolves any layer of the chain).
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return http.StatusGatewayTimeout
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return http.StatusGatewayTimeout
	}

	return http.StatusBadGateway
}
