package proxmox

import (
	"context"
	"testing"
	"time"
)

// Destroy must be quick and must not go through the graceful-ACPI path: a
// running VM is hard-stopped (qm stop), a stopped VM is deleted straight
// away, and neither takes anywhere near the old 30–120 s.
func TestDeleteVMHardStopsARunningVMThenDeletes(t *testing.T) {
	f := newFakePVE(t)
	p := f.provider(t)

	start := time.Now()
	if err := p.DeleteVM(context.Background(), "100"); err != nil {
		t.Fatalf("DeleteVM: %v", err)
	}
	if f.hit("/nodes/pve/qemu/100/status/stop") != 1 {
		t.Errorf("running VM must be hard-stopped exactly once, got %d", f.hit("/nodes/pve/qemu/100/status/stop"))
	}
	if f.hit("/nodes/pve/qemu/100/status/shutdown") != 0 {
		t.Error("destroy must not attempt a graceful ACPI shutdown")
	}
	if !f.deleted {
		t.Error("VM was not deleted")
	}
	if d := time.Since(start); d > 5*time.Second {
		t.Errorf("destroy of a running VM took %s", d)
	}
}

func TestDeleteVMSkipsStopForAStoppedVM(t *testing.T) {
	f := newFakePVE(t)
	f.stopped = true
	p := f.provider(t)

	if err := p.DeleteVM(context.Background(), "100"); err != nil {
		t.Fatalf("DeleteVM: %v", err)
	}
	if f.hit("/nodes/pve/qemu/100/status/stop") != 0 || f.hit("/nodes/pve/qemu/100/status/shutdown") != 0 {
		t.Error("an already-stopped VM needs no stop call")
	}
	if !f.deleted {
		t.Error("VM was not deleted")
	}
}
