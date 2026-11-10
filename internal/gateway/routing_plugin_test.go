package gateway

import (
	"testing"

	"github.com/haiser1/go-api-gateway/internal/config"
)

func pluginChainNames(chain []config.PluginConfig) []string {
	names := make([]string, len(chain))
	for i, plugin := range chain {
		names[i] = plugin.Name
	}
	return names
}

func TestResolvePluginChain_RateLimitThreeLevelPrecedence(t *testing.T) {
	global := []config.PluginConfig{{
		Name:    "rate-limiting",
		Enabled: true,
		Config:  map[string]interface{}{"algorithm": "fixed_window", "requests_per_window": 10},
	}}
	service := config.Service{Plugins: []config.PluginConfig{{
		Name:    "rate-limiting",
		Enabled: true,
		Config:  map[string]interface{}{"algorithm": "sliding_window", "requests_per_window": 20},
	}}}
	route := config.Route{Plugins: []config.PluginConfig{{
		Name:    "rate-limiting",
		Enabled: true,
		Config:  map[string]interface{}{"algorithm": "token_bucket", "requests_per_window": 30},
	}}}

	chain := resolvePluginChain(route, service, global)
	if len(chain) != 1 {
		t.Fatalf("resolved chain length = %d, want 1 (route overrides service and global)", len(chain))
	}
	if got := chain[0].Config["algorithm"]; got != "token_bucket" {
		t.Fatalf("resolved algorithm = %v, want token_bucket", got)
	}

	chain = resolvePluginChain(config.Route{}, service, global)
	if got := chain[0].Config["algorithm"]; got != "sliding_window" {
		t.Fatalf("service override algorithm = %v, want sliding_window", got)
	}

	chain = resolvePluginChain(config.Route{}, config.Service{}, global)
	if got := chain[0].Config["algorithm"]; got != "fixed_window" {
		t.Fatalf("global fallback algorithm = %v, want fixed_window", got)
	}
}

func TestSortPluginChain_AuthBeforeUserRateLimit(t *testing.T) {
	userKeyed := sortPluginChain([]config.PluginConfig{
		{Name: "rate-limiting", Enabled: true, Config: map[string]interface{}{"key_by": "user"}},
		{Name: "auth", Enabled: true},
	})
	if got := pluginChainNames(userKeyed); got[0] != "auth" || got[1] != "rate-limiting" {
		t.Fatalf("user-keyed order = %v, want [auth rate-limiting]", got)
	}

	ipKeyed := sortPluginChain([]config.PluginConfig{
		{Name: "auth", Enabled: true},
		{Name: "rate-limiting", Enabled: true, Config: map[string]interface{}{"key_by": "ip"}},
	})
	if got := pluginChainNames(ipKeyed); got[0] != "rate-limiting" || got[1] != "auth" {
		t.Fatalf("ip-keyed order = %v, want [rate-limiting auth]", got)
	}
}
