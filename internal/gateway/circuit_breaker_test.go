package gateway

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// --- Helpers -------------------------------------------------------------

func recordNTimes(cb *CircuitBreaker, method func(*CircuitBreaker), n int) {
	for i := 0; i < n; i++ {
		method(cb)
	}
}

// consumeProbe polls AllowRequest until the cooldown elapsed and it wins the
// single HALF-OPEN probe slot. The OPEN → HALF-OPEN transition happens inside
// AllowRequest, so there is no way to wait for HALF-OPEN externally.
func consumeProbe(t *testing.T, cb *CircuitBreaker) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		if cb.AllowRequest() {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("probe was never granted, state=%q", cb.GetState())
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func withShortBreakerTimeout(t *testing.T) {
	t.Helper()
	breakerTimeout = 30 * time.Millisecond
	t.Cleanup(func() {
		breakerTimeout = 30 * time.Second
	})
}

// --- Threshold / OPEN / rejection ----------------------------------------

func TestCircuitBreaker_TripsAfterThreshold(t *testing.T) {
	cb := NewCircuitBreaker("svc-trip")
	for i := 0; i < failureThreshold; i++ {
		cb.RecordFailure()
	}
	if got := cb.GetState(); got != "OPEN" {
		t.Fatalf("expected OPEN after %d failures, got %s", failureThreshold, got)
	}
}

func TestCircuitBreaker_DoesNotTripBelowThreshold(t *testing.T) {
	cb := NewCircuitBreaker("svc-under")
	recordNTimes(cb, (*CircuitBreaker).RecordFailure, failureThreshold-1)
	if got := cb.GetState(); got != "CLOSED" {
		t.Fatalf("expected CLOSED below threshold, got %s", got)
	}
	if !cb.AllowRequest() {
		t.Fatal("request must be allowed below threshold")
	}
}

func TestCircuitBreaker_RejectedWhenOpen(t *testing.T) {
	cb := NewCircuitBreaker("svc-open")
	recordNTimes(cb, (*CircuitBreaker).RecordFailure, failureThreshold)
	if cb.AllowRequest() {
		t.Fatal("OPEN breaker must reject requests")
	}
}

func TestCircuitBreaker_SuccessResetsCount(t *testing.T) {
	cb := NewCircuitBreaker("svc-reset")
	recordNTimes(cb, (*CircuitBreaker).RecordFailure, failureThreshold-1)
	recordNTimes(cb, (*CircuitBreaker).RecordSuccess, 1)
	recordNTimes(cb, (*CircuitBreaker).RecordFailure, failureThreshold-1)
	if got := cb.GetState(); got != "CLOSED" {
		t.Fatalf("success must reset the consecutive counter, got %s", got)
	}
}

// --- HALF-OPEN probe lifecycle -------------------------------------------

// While the single probe slot is held, no concurrent request may win one.
func TestCircuitBreaker_HalfOpenSingleProbe(t *testing.T) {
	cb := NewCircuitBreaker("svc-half")
	recordNTimes(cb, (*CircuitBreaker).RecordFailure, failureThreshold)
	withShortBreakerTimeout(t)

	// Consume the probe slot (also performs the OPEN → HALF-OPEN transition).
	consumeProbe(t, cb)
	if got := cb.GetState(); got != "HALF-OPEN" {
		t.Fatalf("expected HALF-OPEN after probe grant, got %s", got)
	}

	// Hammer concurrently: every one of these must be rejected — a probe is
	// already in flight.
	var allowed int32
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if cb.AllowRequest() {
				atomic.AddInt32(&allowed, 1)
			}
		}()
	}
	wg.Wait()

	if allowed != 0 {
		t.Fatalf("no request may pass while the probe slot is taken, got %d allowed", allowed)
	}
	if got := cb.GetState(); got != "HALF-OPEN" {
		t.Fatalf("state must stay HALF-OPEN while probing, got %s", got)
	}
}

func TestCircuitBreaker_ProbeFailureReopens(t *testing.T) {
	cb := NewCircuitBreaker("svc-probe-fail")
	recordNTimes(cb, (*CircuitBreaker).RecordFailure, failureThreshold)
	withShortBreakerTimeout(t)

	consumeProbe(t, cb)
	cb.RecordFailure() // probe fails
	if got := cb.GetState(); got != "OPEN" {
		t.Fatalf("failed probe must reopen the circuit, got %s", got)
	}
	if cb.AllowRequest() {
		t.Fatal("reopened breaker must reject again")
	}
}

func TestCircuitBreaker_RecoveryAfterProbeSuccess(t *testing.T) {
	cb := NewCircuitBreaker("svc-probe-ok")
	recordNTimes(cb, (*CircuitBreaker).RecordFailure, failureThreshold)
	withShortBreakerTimeout(t)

	consumeProbe(t, cb)
	cb.RecordSuccess() // probe succeeds
	if got := cb.GetState(); got != "CLOSED" {
		t.Fatalf("successful probe must close the circuit, got %s", got)
	}
	if !cb.AllowRequest() {
		t.Fatal("recovered breaker must allow traffic again")
	}
}

func TestCircuitBreaker_ClientCancellationReleasesProbeSlot(t *testing.T) {
	cb := NewCircuitBreaker("svc-cancel")
	recordNTimes(cb, (*CircuitBreaker).RecordFailure, failureThreshold)
	withShortBreakerTimeout(t)

	consumeProbe(t, cb)
	// Probe was running but the client vanished — must NOT be counted as an
	// upstream failure and must NOT wedge the breaker in HALF-OPEN.
	cb.RecordClientCancellation()
	if got := cb.GetState(); got != "HALF-OPEN" {
		t.Fatalf("client cancellation must not change state, got %s", got)
	}
	if !cb.AllowRequest() {
		t.Fatal("next request must be able to take the released probe slot")
	}
	if got := cb.GetState(); got != "HALF-OPEN" {
		t.Fatalf("replacement probe must keep HALF-OPEN, got %s", got)
	}
}

// --- Transport-level failure classification ------------------------------

type cbTransportUnderTest struct {
	inner http.RoundTripper
	url   string
}

func wrap(t *testing.T, serviceId string, base http.RoundTripper, url string) *cbTransportUnderTest {
	t.Helper()
	return &cbTransportUnderTest{inner: NewCircuitBreakerTransport(base, serviceId), url: url}
}

func (t *cbTransportUnderTest) roundTrip() (*http.Response, error) {
	req, err := http.NewRequest("GET", t.url, nil)
	if err != nil {
		return nil, err
	}
	return t.inner.RoundTrip(req)
}

func TestTransportClassification_5xxIsFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(503)
	}))
	defer srv.Close()

	tests := wrap(t, "svc-5xx", http.DefaultTransport, srv.URL)
	for i := 0; i < failureThreshold; i++ {
		if _, err := tests.roundTrip(); err != nil {
			t.Fatalf("unexpected transport error: %v", err)
		}
	}
	if got := GetCircuitBreaker("svc-5xx").GetState(); got != "OPEN" {
		t.Fatalf("five 5xx responses must trip the breaker, got %s", got)
	}
}

func TestTransportClassification_4xxIsSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
	}))
	defer srv.Close()

	tests := wrap(t, "svc-4xx", http.DefaultTransport, srv.URL)
	for i := 0; i < failureThreshold+2; i++ {
		if _, err := tests.roundTrip(); err != nil {
			t.Fatalf("unexpected transport error: %v", err)
		}
	}
	if got := GetCircuitBreaker("svc-4xx").GetState(); got != "CLOSED" {
		t.Fatalf("4xx responses must not trip the breaker, got %s", got)
	}
}

func TestTransportClassification_429DefaultSuccessAndOptInFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(429)
	}))
	defer srv.Close()

	tests := wrap(t, "svc-429", http.DefaultTransport, srv.URL)

	// Default policy: 429 does NOT trip the breaker.
	for i := 0; i < failureThreshold+1; i++ {
		if _, err := tests.roundTrip(); err != nil {
			t.Fatalf("unexpected transport error: %v", err)
		}
	}
	if got := GetCircuitBreaker("svc-429").GetState(); got != "CLOSED" {
		t.Fatalf("429 must count as success by default, got %s", got)
	}

	// Opt-in policy: 429 DOES trip the breaker.
	Classify429AsFailure(true)
	defer Classify429AsFailure(false)
	for i := 0; i < failureThreshold; i++ {
		if _, err := tests.roundTrip(); err != nil {
			t.Fatalf("unexpected transport error: %v", err)
		}
	}
	if got := GetCircuitBreaker("svc-429").GetState(); got != "OPEN" {
		t.Fatalf("429 must count as failure with the opt-in policy, got %s", got)
	}
}

func TestTransportClassification_UpstreamTimeoutIsFailure(t *testing.T) {
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(300 * time.Millisecond)
	}))
	defer slow.Close()

	transport := &http.Transport{ResponseHeaderTimeout: 50 * time.Millisecond}
	tests := wrap(t, "svc-timeout", transport, slow.URL)

	for i := 0; i < failureThreshold; i++ {
		_, err := tests.roundTrip()
		if err == nil {
			t.Fatal("expected timeout errors")
		}
	}
	if got := GetCircuitBreaker("svc-timeout").GetState(); got != "OPEN" {
		t.Fatalf("upstream timeouts must trip the breaker, got %s", got)
	}
}

func TestTransportClassification_ClientCancellationIsNeutral(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
	}))
	defer srv.Close()

	transport := &http.Transport{}
	tests := wrap(t, "svc-cancel-neutral", transport, srv.URL)

	canceled := 0
	for i := 0; i < 5; i++ {
		req, err := http.NewRequest("GET", srv.URL, nil)
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(req.Context())
		cancel() // cancel BEFORE the dial — deterministic client cancellation
		req = req.WithContext(ctx)
		if _, err := tests.inner.RoundTrip(req); err != nil && errors.Is(err, context.Canceled) {
			canceled++
			continue
		} else if err != nil {
			t.Fatalf("expected a context.Canceled-wrapped error, got %v", err)
		}
		t.Fatal("expected the canceled dial to error")
	}
	if canceled != 5 {
		t.Fatalf("expected 5 canceled attempts, got %d", canceled)
	}

	// Cancellations were recorded neutrally — breaker still accepts requests.
	if got := GetCircuitBreaker("svc-cancel-neutral").GetState(); got != "CLOSED" {
		t.Fatalf("client cancellation must not affect the breaker, got %s", got)
	}
}

// --- Concurrency ---------------------------------------------------------

func TestCircuitBreaker_ConcurrentRecordsAndAllows(t *testing.T) {
	cb := NewCircuitBreaker("svc-concurrent")

	var wg sync.WaitGroup
	for i := 0; i < 200; i++ {
		wg.Add(3)
		go func() { defer wg.Done(); cb.AllowRequest() }()
		go func() { defer wg.Done(); cb.RecordFailure() }()
		go func() { defer wg.Done(); cb.RecordSuccess() }()
	}
	wg.Wait()

	switch got := cb.GetState(); got {
	case "CLOSED", "OPEN", "HALF-OPEN":
		// valid terminal states
	default:
		t.Fatalf("invalid terminal state %q", got)
	}
}

// --- Reload survival ------------------------------------------------------

func TestCircuitBreaker_StateSurvivesConfigReload(t *testing.T) {
	cb := GetCircuitBreaker("svc-reload")
	recordNTimes(cb, (*CircuitBreaker).RecordFailure, failureThreshold)
	if got := cb.GetState(); got != "OPEN" {
		t.Fatalf("expected OPEN before reload, got %s", got)
	}

	// Simulate a reload: buildRuntimeSnapshot recreates transport wrappers,
	// but the registry is keyed by stable service ID — the SAME breaker
	// instance must come back, state intact.
	for i := 0; i < 3; i++ {
		_ = NewCircuitBreakerTransport(http.DefaultTransport, "svc-reload")
	}
	after := GetCircuitBreaker("svc-reload")
	if after != cb {
		t.Fatal("reload must reuse the same breaker instance for a stable service id")
	}
	if got := after.GetState(); got != "OPEN" {
		t.Fatalf("reload must not reset breaker state, got %s", got)
	}
	if after.AllowRequest() {
		t.Fatal("OPEN breaker must still reject traffic after reload")
	}
}
