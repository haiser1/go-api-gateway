package gateway

import (
	"fmt"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/haiser1/go-api-gateway/internal/config"
)

// transportTestService builds a Service with the given timeouts and defaults
// applied, mirroring what buildRuntimeSnapshot passes to GetServiceTransport.
func transportTestService(id string, connectTimeout, readTimeout int) config.Service {
	svc := config.Service{Id: id, ConnectTimeout: connectTimeout, ReadTimeout: readTimeout}
	svc.ApplyDefaults()
	return svc
}

func uniqueTransportServiceID(prefix string) string {
	return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
}

// TestTransportConfigHash verifies the fingerprint reacts to every
// transport-affecting field and ignores fields that never reach the transport.
func TestTransportConfigHash(t *testing.T) {
	base := transportTestService("svc-hash-base", 5, 10)

	if transportConfigHash(base) != transportConfigHash(transportTestService("svc-hash-other", 5, 10)) {
		t.Error("identical effective params must produce identical fingerprints")
	}

	connectChanged := transportTestService("svc-hash-connect", 7, 10)
	if transportConfigHash(connectChanged) == transportConfigHash(base) {
		t.Error("connect timeout change must change the fingerprint")
	}

	readChanged := transportTestService("svc-hash-read", 5, 27)
	if transportConfigHash(readChanged) == transportConfigHash(base) {
		t.Error("read timeout change must change the fingerprint")
	}

	unrelated := transportTestService("svc-hash-unrelated", 5, 10)
	unrelated.Name = "renamed"
	unrelated.Timeout = 90
	unrelated.Protocol = "https"
	if transportConfigHash(unrelated) != transportConfigHash(base) {
		t.Error("non-transport fields must not change the fingerprint")
	}
}

// Plan tests 1-5: transport created with 5s timeouts, config changes to 10s,
// a subsequent GetServiceTransport call returns a different transport built
// from the latest settings.
func TestGetServiceTransport_ReplacesTransportOnConfigChange(t *testing.T) {
	id := uniqueTransportServiceID("svc-cache-replace")

	t5 := GetServiceTransport(transportTestService(id, 5, 5))
	if t5 == nil {
		t.Fatal("expected non-nil transport")
	}

	// Unchanged config must not create a replacement.
	if again := GetServiceTransport(transportTestService(id, 5, 5)); again != t5 {
		t.Fatal("expected cached transport for unchanged config")
	}

	t10 := GetServiceTransport(transportTestService(id, 10, 5))
	if t10 == t5 {
		t.Fatal("expected replacement when connect timeout changes")
	}

	t30 := GetServiceTransport(transportTestService(id, 10, 30))
	if t30 == t10 {
		t.Fatal("expected replacement when read timeout changes")
	}
	if t30.ResponseHeaderTimeout != 30*time.Second {
		t.Fatalf("transport not rebuilt from latest config: ResponseHeaderTimeout = %v, want 30s", t30.ResponseHeaderTimeout)
	}

	// Switching back must replace again (the reverted config rebuilds a fresh
	// transport; the old object is never resurrected from the cache).
	back := GetServiceTransport(transportTestService(id, 10, 5))
	if back == t30 {
		t.Fatal("expected replacement when reverting to a previous config")
	}
	if back.ResponseHeaderTimeout != 5*time.Second {
		t.Fatalf("reverted transport has ResponseHeaderTimeout %v, want 5s", back.ResponseHeaderTimeout)
	}
}

// ReadTimeout is the only transport-relevant carrier of read settings.
func TestGetServiceTransport_IgnoresNonTransportFields(t *testing.T) {
	id := uniqueTransportServiceID("svc-cache-nontransport")

	first := GetServiceTransport(transportTestService(id, 5, 10))

	changed := transportTestService(id, 5, 10)
	changed.Name = "renamed"
	changed.Timeout = 75
	if got := GetServiceTransport(changed); got != first {
		t.Fatal("expected unchanged transport when only non-transport fields change")
	}
}

// Concurrent calls with the identical config must yield exactly one shared
// transport (double-check locking prevents duplicate creation).
func TestGetServiceTransport_ConcurrentSameConfig(t *testing.T) {
	id := uniqueTransportServiceID("svc-cache-same")
	svc := transportTestService(id, 4, 8)

	const workers = 32
	var wg sync.WaitGroup
	results := make(chan *http.Transport, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results <- GetServiceTransport(svc)
		}()
	}
	wg.Wait()
	close(results)

	unique := make(map[*http.Transport]struct{})
	for tr := range results {
		if tr == nil {
			t.Error("nil transport returned")
			continue
		}
		if tr.ResponseHeaderTimeout != 8*time.Second {
			t.Errorf("unexpected ResponseHeaderTimeout %v, want 8s", tr.ResponseHeaderTimeout)
		}
		unique[tr] = struct{}{}
	}
	if len(unique) != 1 {
		t.Fatalf("expected exactly 1 transport for identical concurrent configs, got %d", len(unique))
	}
}

// Concurrent calls with three alternating configs may receive any valid
// transport while the storm runs (the winner of the lock defines the stored
// config), but after settling on a final config the cache must reflect it
// exactly. Run under -race to validate the replacement path.
func TestGetServiceTransport_ConcurrentMixedConfigs(t *testing.T) {
	id := uniqueTransportServiceID("svc-cache-mix")

	cfgs := []config.Service{
		transportTestService(id, 1, 2),
		transportTestService(id, 2, 5),
		transportTestService(id, 3, 10),
	}

	stop := make(chan struct{})
	var wg sync.WaitGroup
	const workers = 16
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			for j := 0; ; j++ {
				select {
				case <-stop:
					return
				default:
				}
				if tr := GetServiceTransport(cfgs[(worker+j)%len(cfgs)]); tr == nil {
					t.Error("nil transport returned under contention")
					return
				}
			}
		}(i)
	}
	time.Sleep(100 * time.Millisecond)
	close(stop)
	wg.Wait()

	// Settle on cfgs[2] and verify the stored state matches it exactly.
	finalSvc := cfgs[2]
	final := GetServiceTransport(finalSvc)
	if final == nil {
		t.Fatal("nil transport after settle")
	}
	if final.ResponseHeaderTimeout != 10*time.Second {
		t.Fatalf("settled transport has ResponseHeaderTimeout %v, want 10s", final.ResponseHeaderTimeout)
	}

	transportMu.RLock()
	entry, ok := serviceTransports[id]
	transportMu.RUnlock()
	if !ok {
		t.Fatal("cache entry missing after settle")
	}
	if entry.transport != final {
		t.Fatal("cache does not reflect the last committed config")
	}
	if entry.configHash != transportConfigHash(finalSvc) {
		t.Fatal("cache fingerprint does not match the last committed config")
	}
}
