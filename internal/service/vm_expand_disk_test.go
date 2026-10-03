package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/forgemill/forgemill/internal/provider"
)

// A VM with a 40 GB primary and a 20 GB secondary disk. The old guard compared
// every request against the VM-level 40 GB, so the secondary could never grow
// to anything under 41 GB.
func expandTestVM(t *testing.T) (*VMService, int64) {
	t.Helper()
	svc, database, vmID := newNICTestService(t, "esxi")
	if err := database.UpdateManagedVMResources(vmID, 2, 2048, 40); err != nil {
		t.Fatal(err)
	}
	fakeNIC.disks = []provider.Disk{{Key: 0, Label: "Hard disk 1", SizeGB: 40}, {Key: 1, Label: "Hard disk 2", SizeGB: 20}}
	return svc, vmID
}

func TestExpandDiskGrowsASecondaryDiskSmallerThanThePrimary(t *testing.T) {
	svc, vmID := expandTestVM(t)
	if err := svc.ExpandDisk(context.Background(), vmID, 1, 30); err != nil {
		t.Fatalf("expanding the 20 GB secondary to 30 GB must be allowed: %v", err)
	}
	if len(fakeNIC.expandCalls) != 1 || fakeNIC.expandCalls[0] != [2]int{1, 30} {
		t.Errorf("provider should be asked to grow disk 1 to 30 GB, got %v", fakeNIC.expandCalls)
	}
	// Recorded VM size grows by the delta (40 + 10), not to the secondary's size.
	vm, _ := svc.db.GetManagedVM(vmID)
	if vm.DiskGB != 50 {
		t.Errorf("vm.DiskGB = %d, want 50", vm.DiskGB)
	}
}

func TestExpandDiskRejectsShrinkAndUnknownDiskAsCallerErrors(t *testing.T) {
	svc, vmID := expandTestVM(t)
	err := svc.ExpandDisk(context.Background(), vmID, 1, 20)
	if !errors.Is(err, ErrInvalidDiskSize) || !strings.Contains(err.Error(), "Hard disk 2 (20GB)") {
		t.Errorf("same-size request must be rejected naming the disk, got %v", err)
	}
	err = svc.ExpandDisk(context.Background(), vmID, 7, 100)
	if !errors.Is(err, ErrInvalidDiskSize) || !strings.Contains(err.Error(), "disk 7 not found") {
		t.Errorf("unknown key must be rejected, got %v", err)
	}
	if len(fakeNIC.expandCalls) != 0 {
		t.Errorf("rejected requests must not reach the provider: %v", fakeNIC.expandCalls)
	}
}

func TestExpandDiskStillGuardsThePrimaryDisk(t *testing.T) {
	svc, vmID := expandTestVM(t)
	if err := svc.ExpandDisk(context.Background(), vmID, 0, 40); !errors.Is(err, ErrInvalidDiskSize) {
		t.Errorf("primary at its current size must still be rejected, got %v", err)
	}
	if err := svc.ExpandDisk(context.Background(), vmID, 0, 60); err != nil {
		t.Errorf("growing the primary must work: %v", err)
	}
	vm, _ := svc.db.GetManagedVM(vmID)
	if vm.DiskGB != 60 {
		t.Errorf("vm.DiskGB = %d, want 60", vm.DiskGB)
	}
}
