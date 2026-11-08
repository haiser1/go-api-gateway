package gateway

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog/log"
)

// Circuit breaker states follow the standard closed → open → half-open pattern.
const (
	StateClosed   int32 = iota // Normal: all requests pass through
	StateOpen                  // Tripped: all requests are rejected
	StateHalfOpen              // Recovery: one probe request is allowed through
)

// Circuit breaker thresholds.
//
// breakerTimeout is a package VAR only for the benefit of HALF-OPEN tests;
// production code must treat it as a constant.
// Consider making both configurable per-service if different upstreams have
// different reliability profiles.
var (
	failureThreshold = 5                // consecutive failures before tripping to Open
	breakerTimeout   = 30 * time.Second // cooldown before transitioning Open → HalfOpen
)

// CircuitBreaker implements a thread-safe state machine that protects upstream
// services from cascading failures. It uses a combination of atomic reads
// (for the lock-free hot path) and mutex-guarded writes (for state transitions).
//
// RECORDING CONTRACT (per the implementation plan, Phase 6):
//   - the breaker counts every UPSTREAM ATTEMPT (each RoundTrip), not every
//     client request — a request with 3 retries produces up to 3 recordings,
//     because the breaker exists to shield the upstream from repeated
//     attempts;
//   - upstream timeout / network error → failure;
//   - upstream 5xx → failure;
//   - upstream 4xx → success;
//   - 429 → success by default, failure when 429 handling is enabled;
//   - CLIENT cancellation (incoming request context canceled) is neither a
//     failure nor a success — RecordClientCancellation keeps the breaker
//     state and, in HALF-OPEN, releases the probe slot for the next request.
type CircuitBreaker struct {
	serviceId       string
	mu              sync.Mutex
	state           int32        // read via atomic.LoadInt32 (lock-free), written under mu
	failureCount    atomic.Int32 // consecutive failures since last success/reset
	lastFailureTime time.Time    // timestamp of last failure; drives Open → HalfOpen transition
	halfOpenProbe   atomic.Bool  // CAS flag: ensures only one probe request in HalfOpen state
}

// NewCircuitBreaker creates a new circuit breaker in closed state
func NewCircuitBreaker(serviceId string) *CircuitBreaker {
	return &CircuitBreaker{serviceId: serviceId, state: StateClosed}
}

// AllowRequest checks if a request should be allowed through.
// Returns true when:
//   - the state is Closed (lock-free fast path), or
//   - the state is Open but the cooldown elapsed and this caller won the
//     single HALF-OPEN probe slot, or
//   - the state is already HalfOpen and this caller won the probe slot.
func (cb *CircuitBreaker) AllowRequest() bool {
	// Fast path: lock-free check for closed state (hot path optimization)
	// In normal operation (99%+ of calls), this avoids mutex contention entirely
	if atomic.LoadInt32(&cb.state) == StateClosed {
		return true
	}

	// Slow path: only entered when circuit is Open or HalfOpen
	cb.mu.Lock()
	defer cb.mu.Unlock()

	switch atomic.LoadInt32(&cb.state) {
	case StateClosed:
		// Re-check after acquiring lock (might have changed)
		return true

	case StateOpen:
		// Check if cooldown has passed
		if time.Since(cb.lastFailureTime) > breakerTimeout {
			atomic.StoreInt32(&cb.state, StateHalfOpen)
			log.Info().Str("service", cb.serviceId).
				Msg("Circuit Breaker: OPEN → HALF-OPEN (cooldown elapsed)")
			// Fall through to half-open logic
		} else {
			return false
		}
		fallthrough

	case StateHalfOpen:
		// Only allow ONE probe request to prevent thundering herd.
		// Use CompareAndSwap to ensure only one goroutine wins.
		if cb.halfOpenProbe.CompareAndSwap(false, true) {
			log.Debug().Str("service", cb.serviceId).
				Msg("Circuit Breaker: allowing single HALF-OPEN probe request")
			return true
		}
		// Another request is already probing
		return false
	}

	return false
}

// RecordSuccess records a successful upstream attempt. Healthy behavior
// must always reset the consecutive-failure counter — a mixed 4-fail /
// 1-success / 4-fail pattern must NOT trip the breaker (consecutive
// semantics). The reset is lock-free (atomic store), the HALF-OPEN → CLOSED
// transition stays mutex-guarded.
func (cb *CircuitBreaker) RecordSuccess() {
	// Fast path: lock-free on the hot path (99%+ of calls).
	if atomic.LoadInt32(&cb.state) == StateClosed {
		cb.failureCount.Store(0)
		return
	}

	// Slow path: handle HalfOpen → Closed transition
	cb.mu.Lock()
	defer cb.mu.Unlock()

	if atomic.LoadInt32(&cb.state) == StateHalfOpen {
		// Probe succeeded — close the circuit and allow all traffic
		atomic.StoreInt32(&cb.state, StateClosed)
		cb.failureCount.Store(0)
		cb.halfOpenProbe.Store(false)
		log.Info().Str("service", cb.serviceId).
			Msg("Circuit Breaker: HALF-OPEN → CLOSED - Service recovered")
	}
}

// RecordFailure records a failed upstream attempt and potentially trips the
// circuit. In HalfOpen state, any failure immediately reopens the circuit.
// In Closed state, failures are counted and the circuit opens after
// failureThreshold consecutive failures.
func (cb *CircuitBreaker) RecordFailure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.lastFailureTime = time.Now()

	switch state := atomic.LoadInt32(&cb.state); state {
	case StateHalfOpen:
		// Probe failed — reopen the circuit, release the probe slot and
		// start the next CLOSED-phase counter from zero.
		atomic.StoreInt32(&cb.state, StateOpen)
		cb.failureCount.Store(0)
		cb.halfOpenProbe.Store(false)
		log.Warn().Str("service", cb.serviceId).
			Msg("Circuit Breaker: OPEN - Probe request failed")

	case StateClosed:
		failures := cb.failureCount.Add(1)
		if failures >= int32(failureThreshold) {
			atomic.StoreInt32(&cb.state, StateOpen)
			log.Warn().Str("service", cb.serviceId).
				Int32("failures", failures).
				Msg("Circuit Breaker: CLOSED → OPEN (failure threshold reached)")
		}

	default:
		// Already OPEN: just refresh lastFailureTime (done above).
	}
}

// RecordClientCancellation records that an upstream attempt was aborted
// because the CALLING client went away (request context canceled). This is
// explicitly NOT an upstream failure (it must never trip the breaker) and
// NOT a success; if the probe request was the canceled one, the probe slot
// is released so the circuit does not wedge in HALF-OPEN with a lost probe.
func (cb *CircuitBreaker) RecordClientCancellation() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	if atomic.LoadInt32(&cb.state) == StateHalfOpen {
		// The probe was inconclusive (client vanished, not the upstream's
		// fault): free the single probe slot, stay in HALF-OPEN.
		cb.halfOpenProbe.Store(false)
		log.Info().Str("service", cb.serviceId).
			Msg("Circuit Breaker: HALF-OPEN probe aborted by client cancellation; slot released")
	}
}

// GetState returns the current state as a human-readable string for monitoring/logging.
func (cb *CircuitBreaker) GetState() string {
	switch atomic.LoadInt32(&cb.state) {
	case StateClosed:
		return "CLOSED"
	case StateOpen:
		return "OPEN"
	case StateHalfOpen:
		return "HALF-OPEN"
	default:
		return "UNKNOWN"
	}
}

// Reset manually resets the circuit breaker back to the CLOSED state with zero failures.
func (cb *CircuitBreaker) Reset() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	atomic.StoreInt32(&cb.state, StateClosed)
	cb.failureCount.Store(0)
	cb.lastFailureTime = time.Time{}
	cb.halfOpenProbe.Store(false)
	log.Info().Str("service", cb.serviceId).
		Msg("Circuit Breaker: manually reset to CLOSED state")
}

// Stats returns the current state, consecutive failure count, and last failure time.
func (cb *CircuitBreaker) Stats() (state string, failures int, lastFailure time.Time) {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	return cb.GetState(), int(cb.failureCount.Load()), cb.lastFailureTime
}

// --- CIRCUIT BREAKER REGISTRY ---

// circuitBreakers stores circuit breakers by service ID. The registry is
// keyed by the STABLE service ID and NEVER cleared automatically, so a
// config reload (which rebuilds transports and the routing tree) keeps the
// breaker state — an OPEN circuit survives reloads by design (see
// buildRuntimeSnapshot: each rebuild resolves the breaker via this
// registry). Entries are only removed manually via the Admin API Reset or
// when the process exits.
var circuitBreakers sync.Map

// GetCircuitBreaker returns or creates a circuit breaker for a service.
// LoadOrStore guarantees that concurrent first calls for the same service
// observe ONE shared instance — repeated breaker resets would otherwise be
// possible if racing callers each created their own instance.
func GetCircuitBreaker(serviceId string) *CircuitBreaker {
	if cb, ok := circuitBreakers.Load(serviceId); ok {
		return cb.(*CircuitBreaker)
	}
	cb := NewCircuitBreaker(serviceId)
	actual, _ := circuitBreakers.LoadOrStore(serviceId, cb)
	return actual.(*CircuitBreaker)
}
