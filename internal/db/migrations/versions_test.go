package migrations

import (
	"database/sql"
	"fmt"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

// Each migration must record its own version, otherwise it silently re-runs
// on every start (v0.19.0/v0.19.1 shipped V40 and V41 without this and
// duplicated built-in actions on each restart).
func TestEveryMigrationRecordsItsVersion(t *testing.T) {
	for _, m := range migrations {
		want := fmt.Sprintf("INSERT INTO schema_version (version) VALUES (%d);", m.version)
		if !strings.Contains(m.sql, want) {
			t.Errorf("migration v%d does not contain %q", m.version, want)
		}
	}
}

func TestMigrationsAreNoopOnSecondRun(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	if err := Run(db); err != nil {
		t.Fatalf("first run: %v", err)
	}
	var builtins int
	if err := db.QueryRow(`SELECT COUNT(*) FROM actions WHERE builtin = 1`).Scan(&builtins); err != nil {
		t.Fatal(err)
	}
	if err := Run(db); err != nil {
		t.Fatalf("second run: %v", err)
	}
	var version, again, dups int
	if err := db.QueryRow(`SELECT MAX(version) FROM schema_version`).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if latest := migrations[len(migrations)-1].version; version != latest {
		t.Errorf("schema_version = %d, want %d", version, latest)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM actions WHERE builtin = 1`).Scan(&again); err != nil {
		t.Fatal(err)
	}
	if again != builtins {
		t.Errorf("built-in actions grew from %d to %d on a second run", builtins, again)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM (SELECT name FROM actions WHERE builtin = 1 GROUP BY name HAVING COUNT(*) > 1)`).Scan(&dups); err != nil {
		t.Fatal(err)
	}
	if dups != 0 {
		t.Errorf("%d built-in action names duplicated", dups)
	}
}

// A database written by v0.19.x (at version 39 with V40's built-ins inserted
// three times by three restarts, and one execution pointing at a duplicate)
// ends up with one of each and the execution repointed.
func TestV42CollapsesDuplicateBuiltins(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	// Run everything up to V39 by temporarily hiding later migrations.
	saved := migrations
	for i, m := range migrations {
		if m.version == 40 {
			migrations = migrations[:i]
			break
		}
	}
	err = Run(db)
	migrations = saved
	if err != nil {
		t.Fatalf("run to v39: %v", err)
	}
	for i := 0; i < 3; i++ {
		for _, a := range v40BuiltinActions {
			if _, err := db.Exec(`INSERT INTO actions (name, description, category, script, builtin) VALUES (?, '', 'scripts', ?, 1)`, a.name, a.script); err != nil {
				t.Fatal(err)
			}
		}
	}
	var keep, dup int64
	if err := db.QueryRow(`SELECT MIN(id), MAX(id) FROM actions WHERE builtin = 1 AND name = ?`, v40BuiltinActions[0].name).Scan(&keep, &dup); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO users (username, password_hash, role) VALUES ('u', 'x', 'admin')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO targets (name, type, hostname, username, password_encrypted) VALUES ('t', 'esxi', 'h', 'u', 'p')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO managed_vms (target_id, vm_ref, vm_name) VALUES (1, 'vm-1', 'a')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO action_executions (vm_id, action_id, action_name, script, created_by) VALUES (1, ?, 'n', 's', 1)`, dup); err != nil {
		t.Fatal(err)
	}
	if err := Run(db); err != nil {
		t.Fatalf("run v40..: %v", err)
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM actions WHERE builtin = 1 AND name = ?`, v40BuiltinActions[0].name).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("want 1 copy of %q, got %d", v40BuiltinActions[0].name, n)
	}
	var got int64
	if err := db.QueryRow(`SELECT action_id FROM action_executions LIMIT 1`).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != keep {
		t.Errorf("execution points at %d, want surviving action %d", got, keep)
	}
}
