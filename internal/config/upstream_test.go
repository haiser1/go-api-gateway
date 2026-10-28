package config

import (
	"os"
	"testing"
)

func TestManager_UpstreamCRUD(t *testing.T) {
	m, tmpDir := setupTestManager(t)
	defer os.RemoveAll(tmpDir)
	defer m.Close()

	u := Upstream{
		Id:        "u2",
		Name:      "upstream2",
		Algorithm: "round-robin",
		Targets: []UpstreamTarget{
			{Host: "127.0.0.1", Port: 8082, Weight: 100},
		},
	}

	// 1. Add upstream
	if err := m.AddUpstream(u); err != nil {
		t.Fatalf("AddUpstream failed: %v", err)
	}

	// 2. Add duplicate name
	if err := m.AddUpstream(Upstream{Id: "u3", Name: "upstream2"}); err == nil {
		t.Error("expected error adding upstream with duplicate name")
	}

	// 3. Verify reader gets new upstream
	cfg := m.GetConfig()
	found := false
	for _, up := range cfg.Upstreams {
		if up.Id == "u2" {
			found = true
			if up.Targets[0].Port != 8082 {
				t.Errorf("expected port 8082, got %d", up.Targets[0].Port)
			}
			break
		}
	}
	if !found {
		t.Fatal("expected upstream u2 in config")
	}

	// 4. Update upstream
	u.Algorithm = "least-connections"
	u.Targets[0].Port = 8083
	if err := m.UpdateUpstream("u2", u); err != nil {
		t.Fatalf("UpdateUpstream failed: %v", err)
	}

	// Verify update took effect on reader
	cfg = m.GetConfig()
	for _, up := range cfg.Upstreams {
		if up.Id == "u2" {
			if up.Algorithm != "least-connections" {
				t.Errorf("expected least-connections, got %s", up.Algorithm)
			}
			if up.Targets[0].Port != 8083 {
				t.Errorf("expected port 8083, got %d", up.Targets[0].Port)
			}
		}
	}

	// 5. Update with duplicate name
	if err := m.UpdateUpstream("u2", Upstream{Id: "u2", Name: "upstream1"}); err == nil {
		t.Error("expected error updating upstream to existing name")
	}

	// 6. Delete upstream referenced by service (u1 is used by s1 in setupTestManager)
	if err := m.DeleteUpstream("u1"); err == nil {
		t.Error("expected error deleting upstream used by service s1")
	}

	// 7. Delete non-referenced upstream u2
	if err := m.DeleteUpstream("u2"); err != nil {
		t.Fatalf("DeleteUpstream failed: %v", err)
	}

	// Verify deletion
	cfg = m.GetConfig()
	for _, up := range cfg.Upstreams {
		if up.Id == "u2" {
			t.Error("upstream u2 should have been deleted")
		}
	}

	// 8. Delete non-existent upstream
	if err := m.DeleteUpstream("u-nonexistent"); err == nil {
		t.Error("expected error deleting non-existent upstream")
	}
}
