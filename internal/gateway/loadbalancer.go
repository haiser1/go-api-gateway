package gateway

import (
	"net/http"
	"sort"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/haiser1/go-api-gateway/internal/config"
	"github.com/rs/zerolog/log"
)

// --- Upstream runtime (Phase 8) ------------------------------------------
//
// UpstreamRuntime performs request-time, health-aware target selection for
// one upstream. It is created ONCE per snapshot (per upstream ID), shared by
// all routes and services that resolve through the same upstream, and is
// safe for concurrent use — the hot path (round-robin over healthy targets)
// is lock-free (atomic ops only); only smooth weighted balancing and
// recovery decisions take a short mutex.
//
// Supported algorithms (validator one-of):
//   - round-robin
//   - weighted-round-robin (smooth weighted round robin)
//
// Designed for extension: least-connections and consistent hashing plug in
// as new pick strategies behind the same interface once per-target
// connection accounting exists.

const (
	// lbTargetEjectThreshold is the number of consecutive failed upstream
	// attempts after which a target is ejected from the healthy pool.
	// It is deliberately lower than the service-level circuit breaker
	// threshold (failureThreshold) so a single bad target is removed by the
	// selector before the whole-service breaker trips.
	lbTargetEjectThreshold = 3

	// lbTargetCooldown is how long an ejected target stays fully ignored
	// before it becomes eligible again for a re-probe (only happens when
	// either all other targets are unhealthy or it is picked during the
	// recovery sweep).
	lbTargetCooldown = 30 * time.Second
)

// timeoutless state of a single configured target.
type lbTarget struct {
	name   string // "host:port" — the health/exclusion identity
	weight int

	failures   atomic.Int32 // consecutive failed attempts
	lastFailed atomic.Int64 // unix nano of the last failure
	ejected    atomic.Bool  // out of the healthy pool while true
}

// UpstreamRuntime selects targets at REQUEST time (never at route build
// time), so config changes with different targets are picked up on the next
// request and in-flight traffic can spread across all healthy backends.
type UpstreamRuntime struct {
	id        string
	algorithm string
	targets   []*lbTarget

	// hasWeights is true when any target weight differs from 1 — switches
	// pick() to the smooth weighted round-robin path.
	hasWeights bool

	// rr drives the lock-free round-robin position for the uniform case.
	rr atomic.Uint64

	// wmu guards the smooth weighted round-robin counters below.
	wmu       sync.Mutex
	wcounters []int
}

// NewUpstreamRuntime builds the runtime for an (already defaulted) upstream
// snapshot. Targets keep their configured order — the basis of the
// deterministic round-robin walk.
func NewUpstreamRuntime(u config.Upstream) *UpstreamRuntime {
	rt := &UpstreamRuntime{
		id:        u.Id,
		algorithm: u.Algorithm,
		targets:   make([]*lbTarget, len(u.Targets)),
	}

	maxWeight := 1
	minWeight := 1 << 30
	for i, t := range u.Targets {
		lt := &lbTarget{}
		lt.name = t.Host + ":" + itoa(t.Port)
		lt.weight = maxInt(1, t.Weight)
		maxWeight = maxInt(maxWeight, lt.weight)
		minWeight = minInt(minWeight, lt.weight)
		rt.targets[i] = lt
	}
	// Weighted balancing only when weights actually differ: the common
	// uniform case (defaults normalize unset weights, e.g. all 100) keeps
	// the lock-free round-robin hot path.
	rt.hasWeights = maxWeight != minWeight

	if rt.hasWeights {
		rt.wcounters = make([]int, len(rt.targets))
	}
	return rt
}

// pick selects the next healthy target index, excluding already-tried
// targets during retries (exclude may be nil on the first attempt).
//
// Selection policy:
//  1. Healthy targets that are not excluded.
//  2. If ALL healthy candidates are excluded/unavailable and there are no
//     excluded-but-ejected alternatives: ejected targets past their
//     cooldown become re-probe candidates (oldest failure first).
//  3. If EVERY ejected target is present and none is past cooldown (a fully
//     dead upstream), the LEAST recently failed target is force-probed.
//     This fail-open behavior (task 8) lets the service-level circuit
//     breaker remain the single authority for declaring an upstream dead,
//     and gives a recovering upstream a chance to be rediscovered without
//     waiting for the cooldown.
func (u *UpstreamRuntime) pick(exclude map[int]struct{}) (int, bool) {
	if u.hasWeights {
		return u.pickWeighted(exclude)
	}

	// Lock-free round-robin over healthy targets, starting from the next
	// position in the rotation (fairness across concurrent pickers).
	n := len(u.targets)
	for i := 0; i < n; i++ {
		// Add returns the new value; subtract 1 so the very first pick
		// lands on targets[0]. (uint64 wraparound is harmless: -1 mod n
		// cycles back to n-1.)
		idx := int((u.rr.Add(1) - 1) % uint64(n))
		if _, skip := exclude[idx]; skip {
			continue
		}
		if !u.targets[idx].ejected.Load() {
			return idx, true
		}
	}
	return u.pickFallback(exclude)
}

// pickFallback implements recovery policies 2 and 3 when no healthy,
// non-excluded target exists. Must be called only when the fast path failed.
func (u *UpstreamRuntime) pickFallback(exclude map[int]struct{}) (int, bool) {
	// (not reached from weighted path — pickWeighted has its own handling)
	n := len(u.targets)
	oldest, oldestAt := -1, int64(1<<62-1)
	for i := 0; i < n; i++ {
		if _, skip := exclude[i]; skip {
			continue
		}
		tgt := u.targets[i]
		last := tgt.lastFailed.Load()
		if tgt.ejected.Load() && time.Since(time.Unix(0, last)) >= lbTargetCooldown {
			return i, true // re-probe an ejected target past cooldown
		}
		if last < oldestAt {
			oldest, oldestAt = i, last
		}
	}
	// Force probe the least-recently-failed target (fail-open, task 8).
	if oldest != -1 {
		log.Warn().Str("upstream", u.id).Str("target", u.targets[oldest].name).
			Msg("All targets are unhealthy; force-probing least-recently-failed target")
		return oldest, true
	}
	return -1, false
}

// --- Smooth weighted round robin -----------------------------------------

// pickWeighted implements smooth weighted round robin (the same algorithm
// as nginx): on every pick, each target's effective weight is added to its
// counter, the target with the highest counter is chosen and its counter is
// reduced by the total weight. This spreads picks proportionally to weights
// while avoiding bursty runs of the heavy target. Consecutive-weight config
// never starves light targets.
func (u *UpstreamRuntime) pickWeighted(exclude map[int]struct{}) (int, bool) {
	u.wmu.Lock()
	defer u.wmu.Unlock()

	total := 0
	for _, t := range u.targets {
		total += t.weight
	}

	best := -1
	for i, t := range u.targets {
		// Counters advance for EVERY target on every pick (including
		// excluded/ejected ones) — weights never drift while targets are
		// skipped, so excluded targets remain in the rotation after a retry.
		u.wcounters[i] += t.weight
		if _, skip := exclude[i]; skip {
			continue
		}
		if t.ejected.Load() {
			continue
		}
		if best == -1 || u.wcounters[i] > u.wcounters[best] {
			best = i
		}
	}
	if best != -1 {
		u.wcounters[best] -= total
		return best, true
	}

	// No eligible target: hand over to the recovery fallback (re-probe past
	// cooldown / force-probe least-recently-failed, task 8).
	return u.pickFallback(exclude)
}

// --- Health recording -----------------------------------------------------

// MarkSuccess records a healthy upstream attempt: consecutive failures reset
// to zero and an ejected target is immediately readmitted.
func (u *UpstreamRuntime) MarkSuccess(idx int) {
	if idx < 0 || idx >= len(u.targets) {
		return
	}
	tgt := u.targets[idx]
	tgt.failures.Store(0)
	if tgt.ejected.Swap(false) {
		log.Info().Str("upstream", u.id).Str("target", tgt.name).
			Msg("Target recovered — readmitted to the healthy pool")
	}
}

// MarkFailure records a failed upstream attempt. After
// lbTargetEjectThreshold consecutive failures the target is ejected from the
// healthy pool and stops receiving new requests (task 7).
func (u *UpstreamRuntime) MarkFailure(idx int) {
	if idx < 0 || idx >= len(u.targets) {
		return
	}
	tgt := u.targets[idx]
	now := time.Now()
	tgt.lastFailed.Store(now.UnixNano())
	if n := tgt.failures.Add(1); n >= lbTargetEjectThreshold {
		if !tgt.ejected.Swap(true) {
			log.Warn().Str("upstream", u.id).Str("target", tgt.name).
				Int32("consecutive_failures", n).
				Msg("Target ejected from the healthy pool")
		}
	}
}

// healthyCount returns the number of targets currently admitted to the
// healthy pool. Kept for future observability/admin surfaces.
func (u *UpstreamRuntime) healthyCount() int {
	n := 0
	for _, t := range u.targets {
		if !t.ejected.Load() {
			n++
		}
	}
	return n
}

func (u *UpstreamRuntime) TargetNames() []string {
	names := make([]string, 0, len(u.targets))
	for _, t := range u.targets {
		names = append(names, t.name)
	}
	sort.Strings(names)
	return names
}

// --- helpers --------------------------------------------------------------

func itoa(i int) string {
	return strconv.Itoa(i)
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// requestReplayable reports whether a request can be safely replayed against
// another target (retry integration, task 10). A streamed body
// (ContentLength>0, GetBody nil) is never replayed — retrying it would
// either fail with a closed-body error or send partial content twice.
func requestReplayable(r *http.Request) bool {
	if r.Body == nil || r.GetBody != nil {
		return true
	}
	return r.ContentLength <= 0
}
