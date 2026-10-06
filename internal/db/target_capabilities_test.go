package db

import (
	"testing"
	"time"
)

func TestTargetCapabilitiesDefaultAndRoundTrip(t *testing.T) {
	d := openTestDB(t)
	if _, err := d.conn.Exec(`INSERT INTO targets (name, type, hostname, username, password_encrypted) VALUES ('t', 'esxi', 'h', 'u', 'p')`); err != nil {
		t.Fatal(err)
	}
	got, err := d.GetTarget(1)
	if err != nil || !got.DeploySupported || got.LicenseEdition != "" {
		t.Fatalf("new target must default to deploy-supported: %+v err %v", got, err)
	}
	if err := d.UpdateTargetCapabilities(1, "VMware vSphere 8 Hypervisor", false, "inventory-only", nil); err != nil {
		t.Fatal(err)
	}
	got, _ = d.GetTarget(1)
	if got.DeploySupported || got.LicenseEdition != "VMware vSphere 8 Hypervisor" || got.CapabilityNote != "inventory-only" {
		t.Fatalf("after update: %+v", got)
	}
	list, err := d.ListTargets()
	if err != nil || len(list) != 1 || list[0].DeploySupported || list[0].CapabilityNote != "inventory-only" {
		t.Fatalf("list: %+v err %v", list, err)
	}
}

func TestTargetEvaluationExpiryAndWarnedStage(t *testing.T) {
	d := openTestDB(t)
	if _, err := d.conn.Exec(`INSERT INTO targets (name, type, hostname, username, password_encrypted) VALUES ('t', 'esxi', 'h', 'u', 'p')`); err != nil {
		t.Fatal(err)
	}
	exp := time.Now().Add(10 * 24 * time.Hour).UTC().Truncate(time.Second)
	if err := d.UpdateTargetCapabilities(1, "Evaluation Mode", true, "", &exp); err != nil {
		t.Fatal(err)
	}
	got, _ := d.GetTarget(1)
	if got.EvaluationExpiresAt == nil || !got.EvaluationExpiresAt.Equal(exp) || got.EvaluationWarnedStage != -1 {
		t.Fatalf("after set: %+v", got)
	}
	if err := d.UpdateTargetEvaluationWarnedStage(1, 14); err != nil {
		t.Fatal(err)
	}
	// Same expiry keeps the stage; a new expiry (or none) resets it.
	if err := d.UpdateTargetCapabilities(1, "Evaluation Mode", true, "", &exp); err != nil {
		t.Fatal(err)
	}
	if got, _ = d.GetTarget(1); got.EvaluationWarnedStage != 14 {
		t.Errorf("same expiry must keep the warned stage, got %d", got.EvaluationWarnedStage)
	}
	if err := d.UpdateTargetCapabilities(1, "VMware vSphere 8 Standard", true, "", nil); err != nil {
		t.Fatal(err)
	}
	if got, _ = d.GetTarget(1); got.EvaluationExpiresAt != nil || got.EvaluationWarnedStage != -1 {
		t.Errorf("keyed host must clear expiry and stage: %+v", got)
	}
}
