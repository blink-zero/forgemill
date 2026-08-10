package service

import (
	"encoding/json"
	"strconv"
	"testing"
	"time"

	"github.com/forgemill/forgemill/internal/db/models"
)

func TestGetTimelineMergesLogsAndAuditEventsInOrder(t *testing.T) {
	database := newTestDB(t)
	targetID := newTestTarget(t, database)
	templateID := newTestTemplate(t, database, targetID)

	user := &models.User{Username: "alice", Role: "user", IsActive: true}
	if err := database.CreateUser(user); err != nil {
		t.Fatalf("create test user: %v", err)
	}
	deployment := &models.Deployment{
		TemplateID: &templateID,
		TargetID:   targetID,
		VMName:     "web-01",
		Status:     "completed",
		ConfigJSON: `{}`,
		CreatedBy:  user.ID,
	}
	if err := database.CreateDeployment(deployment); err != nil {
		t.Fatalf("create test deployment: %v", err)
	}

	// Deliberately insert out of chronological order to prove GetTimeline sorts.
	if err := database.AddDeploymentLog(&models.DeploymentLog{DeploymentID: deployment.ID, Level: "info", Message: "provisioning finished"}); err != nil {
		t.Fatalf("add log: %v", err)
	}
	if err := database.CreateAuditLog(&models.AuditLog{
		Actor: "alice", ActorID: &user.ID, Action: "deployment.start",
		ResourceType: "deployment", ResourceID: "irrelevant", Metadata: json.RawMessage(`{}`),
	}); err != nil {
		t.Fatalf("create noise audit log for a different resource: %v", err)
	}

	svc := NewDeployService(database, NewTargetService(database, nil), nil, nil, nil)
	timeline, err := svc.GetTimeline(deployment.ID)
	if err != nil {
		t.Fatalf("get timeline: %v", err)
	}

	// The audit log above was tagged with the wrong resource_id, so it must
	// not appear here — only the deployment log should.
	if len(timeline) != 1 {
		t.Fatalf("expected 1 event (mismatched resource_id audit log excluded), got %d: %+v", len(timeline), timeline)
	}
	if timeline[0].Source != "log" || timeline[0].Message != "provisioning finished" {
		t.Errorf("unexpected event: %+v", timeline[0])
	}
}

func TestGetTimelineSortsAcrossSourcesAndLabelsKnownActions(t *testing.T) {
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
		VMName:     "web-02",
		Status:     "completed",
		ConfigJSON: `{}`,
		CreatedBy:  user.ID,
	}
	if err := database.CreateDeployment(deployment); err != nil {
		t.Fatalf("create test deployment: %v", err)
	}

	resourceID := strconv.FormatInt(deployment.ID, 10)
	if err := database.CreateAuditLog(&models.AuditLog{
		Actor: "bob", ActorID: &user.ID, Action: "deployment.start",
		ResourceType: "deployment", ResourceID: resourceID, Metadata: json.RawMessage(`{}`),
	}); err != nil {
		t.Fatalf("create start audit log: %v", err)
	}
	// deployment_logs/audit_logs timestamps default to SQLite's
	// CURRENT_TIMESTAMP, which is second-resolution — sleep past a second
	// boundary so ordering is deterministic rather than flaky.
	time.Sleep(1100 * time.Millisecond)
	if err := database.AddDeploymentLog(&models.DeploymentLog{DeploymentID: deployment.ID, Level: "info", Message: "provisioning started"}); err != nil {
		t.Fatalf("add log: %v", err)
	}

	svc := NewDeployService(database, NewTargetService(database, nil), nil, nil, nil)
	timeline, err := svc.GetTimeline(deployment.ID)
	if err != nil {
		t.Fatalf("get timeline: %v", err)
	}
	if len(timeline) != 2 {
		t.Fatalf("expected 2 events, got %d", len(timeline))
	}
	if timeline[0].Source != "audit" || timeline[0].Message != "Deployment requested" {
		t.Errorf("expected the audit event first with a human-readable label, got %+v", timeline[0])
	}
	if timeline[1].Source != "log" {
		t.Errorf("expected the log event second, got %+v", timeline[1])
	}
	if !timeline[0].Timestamp.Before(timeline[1].Timestamp) {
		t.Errorf("expected events in chronological order, got %+v then %+v", timeline[0], timeline[1])
	}
}
