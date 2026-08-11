package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/forgemill/forgemill/internal/db/models"
)

func TestDeploySpecForValidationMapsFieldsCorrectly(t *testing.T) {
	// This mapping feeds ValidateDeploySpec, whose live-connection path
	// can't be exercised in CI — a typo'd field here (e.g. Cluster:
	// req.Datacenter) would otherwise only surface against a real target.
	req := &DeployRequest{
		VMName:     "web-01",
		TemplateID: 1,
		TargetID:   1,
		Datacenter: "DC1",
		Cluster:    "Cluster1",
		Datastore:  "ds1",
		Folder:     "folder1",
		Network:    "/DC1/network/VLAN 150",
		Host:       "esxi01.example.com",
		CPU:        2,
		MemoryMB:   2048,
	}
	spec := deploySpecForValidation(req)
	if spec.Datacenter != req.Datacenter {
		t.Errorf("Datacenter: got %q, want %q", spec.Datacenter, req.Datacenter)
	}
	if spec.Cluster != req.Cluster {
		t.Errorf("Cluster: got %q, want %q", spec.Cluster, req.Cluster)
	}
	if spec.Datastore != req.Datastore {
		t.Errorf("Datastore: got %q, want %q", spec.Datastore, req.Datastore)
	}
	if spec.Folder != req.Folder {
		t.Errorf("Folder: got %q, want %q", spec.Folder, req.Folder)
	}
	if spec.Network != req.Network {
		t.Errorf("Network: got %q, want %q", spec.Network, req.Network)
	}
	if spec.Host != req.Host {
		t.Errorf("Host: got %q, want %q", spec.Host, req.Host)
	}
}

func TestPreflightRejectsBadFieldsWithoutTouchingTargetOrTemplate(t *testing.T) {
	database := newTestDB(t)
	svc := NewDeployService(database, NewTargetService(database, nil), nil, nil, nil)

	req := &DeployRequest{
		VMName:     "not valid!!",
		TemplateID: 999, // doesn't exist — must not even be reached
		TargetID:   999, // doesn't exist — must not even be reached
		CPU:        2,
		MemoryMB:   2048,
	}
	result, err := svc.Preflight(context.Background(), req)
	if err != nil {
		t.Fatalf("preflight: %v", err)
	}
	if result.Valid {
		t.Error("expected invalid VM name to fail preflight")
	}
	if len(result.Blockers) != 1 {
		t.Fatalf("expected exactly 1 blocker (field validation short-circuits), got %v", result.Blockers)
	}
}

func TestPreflightRejectsMissingTemplate(t *testing.T) {
	database := newTestDB(t)
	targetID := newTestTarget(t, database)
	svc := NewDeployService(database, NewTargetService(database, nil), nil, nil, nil)

	req := &DeployRequest{
		VMName:     "web-01",
		TemplateID: 999,
		TargetID:   targetID,
		CPU:        2,
		MemoryMB:   2048,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	result, err := svc.Preflight(ctx, req)
	if err != nil {
		t.Fatalf("preflight: %v", err)
	}
	if result.Valid {
		t.Error("expected missing template to fail preflight")
	}
	found := false
	for _, b := range result.Blockers {
		if strings.Contains(b, "template") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected a template-not-found blocker, got %v", result.Blockers)
	}
}

func TestPreflightCatchesVMNameCollisionOnSameTarget(t *testing.T) {
	database := newTestDB(t)
	targetID := newTestTarget(t, database)
	templateID := newTestTemplate(t, database, targetID)
	svc := NewDeployService(database, NewTargetService(database, nil), nil, nil, nil)

	existing := &models.ManagedVM{TargetID: targetID, VMName: "web-01", VMRef: "vm-1"}
	if err := database.CreateManagedVM(existing); err != nil {
		t.Fatalf("create managed vm: %v", err)
	}

	req := &DeployRequest{
		VMName:     "web-01",
		TemplateID: templateID,
		TargetID:   targetID,
		CPU:        2,
		MemoryMB:   2048,
	}
	// The target isn't a real reachable hypervisor, so the resource-existence
	// check will fail to connect — that must surface as a warning, not abort
	// the whole preflight (the collision blocker must still come through).
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	result, err := svc.Preflight(ctx, req)
	if err != nil {
		t.Fatalf("preflight: %v", err)
	}
	if result.Valid {
		t.Error("expected VM name collision to fail preflight")
	}
	found := false
	for _, b := range result.Blockers {
		if strings.Contains(b, "already exists") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected a name-collision blocker, got %v", result.Blockers)
	}
	if len(result.Warnings) == 0 {
		t.Error("expected a warning about being unable to verify target resources against an unreachable target")
	}
}

func TestPreflightPassesForNonCollidingValidRequest(t *testing.T) {
	database := newTestDB(t)
	targetID := newTestTarget(t, database)
	templateID := newTestTemplate(t, database, targetID)
	svc := NewDeployService(database, NewTargetService(database, nil), nil, nil, nil)

	req := &DeployRequest{
		VMName:     "web-02",
		TemplateID: templateID,
		TargetID:   targetID,
		CPU:        2,
		MemoryMB:   2048,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	result, err := svc.Preflight(ctx, req)
	if err != nil {
		t.Fatalf("preflight: %v", err)
	}
	if !result.Valid {
		t.Errorf("expected a clean request with no collision to pass, got blockers=%v", result.Blockers)
	}
}
