package service

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// A target recorded as inventory-only (free-licensed ESXi) is a preflight
// blocker and Start refuses with a 400-class error before any deployment
// row exists — the hypervisor would reject the first write anyway.
func TestDeployRefusedOnInventoryOnlyTarget(t *testing.T) {
	s, _, tmplID, targetID := newDeployTestService(t)
	if err := s.db.UpdateTargetCapabilities(targetID, "VMware vSphere 8 Hypervisor", false, "free license: inventory only", nil); err != nil {
		t.Fatal(err)
	}
	req := &DeployRequest{TemplateID: tmplID, TargetID: targetID, VMName: "web-09", CPU: 2, MemoryMB: 2048, DiskGB: 20}

	pf, err := s.Preflight(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if pf.Valid || len(pf.Blockers) != 1 || !strings.Contains(pf.Blockers[0], "free license: inventory only") {
		t.Fatalf("preflight must block with the target's note: %+v", pf)
	}

	before, _ := s.db.CountDeploymentsByTarget(targetID)
	_, err = s.Start(req, 1)
	if !errors.Is(err, ErrInvalidDeployRequest) || !strings.Contains(err.Error(), "inventory only") {
		t.Fatalf("want ErrInvalidDeployRequest with the note, got %v", err)
	}
	after, _ := s.db.CountDeploymentsByTarget(targetID)
	if after != before {
		t.Errorf("no deployment row may be created, had %d now %d", before, after)
	}

	// Back to a capable host: the same request passes preflight.
	if err := s.db.UpdateTargetCapabilities(targetID, "Evaluation Mode", true, "", nil); err != nil {
		t.Fatal(err)
	}
	pf, _ = s.Preflight(context.Background(), req)
	for _, b := range pf.Blockers {
		if strings.Contains(b, "inventory only") {
			t.Errorf("blocker must clear when the target is capable again: %v", pf.Blockers)
		}
	}
}

func TestSanitizeHypervisorErrorExplainsLicenseGate(t *testing.T) {
	msg := sanitizeHypervisorError(errors.New("start VMDK copy: ServerFaultCode: Current license or ESXi version prohibits execution of the requested operation."))
	if !strings.Contains(msg, "free vSphere Hypervisor license or its evaluation has expired") || strings.Contains(msg, "ServerFaultCode") {
		t.Errorf("deploy failure must be explained, got %q", msg)
	}
}
