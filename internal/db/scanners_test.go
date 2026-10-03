package db

import (
	"reflect"
	"testing"
	"time"

	"github.com/forgemill/forgemill/internal/db/models"
)

// The Get and List queries for a table share one column list and one scanner.
// These tests pin that the single-row and multi-row paths return identical
// structs — including the joined names and lifecycle fields — so a column
// added to the constant but not the scanner (or vice versa) fails here, not
// at runtime in a handler.

func seedDeploymentAndVM(t *testing.T, database *DB) (*models.Deployment, *models.ManagedVM) {
	t.Helper()
	target := &models.Target{Name: "lab-vcenter", Type: "vcenter", Hostname: "vc.example.com", Port: 443, Username: "admin", PasswordEncrypt: "enc"}
	if err := database.CreateTarget(target); err != nil {
		t.Fatal(err)
	}
	user := &models.User{Username: "alice", PasswordHash: "x", Role: "admin"}
	if err := database.CreateUser(user); err != nil {
		t.Fatal(err)
	}
	tmpl := &models.Template{TargetID: target.ID, Name: "ubuntu-24.04", Moref: "vm-100", OSType: "linux", CPU: 2, MemoryMB: 2048, DiskGB: 20}
	if err := database.CreateTemplate(tmpl); err != nil {
		t.Fatal(err)
	}
	dep := &models.Deployment{TemplateID: &tmpl.ID, TargetID: target.ID, VMName: "web-01", Status: "pending", ConfigJSON: `{"cpu":2}`, CreatedBy: user.ID, InitialUsername: "forgemill", InitialPwdEnc: "sealed"}
	if err := database.CreateDeployment(dep); err != nil {
		t.Fatal(err)
	}
	// Walk the status guard: pending → running → completed.
	for _, st := range []string{"running", "completed"} {
		if err := database.UpdateDeploymentStatus(dep.ID, st, ""); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now().UTC().Truncate(time.Second)
	earlier := now.Add(-2 * time.Hour)
	vm := &models.ManagedVM{
		DeploymentID: &dep.ID, TargetID: target.ID, VMName: "web-01", VMRef: "vm-101",
		PowerState: "poweredOn", IPAddress: "10.0.0.5", CPU: 2, MemoryMB: 2048, DiskGB: 20, OSType: "linux",
		LastSyncedAt: &now, StateChangedAt: &earlier, LastPoweredOnAt: &earlier, TotalRuntimeSeconds: 7200,
	}
	if err := database.CreateManagedVM(vm); err != nil {
		t.Fatal(err)
	}
	return dep, vm
}

func TestManagedVMGetAndListScanIdentically(t *testing.T) {
	database := openTestDB(t)
	_, seeded := seedDeploymentAndVM(t, database)

	got, err := database.GetManagedVM(seeded.ID)
	if err != nil {
		t.Fatal(err)
	}
	list, err := database.ListManagedVMs()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("expected one VM, got %d", len(list))
	}
	if !reflect.DeepEqual(*got, list[0]) {
		t.Errorf("Get and List disagree:\n get=%+v\nlist=%+v", *got, list[0])
	}
	// Joined and lifecycle columns must come through both paths.
	if got.TargetName != "lab-vcenter" || got.TemplateName != "ubuntu-24.04" || got.Platform != "linux" {
		t.Errorf("joined columns not populated: target=%q template=%q platform=%q", got.TargetName, got.TemplateName, got.Platform)
	}
	if got.TotalRuntimeSeconds != 7200 || got.StateChangedAt == nil || got.LastPoweredOnAt == nil || got.LastPoweredOffAt != nil {
		t.Errorf("lifecycle columns wrong: %+v", got)
	}
	if got.IPAddress != "10.0.0.5" || got.PowerState != "poweredOn" || got.CPU != 2 || got.MemoryMB != 2048 || got.DiskGB != 20 {
		t.Errorf("resource columns wrong: %+v", got)
	}
}

func TestDeploymentGetAndListsShareColumns(t *testing.T) {
	database := openTestDB(t)
	dep, vm := seedDeploymentAndVM(t, database)

	got, err := database.GetDeployment(dep.ID)
	if err != nil {
		t.Fatal(err)
	}
	// Detail-only columns scanned after the shared set.
	if got.InitialUsername != "forgemill" || got.InitialPwdEnc != "sealed" {
		t.Errorf("initial credentials not scanned: %+v", got)
	}
	if got.VMID == nil || *got.VMID != vm.ID {
		t.Errorf("vm_id subquery not scanned: %v", got.VMID)
	}
	if got.TemplateName != "ubuntu-24.04" || got.TargetName != "lab-vcenter" || got.Status != "completed" || got.CompletedAt == nil {
		t.Errorf("shared columns wrong on Get: %+v", got)
	}

	// Each list path must agree with Get on every shared field.
	shared := func(d models.Deployment) models.Deployment {
		d.InitialUsername, d.InitialPwdEnc, d.VMID = "", "", nil
		return d
	}
	want := shared(*got)

	paged, err := database.ListDeployments(DeploymentFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if paged.Total != 1 || len(paged.Data) != 1 {
		t.Fatalf("expected one deployment, got total=%d len=%d", paged.Total, len(paged.Data))
	}
	if !reflect.DeepEqual(paged.Data[0], want) {
		t.Errorf("ListDeployments disagrees with Get:\n list=%+v\n want=%+v", paged.Data[0], want)
	}
	recent, err := database.GetRecentDeployments(5)
	if err != nil {
		t.Fatal(err)
	}
	if len(recent) != 1 || !reflect.DeepEqual(recent[0], want) {
		t.Errorf("GetRecentDeployments disagrees with Get: %+v", recent)
	}
}
