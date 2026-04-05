package gateway

import (
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/haiser1/go-api-gateway/internal/config"
	"github.com/rs/zerolog/log"
)

type transportEntry struct {
	transport      *http.Transport
	connectTimeout int
	readTimeout    int
}

// serviceTransports caches HTTP transports per service ID.
// Each service gets its own transport with a dedicated connection pool,
// allowing per-service timeout configuration without cross-contamination.
var serviceTransports sync.Map

// createTransport builds a new *http.Transport configured with the given service's timeouts.
//
// Key tuning parameters:
//   - MaxIdleConns / MaxIdleConnsPerHost: control idle connection pool size.
//   - MaxConnsPerHost: caps total active connections per host to prevent fd exhaustion.
//   - WriteBufferSize / ReadBufferSize: larger buffers reduce syscall overhead for big payloads.
//   - ForceAttemptHTTP2: required because custom DialContext disables HTTP/2 by default.
func createTransport(service config.Service) *http.Transport {
	return &http.Transport{
		DialContext: (&net.Dialer{
			Timeout:   time.Duration(service.ConnectTimeout) * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		ForceAttemptHTTP2:     true,  // Required: custom DialContext disables HTTP/2 otherwise
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: time.Duration(service.ReadTimeout) * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		MaxIdleConns:          500, // total idle pool across all hosts
		MaxIdleConnsPerHost:   100, // idle pool per individual host
		MaxConnsPerHost:       250, // cap total active connections per host (prevents fd exhaustion)
		IdleConnTimeout:       90 * time.Second,
		DisableCompression:    false, // set true if upstream already compresses responses
		WriteBufferSize:       32 * 1024, // 32KB — reduces syscalls for large payloads
		ReadBufferSize:        32 * 1024, // 32KB
	}
}

// GetServiceTransport returns a cached *http.Transport for the given service.
// If the service's timeout settings have changed since the transport was created,
// the old transport is replaced with a new one using LoadOrStore to prevent
// duplicate creation under concurrent access.
//
// Each transport maintains its own connection pool (MaxIdleConns, MaxIdleConnsPerHost)
// scoped to that service.
func GetServiceTransport(service config.Service) *http.Transport {
	// Fast path: check cache for existing transport with matching timeouts
	if cached, ok := serviceTransports.Load(service.Id); ok {
		entry := cached.(*transportEntry)
		if entry.connectTimeout == service.ConnectTimeout && entry.readTimeout == service.ReadTimeout {
			return entry.transport
		}
		// Timeouts changed — evict stale entry and fall through to create a new one.
		// CloseIdleConnections is safe to call even if in-flight requests are using
		// the transport; it only closes currently-idle connections.
		entry.transport.CloseIdleConnections()
	}

	// Create new transport with service-specific timeouts
	newTransport := createTransport(service)
	entry := &transportEntry{
		transport:      newTransport,
		connectTimeout: service.ConnectTimeout,
		readTimeout:    service.ReadTimeout,
	}

	// LoadOrStore ensures only one transport is created if multiple goroutines
	// race on the same service ID. The loser closes its unused transport.
	actual, loaded := serviceTransports.LoadOrStore(service.Id, entry)
	if loaded {
		newTransport.CloseIdleConnections()
		return actual.(*transportEntry).transport
	}

	log.Info().Str("service", service.Id).Msg("Created new transport for service")
	return newTransport
}

// circuitBreakerTransport wraps an http.RoundTripper with circuit breaker
// protection. It delegates to GetCircuitBreaker for state management,
// recording successes/failures and blocking requests when the circuit is open.
type circuitBreakerTransport struct {
	wrapped   http.RoundTripper
	serviceId string
}

// NewCircuitBreakerTransport wraps the given transport with circuit breaker logic.
// The circuit breaker state is shared per serviceId (via the global registry),
// so multiple routes pointing to the same service share one breaker.
func NewCircuitBreakerTransport(transport http.RoundTripper, serviceId string) http.RoundTripper {
	return &circuitBreakerTransport{
		wrapped:   transport,
		serviceId: serviceId,
	}
}

// RoundTrip implements http.RoundTripper. It checks the circuit breaker before
// forwarding the request, and records success/failure after receiving the response.
// HTTP 5xx responses are treated as failures.
func (t *circuitBreakerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	cb := GetCircuitBreaker(t.serviceId)

	if !cb.AllowRequest() {
		return nil, &CircuitBreakerOpenError{ServiceId: t.serviceId}
	}

	resp, err := t.wrapped.RoundTrip(r)

	if err != nil || (resp != nil && resp.StatusCode >= 500) {
		cb.RecordFailure()
		if err != nil {
			log.Error().Err(err).Str("service", t.serviceId).Msg("Upstream failure")
		} else {
			log.Warn().Int("status", resp.StatusCode).Str("service", t.serviceId).Msg("Upstream response error")
		}
	} else {
		cb.RecordSuccess()
	}

	return resp, err
}

// CircuitBreakerOpenError is returned when a request is blocked by an open circuit breaker.
// This is a typed error so callers can distinguish circuit breaker rejections
// from actual network errors (e.g. via errors.As).
type CircuitBreakerOpenError struct {
	ServiceId string
}

func (e *CircuitBreakerOpenError) Error() string {
	return "circuit breaker is open for service " + e.ServiceId
}
