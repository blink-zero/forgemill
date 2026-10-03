package service

import (
	"context"
	"testing"

	"github.com/forgemill/forgemill/internal/provider"
)

func TestSyncAllRecordsAPerTargetOutcomeForDiagnostics(t *testing.T) {
	svc, _, _ := newNICTestService(t, "esxi") // fakeNIC.vms nil => ListVMs fails, per-VM status still works
	if _, err := svc.SyncAll(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	last := svc.LastSyncByTarget()
	if len(last) != 1 {
		t.Fatalf("expected one target recorded, got %v", last)
	}
	for _, info := range last {
		if info.Synced != 1 || info.Orphaned != 0 || info.At.IsZero() {
			t.Errorf("outcome: %+v", info)
		}
		if len(info.Errors) != 1 || info.Errors[0] == "" {
			t.Errorf("the listing failure must be in this target's errors: %+v", info.Errors)
		}
	}

	// A dry run must not overwrite the recorded outcome.
	fakeNIC.vms = []provider.VMInfo{{ID: "vm-100", Name: "web-01", PowerState: "poweredOn", IPAddress: "10.0.0.5"}}
	if _, err := svc.SyncAll(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	for _, info := range svc.LastSyncByTarget() {
		if len(info.Errors) != 1 {
			t.Errorf("dry run overwrote the recorded outcome: %+v", info)
		}
	}
}
