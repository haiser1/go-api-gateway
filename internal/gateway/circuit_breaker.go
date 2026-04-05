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
// These are compile-time constants; consider making them configurable
// per-service if different upstreams have different reliability profiles.
const (
	failureThreshold = 5                // consecutive failures before tripping to Open
	breakerTimeout   = 30 * time.Second // cooldown before transitioning Open → HalfOpen
)

// CircuitBreaker implements a thread-safe state machine that protects upstream
// services from cascading failures. It uses a combination of atomic reads
// (for the lock-free hot path) and mutex-guarded writes (for state transitions).
type CircuitBreaker struct {
	mu              sync.Mutex
	state           int32       // read via atomic.LoadInt32 (lock-free), written under mu
	failureCount    int         // consecutive failures since last success/reset
	lastFailureTime time.Time   // timestamp of last failure; drives Open → HalfOpen transition
	halfOpenProbe   atomic.Bool // CAS flag: ensures only one probe request in HalfOpen state
}

// NewCircuitBreaker creates a new circuit breaker in closed state
func NewCircuitBreaker() *CircuitBreaker {
	return &CircuitBreaker{state: StateClosed}
}

// AllowRequest checks if a request should be allowed through
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
		// Check if timeout has passed
		if time.Since(cb.lastFailureTime) > breakerTimeout {
			atomic.StoreInt32(&cb.state, StateHalfOpen)
			log.Info().Msg("Circuit Breaker: Transitioned to HALF-OPEN state")
			// Fall through to half-open logic
		} else {
			return false
		}
		fallthrough

	case StateHalfOpen:
		// Only allow ONE probe request to prevent thundering herd
		// Use CompareAndSwap to ensure only one goroutine wins
		if cb.halfOpenProbe.CompareAndSwap(false, true) {
			log.Debug().Msg("Circuit Breaker: Allowing single probe request")
			return true
		}
		// Another request is already probing
		return false
	}

	return false
}

// RecordSuccess records a successful request.
// On the hot path (Closed state), this is a no-op — failure count is already
// reset during every state transition to Closed, so no lock is needed.
func (cb *CircuitBreaker) RecordSuccess() {
	// Fast path: nothing to do if closed (99%+ of calls hit this)
	if atomic.LoadInt32(&cb.state) == StateClosed {
		return
	}

	// Slow path: handle HalfOpen → Closed transition
	cb.mu.Lock()
	defer cb.mu.Unlock()

	if atomic.LoadInt32(&cb.state) == StateHalfOpen {
		// Probe succeeded — close the circuit and allow all traffic
		atomic.StoreInt32(&cb.state, StateClosed)
		cb.failureCount = 0
		cb.halfOpenProbe.Store(false)
		log.Info().Msg("Circuit Breaker: CLOSED - Service recovered")
	}
}

// RecordFailure records a failed request and potentially trips the circuit.
// In HalfOpen state, any failure immediately reopens the circuit.
// In Closed state, failures are counted and the circuit opens after failureThreshold consecutive failures.
func (cb *CircuitBreaker) RecordFailure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.lastFailureTime = time.Now()

	if atomic.LoadInt32(&cb.state) == StateHalfOpen {
		// Probe failed — reopen the circuit and reset the probe flag
		atomic.StoreInt32(&cb.state, StateOpen)
		cb.halfOpenProbe.Store(false)
		log.Warn().Msg("Circuit Breaker: OPEN - Probe request failed")
		return
	}

	cb.failureCount++
	if cb.failureCount >= failureThreshold {
		atomic.StoreInt32(&cb.state, StateOpen)
		log.Warn().Int("failures", cb.failureCount).Msg("Circuit Breaker: OPEN - Failure threshold reached")
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

// --- CIRCUIT BREAKER REGISTRY ---

// circuitBreakers stores circuit breakers by service ID
var circuitBreakers sync.Map

// GetCircuitBreaker returns or creates a circuit breaker for a service
func GetCircuitBreaker(serviceId string) *CircuitBreaker {
	if cb, ok := circuitBreakers.Load(serviceId); ok {
		return cb.(*CircuitBreaker)
	}
	cb := NewCircuitBreaker()
	actual, _ := circuitBreakers.LoadOrStore(serviceId, cb)
	return actual.(*CircuitBreaker)
}
