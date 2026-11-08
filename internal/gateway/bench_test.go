package gateway

import (
	"fmt"
	"testing"

	"github.com/haiser1/go-api-gateway/internal/config"
)

// NOTE: middleware-execution benchmarks live in internal/middleware
// (bench_test.go) — importing internal/middleware here would be an import
// cycle in tests, and the real plugin factories live in that package.

// --- Route-count sweep (plan §4 benchmark minimum) -------------------------

// Benchmarks: 100 / 1 000 / 10 000 routes — a realistic static+parameterized
// route table searched by both a static hit and a parameterized hit.
func BenchmarkRadixTree_RouteSweep(b *testing.B) {
	for _, count := range []int{100, 1000, 10000} {
		b.Run(fmt.Sprintf("routes_%d", count), func(b *testing.B) {
			tree := NewRadixTree()
			for i := 0; i < count; i++ {
				static := fmt.Sprintf("/api/v%d/svc%d/resource", i%32, i)
				tree.Insert("GET", static, makeRoute("GET", static))
				param := fmt.Sprintf("/api/v%d/svc%d/item/:id", i%32, i)
				tree.Insert("GET", param, makeRoute("GET", param))
			}

			b.Run("static_hit", func(b *testing.B) {
				for i := 0; i < b.N; i++ {
					tree.Search("GET", fmt.Sprintf("/api/v%d/svc%d/resource", i%32, i%256))
				}
			})
			b.Run("param_hit", func(b *testing.B) {
				for i := 0; i < b.N; i++ {
					tree.Search("GET", fmt.Sprintf("/api/v%d/svc%d/item/12345", i%32, (i*7919)%count))
				}
			})
		})
	}
}

// --- Runtime snapshot lookup ------------------------------------------------

// The proxy hot path component: snapshot load + radix tree lookup per
// request (one atomic pointer read + O(segments) tree walk).
func BenchmarkRuntimeSnapshot_Lookup(b *testing.B) {
	up := config.Upstream{
		Id: "u-bench", Name: "bench", Algorithm: "round-robin",
		Targets: []config.UpstreamTarget{{Host: "127.0.0.1", Port: 1}},
	}
	up.ApplyDefaults()
	svc := config.Service{Id: "s-bench", Name: "bench", UpstreamId: "u-bench", Protocol: "http"}
	svc.ApplyDefaults()
	cfg := &config.Config{
		Server:    config.ServerConfig{ProxyPort: 8080},
		Upstreams: []config.Upstream{up},
		Services:  []config.Service{svc},
		Routes: func() (rs []config.Route) {
			for i := 0; i < 10; i++ {
				rs = append(rs, config.Route{
					Id: fmt.Sprintf("r%d", i), Name: fmt.Sprintf("route%d", i),
					ServiceId: "s-bench", Methods: []string{"GET"},
					Paths: []string{fmt.Sprintf("/bench/api%d", i)},
				})
			}
			return rs
		}(),
	}

	p, err := NewProxy(cfg)
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		snap := p.snapshot.Load()
		if snap == nil {
			b.Fatal("no snapshot")
		}
		route, _, _ := snap.Tree.SearchWithAllowed("GET", "/bench/api3")
		if route == nil {
			b.Fatal("route vanished")
		}
	}
}

// NOTE: middleware-execution benchmarks live in internal/middleware
// (bench_test.go) — importing internal/middleware here is an import cycle in
// tests, and the real plugin factories (rate limiting, transform, cors) live
// in that package.
