package migrations

import (
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
)

// backupDatabase creates a copy of the SQLite database using the VACUUM INTO command.
func backupDatabase(db *sql.DB, backupPath string) error {
	// Remove existing backup at this path if present
	os.Remove(backupPath)
	_, err := db.Exec(`VACUUM INTO ?`, backupPath)
	if err != nil {
		return fmt.Errorf("VACUUM INTO: %w", err)
	}
	return nil
}

var migrations = []struct {
	version int
	sql     string
}{
	{1, migrationV1},
	{2, migrationV2},
	{3, migrationV3},
	{4, migrationV4},
	{5, migrationV5},
	{6, migrationV6},
	{7, migrationV7},
	{8, migrationV8},
	{9, migrationV9},
	{10, migrationV10},
	{11, migrationV11},
	{12, migrationV12},
	{13, migrationV13},
	{14, migrationV14},
	{15, migrationV15},
	{16, migrationV16},
	{17, migrationV17},
	{18, migrationV18},
	{19, migrationV19},
	{20, migrationV20},
	{21, migrationV21},
	{22, migrationV22},
	{23, migrationV23},
	{24, migrationV24},
	{25, migrationV25},
	{26, migrationV26},
	{27, migrationV27},
	{28, migrationV28},
	{29, migrationV29},
	{30, migrationV30},
	{31, migrationV31},
	{32, migrationV32},
	{33, migrationV33},
	{34, migrationV34},
	{35, migrationV35},
}

const migrationV1 = `
CREATE TABLE IF NOT EXISTS users (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    username TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    display_name TEXT,
    role TEXT DEFAULT 'user' CHECK(role IN ('admin', 'user', 'viewer')),
    is_active BOOLEAN DEFAULT TRUE,
    last_login_at DATETIME,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS targets (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    type TEXT NOT NULL CHECK(type IN ('vcenter', 'esxi', 'proxmox')),
    hostname TEXT NOT NULL,
    port INTEGER DEFAULT 443,
    username TEXT NOT NULL,
    password_encrypted TEXT NOT NULL,
    validate_certs BOOLEAN DEFAULT FALSE,
    is_default BOOLEAN DEFAULT FALSE,
    status TEXT DEFAULT 'unknown',
    last_connected_at DATETIME,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS templates (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    target_id INTEGER NOT NULL REFERENCES targets(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    moref TEXT,
    os_type TEXT,
    os_name TEXT,
    guest_id TEXT,
    cpu INTEGER,
    memory_mb INTEGER,
    disk_gb INTEGER,
    notes TEXT,
    icon TEXT,
    last_synced_at DATETIME,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS deployments (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    template_id INTEGER NOT NULL REFERENCES templates(id),
    target_id INTEGER NOT NULL REFERENCES targets(id),
    vm_name TEXT NOT NULL,
    status TEXT DEFAULT 'pending' CHECK(status IN ('pending','running','completed','failed','cancelled')),
    config_json TEXT,
    started_at DATETIME,
    completed_at DATETIME,
    error_message TEXT,
    created_by INTEGER REFERENCES users(id),
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS deployment_logs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    deployment_id INTEGER NOT NULL REFERENCES deployments(id) ON DELETE CASCADE,
    timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
    level TEXT DEFAULT 'info',
    message TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS schema_version (
    version INTEGER PRIMARY KEY
);

INSERT INTO schema_version (version) VALUES (1);
`

const migrationV2 = `
CREATE TABLE IF NOT EXISTS template_sources (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    os_type TEXT NOT NULL,
    iso_url TEXT NOT NULL,
    checksum_url TEXT,
    packer_config TEXT,
    auto_refresh BOOLEAN DEFAULT FALSE,
    refresh_interval_days INTEGER DEFAULT 30,
    last_built_at DATETIME,
    target_id INTEGER REFERENCES targets(id),
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS api_keys (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id INTEGER NOT NULL REFERENCES users(id),
    name TEXT NOT NULL,
    key_hash TEXT NOT NULL,
    prefix TEXT NOT NULL,
    last_used_at DATETIME,
    expires_at DATETIME,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS webhooks (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    url TEXT NOT NULL,
    events TEXT NOT NULL,
    secret TEXT,
    is_active BOOLEAN DEFAULT TRUE,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

INSERT INTO schema_version (version) VALUES (2);
`

const migrationV3 = `
CREATE TABLE IF NOT EXISTS managed_vms (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    deployment_id INTEGER REFERENCES deployments(id),
    target_id INTEGER NOT NULL REFERENCES targets(id),
    vm_name TEXT NOT NULL,
    vm_ref TEXT NOT NULL,
    power_state TEXT DEFAULT 'unknown',
    ip_address TEXT DEFAULT '',
    cpu INTEGER DEFAULT 0,
    memory_mb INTEGER DEFAULT 0,
    disk_gb INTEGER DEFAULT 0,
    os_type TEXT DEFAULT '',
    last_synced_at DATETIME,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS vm_snapshots (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    vm_id INTEGER NOT NULL REFERENCES managed_vms(id) ON DELETE CASCADE,
    snapshot_ref TEXT NOT NULL,
    name TEXT NOT NULL,
    description TEXT DEFAULT '',
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS blueprints (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    description TEXT DEFAULT '',
    template_id INTEGER REFERENCES templates(id),
    target_id INTEGER REFERENCES targets(id),
    config_json TEXT NOT NULL DEFAULT '{}',
    created_by INTEGER REFERENCES users(id),
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS bulk_deployments (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    status TEXT DEFAULT 'pending',
    total_vms INTEGER NOT NULL DEFAULT 0,
    completed_vms INTEGER DEFAULT 0,
    failed_vms INTEGER DEFAULT 0,
    parallel BOOLEAN DEFAULT FALSE,
    created_by INTEGER REFERENCES users(id),
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    completed_at DATETIME
);

ALTER TABLE deployments ADD COLUMN bulk_deployment_id INTEGER REFERENCES bulk_deployments(id);

CREATE TABLE IF NOT EXISTS auth_sources (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    type TEXT NOT NULL CHECK(type IN ('local', 'ldap')),
    config_json TEXT DEFAULT '{}',
    is_default BOOLEAN DEFAULT FALSE,
    enabled BOOLEAN DEFAULT TRUE,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

ALTER TABLE users ADD COLUMN auth_source_id INTEGER REFERENCES auth_sources(id);
ALTER TABLE users ADD COLUMN external_id TEXT DEFAULT '';

INSERT INTO schema_version (version) VALUES (3);
`

const migrationV4 = `
CREATE TABLE IF NOT EXISTS template_builds (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    os_definition_id TEXT NOT NULL,
    target_id INTEGER NOT NULL REFERENCES targets(id),
    status TEXT DEFAULT 'pending' CHECK(status IN ('pending','downloading','building','converting','completed','failed','cancelled')),
    template_name TEXT NOT NULL,
    config_json TEXT NOT NULL,
    iso_url TEXT DEFAULT '',
    iso_checksum TEXT DEFAULT '',
    packer_template TEXT DEFAULT '',
    autoinstall_config TEXT DEFAULT '',
    packer_log TEXT DEFAULT '',
    started_at DATETIME,
    completed_at DATETIME,
    error_message TEXT DEFAULT '',
    created_by INTEGER REFERENCES users(id),
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

INSERT INTO schema_version (version) VALUES (4);
`

const migrationV5 = `
-- Template lineage columns
ALTER TABLE templates ADD COLUMN build_id INTEGER REFERENCES template_builds(id);
ALTER TABLE templates ADD COLUMN managed_by_forgemill BOOLEAN DEFAULT FALSE;
ALTER TABLE templates ADD COLUMN version INTEGER DEFAULT 1;
ALTER TABLE templates ADD COLUMN iso_checksum TEXT DEFAULT '';
ALTER TABLE templates ADD COLUMN built_at DATETIME;
ALTER TABLE templates ADD COLUMN lifecycle_status TEXT DEFAULT 'active' CHECK(lifecycle_status IN ('active', 'superseded', 'pending_delete'));
ALTER TABLE templates ADD COLUMN superseded_by INTEGER REFERENCES templates(id);
ALTER TABLE templates ADD COLUMN retain_until DATETIME;

-- Template build lineage columns
ALTER TABLE template_builds ADD COLUMN template_id INTEGER REFERENCES templates(id);
ALTER TABLE template_builds ADD COLUMN version INTEGER DEFAULT 1;
ALTER TABLE template_builds ADD COLUMN previous_build_id INTEGER REFERENCES template_builds(id);
ALTER TABLE template_builds ADD COLUMN auto_triggered BOOLEAN DEFAULT FALSE;

-- Template schedules table
CREATE TABLE IF NOT EXISTS template_schedules (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    template_id INTEGER NOT NULL REFERENCES templates(id),
    build_config_json TEXT NOT NULL DEFAULT '{}',
    strategy TEXT NOT NULL CHECK(strategy IN ('interval', 'on_update', 'both')),
    interval_days INTEGER DEFAULT 30,
    check_interval_hours INTEGER DEFAULT 24,
    last_checked_at DATETIME,
    last_rebuilt_at DATETIME,
    next_check_at DATETIME,
    enabled BOOLEAN DEFAULT TRUE,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

INSERT INTO schema_version (version) VALUES (5);
`

// M4: Add token_version column for JWT revocation
const migrationV6 = `
ALTER TABLE users ADD COLUMN token_version INTEGER DEFAULT 0;

INSERT INTO schema_version (version) VALUES (6);
`

// V3-M12: Change validate_certs default to TRUE for new targets
// Note: Only sets the default for new targets at the application layer.
// Existing targets are NOT modified to preserve user intent (e.g., lab environments with self-signed certs).
const migrationV7 = `
-- New targets created after this migration will use the application-level default of TRUE
-- via the Go service layer (target.go Create method).
-- Existing targets are intentionally left unchanged to preserve user configuration.

INSERT INTO schema_version (version) VALUES (7);
`

const migrationV8 = `
CREATE TABLE IF NOT EXISTS app_settings (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

INSERT INTO schema_version (version) VALUES (8);
`

// B-1: Expand auth_sources type CHECK to include saml/oidc.
// A-5: Add indexes on frequently queried columns for performance.
// SQLite doesn't support ALTER CHECK, so recreate the table.
const migrationV9 = `
-- F-12: Disable FK enforcement while recreating auth_sources to avoid
-- breaking users.auth_source_id references during the DROP+RENAME.
PRAGMA foreign_keys=OFF;

CREATE TABLE IF NOT EXISTS auth_sources_new (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    type TEXT NOT NULL CHECK(type IN ('local', 'ldap', 'saml', 'oidc')),
    config_json TEXT DEFAULT '{}',
    is_default BOOLEAN DEFAULT FALSE,
    enabled BOOLEAN DEFAULT TRUE,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
INSERT INTO auth_sources_new SELECT * FROM auth_sources;
DROP TABLE auth_sources;
ALTER TABLE auth_sources_new RENAME TO auth_sources;

PRAGMA foreign_keys=ON;

CREATE INDEX IF NOT EXISTS idx_deployments_status ON deployments(status);
CREATE INDEX IF NOT EXISTS idx_deployments_target_id ON deployments(target_id);
CREATE INDEX IF NOT EXISTS idx_managed_vms_target_id ON managed_vms(target_id);
CREATE INDEX IF NOT EXISTS idx_template_builds_status ON template_builds(status);

INSERT INTO schema_version (version) VALUES (9);
`

// migrationV10: disk_gb column may already exist if managed_vms was created by v3.
// Use a no-op SQL here; the column addition is handled in Run() with a column check.
const migrationV10 = `
INSERT INTO schema_version (version) VALUES (10);
`

// BUG-01: Add unique index on (target_id, moref) to support upsert in SyncTemplates
const migrationV11 = `
CREATE UNIQUE INDEX IF NOT EXISTS idx_templates_target_moref ON templates(target_id, moref);

INSERT INTO schema_version (version) VALUES (11);
`

// Add UNIQUE constraint on (vm_ref, target_id) to prevent duplicate VM registrations.
// Clean up any existing duplicates first (keep the lowest ID).
const migrationV12 = `
DELETE FROM managed_vms WHERE id NOT IN (
    SELECT MIN(id) FROM managed_vms GROUP BY vm_ref, target_id
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_managed_vms_ref_target ON managed_vms(vm_ref, target_id);

INSERT INTO schema_version (version) VALUES (12);
`

func columnExists(db *sql.DB, table, column string) bool {
	rows, err := db.Query(fmt.Sprintf("PRAGMA table_info(%s)", table))
	if err != nil {
		return false
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, typ string
		var notnull int
		var dflt *string
		var pk int
		if err := rows.Scan(&cid, &name, &typ, &notnull, &dflt, &pk); err != nil {
			continue
		}
		if name == column {
			return true
		}
	}
	return false
}

// RunWithBackup runs migrations with an automatic backup before applying any new migrations.
// dbPath is the filesystem path to the SQLite database file.
func RunWithBackup(db *sql.DB, dbPath string) error {
	return runMigrations(db, dbPath)
}

// Run runs migrations without backup (legacy entrypoint).
func Run(db *sql.DB) error {
	return runMigrations(db, "")
}

func runMigrations(db *sql.DB, dbPath string) error {
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS schema_version (version INTEGER PRIMARY KEY)`)
	if err != nil {
		return fmt.Errorf("create schema_version table: %w", err)
	}

	var current int
	row := db.QueryRow(`SELECT COALESCE(MAX(version), 0) FROM schema_version`)
	if err := row.Scan(&current); err != nil {
		return fmt.Errorf("get schema version: %w", err)
	}

	// Determine if any migrations need to run
	pending := 0
	for _, m := range migrations {
		if m.version > current {
			pending++
		}
	}

	// Auto-backup before applying migrations
	if pending > 0 && dbPath != "" {
		backupPath := fmt.Sprintf("%s.backup-v%d", dbPath, current)
		slog.Info("backing up database before migrations", "from_version", current, "pending", pending, "backup", backupPath)
		if err := backupDatabase(db, backupPath); err != nil {
			slog.Warn("database backup failed — proceeding with migrations", "error", err)
		} else {
			slog.Info("database backup created", "path", backupPath)
		}
	}

	for _, m := range migrations {
		if m.version <= current {
			continue
		}
		slog.Info("running migration", "version", m.version)

		// V10: conditionally add disk_gb column (may already exist from v3 schema)
		if m.version == 10 {
			if !columnExists(db, "managed_vms", "disk_gb") {
				if _, err := db.Exec(`ALTER TABLE managed_vms ADD COLUMN disk_gb INTEGER DEFAULT 0`); err != nil {
					return fmt.Errorf("migration v10 add column: %w", err)
				}
			}
		}

		// V3-L11: Wrap each migration in a transaction to prevent half-applied states
		tx, txErr := db.Begin()
		if txErr != nil {
			return fmt.Errorf("begin transaction for migration v%d: %w", m.version, txErr)
		}
		if _, err := tx.Exec(m.sql); err != nil {
			tx.Rollback()
			return fmt.Errorf("migration v%d: %w", m.version, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit migration v%d: %w", m.version, err)
		}

		// V16 post-migration: convert built-in actions from cloud-init JSON to bash scripts
		if m.version == 16 {
			for name, script := range builtinScripts {
				if _, err := db.Exec(`UPDATE actions SET script = ? WHERE name = ? AND builtin = 1`, script, name); err != nil {
					slog.Warn("failed to update builtin action script", "name", name, "error", err)
				}
			}
		}

		// V18 post-migration: update Set Timezone and SSH Key built-ins
		if m.version == 18 {
			// Rename "Set Timezone" → "Set Timezone (UTC)" and update script/description
			for _, a := range v17BuiltinActions {
				if _, err := db.Exec(
					`UPDATE actions SET name = ?, description = ?, script = ?, updated_at = CURRENT_TIMESTAMP WHERE builtin = 1 AND (name = ? OR name = ?)`,
					a.name, a.description, a.script, a.name, strings.TrimSuffix(a.name, " (UTC)"),
				); err != nil {
					slog.Warn("failed to update builtin action", "name", a.name, "error", err)
				}
			}
		}

		// V17 post-migration: insert new built-in actions
		if m.version == 17 {
			for _, a := range v17BuiltinActions {
				if _, err := db.Exec(
					`INSERT INTO actions (name, description, category, script, builtin, created_at, updated_at) VALUES (?, ?, ?, ?, 1, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`,
					a.name, a.description, a.category, a.script,
				); err != nil {
					slog.Warn("failed to insert builtin action", "name", a.name, "error", err)
				}
			}
		}

		// V21 post-migration: seed template families from existing managed templates
		if m.version == 21 {
			if err := seedTemplateFamilies(db); err != nil {
				slog.Warn("failed to seed template families", "error", err)
			}
		}

		// V23 post-migration: fix versioning data integrity
		if m.version == 23 {
			// 1. For each family, supersede all but the highest-version active template.
			rows, err := db.Query(`
				SELECT DISTINCT family_id FROM templates
				WHERE family_id IS NOT NULL AND lifecycle_status = 'active'`)
			if err == nil {
				var familyIDs []int64
				for rows.Next() {
					var fid int64
					if err := rows.Scan(&fid); err == nil {
						familyIDs = append(familyIDs, fid)
					}
				}
				rows.Close()
				for _, fid := range familyIDs {
					// Find the highest-version active template in this family
					var keepID int64
					if err := db.QueryRow(`
						SELECT id FROM templates
						WHERE family_id = ? AND lifecycle_status = 'active'
						ORDER BY version DESC LIMIT 1`, fid).Scan(&keepID); err == nil {
						// Supersede all other active templates in this family
						if _, err := db.Exec(`
							UPDATE templates SET lifecycle_status='superseded', superseded_by=?
							WHERE family_id=? AND id!=? AND lifecycle_status='active'`,
							keepID, fid, keepID); err != nil {
							slog.Warn("V23: failed to supersede old active templates", "family_id", fid, "error", err)
						}
					}
				}
			}

			// 2. Align family.latest_version with actual max template version.
			if _, err := db.Exec(`
				UPDATE template_families SET latest_version = (
					SELECT COALESCE(MAX(t.version), 0)
					FROM templates t WHERE t.family_id = template_families.id
				)`); err != nil {
				slog.Warn("V23: failed to align family latest_version", "error", err)
			}

			// 3. Remove orphaned families (no templates, no in-progress builds).
			if _, err := db.Exec(`
				DELETE FROM template_families WHERE id NOT IN (
					SELECT DISTINCT family_id FROM templates WHERE family_id IS NOT NULL
				) AND id NOT IN (
					SELECT DISTINCT f.id FROM template_families f
					JOIN template_builds b ON b.target_id = f.target_id
					  AND b.os_definition_id = f.os_definition_id
					WHERE b.status IN ('building', 'pending')
				)`); err != nil {
				slog.Warn("V23: failed to remove orphaned families", "error", err)
			}

			slog.Info("V23: versioning data integrity migration complete")
		}

		// V24 post-migration: merge duplicate families (same os_definition_id + target_id)
		if m.version == 24 {
			// Collect duplicate groups first (avoid nested queries which deadlock in SQLite)
			type dupGroup struct {
				osDefID      string
				targetID     int64
				familyIDsStr string
			}
			var dupGroups []dupGroup

			rows, err := db.Query(`
				SELECT os_definition_id, target_id, GROUP_CONCAT(id) as family_ids
				FROM template_families
				GROUP BY os_definition_id, target_id
				HAVING COUNT(*) > 1`)
			if err == nil {
				for rows.Next() {
					var g dupGroup
					if err := rows.Scan(&g.osDefID, &g.targetID, &g.familyIDsStr); err == nil {
						dupGroups = append(dupGroups, g)
					}
				}
				rows.Close()
			}

			// Process each duplicate group
			for _, g := range dupGroups {
				// Parse family IDs
				var familyIDs []int64
				for _, idStr := range strings.Split(g.familyIDsStr, ",") {
					if id, err := strconv.ParseInt(strings.TrimSpace(idStr), 10, 64); err == nil {
						familyIDs = append(familyIDs, id)
					}
				}
				if len(familyIDs) < 2 {
					continue
				}

				// Find the family to keep: prefer one with templates, else lowest ID
				keepID := familyIDs[0]
				for _, fid := range familyIDs {
					var templateCount int
					db.QueryRow(`SELECT COUNT(*) FROM templates WHERE family_id = ?`, fid).Scan(&templateCount)
					if templateCount > 0 {
						keepID = fid
						break
					}
				}

				// Move all templates from duplicate families to the keeper
				for _, fid := range familyIDs {
					if fid != keepID {
						db.Exec(`UPDATE templates SET family_id = ? WHERE family_id = ?`, keepID, fid)
						db.Exec(`DELETE FROM template_families WHERE id = ?`, fid)
						slog.Info("V24: merged duplicate family", "deleted_id", fid, "kept_id", keepID, "os", g.osDefID, "target", g.targetID)
					}
				}
			}

			// Try to add unique index (will fail gracefully if duplicates still exist)
			if _, err := db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_families_os_target ON template_families(os_definition_id, target_id)`); err != nil {
				slog.Warn("V24: could not create unique index (duplicates may still exist)", "error", err)
			}

			slog.Info("V24: family deduplication complete")
		}

		// V28 post-migration: insert 5 new built-in actions with parameters
		if m.version == 28 {
			for _, a := range v28BuiltinActions {
				paramsJSON := "NULL"
				if a.parameters != "" {
					paramsJSON = "'" + a.parameters + "'"
				}
				q := fmt.Sprintf(
					`INSERT INTO actions (name, description, category, script, script_type, platform, builtin, parameters, created_at, updated_at) VALUES (?, ?, ?, ?, 'bash', 'linux', 1, %s, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`,
					paramsJSON,
				)
				if _, err := db.Exec(q, a.name, a.description, a.category, a.script); err != nil {
					slog.Warn("failed to insert V28 builtin action", "name", a.name, "error", err)
				}
			}
		}

		// V30 post-migration: update Set Timezone to use parameter, refresh improved scripts
		if m.version == 30 {
			// Update Set Timezone to accept timezone parameter
			if _, err := db.Exec(
				`UPDATE actions SET name = ?, description = ?, script = ?, parameters = ?, updated_at = CURRENT_TIMESTAMP WHERE builtin = 1 AND name LIKE '%Set Timezone%'`,
				"Set Timezone",
				"Set the system timezone. Defaults to UTC if no timezone is specified.",
				v30SetTimezoneScript,
				`[{"name":"TIMEZONE","label":"Timezone","type":"string","required":false,"default":"UTC","placeholder":"Australia/Sydney","options":null,"description":"IANA timezone name (e.g. UTC, Australia/Sydney, America/New_York). Run timedatectl list-timezones on a VM for the full list."}]`,
			); err != nil {
				slog.Warn("v30: failed to update Set Timezone", "error", err)
			}

			// Refresh Security Hardening with sshd_config.d support + PermitEmptyPasswords
			if _, err := db.Exec(
				`UPDATE actions SET script = ?, updated_at = CURRENT_TIMESTAMP WHERE name = 'Security Hardening' AND builtin = 1`,
				v28SecurityHardeningScript,
			); err != nil {
				slog.Warn("v30: failed to update Security Hardening", "error", err)
			}

			// Refresh Network Connectivity Validation with DNS fallback chain
			if _, err := db.Exec(
				`UPDATE actions SET script = ?, updated_at = CURRENT_TIMESTAMP WHERE name = 'Network Connectivity Validation' AND builtin = 1`,
				v28NetworkValidationScript,
			); err != nil {
				slog.Warn("v30: failed to update Network Connectivity Validation", "error", err)
			}

			// Refresh User & Access Provisioning with SSH key deduplication
			if _, err := db.Exec(
				`UPDATE actions SET script = ?, updated_at = CURRENT_TIMESTAMP WHERE name = ? AND builtin = 1`,
				v28UserProvisioningScript, "User & Access Provisioning",
			); err != nil {
				slog.Warn("v30: failed to update User & Access Provisioning", "error", err)
			}
		}

		// V29 post-migration: update existing built-in actions to be cross-platform
		if m.version == 29 {
			for _, u := range v29BuiltinUpdates {
				if _, err := db.Exec(
					`UPDATE actions SET description = ?, script = ?, updated_at = CURRENT_TIMESTAMP WHERE name = ? AND builtin = 1`,
					u.description, u.script, u.name,
				); err != nil {
					slog.Warn("v29: failed to update builtin action", "name", u.name, "error", err)
				}
			}
		}

		// V34 post-migration: insert "Change VM Password" and "Add SSH Authorized Key" built-in actions
		if m.version == 34 {
			for _, a := range v34BuiltinActions {
				paramsJSON := "NULL"
				if a.parameters != "" {
					paramsJSON = "'" + a.parameters + "'"
				}
				q := fmt.Sprintf(
					`INSERT INTO actions (name, description, category, script, script_type, platform, builtin, parameters, created_at, updated_at) VALUES (?, ?, ?, ?, 'bash', 'linux', 1, %s, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`,
					paramsJSON,
				)
				if _, err := db.Exec(q, a.name, a.description, a.category, a.script); err != nil {
					slog.Warn("failed to insert V34 builtin action", "name", a.name, "error", err)
				}
			}
		}

		// V22 post-migration: insert "Collect VM Info" built-in action
		if m.version == 22 {
			if _, err := db.Exec(
				`INSERT INTO actions (name, description, category, script, script_type, platform, builtin, created_at, updated_at) VALUES (?, ?, ?, ?, 'bash', 'linux', 1, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`,
				"Collect VM Info",
				"Gather system information including hostname, OS, network, resources, disk usage, and services status.",
				"scripts",
				v22VMInfoScript,
			); err != nil {
				slog.Warn("failed to insert Collect VM Info builtin action", "error", err)
			}
		}
	}
	return nil
}

const migrationV13 = `
ALTER TABLE deployments ADD COLUMN initial_username TEXT DEFAULT '';
ALTER TABLE deployments ADD COLUMN initial_password_enc TEXT DEFAULT '';

INSERT INTO schema_version (version) VALUES (13);
`

const migrationV14 = `
CREATE TABLE IF NOT EXISTS actions (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    description TEXT DEFAULT '',
    category TEXT DEFAULT 'custom' CHECK(category IN ('packages', 'scripts', 'security', 'monitoring', 'custom')),
    cloud_config JSON NOT NULL,
    builtin INTEGER DEFAULT 0,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS deployment_actions (
    deployment_id INTEGER NOT NULL REFERENCES deployments(id),
    action_id INTEGER NOT NULL REFERENCES actions(id),
    sort_order INTEGER DEFAULT 0,
    PRIMARY KEY (deployment_id, action_id)
);

-- Built-in actions
INSERT INTO actions (name, description, category, cloud_config, builtin) VALUES
    ('Update System Packages', 'Run apt-get update and upgrade to install latest security patches and package updates.', 'packages', '{"runcmd":["apt-get update -y","apt-get upgrade -y"]}', 1),
    ('Install Docker', 'Install Docker Engine using the official install script for Ubuntu/Debian.', 'packages', '{"runcmd":["curl -fsSL https://get.docker.com | sh","systemctl enable --now docker"]}', 1),
    ('Install QEMU Guest Agent', 'Install and enable the QEMU guest agent for better VM management in Proxmox.', 'packages', '{"packages":["qemu-guest-agent"],"runcmd":["systemctl enable --now qemu-guest-agent"]}', 1),
    ('Enable UFW Firewall', 'Install and configure UFW with sensible defaults: deny incoming, allow outgoing, allow SSH.', 'security', '{"packages":["ufw"],"runcmd":["ufw default deny incoming","ufw default allow outgoing","ufw allow ssh","ufw --force enable"]}', 1),
    ('Install Monitoring (node_exporter)', 'Download and install Prometheus node_exporter for system metrics collection.', 'monitoring', '{"runcmd":["useradd --no-create-home --shell /bin/false node_exporter || true","curl -fsSL https://github.com/prometheus/node_exporter/releases/download/v1.7.0/node_exporter-1.7.0.linux-amd64.tar.gz | tar xz -C /tmp","cp /tmp/node_exporter-1.7.0.linux-amd64/node_exporter /usr/local/bin/","chown node_exporter:node_exporter /usr/local/bin/node_exporter"],"write_files":[{"path":"/etc/systemd/system/node_exporter.service","content":"[Unit]\\nDescription=Prometheus Node Exporter\\nAfter=network.target\\n\\n[Service]\\nUser=node_exporter\\nGroup=node_exporter\\nType=simple\\nExecStart=/usr/local/bin/node_exporter\\n\\n[Install]\\nWantedBy=multi-user.target\\n","permissions":"0644"}]}', 1);

INSERT INTO schema_version (version) VALUES (14);
`

const migrationV15 = `
CREATE TABLE IF NOT EXISTS action_executions (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    vm_id INTEGER NOT NULL REFERENCES managed_vms(id),
    action_id INTEGER REFERENCES actions(id),
    action_name TEXT NOT NULL,
    script TEXT NOT NULL,
    status TEXT DEFAULT 'pending' CHECK(status IN ('pending', 'running', 'completed', 'failed', 'cancelled')),
    exit_code INTEGER,
    output TEXT DEFAULT '',
    started_at DATETIME,
    completed_at DATETIME,
    created_by INTEGER NOT NULL REFERENCES users(id),
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_action_executions_vm_id ON action_executions(vm_id);
CREATE INDEX IF NOT EXISTS idx_action_executions_status ON action_executions(status);

INSERT INTO schema_version (version) VALUES (15);
`

// migrationV16: Rename cloud_config to script (actions are now bash scripts).
const migrationV16 = `
ALTER TABLE actions RENAME COLUMN cloud_config TO script;
INSERT INTO schema_version (version) VALUES (16);
`

// builtinScripts maps built-in action names to their bash script equivalents.
// Applied as a post-migration step for V16.
var builtinScripts = map[string]string{
	"Update System Packages": "#!/bin/bash\nset -euo pipefail\nexport DEBIAN_FRONTEND=noninteractive\n\napt-get update -y\napt-get upgrade -y",
	"Install Docker":        "#!/bin/bash\nset -euo pipefail\n\ncurl -fsSL https://get.docker.com | sh\nsystemctl enable --now docker",
	"Install QEMU Guest Agent": "#!/bin/bash\nset -euo pipefail\nexport DEBIAN_FRONTEND=noninteractive\n\napt-get update -y\napt-get install -y qemu-guest-agent\nsystemctl enable --now qemu-guest-agent",
	"Enable UFW Firewall": "#!/bin/bash\nset -euo pipefail\nexport DEBIAN_FRONTEND=noninteractive\n\napt-get update -y\napt-get install -y ufw\nufw default deny incoming\nufw default allow outgoing\nufw allow ssh\nufw --force enable",
	"Install Monitoring (node_exporter)": "#!/bin/bash\nset -euo pipefail\n\nuseradd --no-create-home --shell /bin/false node_exporter || true\ncurl -fsSL https://github.com/prometheus/node_exporter/releases/download/v1.7.0/node_exporter-1.7.0.linux-amd64.tar.gz | tar xz -C /tmp\ncp /tmp/node_exporter-1.7.0.linux-amd64/node_exporter /usr/local/bin/\nchown node_exporter:node_exporter /usr/local/bin/node_exporter\n\ncat > /etc/systemd/system/node_exporter.service << 'EOF'\n[Unit]\nDescription=Prometheus Node Exporter\nAfter=network.target\n\n[Service]\nUser=node_exporter\nGroup=node_exporter\nType=simple\nExecStart=/usr/local/bin/node_exporter\n\n[Install]\nWantedBy=multi-user.target\nEOF\n\nsystemctl daemon-reload\nsystemctl enable --now node_exporter",
}

// migrationV17: Replace platform-specific built-in actions with universal ones.
// Remove QEMU Guest Agent and node_exporter, add Set Timezone and Add SSH Key.
const migrationV17 = `
DELETE FROM actions WHERE name = 'Install QEMU Guest Agent' AND builtin = 1;
DELETE FROM actions WHERE name = 'Install Monitoring (node_exporter)' AND builtin = 1;
INSERT INTO schema_version (version) VALUES (17);
`

// migrationV18: Update Set Timezone and Add SSH Key built-in descriptions/scripts.
const migrationV18 = `
INSERT INTO schema_version (version) VALUES (18);
`

// migrationV19: Add script_type to actions and platform to managed_vms/templates
// for future Windows support. Defaults to bash/linux so nothing breaks.
const migrationV19 = `
ALTER TABLE actions ADD COLUMN script_type TEXT NOT NULL DEFAULT 'bash' CHECK(script_type IN ('bash', 'powershell', 'python'));
ALTER TABLE actions ADD COLUMN platform TEXT NOT NULL DEFAULT 'linux' CHECK(platform IN ('linux', 'windows', 'any'));

ALTER TABLE managed_vms ADD COLUMN platform TEXT NOT NULL DEFAULT 'linux' CHECK(platform IN ('linux', 'windows'));

ALTER TABLE templates ADD COLUMN platform TEXT NOT NULL DEFAULT 'linux' CHECK(platform IN ('linux', 'windows'));

INSERT INTO schema_version (version) VALUES (19);
`

// migrationV20: Add SSH host key fingerprint for TOFU verification.
const migrationV20 = `
ALTER TABLE managed_vms ADD COLUMN host_key_fp TEXT NOT NULL DEFAULT '';

INSERT INTO schema_version (version) VALUES (20);
`

// migrationV21: Add template families for template versioning redesign.
const migrationV21 = `
-- Create template_families table
CREATE TABLE template_families (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    base_name TEXT NOT NULL,
    target_id INTEGER NOT NULL REFERENCES targets(id),
    os_definition_id TEXT NOT NULL DEFAULT '',
    latest_version INTEGER NOT NULL DEFAULT 0,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(base_name, target_id)
);

-- Add family_id column to templates
ALTER TABLE templates ADD COLUMN family_id INTEGER REFERENCES template_families(id);

INSERT INTO schema_version (version) VALUES (21);
`

// v17BuiltinActions are new built-in actions added by migration V17.
var v17BuiltinActions = []struct {
	name, description, category, script string
}{
	{
		name:        "Set Timezone (UTC)",
		description: "Sets the system timezone to UTC. Create your own copy to use a different timezone.",
		category:    "scripts",
		script:      "#!/bin/bash\nset -euo pipefail\n\n# This action sets the timezone to UTC.\n# To use a different timezone, create a new action based on this one.\n# Run 'timedatectl list-timezones' on a VM to see all options.\n# Example: Australia/Sydney, America/New_York, Europe/London\n\ntimedatectl set-timezone UTC\necho \"Timezone set to $(timedatectl show -p Timezone --value)\"",
	},
	{
		name:        "Add SSH Authorized Key",
		description: "Example template for adding SSH keys. Create your own copy with your public key.",
		category:    "security",
		script:      "#!/bin/bash\nset -euo pipefail\n\n# This is a template action — create your own copy and replace the key below.\n# Built-in actions cannot be modified.\n#\n# To use: Create New Action → paste this script → replace PUBKEY with yours.\n\nPUBKEY=\"ssh-ed25519 AAAA... user@host\"\n\nif [ \"$PUBKEY\" = \"ssh-ed25519 AAAA... user@host\" ]; then\n  echo \"ERROR: Replace the placeholder PUBKEY with your actual public key.\"\n  echo \"Create a copy of this action and edit it with your key.\"\n  exit 1\nfi\n\nUSER=$(ls /home | head -1)\nif [ -z \"$USER\" ]; then\n  echo \"ERROR: No user found in /home\"\n  exit 1\nfi\n\nmkdir -p /home/$USER/.ssh\nchmod 700 /home/$USER/.ssh\necho \"$PUBKEY\" >> /home/$USER/.ssh/authorized_keys\nchmod 600 /home/$USER/.ssh/authorized_keys\nchown -R $USER:$USER /home/$USER/.ssh\necho \"SSH key added for $USER\"",
	},
}

// baseName strips version suffixes like "-v2" from a template name.
// Duplicated from factory package to avoid import cycle.
func baseName(name string) string {
	// Check if name ends with -vN pattern
	for i := len(name) - 1; i >= 0; i-- {
		if name[i] == 'v' && i > 0 && name[i-1] == '-' {
			// Verify the rest is digits
			allDigits := true
			for _, c := range name[i+1:] {
				if c < '0' || c > '9' {
					allDigits = false
					break
				}
			}
			if allDigits && len(name[i+1:]) > 0 {
				return name[:i-1]
			}
		}
	}
	return name
}

// seedTemplateFamilies creates template families from existing managed templates.
func seedTemplateFamilies(db *sql.DB) error {
	// Get all managed templates
	rows, err := db.Query(`SELECT id, name, target_id, version, build_id FROM templates WHERE managed_by_forgemill = 1`)
	if err != nil {
		return fmt.Errorf("query managed templates: %w", err)
	}
	defer rows.Close()

	// Group templates by (base_name, target_id)
	type familyKey struct {
		baseName string
		targetID int64
	}
	families := make(map[familyKey][]struct {
		id       int64
		version  int
		buildID  *int64
	})

	for rows.Next() {
		var id, targetID int64
		var name string
		var version int
		var buildID *int64
		if err := rows.Scan(&id, &name, &targetID, &version, &buildID); err != nil {
			return fmt.Errorf("scan template: %w", err)
		}

		base := baseName(name)
		key := familyKey{baseName: base, targetID: targetID}
		families[key] = append(families[key], struct {
			id       int64
			version  int
			buildID  *int64
		}{id: id, version: version, buildID: buildID})
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate templates: %w", err)
	}

	// Create family for each group and update templates
	for key, templates := range families {
		// Get OS definition ID from the latest template's build
		var osDefinitionID string
		latestVersion := 0
		for _, t := range templates {
			if t.version > latestVersion {
				latestVersion = t.version
				if t.buildID != nil {
					var buildOSDefID sql.NullString
					err := db.QueryRow(`SELECT os_definition_id FROM template_builds WHERE id = ?`, *t.buildID).Scan(&buildOSDefID)
					if err == nil && buildOSDefID.Valid {
						osDefinitionID = buildOSDefID.String
					}
				}
			}
		}

		// Create family
		result, err := db.Exec(`INSERT INTO template_families (base_name, target_id, os_definition_id, latest_version) VALUES (?, ?, ?, ?)`,
			key.baseName, key.targetID, osDefinitionID, latestVersion)
		if err != nil {
			slog.Warn("failed to create template family", "base_name", key.baseName, "target_id", key.targetID, "error", err)
			continue
		}

		familyID, err := result.LastInsertId()
		if err != nil {
			slog.Warn("failed to get family ID", "base_name", key.baseName, "error", err)
			continue
		}

		// Update all templates in this family
		for _, template := range templates {
			_, err := db.Exec(`UPDATE templates SET family_id = ? WHERE id = ?`, familyID, template.id)
			if err != nil {
				slog.Warn("failed to set family_id on template", "template_id", template.id, "family_id", familyID, "error", err)
			}
		}

		slog.Info("created template family", "family_id", familyID, "base_name", key.baseName, "target_id", key.targetID, "templates", len(templates), "latest_version", latestVersion)
	}

	return nil
}

// migrationV22: Add "Collect VM Info" built-in action.
const migrationV22 = `
INSERT INTO schema_version (version) VALUES (22);
`

// migrationV23: Fix versioning data integrity.
//   1. Ensure only the highest-versioned template per family is "active";
//      older ones are marked "superseded".
//   2. Align family.latest_version with the actual max template version so
//      the counter reflects reality rather than a stale high-water mark.
//   3. Remove orphaned families (no templates, no in-progress builds).
const migrationV23 = `
INSERT INTO schema_version (version) VALUES (23);
`

// migrationV24: Change family identity from (base_name, target_id) to (os_definition_id, target_id).
// This makes versioning robust — families are identified by OS+target, not template name.
// Post-migration Go code handles: merge duplicate families, re-create table with new constraint.
const migrationV24 = `
INSERT INTO schema_version (version) VALUES (24);
`

// migrationV25: Add audit_logs table for persisting audit events.
const migrationV25 = `
CREATE TABLE IF NOT EXISTS audit_logs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    actor TEXT NOT NULL DEFAULT 'system',
    actor_id INTEGER,
    action TEXT NOT NULL,
    resource_type TEXT DEFAULT '',
    resource_id TEXT DEFAULT '',
    metadata TEXT DEFAULT '{}',
    ip_address TEXT DEFAULT '',
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_audit_logs_created_at ON audit_logs(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_audit_logs_actor_id ON audit_logs(actor_id);
CREATE INDEX IF NOT EXISTS idx_audit_logs_action ON audit_logs(action);
INSERT INTO schema_version (version) VALUES (25);
`

// migrationV26: Soft-unlink deployments from deleted templates.
// Makes deployments.template_id nullable and adds a template_name snapshot column
// so deployments retain their template name even after the template is deleted.
const migrationV26 = `
CREATE TABLE deployments_new (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    template_id INTEGER REFERENCES templates(id),
    target_id INTEGER NOT NULL REFERENCES targets(id),
    vm_name TEXT NOT NULL,
    status TEXT DEFAULT 'pending' CHECK(status IN ('pending','running','completed','failed','cancelled')),
    config_json TEXT,
    started_at DATETIME,
    completed_at DATETIME,
    error_message TEXT,
    created_by INTEGER REFERENCES users(id),
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    bulk_deployment_id INTEGER REFERENCES bulk_deployments(id),
    initial_username TEXT DEFAULT '',
    initial_password_enc TEXT DEFAULT '',
    template_name TEXT DEFAULT ''
);
INSERT INTO deployments_new
    SELECT d.id, d.template_id, d.target_id, d.vm_name, d.status, d.config_json,
           d.started_at, d.completed_at, d.error_message, d.created_by, d.created_at,
           d.bulk_deployment_id,
           COALESCE(d.initial_username, ''),
           COALESCE(d.initial_password_enc, ''),
           COALESCE(t.name, '')
    FROM deployments d
    LEFT JOIN templates t ON d.template_id = t.id;
DROP TABLE deployments;
ALTER TABLE deployments_new RENAME TO deployments;
INSERT INTO schema_version (version) VALUES (26);
`

// migrationV27: Add platform-specific fields to targets table.
// Proxmox needs storage_pool and network_bridge.
// vSphere needs datacenter, datastore, and network.
const migrationV27 = `
ALTER TABLE targets ADD COLUMN storage_pool TEXT DEFAULT '';
ALTER TABLE targets ADD COLUMN network_bridge TEXT DEFAULT '';
ALTER TABLE targets ADD COLUMN datacenter TEXT DEFAULT '';
ALTER TABLE targets ADD COLUMN datastore TEXT DEFAULT '';
ALTER TABLE targets ADD COLUMN network TEXT DEFAULT '';
INSERT INTO schema_version (version) VALUES (27);
`

// v22VMInfoScript is the bash script for the Collect VM Info built-in action.
const v22VMInfoScript = `#!/bin/bash
set -euo pipefail

echo "=== System Information ==="
echo ""
echo "Hostname:     $(hostname)"
echo "FQDN:         $(hostname -f 2>/dev/null || echo 'N/A')"
echo "OS:           $(grep PRETTY_NAME /etc/os-release 2>/dev/null | cut -d'"' -f2 || echo 'Unknown')"
echo "Kernel:       $(uname -r)"
echo "Architecture: $(uname -m)"
echo "Uptime:       $(uptime -p 2>/dev/null || uptime)"
echo ""

echo "=== Network ==="
echo ""
ip -4 addr show 2>/dev/null | grep -E "inet " | awk '{print $NF ": " $2}' || echo "N/A"
echo ""
echo "Default Gateway: $(ip route 2>/dev/null | grep default | awk '{print $3}' | head -1 || echo 'N/A')"
echo "DNS Servers:"
grep nameserver /etc/resolv.conf 2>/dev/null | awk '{print "  " $2}' || echo "  N/A"
echo ""

echo "=== Resources ==="
echo ""
echo "CPU:    $(nproc) cores ($(grep 'model name' /proc/cpuinfo 2>/dev/null | head -1 | cut -d: -f2 | xargs || echo 'Unknown'))"
echo "Memory: $(free -h | awk '/^Mem:/{print $2}') total, $(free -h | awk '/^Mem:/{print $3}') used, $(free -h | awk '/^Mem:/{print $7}') available"
echo "Swap:   $(free -h | awk '/^Swap:/{print $2}') total, $(free -h | awk '/^Swap:/{print $3}') used"
echo ""

echo "=== Disk Usage ==="
echo ""
df -h --output=target,size,used,avail,pcent -x tmpfs -x devtmpfs 2>/dev/null || df -h
echo ""

echo "=== Services ==="
echo ""
echo "VMware Tools: $(vmware-toolbox-cmd -v 2>/dev/null || echo 'Not installed')"
echo "QEMU Agent:   $(systemctl is-active qemu-guest-agent 2>/dev/null || echo 'Not installed')"
echo "SSH:          $(systemctl is-active sshd 2>/dev/null || systemctl is-active ssh 2>/dev/null || echo 'Not running')"
echo "Docker:       $(docker --version 2>/dev/null || echo 'Not installed')"
echo "Firewall:     $(ufw status 2>/dev/null | head -1 || echo 'UFW not installed')"
echo ""

echo "=== Time ==="
echo ""
echo "Timezone:  $(timedatectl show -p Timezone --value 2>/dev/null || cat /etc/timezone 2>/dev/null || echo 'N/A')"
echo "Boot time: $(who -b 2>/dev/null | awk '{print $3, $4}' || echo 'N/A')"
echo "NTP sync:  $(timedatectl show -p NTPSynchronized --value 2>/dev/null || echo 'N/A')"
`

// migrationV28: Add parameters column to actions and parameter_values to action_executions.
const migrationV28 = `
ALTER TABLE actions ADD COLUMN parameters TEXT DEFAULT NULL;
ALTER TABLE action_executions ADD COLUMN parameter_values TEXT DEFAULT NULL;
INSERT INTO schema_version (version) VALUES (28);
`

// v28BuiltinActions defines the 5 new built-in actions added in V28.
var v28BuiltinActions = []struct {
	name, description, category, script, parameters string
}{
	{
		name:        "Security Hardening",
		description: "Disable root SSH login, disable password auth, install and configure firewall and fail2ban.",
		category:    "security",
		script:      v28SecurityHardeningScript,
		parameters:  "",
	},
	{
		name:        "Network Connectivity Validation",
		description: "Validate gateway, DNS, NTP, outbound HTTPS, and show network configuration with PASS/FAIL summary.",
		category:    "monitoring",
		script:      v28NetworkValidationScript,
		parameters:  "",
	},
	{
		name:        "Deploy Monitoring Agent",
		description: "Download and install Prometheus node_exporter as a systemd service with security hardening.",
		category:    "monitoring",
		script:      v28DeployMonitoringScript,
		parameters:  `[{"name":"EXPORTER_PORT","label":"Exporter Port","type":"number","required":false,"default":"9100","placeholder":"9100","options":null,"description":"Port for node_exporter to listen on"}]`,
	},
	{
		name:        "Configure Log Forwarding",
		description: "Install and configure rsyslog to forward logs to a remote syslog server via TCP or UDP.",
		category:    "scripts",
		script:      v28LogForwardingScript,
		parameters:  `[{"name":"SYSLOG_SERVER","label":"Syslog Server","type":"string","required":true,"default":"","placeholder":"10.0.0.50","options":null,"description":"IP or hostname of the syslog server"},{"name":"SYSLOG_PORT","label":"Syslog Port","type":"number","required":false,"default":"514","placeholder":"514","options":null,"description":"Port of the syslog server"},{"name":"SYSLOG_PROTOCOL","label":"Protocol","type":"select","required":false,"default":"tcp","placeholder":"","options":["tcp","udp"],"description":"Transport protocol for syslog"}]`,
	},
	{
		name:        "User & Access Provisioning",
		description: "Create a user account with optional SSH key, sudo access, and group membership.",
		category:    "security",
		script:      v28UserProvisioningScript,
		parameters:  `[{"name":"USERNAME","label":"Username","type":"string","required":true,"default":"","placeholder":"deploy","options":null,"description":"Username to create"},{"name":"SSH_PUBLIC_KEY","label":"SSH Public Key","type":"string","required":false,"default":"","placeholder":"ssh-rsa AAAA...","options":null,"description":"SSH public key for authorized_keys"},{"name":"SUDO_ACCESS","label":"Sudo Access","type":"select","required":false,"default":"full","placeholder":"","options":["full","limited","none"],"description":"Level of sudo access to grant"},{"name":"GROUPS","label":"Additional Groups","type":"string","required":false,"default":"","placeholder":"docker,sudo","options":null,"description":"Comma-separated additional groups"}]`,
	},
}

// migrationV29: Make existing built-in actions cross-platform (Ubuntu + RHEL/Rocky).
// Remove superseded actions (Enable UFW, node_exporter, Add SSH Key) replaced by V28 actions.
const migrationV29 = `
DELETE FROM actions WHERE name = 'Enable UFW Firewall' AND builtin = 1;
DELETE FROM actions WHERE name = 'Install Monitoring (node_exporter)' AND builtin = 1;
DELETE FROM actions WHERE name = 'Add SSH Authorized Key' AND builtin = 1;
INSERT INTO schema_version (version) VALUES (29);
`

// v29BuiltinUpdates are existing built-in actions updated to be cross-platform.
var v29BuiltinUpdates = []struct {
	name, description, script string
}{
	{
		name:        "Update System Packages",
		description: "Detect OS and run the appropriate package manager to install latest updates and security patches.",
		script:      v29UpdatePackagesScript,
	},
	{
		name:        "Install Docker",
		description: "Install Docker Engine using the official install script. Works on Ubuntu, Debian, Rocky, CentOS, Fedora, and SUSE.",
		script:      v29InstallDockerScript,
	},
	{
		name:        "Collect VM Info",
		description: "Collect and display comprehensive system information including OS, hardware, network, and storage details.",
		script:      v29CollectVMInfoScript,
	},
}

// migrationV30: Update Set Timezone to use parameter, update improved action scripts.
const migrationV30 = `
INSERT INTO schema_version (version) VALUES (30);
`

// migrationV31: Add SSH host key fingerprint to targets for TOFU verification.
const migrationV31 = `
ALTER TABLE targets ADD COLUMN ssh_host_key_fp TEXT NOT NULL DEFAULT '';
INSERT INTO schema_version (version) VALUES (31);
`

// migrationV32: Add indexes on frequently-queried foreign key columns for performance.
const migrationV32 = `
CREATE INDEX IF NOT EXISTS idx_templates_target_id ON templates(target_id);
CREATE INDEX IF NOT EXISTS idx_templates_family_id ON templates(family_id);
CREATE INDEX IF NOT EXISTS idx_managed_vms_target_id ON managed_vms(target_id);
CREATE INDEX IF NOT EXISTS idx_managed_vms_deployment_id ON managed_vms(deployment_id);
CREATE INDEX IF NOT EXISTS idx_deployments_target_id ON deployments(target_id);
CREATE INDEX IF NOT EXISTS idx_deployments_template_id ON deployments(template_id);
CREATE INDEX IF NOT EXISTS idx_template_builds_target_id ON template_builds(target_id);
CREATE INDEX IF NOT EXISTS idx_template_builds_template_id ON template_builds(template_id);
CREATE INDEX IF NOT EXISTS idx_action_executions_vm_id ON action_executions(vm_id);
CREATE INDEX IF NOT EXISTS idx_deployment_actions_deployment_id ON deployment_actions(deployment_id);
CREATE INDEX IF NOT EXISTS idx_template_schedules_template_id ON template_schedules(template_id);
INSERT INTO schema_version (version) VALUES (32);
`

// migrationV33: Per-user preferences table for UI settings (view mode, etc.)
const migrationV33 = `
CREATE TABLE IF NOT EXISTS user_preferences (
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    key TEXT NOT NULL,
    value TEXT NOT NULL,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (user_id, key)
);
INSERT INTO schema_version (version) VALUES (33);
`

// migrationV34: Add "Change VM Password" and "Add SSH Authorized Key" built-in actions.
const migrationV34 = `
INSERT INTO schema_version (version) VALUES (34);
`

// migrationV35: In-app notifications table. user_id nullable for broadcast
// notifications. Recipients index lets us filter unread quickly.
const migrationV35 = `
CREATE TABLE IF NOT EXISTS notifications (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id INTEGER REFERENCES users(id) ON DELETE CASCADE,
    level TEXT NOT NULL DEFAULT 'info' CHECK(level IN ('info', 'success', 'warning', 'error')),
    title TEXT NOT NULL,
    body TEXT DEFAULT '',
    link TEXT DEFAULT '',
    event TEXT DEFAULT '',
    is_read BOOLEAN DEFAULT FALSE,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    read_at DATETIME
);
CREATE INDEX IF NOT EXISTS idx_notifications_user_created ON notifications(user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_notifications_user_unread ON notifications(user_id, is_read);
INSERT INTO schema_version (version) VALUES (35);
`

// v34BuiltinActions defines the 2 new built-in actions added in V34.
var v34BuiltinActions = []struct {
	name, description, category, script, parameters string
}{
	{
		name:        "Change VM Password",
		description: "Change the password for a user account on the VM.",
		category:    "security",
		script:      v34ChangePasswordScript,
		parameters:  `[{"name":"USERNAME","label":"Username","type":"string","required":true,"default":"","placeholder":"forgemill","options":null,"description":"The user account whose password will be changed"},{"name":"NEW_PASSWORD","label":"New Password","type":"password","required":true,"default":"","placeholder":"","options":null,"description":"The new password to set for the user"}]`,
	},
	{
		name:        "Add SSH Authorized Key",
		description: "Add an SSH public key to a user's authorized_keys file for key-based authentication.",
		category:    "security",
		script:      v34AddSSHKeyScript,
		parameters:  `[{"name":"USERNAME","label":"Username","type":"string","required":true,"default":"","placeholder":"forgemill","options":null,"description":"The user account to add the SSH key to"},{"name":"SSH_PUBLIC_KEY","label":"SSH Public Key","type":"string","required":true,"default":"","placeholder":"ssh-ed25519 AAAA... user@host","options":null,"description":"The full SSH public key string to add to authorized_keys"}]`,
	},
}
