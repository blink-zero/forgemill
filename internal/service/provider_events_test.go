package service

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestProviderWarningsDuringDeployLandInTheDeploymentLog(t *testing.T) {
	s, fake, tmplID, targetID := newDeployTestService(t)
	fake.deployWarn = "VMDK copy with the requested format failed, retrying without a format"
	resp, err := s.Start(&DeployRequest{TemplateID: tmplID, TargetID: targetID, VMName: "web-05", CPU: 2, MemoryMB: 2048, DiskGB: 20}, 1)
	if err != nil {
		t.Fatal(err)
	}
	d := waitDeployment(t, s, resp.ID)
	if d.Status != "completed" {
		t.Fatalf("status %s: %s", d.Status, d.ErrorMessage)
	}
	logs := logsOf(t, s, resp.ID)
	if !strings.Contains(logs, "warn: VMDK copy with the requested format failed, retrying without a format (error=simulated)") {
		t.Errorf("provider warning should be in the deployment log with its details:\n%s", logs)
	}
}

func TestPartialDeployKeepsTheVMAndFailsWithTheReason(t *testing.T) {
	s, fake, tmplID, targetID := newDeployTestService(t)
	fake.deployPartialErr = errors.New("resize disk scsi0 to 40 GB: proxmox request failed (HTTP 500): storage full")
	resp, err := s.Start(&DeployRequest{TemplateID: tmplID, TargetID: targetID, VMName: "web-06", CPU: 2, MemoryMB: 2048, DiskGB: 40}, 1)
	if err != nil {
		t.Fatal(err)
	}
	d := waitDeployment(t, s, resp.ID)
	if d.Status != "failed" || !strings.Contains(d.ErrorMessage, "VM was created but resize disk scsi0 to 40 GB") || !strings.Contains(d.ErrorMessage, "storage full") {
		t.Errorf("want a failed deployment with the hypervisor's reason, got %s: %s", d.Status, d.ErrorMessage)
	}
	vms, _ := s.db.ListManagedVMs()
	kept := false
	for _, vm := range vms {
		if vm.VMRef == "vm-200" && vm.VMName == "web-06" {
			kept = true
		}
	}
	if !kept {
		t.Error("a partially deployed VM must be tracked so it can be fixed or destroyed")
	}
}

func TestVMOperationsRecordEventsIncludingProviderWarnings(t *testing.T) {
	svc, _, vmID := newNICTestService(t, "esxi")
	if _, err := svc.AddNIC(context.Background(), vmID, AddNICRequest{Network: "VM Network", Connected: true}); err != nil {
		t.Fatal(err)
	}
	events, err := svc.ListVMEvents(vmID, 50)
	if err != nil {
		t.Fatal(err)
	}
	var sawWarn, sawAttached bool
	for _, e := range events {
		if e.Level == "warn" && strings.Contains(e.Message, "connect reconfigure failed") && strings.Contains(e.Message, "error=simulated") {
			sawWarn = true
		}
		if e.Level == "info" && strings.Contains(e.Message, "Attached network adapter") && strings.Contains(e.Message, "VM Network") {
			sawAttached = true
		}
		if e.VMID != vmID {
			t.Errorf("event for the wrong VM: %+v", e)
		}
	}
	if !sawWarn || !sawAttached {
		t.Errorf("expected the provider warning and the attach result as VM events, got %+v", events)
	}
	if _, err := svc.ListVMEvents(999999, 10); !errors.Is(err, ErrVMNotFound) {
		t.Errorf("unknown VM must be ErrVMNotFound, got %v", err)
	}
}
