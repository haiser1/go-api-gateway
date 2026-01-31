package gateway

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog/log"
)

// Circuit breaker states
const (
	StateClosed   = iota // Normal operation
	StateOpen            // Blocking requests
	StateHalfOpen        // Testing with single request
)

// Circuit breaker configuration
const (
	failureThreshold = 5                // Open after 5 consecutive failures
	breakerTimeout   = 30 * time.Second // Time before trying half-open
)

type CircuitBreaker struct {
	mu              sync.Mutex
	state           int
	failureCount    int
	lastFailureTime time.Time
	halfOpenProbe   atomic.Bool // Prevents thundering herd in half-open
}

// NewCircuitBreaker creates a new circuit breaker in closed state
func NewCircuitBreaker() *CircuitBreaker {
	return &CircuitBreaker{state: StateClosed}
}

// AllowRequest checks if a request should be allowed through
func (cb *CircuitBreaker) AllowRequest() bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	switch cb.state {
	case StateClosed:
		return true

	case StateOpen:
		// Check if timeout has passed
		if time.Since(cb.lastFailureTime) > breakerTimeout {
			cb.state = StateHalfOpen
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

// RecordSuccess records a successful request
func (cb *CircuitBreaker) RecordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	switch cb.state {
	case StateHalfOpen:
		// Probe succeeded, close the circuit
		cb.state = StateClosed
		cb.failureCount = 0
		cb.halfOpenProbe.Store(false)
		log.Info().Msg("Circuit Breaker: CLOSED - Service recovered")
	case StateClosed:
		// Reset failure count on success
		cb.failureCount = 0
	}
}

// RecordFailure records a failed request
func (cb *CircuitBreaker) RecordFailure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.lastFailureTime = time.Now()

	if cb.state == StateHalfOpen {
		// Probe failed, back to open
		cb.state = StateOpen
		cb.halfOpenProbe.Store(false)
		log.Warn().Msg("Circuit Breaker: OPEN - Probe request failed")
		return
	}

	cb.failureCount++
	if cb.failureCount >= failureThreshold {
		cb.state = StateOpen
		log.Warn().Int("failures", cb.failureCount).Msg("Circuit Breaker: OPEN - Failure threshold reached")
	}
}

// GetState returns current state for monitoring
func (cb *CircuitBreaker) GetState() string {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	switch cb.state {
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
