package gateway

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/haiser1/go-api-gateway/internal/config"
	"github.com/rs/zerolog/log"
)

// Transport tuning constants. Both createTransport and transportConfigHash
// reference the same values so the fingerprint always describes exactly what
// a transport was built from.
const (
	dialerKeepAlive       = 30 * time.Second
	tlsHandshakeTimeout   = 10 * time.Second
	expectContinueTimeout = 1 * time.Second
	idleConnTimeout       = 90 * time.Second

	maxIdleConns        = 500
	maxIdleConnsPerHost = 100
	maxConnsPerHost     = 250

	transportWriteBuffer = 32 * 1024 // 32KB — reduces syscalls for large payloads
	transportReadBuffer  = 32 * 1024

	forceAttemptHTTP2  = true  // Required: custom DialContext disables HTTP/2 otherwise
	disableCompression = false // set true if upstream already compresses responses
)

type transportEntry struct {
	transport *http.Transport

	// configHash fingerprints every transport-affecting configuration field.
	configHash string
}

// serviceTransports caches HTTP transports per service ID.
// Each service gets its own transport with a dedicated connection pool,
// allowing per-service timeout configuration without cross-contamination.
// An entry is atomically replaced whenever the transport-affecting config of
// a service changes; superseded entries are closed for idle connections only.
var (
	serviceTransports = make(map[string]*transportEntry)
	transportMu       sync.RWMutex
)

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
			KeepAlive: dialerKeepAlive,
		}).DialContext,
		ForceAttemptHTTP2:     forceAttemptHTTP2,
		TLSHandshakeTimeout:   tlsHandshakeTimeout,
		ResponseHeaderTimeout: time.Duration(service.ReadTimeout) * time.Second,
		ExpectContinueTimeout: expectContinueTimeout,
		MaxIdleConns:          maxIdleConns,
		MaxIdleConnsPerHost:   maxIdleConnsPerHost,
		MaxConnsPerHost:       maxConnsPerHost,
		IdleConnTimeout:       idleConnTimeout,
		DisableCompression:    disableCompression,
		WriteBufferSize:       transportWriteBuffer,
		ReadBufferSize:        transportReadBuffer,
	}
}

// transportConfigHash returns a fingerprint of every field consumed by
// createTransport. Service.Timeout is deliberately excluded — it is a total
// request timeout enforced outside the transport — so changing it does not
// trigger an unnecessary replacement.
func transportConfigHash(service config.Service) string {
	payload := fmt.Sprintf(
		"connect_timeout=%d|response_header_timeout=%d|dialer_keep_alive=%s|tls_handshake_timeout=%s|expect_continue_timeout=%s|idle_conn_timeout=%s|force_attempt_http2=%t|disable_compression=%t|max_idle_conns=%d|max_idle_conns_per_host=%d|max_conns_per_host=%d|write_buffer_size=%d|read_buffer_size=%d|proxy=nil",
		service.ConnectTimeout,
		service.ReadTimeout,
		dialerKeepAlive,
		tlsHandshakeTimeout,
		expectContinueTimeout,
		idleConnTimeout,
		forceAttemptHTTP2,
		disableCompression,
		maxIdleConns,
		maxIdleConnsPerHost,
		maxConnsPerHost,
		transportWriteBuffer,
		transportReadBuffer,
	)
	sum := sha256.Sum256([]byte(payload))
	return hex.EncodeToString(sum[:])
}

// GetServiceTransport returns a cached *http.Transport for the given service.
// The cached entry carries a fingerprint of all transport-affecting config;
// when the config changes, the entry is replaced under an exclusive lock with
// a transport built from the latest settings. The superseded transport is
// closed for idle connections only, so in-flight requests that still use it
// are never interrupted.
func GetServiceTransport(service config.Service) *http.Transport {
	// Normalize zero values first so e.g. ConnectTimeout 0 (default 10) and
	// an explicit 10 never produce two distinct transports.
	svc := service
	svc.ApplyDefaults()

	hash := transportConfigHash(svc)

	// Fast path: read-locked lookup
	transportMu.RLock()
	entry, ok := serviceTransports[svc.Id]
	transportMu.RUnlock()
	if ok && entry.configHash == hash {
		return entry.transport
	}

	// Slow path: exclusive replacement
	transportMu.Lock()
	defer transportMu.Unlock()

	// Double-check: another goroutine may have created or replaced the entry
	// while we waited for the write lock.
	if entry, ok := serviceTransports[svc.Id]; ok && entry.configHash == hash {
		return entry.transport
	}

	newTransport := createTransport(svc)
	old, existed := serviceTransports[svc.Id]
	serviceTransports[svc.Id] = &transportEntry{transport: newTransport, configHash: hash}

	if existed {
		log.Info().
			Str("service", svc.Id).
			Int("connect_timeout_sec", svc.ConnectTimeout).
			Int("read_timeout_sec", svc.ReadTimeout).
			Msg("Replaced transport for service (config changed)")
		// The superseded entry is no longer served to new callers. Closing
		// idle connections does not touch connections owned by in-flight
		// requests, so requests using the old transport complete normally.
		old.transport.CloseIdleConnections()
	} else {
		log.Info().
			Str("service", svc.Id).
			Int("connect_timeout_sec", svc.ConnectTimeout).
			Int("read_timeout_sec", svc.ReadTimeout).
			Msg("Created new transport for service")
	}

	return newTransport
}

// DropTransport evicts the cache entry for a service when it still maps to
// the given transport (e.g. a retired runtime's transport after the service
// was removed or replaced). Returns true when an entry was removed; false
// means the entry was replaced by a newer transport and is left intact.
func DropTransport(serviceId string, transport *http.Transport) bool {
	transportMu.Lock()
	defer transportMu.Unlock()

	entry, ok := serviceTransports[serviceId]
	if !ok || entry.transport != transport {
		return false
	}
	delete(serviceTransports, serviceId)
	log.Info().Str("service", serviceId).Msg("Dropped transport cache entry for retired runtime")
	return true
}

// classify429AsFailure toggles whether upstream HTTP 429 (rate-limited)
// responses count as circuit breaker failures. Default: disabled (429 is a
// normal, expected "busy-but-alive" signal; the upstream is healthy). Upstream
// owners who want rate limiting to strain breakers can enable it globally at
// startup or via the runtime policy (task 4 of Phase 6: "429 = configurable").
var classify429AsFailure atomic.Bool

// Classify429AsFailure sets the global 429 failure policy (thread-safe).
func Classify429AsFailure(enabled bool) {
	classify429AsFailure.Store(enabled)
}

// isClientCancellation reports whether a RoundTrip error means the CALLING
// client (or the gateway itself) canceled the request so the upstream never
// had the chance to be judged. errors.Is unwraps through *url.Error and SOCKS
// the whole wrapping chain, covering both plain cancellation and
// "operation was canceled" variants surfaced by the transport.
func isClientCancellation(err error) bool {
	return errors.Is(err, context.Canceled)
}

// upstreamOutcome classifies a single upstream attempt (Phase 6 taxonomy,
// shared by the circuit-breaker transport and the per-target health
// tracking of the load balancer so both layers judge the same way):
type upstreamOutcome uint8

const (
	outSuccess upstreamOutcome = iota
	outFailure
	outNeutral // client cancellation — no signal about upstream health
)

// classifyUpstreamOutcome maps a finished attempt to its outcome:
//
//	client cancellation (context.Canceled) → neutral
//	network / transport / timeout error    → failure
//	upstream 5xx (incl. 502/503/504)       → failure
//	upstream 429 (policy enabled)          → failure
//	any other upstream response            → success
func classifyUpstreamOutcome(resp *http.Response, err error) upstreamOutcome {
	if isClientCancellation(err) {
		return outNeutral
	}
	if err != nil {
		return outFailure
	}
	if resp == nil {
		return outNeutral
	}
	if resp.StatusCode >= 500 {
		return outFailure
	}
	if resp.StatusCode == 429 && classify429AsFailure.Load() {
		return outFailure
	}
	return outSuccess
}

// lbTargetTransport records the outcome of each upstream attempt against the
// per-target health tracker of the load balancer runtime — the wiring that
// makes target selection health-aware (Phase 8 tasks 4 and 7). It wraps the
// service-level circuit-breaker transport, which already recorded the same
// attempt against the shared per-service breaker, so both layers observe
// every attempt exactly once.
type lbTargetTransport struct {
	inner     http.RoundTripper
	runtime   *UpstreamRuntime
	targetIdx int
}

func NewLbTargetTransport(inner http.RoundTripper, runtime *UpstreamRuntime, targetIdx int) http.RoundTripper {
	return &lbTargetTransport{inner: inner, runtime: runtime, targetIdx: targetIdx}
}

func (t *lbTargetTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	resp, err := t.inner.RoundTrip(r)
	switch classifyUpstreamOutcome(resp, err) {
	case outFailure:
		t.runtime.MarkFailure(t.targetIdx)
	case outSuccess:
		t.runtime.MarkSuccess(t.targetIdx)
	default:
		// Neutral: the caller went away — no upstream health signal.
	}
	return resp, err
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
// forwarding the request, and classifies the outcome after receiving the
// response (failure classification per the Phase 6 plan):
//
//	client cancellation (context.Canceled) → RecordClientCancellation (neutral)
//	network error / upstream timeout       → RecordFailure
//	upstream 5xx (incl. 502/503/504)       → RecordFailure
//	upstream 429 (policy enabled)          → RecordFailure
//	any other upstream response            → RecordSuccess
//
// Every upstream attempt is recorded (retries included — the breaker
// protects the upstream from repeated attempts, each attempt counts).
func (t *circuitBreakerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	cb := GetCircuitBreaker(t.serviceId)

	if !cb.AllowRequest() {
		return nil, &CircuitBreakerOpenError{ServiceId: t.serviceId}
	}

	resp, err := t.wrapped.RoundTrip(r)

	switch classifyUpstreamOutcome(resp, err) {
	case outNeutral:
		// The caller went away — NOT a signal about upstream health.
		cb.RecordClientCancellation()
		log.Debug().Err(err).Str("service", t.serviceId).
			Msg("Upstream attempt aborted by client cancellation")
		return resp, err

	case outFailure:
		cb.RecordFailure()
		switch {
		case err != nil:
			log.Error().Err(err).Str("service", t.serviceId).Msg("Upstream failure")
		case resp.StatusCode >= 500:
			log.Warn().Int("status", resp.StatusCode).Str("service", t.serviceId).Msg("Upstream response error")
		default:
			log.Warn().Int("status", resp.StatusCode).Str("service", t.serviceId).
				Msg("Upstream rate-limited (429 counting as failure)")
		}

	default:
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
