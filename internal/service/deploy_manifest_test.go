package service

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"

	"github.com/forgemill/forgemill/internal/db"
	"github.com/forgemill/forgemill/internal/db/models"
)

func newTestTemplate(t *testing.T, database *db.DB, targetID int64) int64 {
	t.Helper()
	tpl := &models.Template{TargetID: targetID, Name: "ubuntu-22.04"}
	if err := database.CreateTemplate(tpl); err != nil {
		t.Fatalf("create test template: %v", err)
	}
	return tpl.ID
}

func TestGetManifestAssemblesReceiptFromExistingData(t *testing.T) {
	database := newTestDB(t)
	targetID := newTestTarget(t, database)
	templateID := newTestTemplate(t, database, targetID)

	user := &models.User{Username: "alice", Role: "user", IsActive: true}
	if err := database.CreateUser(user); err != nil {
		t.Fatalf("create test user: %v", err)
	}

	deployment := &models.Deployment{
		TemplateID:      &templateID,
		TargetID:        targetID,
		VMName:          "web-01",
		Status:          "completed",
		ConfigJSON:      `{"vm_name":"web-01","cpu":2,"memory_mb":4096}`,
		CreatedBy:       user.ID,
		InitialUsername: "forgemill",
		InitialPwdEnc:   "encrypted-blob",
	}
	if err := database.CreateDeployment(deployment); err != nil {
		t.Fatalf("create test deployment: %v", err)
	}

	if err := database.AddDeploymentLog(&models.DeploymentLog{DeploymentID: deployment.ID, Level: "info", Message: "provisioning started"}); err != nil {
		t.Fatalf("add deployment log: %v", err)
	}

	if err := database.CreateAuditLog(&models.AuditLog{
		Actor: "alice", ActorID: &user.ID, Action: "deployment.start",
		ResourceType: "deployment", ResourceID: "irrelevant-until-set", Metadata: json.RawMessage(`{}`),
	}); err != nil {
		t.Fatalf("create audit log: %v", err)
	}
	// The audit entry needs the real deployment ID as its resource_id to be picked up.
	if err := database.CreateAuditLog(&models.AuditLog{
		Actor: "alice", ActorID: &user.ID, Action: "deployment.start",
		ResourceType: "deployment", ResourceID: strconv.FormatInt(deployment.ID, 10), Metadata: json.RawMessage(`{}`),
	}); err != nil {
		t.Fatalf("create audit log: %v", err)
	}

	vm := &models.ManagedVM{DeploymentID: &deployment.ID, TargetID: targetID, VMName: "web-01", VMRef: "vm-1"}
	if err := database.CreateManagedVM(vm); err != nil {
		t.Fatalf("create managed vm: %v", err)
	}

	svc := NewDeployService(database, NewTargetService(database, nil), nil, nil, nil)
	manifest, err := svc.GetManifest(deployment.ID)
	if err != nil {
		t.Fatalf("get manifest: %v", err)
	}

	if manifest.TriggeredBy != "alice" {
		t.Errorf("expected triggered_by=alice, got %q", manifest.TriggeredBy)
	}
	if !strings.Contains(manifest.What, "web-01") || !strings.Contains(manifest.What, "ubuntu-22.04") {
		t.Errorf("expected What to mention VM and template, got %q", manifest.What)
	}
	if !manifest.HasCredentials {
		t.Error("expected has_credentials=true")
	}
	if manifest.CredentialsRef == "" {
		t.Error("expected a non-empty credentials_ref, not the value itself")
	}
	if strings.Contains(manifest.CredentialsRef, "encrypted-blob") {
		t.Error("credentials_ref must never contain the actual credential value")
	}
	if len(manifest.Logs) != 1 {
		t.Errorf("expected 1 deployment log, got %d", len(manifest.Logs))
	}
	// Only the correctly-tagged audit entry should surface, not the one with a bogus resource_id.
	if len(manifest.AuditEvents) != 1 {
		t.Errorf("expected 1 matching audit event, got %d", len(manifest.AuditEvents))
	}
	if len(manifest.UndoOptions) == 0 {
		t.Error("expected undo options once a VM has been created")
	}
	if string(manifest.Inputs) != deployment.ConfigJSON {
		t.Errorf("expected inputs to mirror config_json verbatim, got %s", manifest.Inputs)
	}
}

func TestGetManifestNoVMMeansNoUndoOptions(t *testing.T) {
	database := newTestDB(t)
	targetID := newTestTarget(t, database)
	templateID := newTestTemplate(t, database, targetID)

	user := &models.User{Username: "bob", Role: "user", IsActive: true}
	if err := database.CreateUser(user); err != nil {
		t.Fatalf("create test user: %v", err)
	}

	deployment := &models.Deployment{
		TemplateID: &templateID,
		TargetID:   targetID,
		VMName:     "failed-vm",
		Status:     "failed",
		ConfigJSON: `{}`,
		CreatedBy:  user.ID,
	}
	if err := database.CreateDeployment(deployment); err != nil {
		t.Fatalf("create test deployment: %v", err)
	}
	if err := database.UpdateDeploymentStatus(deployment.ID, "failed", "connect: timeout"); err != nil {
		// Some state-machine guards only allow specific transitions from 'pending';
		// this failure path isn't the point of the test, so ignore it here.
		_ = err
	}

	svc := NewDeployService(database, NewTargetService(database, nil), nil, nil, nil)
	manifest, err := svc.GetManifest(deployment.ID)
	if err != nil {
		t.Fatalf("get manifest: %v", err)
	}

	if manifest.HasCredentials {
		t.Error("expected has_credentials=false when no password was issued")
	}
	if len(manifest.UndoOptions) != 0 {
		t.Errorf("expected no undo options when no VM was ever created, got %v", manifest.UndoOptions)
	}
}
