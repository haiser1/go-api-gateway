package config

import (
	"os"
	"testing"
)

func TestManager_RouteCRUD(t *testing.T) {
	m, tmpDir := setupTestManager(t)
	defer os.RemoveAll(tmpDir)
	defer m.Close()

	r := Route{
		Id:        "r2",
		Name:      "route2",
		ServiceId: "s1",
		Paths:     []string{"/v2"},
	}

	// Add
	err := m.AddRoute(r)
	if err != nil {
		t.Fatalf("AddRoute failed: %v", err)
	}

	// Add duplicate name
	err = m.AddRoute(Route{Id: "r3", Name: "route2", ServiceId: "s1"})
	if err == nil {
		t.Error("expected error adding route with duplicate name")
	}

	// Add with non-existent service
	err = m.AddRoute(Route{Id: "r3", Name: "route3", ServiceId: "non-existent"})
	if err == nil {
		t.Error("expected error adding route with non-existent service")
	}

	// Update
	r.Paths = []string{"/v2-updated"}
	err = m.UpdateRoute("r2", r)
	if err != nil {
		t.Fatalf("UpdateRoute failed: %v", err)
	}

	// Update with duplicate name
	err = m.UpdateRoute("r2", Route{Id: "r2", Name: "route1", ServiceId: "s1"})
	if err == nil {
		t.Error("expected error updating route to duplicate name")
	}

	// Delete
	err = m.DeleteRoute("r2")
	if err != nil {
		t.Fatalf("DeleteRoute failed: %v", err)
	}

	// Delete non-existent
	err = m.DeleteRoute("r2")
	if err == nil {
		t.Error("expected error deleting non-existent route")
	}
}
