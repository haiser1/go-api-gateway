package config

import (
	"os"
	"testing"
)

func TestManager_ServiceCRUD(t *testing.T) {
	m, tmpDir := setupTestManager(t)
	defer os.RemoveAll(tmpDir)
	defer m.Close()

	s := Service{
		Id:         "s2",
		Name:       "service2",
		UpstreamId: "u1",
	}

	// Add
	err := m.AddService(s)
	if err != nil {
		t.Fatalf("AddService failed: %v", err)
	}

	// Add duplicate name
	err = m.AddService(Service{Id: "s3", Name: "service2", UpstreamId: "u1"})
	if err == nil {
		t.Error("expected error adding service with duplicate name")
	}

	// Add with non-existent upstream
	err = m.AddService(Service{Id: "s3", Name: "service3", UpstreamId: "non-existent"})
	if err == nil {
		t.Error("expected error adding service with non-existent upstream")
	}

	// Update
	s.Protocol = "https"
	err = m.UpdateService("s2", s)
	if err != nil {
		t.Fatalf("UpdateService failed: %v", err)
	}

	// Update with duplicate name
	err = m.UpdateService("s2", Service{Id: "s2", Name: "service1", UpstreamId: "u1"})
	if err == nil {
		t.Error("expected error updating service to duplicate name")
	}

	// Update with non-existent upstream
	err = m.UpdateService("s2", Service{Id: "s2", Name: "service2", UpstreamId: "non-existent"})
	if err == nil {
		t.Error("expected error updating service with non-existent upstream")
	}

	// Delete service in use by route
	err = m.DeleteService("s1")
	if err == nil {
		t.Error("expected error deleting service used by route")
	}

	// Delete unused service
	err = m.DeleteService("s2")
	if err != nil {
		t.Fatalf("DeleteService failed: %v", err)
	}

	// Delete non-existent
	err = m.DeleteService("s2")
	if err == nil {
		t.Error("expected error deleting non-existent service")
	}
}
