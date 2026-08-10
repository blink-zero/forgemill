package db

import (
	"path/filepath"
	"testing"

	"github.com/forgemill/forgemill/internal/db/models"
)

func newTestDB(t *testing.T) *DB {
	t.Helper()
	database, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	return database
}

func TestCreateActionStartsAtVersion1(t *testing.T) {
	database := newTestDB(t)
	a := &models.Action{Name: "install-nginx", Category: "packages", Script: "apt-get install -y nginx"}
	if err := database.CreateAction(a); err != nil {
		t.Fatalf("create action: %v", err)
	}
	if a.Version != 1 {
		t.Errorf("expected version 1, got %d", a.Version)
	}

	history, err := database.ListActionVersionHistory(a.ID)
	if err != nil {
		t.Fatalf("list history: %v", err)
	}
	if len(history) != 0 {
		t.Errorf("expected no superseded versions right after creation, got %d", len(history))
	}
}

func TestUpdateActionSnapshotsPriorVersionRetroactively(t *testing.T) {
	database := newTestDB(t)
	userID := int64(7)

	a := &models.Action{Name: "install-nginx", Category: "packages", Script: "apt-get install -y nginx"}
	if err := database.CreateAction(a); err != nil {
		t.Fatalf("create action: %v", err)
	}

	// First edit of a pre-existing action: this is what retroactively
	// captures "version 1" content, since creation itself writes no
	// history row.
	a.Script = "apt-get install -y nginx=1.24.*"
	if err := database.UpdateAction(a, &userID); err != nil {
		t.Fatalf("update action: %v", err)
	}
	if a.Version != 2 {
		t.Errorf("expected version 2 after first edit, got %d", a.Version)
	}

	history, err := database.ListActionVersionHistory(a.ID)
	if err != nil {
		t.Fatalf("list history: %v", err)
	}
	if len(history) != 1 {
		t.Fatalf("expected 1 superseded version, got %d", len(history))
	}
	if history[0].Version != 1 || history[0].Script != "apt-get install -y nginx" {
		t.Errorf("expected version 1 to hold the original script, got %+v", history[0])
	}
	if history[0].ChangedBy == nil || *history[0].ChangedBy != userID {
		t.Errorf("expected changed_by=%d, got %+v", userID, history[0].ChangedBy)
	}

	// Second edit: now version 2's content should also become retrievable history.
	a.Script = "apt-get install -y nginx=1.26.*"
	if err := database.UpdateAction(a, &userID); err != nil {
		t.Fatalf("update action (2nd): %v", err)
	}
	if a.Version != 3 {
		t.Errorf("expected version 3, got %d", a.Version)
	}
	v2, err := database.GetActionVersion(a.ID, 2)
	if err != nil {
		t.Fatalf("get version 2: %v", err)
	}
	if v2.Script != "apt-get install -y nginx=1.24.*" {
		t.Errorf("expected version 2 to hold the mid-point script, got %q", v2.Script)
	}
}

func TestRollbackActionCreatesNewVersionInsteadOfRewritingHistory(t *testing.T) {
	database := newTestDB(t)
	userID := int64(3)

	a := &models.Action{Name: "configure-firewall", Category: "security", Script: "ufw allow 22"}
	if err := database.CreateAction(a); err != nil {
		t.Fatalf("create action: %v", err)
	}
	a.Script = "ufw allow 22; ufw allow 443"
	if err := database.UpdateAction(a, &userID); err != nil {
		t.Fatalf("update action: %v", err)
	}
	if a.Version != 2 {
		t.Fatalf("expected version 2, got %d", a.Version)
	}

	restored, err := database.RollbackAction(a.ID, 1, &userID)
	if err != nil {
		t.Fatalf("rollback: %v", err)
	}
	if restored.Version != 3 {
		t.Errorf("rollback should mint a new version (3), not reuse version 1, got %d", restored.Version)
	}
	if restored.Script != "ufw allow 22" {
		t.Errorf("expected rollback to restore version 1's script, got %q", restored.Script)
	}

	// Version 1 and version 2 must both still be intact in history — rollback
	// must not have deleted or overwritten anything.
	v1, err := database.GetActionVersion(a.ID, 1)
	if err != nil || v1.Script != "ufw allow 22" {
		t.Errorf("expected version 1 untouched, got %+v, err=%v", v1, err)
	}
	v2, err := database.GetActionVersion(a.ID, 2)
	if err != nil || v2.Script != "ufw allow 22; ufw allow 443" {
		t.Errorf("expected version 2 untouched, got %+v, err=%v", v2, err)
	}
}

func TestRollbackActionRejectsCurrentVersionAndBuiltins(t *testing.T) {
	database := newTestDB(t)
	userID := int64(1)

	a := &models.Action{Name: "noop", Category: "custom", Script: "true"}
	if err := database.CreateAction(a); err != nil {
		t.Fatalf("create action: %v", err)
	}
	if _, err := database.RollbackAction(a.ID, 1, &userID); err == nil {
		t.Error("expected error rolling back to the already-current version")
	}

	if _, err := database.conn.Exec(`INSERT INTO actions (name, script, builtin, version) VALUES ('builtin-one', 'true', 1, 1)`); err != nil {
		t.Fatalf("seed builtin action: %v", err)
	}
	var builtinID int64
	if err := database.conn.QueryRow(`SELECT id FROM actions WHERE name = 'builtin-one'`).Scan(&builtinID); err != nil {
		t.Fatalf("find builtin id: %v", err)
	}
	if _, err := database.RollbackAction(builtinID, 1, &userID); err == nil {
		t.Error("expected error rolling back a builtin action")
	}
}
