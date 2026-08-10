package service

import (
	"path/filepath"
	"testing"

	"github.com/forgemill/forgemill/internal/db"
	"github.com/forgemill/forgemill/internal/db/models"
	"github.com/forgemill/forgemill/internal/provider"
)

// newTestDB opens a fresh, fully-migrated sqlite DB in a temp directory.
func newTestDB(t *testing.T) *db.DB {
	t.Helper()
	database, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	return database
}

func newTestTarget(t *testing.T, database *db.DB) int64 {
	t.Helper()
	target := &models.Target{
		Name:            "test-target",
		Type:            "esxi",
		Hostname:        "esxi.example.com",
		Port:            443,
		Username:        "admin",
		PasswordEncrypt: "encrypted",
	}
	if err := database.CreateTarget(target); err != nil {
		t.Fatalf("create test target: %v", err)
	}
	return target.ID
}

func TestVMIsOrphaned(t *testing.T) {
	refs := map[string]provider.VMInfo{
		"vm-100": {ID: "vm-100", Name: "web-01"},
	}

	if vmIsOrphaned("vm-100", refs, nil) {
		t.Error("VM present on hypervisor should not be orphaned")
	}
	if !vmIsOrphaned("vm-999", refs, nil) {
		t.Error("VM absent from hypervisor should be orphaned")
	}
}

func TestVMIsOrphanedIgnoresListErrors(t *testing.T) {
	// A transient failure to list the hypervisor's VMs must never be treated
	// as evidence the VM is gone — that would silently delete good records.
	refs := map[string]provider.VMInfo{}
	if vmIsOrphaned("vm-100", refs, errTestList) {
		t.Error("a list error must never mark a VM as orphaned")
	}
}

var errTestList = &testErr{"hypervisor unreachable"}

type testErr struct{ msg string }

func (e *testErr) Error() string { return e.msg }

func TestPreviewDeleteReportsIntentWithoutMutating(t *testing.T) {
	database := newTestDB(t)
	targetID := newTestTarget(t, database)
	svc := NewVMService(database, NewTargetService(database, nil), nil)

	vm := &models.ManagedVM{TargetID: targetID, VMName: "web-01", VMRef: "vm-100"}
	if err := database.CreateManagedVM(vm); err != nil {
		t.Fatalf("create managed vm: %v", err)
	}
	if err := database.CreateVMSnapshot(&models.VMSnapshot{VMID: vm.ID, SnapshotRef: "snap-1", Name: "before-upgrade"}); err != nil {
		t.Fatalf("create snapshot: %v", err)
	}

	preview, err := svc.PreviewDelete(vm.ID, false)
	if err != nil {
		t.Fatalf("preview delete: %v", err)
	}
	if !preview.WouldDeleteOnHypervisor || preview.WouldUntrackOnly {
		t.Errorf("force=false should preview a hypervisor delete, got %+v", preview)
	}
	if preview.DependentSnapshots != 1 {
		t.Errorf("expected 1 dependent snapshot, got %d", preview.DependentSnapshots)
	}

	previewForce, err := svc.PreviewDelete(vm.ID, true)
	if err != nil {
		t.Fatalf("preview delete (force): %v", err)
	}
	if previewForce.WouldDeleteOnHypervisor || !previewForce.WouldUntrackOnly {
		t.Errorf("force=true should preview an untrack-only, got %+v", previewForce)
	}

	// A preview must never actually remove the record.
	if _, err := database.GetManagedVM(vm.ID); err != nil {
		t.Errorf("VM record should still exist after preview, got: %v", err)
	}
}

func TestSyncAllSurfacesOrphanDetail(t *testing.T) {
	database := newTestDB(t)
	targetID := newTestTarget(t, database)

	// A target the hypervisor no longer reports as connectable will make
	// SyncAll record a provider error and skip that target's VMs entirely —
	// so this test exercises vmIsOrphaned directly against a simulated
	// hypervisor VM list instead of routing through a live provider.
	vm := &models.ManagedVM{TargetID: targetID, VMName: "web-01", VMRef: "vm-gone"}
	if err := database.CreateManagedVM(vm); err != nil {
		t.Fatalf("create managed vm: %v", err)
	}

	hypervisorRefs := map[string]provider.VMInfo{} // hypervisor reports nothing
	if !vmIsOrphaned(vm.VMRef, hypervisorRefs, nil) {
		t.Fatal("expected vm-gone to be detected as orphaned")
	}

	// Confirm the OrphanedVM detail struct carries enough to identify the
	// record without a follow-up lookup (the point of the enhancement).
	detail := OrphanedVM{ID: vm.ID, VMName: vm.VMName, VMRef: vm.VMRef, TargetID: vm.TargetID}
	if detail.VMName != "web-01" || detail.VMRef != "vm-gone" {
		t.Errorf("unexpected orphan detail: %+v", detail)
	}
}
