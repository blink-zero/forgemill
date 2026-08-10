package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/forgemill/forgemill/internal/db/models"
	"github.com/forgemill/forgemill/internal/provider"
)

func TestResourceItemMatchesOnNameIDOrPath(t *testing.T) {
	// vCenter networks are submitted by the frontend as their inventory Path
	// (not Name or ID) whenever one is available, to disambiguate portgroups
	// that share a name across different folders. A real, correctly-selected
	// network must not be flagged as "not found" just because the check only
	// compared against Name/ID.
	item := provider.ResourceItem{Name: "VM Network", ID: "network-17", Path: "/Datacenter/network/VM Network"}

	cases := []struct {
		name  string
		value string
		want  bool
	}{
		{"matches by name", "VM Network", true},
		{"matches by id", "network-17", true},
		{"matches by path", "/Datacenter/network/VM Network", true},
		{"does not match an unrelated value", "some-other-network", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := resourceItemMatches(tc.value, item); got != tc.want {
				t.Errorf("resourceItemMatches(%q) = %v, want %v", tc.value, got, tc.want)
			}
		})
	}
}

func TestResourceItemMatchesIgnoresEmptyPath(t *testing.T) {
	// Providers that don't populate Path (e.g. Proxmox bridges) leave it "" —
	// an empty submitted value must never accidentally match an item that
	// also has an empty Path.
	item := provider.ResourceItem{Name: "vmbr0", ID: "vmbr0", Path: ""}
	if resourceItemMatches("", item) {
		t.Error("an empty value must not match an item with an empty Path")
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
