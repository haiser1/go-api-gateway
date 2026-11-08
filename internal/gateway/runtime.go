package gateway

import (
	"net/http"
	"sync/atomic"
	"time"

	"github.com/haiser1/go-api-gateway/internal/auth"
)

// RuntimeSnapshot is the complete dependency set the proxy serves requests
// from. Once published it is immutable: every request reads it exactly once
// via the proxy's atomic pointer and keeps using it even after a newer
// version is published.
//
// Circuit breakers are the deliberate exception — they live in a shared
// process-wide registry keyed by stable service ID (circuit_breaker.go) so
// breaker state survives config reloads. Everything else a request touches
// (tree, compiled handlers, reverse proxies, transports, upstream data) is
// resolved inside this snapshot.
type RuntimeSnapshot struct {
	// Version increases by one on every successful publication; a failed
	// build never bumps it.
	Version   uint64
	CreatedAt time.Time

	// Auth is the compiled authentication policy for this snapshot. Nil
	// means authentication is disabled.
	Auth *auth.Runtime

	// Tree maps method+path to *ResolvedRoute entries.
	Tree *RadixTree

	// Routes lists every route that was successfully compiled into the tree.
	Routes []*ResolvedRoute

	// Transports holds the connection pools used by this runtime's routes,
	// keyed by service ID. Each route's ReverseProxy holds the same pointer.
	Transports map[string]*http.Transport

	// Lease bookkeeping: snapshots are reference-counted so retirement of
	// their transports waits until the last in-flight request completed. The
	// atomic pointer swap alone cannot tell whether an old snapshot is still
	// being used.
	inflight atomic.Int64 // requests currently serving from this snapshot
	draining atomic.Bool  // retirement triggered (CAS — idempotent)
	drainFn  func()       // invoked once when inflight reaches 0 after retirement
}

// trackRequest registers an in-flight request against this snapshot.
func (s *RuntimeSnapshot) trackRequest() {
	s.inflight.Add(1)
}

// releaseRequest marks an in-flight request as finished. When retirement has
// been triggered and this was the last request, the drain callback runs
// (exactly once) to release the snapshot's resources.
func (s *RuntimeSnapshot) releaseRequest() {
	if n := s.inflight.Add(-1); n == 0 && s.draining.Load() && s.drainFn != nil {
		s.drainFn()
	}
}
