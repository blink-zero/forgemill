package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/forgemill/forgemill/internal/crypto"
	"github.com/forgemill/forgemill/internal/db/models"
)

type noopHub struct{}

func (noopHub) SendProgress(int64, any) {}

// newDeployTestService: an esxi target served by the fake provider, one
// template on it, and a DeployService wired with a no-op progress hub.
func newDeployTestService(t *testing.T) (*DeployService, *fakeNICProvider, int64, int64) {
	t.Helper()
	svc, database, _ := newNICTestService(t, "esxi")
	fakeNIC.deployOK = true
	targets, err := database.ListTargets()
	if err != nil || len(targets) == 0 {
		t.Fatalf("targets: %v", err)
	}
	tmpl := &models.Template{TargetID: targets[0].ID, Name: "ubuntu-24.04", Moref: "vm-100", OSType: "linux", CPU: 2, MemoryMB: 2048, DiskGB: 20}
	if err := database.CreateTemplate(tmpl); err != nil {
		t.Fatal(err)
	}
	if err := database.CreateUser(&models.User{Username: "deployer", PasswordHash: "x", Role: "admin", IsActive: true}); err != nil {
		t.Fatal(err)
	}
	enc, _ := crypto.NewEncryptor(nicTestKey)
	return NewDeployService(database, svc.targets, noopHub{}, nil, enc), fakeNIC, tmpl.ID, targets[0].ID
}

func waitDeployment(t *testing.T, s *DeployService, id int64) *models.Deployment {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		d, err := s.db.GetDeployment(id)
		if err == nil && (d.Status == "completed" || d.Status == "failed") {
			return d
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("deployment did not finish")
	return nil
}

func logsOf(t *testing.T, s *DeployService, id int64) string {
	t.Helper()
	logs, err := s.db.GetDeploymentLogs(id)
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	for _, l := range logs {
		b.WriteString(l.Level + ": " + l.Message + "\n")
	}
	return b.String()
}

func TestDeployAttachesExtraDisksAndNICsAfterTheClone(t *testing.T) {
	s, fake, tmplID, targetID := newDeployTestService(t)
	off := false
	resp, err := s.Start(&DeployRequest{TemplateID: tmplID, TargetID: targetID, VMName: "web-01", CPU: 2, MemoryMB: 2048, DiskGB: 20,
		ExtraDisks: []ExtraDisk{{SizeGB: 10}, {SizeGB: 5, Datastore: "ds-sata-01", Provisioning: "Thick"}},
		ExtraNICs:  []ExtraNIC{{Network: "dvPG-Backend"}, {Network: "dvPG-Mgmt", AdapterType: "e1000e", Connected: &off}},
	}, 1)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	d := waitDeployment(t, s, resp.ID)
	if d.Status != "completed" {
		t.Fatalf("deployment %s: %s\n%s", d.Status, d.ErrorMessage, logsOf(t, s, resp.ID))
	}
	if len(fake.addDiskSpec) != 2 || fake.addDiskSpec[0].SizeGB != 10 || fake.addDiskSpec[1].Datastore != "ds-sata-01" || fake.addDiskSpec[1].Provisioning != "thick" {
		t.Errorf("extra disks not attached as requested: %+v", fake.addDiskSpec)
	}
	if len(fake.addNICCalls) != 2 || fake.addNICCalls[0].Network != "dvPG-Backend" || !fake.addNICCalls[0].Connected || fake.addNICCalls[1].AdapterType != "e1000e" || fake.addNICCalls[1].Connected {
		t.Errorf("extra NICs not attached as requested: %+v", fake.addNICCalls)
	}
	logs := logsOf(t, s, resp.ID)
	for _, want := range []string{"Attaching 2 extra disk(s) and 2 extra network adapter(s)", "Attached extra disk 1:", "Attached extra disk 2:", "Attached extra network adapter 1:", "Attached extra network adapter 2:", "Deployment completed successfully"} {
		if !strings.Contains(logs, want) {
			t.Errorf("deployment log missing %q:\n%s", want, logs)
		}
	}
	// The deployment record carries the extras in its config.
	if !strings.Contains(d.ConfigJSON, `"extra_disks"`) || !strings.Contains(d.ConfigJSON, `"extra_nics"`) {
		t.Errorf("config_json should include the extras: %s", d.ConfigJSON)
	}
	// The VM is registered after the extras.
	vms, _ := s.db.ListManagedVMs()
	found := false
	for _, vm := range vms {
		if vm.VMRef == "vm-200" && vm.VMName == "web-01" {
			found = true
		}
	}
	if !found {
		t.Error("deployed VM should be registered in managed_vms")
	}
}

func TestDeployWithoutExtrasIsUnchanged(t *testing.T) {
	s, fake, tmplID, targetID := newDeployTestService(t)
	resp, err := s.Start(&DeployRequest{TemplateID: tmplID, TargetID: targetID, VMName: "web-02", CPU: 2, MemoryMB: 2048, DiskGB: 20}, 1)
	if err != nil {
		t.Fatal(err)
	}
	d := waitDeployment(t, s, resp.ID)
	if d.Status != "completed" || len(fake.addDiskSpec) != 0 || len(fake.addNICCalls) != 0 {
		t.Errorf("plain deploy: status=%s disks=%v nics=%v", d.Status, fake.addDiskSpec, fake.addNICCalls)
	}
	if strings.Contains(logsOf(t, s, resp.ID), "extra") {
		t.Error("no extras requested → no extras log lines")
	}
}

func TestDeployFailsLoudlyWhenAnExtraCannotBeAttachedButKeepsTheVM(t *testing.T) {
	s, fake, tmplID, targetID := newDeployTestService(t)
	fake.addDiskErr = errors.New("datastore not accessible from the VM's host: \"ds-x\" is not mounted on host esx1")
	resp, err := s.Start(&DeployRequest{TemplateID: tmplID, TargetID: targetID, VMName: "web-03", CPU: 2, MemoryMB: 2048, DiskGB: 20,
		ExtraDisks: []ExtraDisk{{SizeGB: 10, Datastore: "ds-x"}}, ExtraNICs: []ExtraNIC{{Network: "dvPG-Backend"}}}, 1)
	if err != nil {
		t.Fatal(err)
	}
	d := waitDeployment(t, s, resp.ID)
	if d.Status != "failed" || !strings.Contains(d.ErrorMessage, "extra disk 1 (10 GB)") || !strings.Contains(d.ErrorMessage, "not mounted on host esx1") || !strings.Contains(d.ErrorMessage, "VM is left in place") {
		t.Errorf("want a failed deployment naming the disk and the hypervisor reason, got %s: %s", d.Status, d.ErrorMessage)
	}
	if len(fake.addNICCalls) != 0 {
		t.Error("after a failed disk the NICs must not be attempted")
	}
	vms, _ := s.db.ListManagedVMs()
	kept := false
	for _, vm := range vms {
		if vm.VMRef == "vm-200" {
			kept = true
		}
	}
	if !kept {
		t.Error("the created VM must still be tracked so it can be fixed or destroyed")
	}
}

func TestDeployExtrasAreValidatedAgainstTheProviderBeforeAnything(t *testing.T) {
	s, fake, tmplID, targetID := newDeployTestService(t)
	base := DeployRequest{TemplateID: tmplID, TargetID: targetID, VMName: "web-04", CPU: 2, MemoryMB: 2048, DiskGB: 20}

	bad := base
	bad.ExtraNICs = []ExtraNIC{{Network: "net", VLANTag: 20}} // esxi: no VLAN tagging
	if _, err := s.Start(&bad, 1); !errors.Is(err, ErrInvalidDeployRequest) || !strings.Contains(err.Error(), "VLAN tag is not supported") {
		t.Errorf("VLAN on vSphere must be a 400, got %v", err)
	}
	bad = base
	bad.ExtraDisks = []ExtraDisk{{SizeGB: 10, Provisioning: "sparse"}}
	if _, err := s.Start(&bad, 1); !errors.Is(err, ErrInvalidDeployRequest) || !strings.Contains(err.Error(), "provisioning") {
		t.Errorf("unknown provisioning must be a 400, got %v", err)
	}
	bad = base
	bad.ExtraDisks = []ExtraDisk{{SizeGB: 0}}
	if _, err := s.Start(&bad, 1); !errors.Is(err, ErrInvalidDeployRequest) {
		t.Errorf("size 0 must be a 400, got %v", err)
	}
	bad = base
	bad.ExtraNICs = []ExtraNIC{{Network: "  "}}
	if _, err := s.Start(&bad, 1); !errors.Is(err, ErrInvalidDeployRequest) {
		t.Errorf("empty network must be a 400, got %v", err)
	}
	if len(fake.addDiskSpec) != 0 || len(fake.addNICCalls) != 0 {
		t.Error("nothing may reach the provider for a rejected request")
	}

	// Preflight reports the same thing as a blocker.
	res, err := s.Preflight(context.Background(), &bad)
	if err != nil || res.Valid || len(res.Blockers) == 0 {
		t.Errorf("preflight should block: %+v %v", res, err)
	}
}

func TestPreflightBlocksExtrasTheProviderWouldRefuse(t *testing.T) {
	s, _, tmplID, targetID := newDeployTestService(t)
	req := DeployRequest{TemplateID: tmplID, TargetID: targetID, VMName: "web-07", CPU: 2, MemoryMB: 2048, DiskGB: 20,
		ExtraNICs:  []ExtraNIC{{Network: "bad-net"}, {Network: "ok-net"}},
		ExtraDisks: []ExtraDisk{{SizeGB: 5, Datastore: "bad-ds"}, {SizeGB: 5}}}
	res, err := s.Preflight(context.Background(), &req)
	if err != nil {
		t.Fatal(err)
	}
	if res.Valid {
		t.Fatalf("preflight should block, got %+v", res)
	}
	joined := strings.Join(res.Blockers, "\n")
	if !strings.Contains(joined, "extra network adapter 1: network not found") || !strings.Contains(joined, "extra disk 1: datastore not found") {
		t.Errorf("blockers should name the extra and the reason:\n%s", joined)
	}
	if strings.Contains(joined, "adapter 2") || strings.Contains(joined, "disk 2") {
		t.Errorf("valid extras must not be blocked:\n%s", joined)
	}
}
