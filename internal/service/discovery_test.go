package service

import (
	"context"
	"errors"
	"testing"

	"github.com/forgemill/forgemill/internal/db/models"
	"github.com/forgemill/forgemill/internal/provider"
)

// The fake target has one managed VM (vm-100). The listing adds two more
// plus the managed one; templates are already filtered by real providers.
func discoveryFixture(t *testing.T) (*VMService, int64) {
	t.Helper()
	svc, database, _ := newNICTestService(t, "esxi")
	targets, _ := database.ListTargets()
	fakeNIC.vms = []provider.VMInfo{
		{ID: "vm-100", Name: "web-01", PowerState: "poweredOn", IPAddress: "10.0.0.5", CPU: 2, MemoryMB: 2048, DiskGB: 40},
		{ID: "vm-200", Name: "jenkins-agent-02", PowerState: "poweredOn", IPAddress: "10.0.0.44", CPU: 4, MemoryMB: 8192, DiskGB: 80, GuestID: "ubuntu64Guest"},
		{ID: "vm-300", Name: "old-test-vm", PowerState: "poweredOff", CPU: 2, MemoryMB: 2048, DiskGB: 20},
	}
	return svc, targets[0].ID
}

func TestDiscoverListsOnlyUnmanagedVMsAndRecordsTheCount(t *testing.T) {
	svc, targetID := discoveryFixture(t)
	res, err := svc.Discover(context.Background(), targetID, false)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if res.Managed != 1 || res.Unmanaged != 2 || res.Ignored != 0 || len(res.VMs) != 2 {
		t.Fatalf("counts/list wrong: %+v", res)
	}
	if res.VMs[0].Name != "jenkins-agent-02" || res.VMs[1].Name != "old-test-vm" {
		t.Errorf("sorted by name, managed vm-100 excluded: %+v", res.VMs)
	}
	if res.VMs[0].IPAddress != "10.0.0.44" || res.VMs[0].CPU != 4 || res.VMs[0].GuestID != "ubuntu64Guest" {
		t.Errorf("listing details should pass through: %+v", res.VMs[0])
	}
	target, _ := svc.targets.Get(targetID)
	if target.UnmanagedVMs != 2 || target.UnmanagedCheckedAt == nil {
		t.Errorf("target should record the unmanaged count: %d %v", target.UnmanagedVMs, target.UnmanagedCheckedAt)
	}
}

func TestIgnoreHidesVMsFromDiscoverUntilUnignored(t *testing.T) {
	svc, targetID := discoveryFixture(t)
	if err := svc.IgnoreDiscovered(targetID, []string{"vm-300"}, map[string]string{"vm-300": "old-test-vm"}, 1); err != nil {
		t.Fatal(err)
	}
	res, _ := svc.Discover(context.Background(), targetID, false)
	if res.Unmanaged != 1 || res.Ignored != 1 || len(res.VMs) != 1 || res.VMs[0].Ref != "vm-200" {
		t.Errorf("ignored VM must be hidden and counted: %+v", res)
	}
	withIgnored, _ := svc.Discover(context.Background(), targetID, true)
	if len(withIgnored.VMs) != 2 || !withIgnored.VMs[1].Ignored {
		t.Errorf("include_ignored shows it flagged: %+v", withIgnored.VMs)
	}
	ignored, _ := svc.ListIgnored(targetID)
	if len(ignored) != 1 || ignored[0].VMName != "old-test-vm" {
		t.Errorf("ignore list: %+v", ignored)
	}
	if err := svc.UnignoreDiscovered(targetID, []string{"vm-300"}); err != nil {
		t.Fatal(err)
	}
	res, _ = svc.Discover(context.Background(), targetID, false)
	if res.Unmanaged != 2 {
		t.Errorf("after un-ignore both are unmanaged again: %+v", res)
	}
}

func TestAdoptCreatesAdoptedVMsIdempotentlyAndSkipsUnknownOrManaged(t *testing.T) {
	svc, targetID := discoveryFixture(t)
	if err := svc.IgnoreDiscovered(targetID, []string{"vm-200"}, nil, 1); err != nil {
		t.Fatal(err)
	}
	res, err := svc.Adopt(context.Background(), targetID, []string{"vm-200", "vm-300", "vm-100", "vm-999", "vm-200"}, 1, "alice")
	if err != nil {
		t.Fatalf("Adopt: %v", err)
	}
	if len(res.Adopted) != 2 {
		t.Fatalf("expected vm-200 and vm-300 adopted, got %+v", res.Adopted)
	}
	for _, vm := range res.Adopted {
		if vm.Origin != models.VMOriginAdopted || vm.AdoptedAt == nil || vm.AdoptedBy == nil || *vm.AdoptedBy != 1 || vm.DeploymentID != nil {
			t.Errorf("adopted VM shape: %+v", vm)
		}
		if vm.ID == 0 || vm.TargetID != targetID {
			t.Errorf("adopted VM not persisted properly: %+v", vm)
		}
	}
	reasons := map[string]string{}
	for _, s := range res.Skipped {
		reasons[s.Ref] = s.Reason
	}
	if reasons["vm-100"] != "already managed" || reasons["vm-999"] == "" || len(res.Skipped) != 2 {
		t.Errorf("skipped: %+v", res.Skipped)
	}
	// The listing seeded the record; the initial sync ran (status call).
	if fakeNIC.statusCalls == 0 {
		t.Error("adopt should sync each VM right away")
	}
	// Adopting clears the ignore entry and leaves nothing unmanaged.
	ignored, _ := svc.ListIgnored(targetID)
	if len(ignored) != 0 {
		t.Errorf("adopted VM must leave the ignore list: %+v", ignored)
	}
	target, _ := svc.targets.Get(targetID)
	if target.UnmanagedVMs != 0 {
		t.Errorf("unmanaged count after adopting everything: %d", target.UnmanagedVMs)
	}
	// Second adopt of the same refs is a no-op reported as skipped.
	again, err := svc.Adopt(context.Background(), targetID, []string{"vm-200"}, 1, "alice")
	if err != nil || len(again.Adopted) != 0 || len(again.Skipped) != 1 || again.Skipped[0].Reason != "already managed" {
		t.Errorf("idempotent adopt: %+v %v", again, err)
	}
	// Events record the adoption.
	events, _ := svc.ListVMEvents(res.Adopted[0].ID, 10)
	found := false
	for _, e := range events {
		if e.Level == "info" && e.Message == "Adopted from t by alice" {
			found = true
		}
	}
	if !found {
		t.Errorf("adoption event missing: %+v", events)
	}
}

func TestAdoptAndDiscoverUnknownTarget(t *testing.T) {
	svc, _ := discoveryFixture(t)
	if _, err := svc.Discover(context.Background(), 9999, false); !errors.Is(err, ErrTargetNotFound) {
		t.Errorf("discover unknown target: %v", err)
	}
	if _, err := svc.Adopt(context.Background(), 9999, []string{"x"}, 1, "a"); !errors.Is(err, ErrTargetNotFound) {
		t.Errorf("adopt unknown target: %v", err)
	}
}

func TestSyncAllRecordsUnmanagedCountAndRegisterIsTaggedRegistered(t *testing.T) {
	svc, targetID := discoveryFixture(t)
	if _, err := svc.SyncAll(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	target, _ := svc.targets.Get(targetID)
	if target.UnmanagedVMs != 2 {
		t.Errorf("sync should record 2 unmanaged VMs, got %d", target.UnmanagedVMs)
	}
	vm := &models.ManagedVM{TargetID: targetID, VMName: "old-test-vm", VMRef: "vm-300"}
	if err := svc.Create(vm); err != nil {
		t.Fatal(err)
	}
	got, _ := svc.db.GetManagedVM(vm.ID)
	if got.Origin != models.VMOriginRegistered {
		t.Errorf("register-by-ref should be origin=registered, got %q", got.Origin)
	}
	vms, _ := svc.List()
	for _, v := range vms {
		if v.VMRef == "vm-100" && v.Origin != models.VMOriginDeployed {
			t.Errorf("pre-existing VMs default to origin=deployed, got %q", v.Origin)
		}
	}
}
