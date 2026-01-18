package gateway

import (
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

// --- CIRCUIT BREAKER & TRANSPORT ---

type BreakerState struct {
	mu          sync.RWMutex
	isDown      bool
	lastFailure time.Time
}

const breakerTimeout = 30 * time.Second

func (b *BreakerState) IsDown() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	if !b.isDown {
		return false
	}
	if time.Since(b.lastFailure) > breakerTimeout {
		return false // Half-open logic (allow retry)
	}
	return true
}

func (b *BreakerState) RecordFailure() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.isDown = true
	b.lastFailure = time.Now()
}

func (b *BreakerState) RecordSuccess() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.isDown {
		b.isDown = false
		log.Info().Msg("Circuit Breaker: Sirkuit ditutup, service kembali online.")
	}
}

var serviceBreakers sync.Map

func getBreaker(host string) *BreakerState {
	iface, _ := serviceBreakers.LoadOrStore(host, &BreakerState{})
	return iface.(*BreakerState)
}

type latencyTransport struct {
	wrapped http.RoundTripper
}

func (t *latencyTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	host := r.Host
	breaker := getBreaker(host)

	if breaker.IsDown() {
		return nil, fmt.Errorf("circuit breaker is open for %s", host)
	}

	resp, err := t.wrapped.RoundTrip(r)

	if err != nil || (resp != nil && resp.StatusCode >= 500) {
		breaker.RecordFailure()
		log.Error().Err(err).Str("host", host).Msg("Upstream failure")
	} else {
		breaker.RecordSuccess()
	}

	return resp, err
}
