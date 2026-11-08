package middleware

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/haiser1/go-api-gateway/internal/config"
)

func TestRepoConfigLoadsWithRegisteredPlugins(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "configs", "config.yaml"))
	if err != nil {
		t.Skipf("repo config unavailable: %v", err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	manager, err := config.NewManager(dir)
	if err != nil {
		t.Fatalf("repo config must load with the real plugin registry: %v", err)
	}
	_ = manager.Close()
}
