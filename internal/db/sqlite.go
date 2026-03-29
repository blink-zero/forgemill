package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/forgemill/forgemill/internal/db/migrations"
	"github.com/forgemill/forgemill/internal/db/models"
	_ "modernc.org/sqlite"
)

// escapeLike escapes SQL LIKE wildcard characters so user input is matched literally.
func escapeLike(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `%`, `\%`)
	s = strings.ReplaceAll(s, `_`, `\_`)
	return s
}

type DB struct {
	conn *sql.DB
}

func Open(path string) (*DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0750); err != nil {
		return nil, fmt.Errorf("create data directory: %w", err)
	}

	conn, err := sql.Open("sqlite", path+"?_journal_mode=WAL&_busy_timeout=5000&_foreign_keys=ON")
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	conn.SetMaxOpenConns(1)

	if err := migrations.RunWithBackup(conn, path); err != nil {
		conn.Close()
		return nil, fmt.Errorf("run migrations: %w", err)
	}

	slog.Info("database initialized", "path", path)
	return &DB{conn: conn}, nil
}

func (db *DB) Close() error {
	return db.conn.Close()
}

// V3-M16/H6: Begin starts a database transaction.
func (db *DB) Begin() (*sql.Tx, error) {
	return db.conn.Begin()
}

// V3-H9: UpdateUserRole updates a user's role.
func (db *DB) UpdateUserRole(id int64, role string) error {
	// MED-23: Validate role in service layer instead of relying on DB CHECK constraint
	validRoles := map[string]bool{"admin": true, "user": true, "viewer": true}
	if !validRoles[role] {
		return fmt.Errorf("invalid role %q: must be admin, user, or viewer", role)
	}
	_, err := db.conn.Exec(`UPDATE users SET role = ? WHERE id = ?`, role, id)
	return err
}

// --- Users ---

func (db *DB) CreateUser(u *models.User) error {
	res, err := db.conn.Exec(
		`INSERT INTO users (username, password_hash, display_name, role, is_active) VALUES (?, ?, ?, ?, ?)`,
		u.Username, u.PasswordHash, u.DisplayName, u.Role, u.IsActive,
	)
	if err != nil {
		return err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return fmt.Errorf("get last insert id: %w", err)
	}
	u.ID = id
	return nil
}

func (db *DB) GetUserByUsername(username string) (*models.User, error) {
	u := &models.User{}
	err := db.conn.QueryRow(
		`SELECT id, username, password_hash, display_name, role, is_active, last_login_at, created_at, COALESCE(token_version, 0) FROM users WHERE username = ?`,
		username,
	).Scan(&u.ID, &u.Username, &u.PasswordHash, &u.DisplayName, &u.Role, &u.IsActive, &u.LastLoginAt, &u.CreatedAt, &u.TokenVersion)
	if err != nil {
		return nil, err
	}
	return u, nil
}

func (db *DB) GetUserByID(id int64) (*models.User, error) {
	u := &models.User{}
	err := db.conn.QueryRow(
		`SELECT id, username, password_hash, display_name, role, is_active, last_login_at, created_at, COALESCE(token_version, 0) FROM users WHERE id = ?`,
		id,
	).Scan(&u.ID, &u.Username, &u.PasswordHash, &u.DisplayName, &u.Role, &u.IsActive, &u.LastLoginAt, &u.CreatedAt, &u.TokenVersion)
	if err != nil {
		return nil, err
	}
	return u, nil
}

func (db *DB) ListUsers() ([]models.User, error) {
	// V3-M13: Exclude password_hash from list query — sensitive data not needed for listing
	rows, err := db.conn.Query(`SELECT id, username, display_name, role, is_active, last_login_at, created_at, COALESCE(token_version, 0) FROM users ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	users := []models.User{}
	for rows.Next() {
		var u models.User
		if err := rows.Scan(&u.ID, &u.Username, &u.DisplayName, &u.Role, &u.IsActive, &u.LastLoginAt, &u.CreatedAt, &u.TokenVersion); err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	return users, rows.Err()
}

// IncrementTokenVersion increments the token_version for a user, invalidating all existing JWTs.
func (db *DB) IncrementTokenVersion(userID int64) error {
	_, err := db.conn.Exec(`UPDATE users SET token_version = COALESCE(token_version, 0) + 1 WHERE id = ?`, userID)
	return err
}

func (db *DB) UpdateUserPassword(id int64, passwordHash string) error {
	_, err := db.conn.Exec(`UPDATE users SET password_hash = ? WHERE id = ?`, passwordHash, id)
	return err
}

func (db *DB) DeleteUser(id int64) error {
	_, err := db.conn.Exec(`DELETE FROM users WHERE id = ?`, id)
	return err
}

func (db *DB) UpdateUserLogin(id int64) error {
	_, err := db.conn.Exec(`UPDATE users SET last_login_at = ? WHERE id = ?`, time.Now(), id)
	return err
}

func (db *DB) UserCount() (int, error) {
	var count int
	err := db.conn.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&count)
	return count, err
}

// --- Targets ---

func (db *DB) CreateTarget(t *models.Target) error {
	res, err := db.conn.Exec(
		`INSERT INTO targets (name, type, hostname, port, username, password_encrypted, validate_certs, is_default, storage_pool, network_bridge, datacenter, datastore, network)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		t.Name, t.Type, t.Hostname, t.Port, t.Username, t.PasswordEncrypt, t.ValidateCerts, t.IsDefault,
		t.StoragePool, t.NetworkBridge, t.Datacenter, t.Datastore, t.Network,
	)
	if err != nil {
		return err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return fmt.Errorf("get last insert id: %w", err)
	}
	t.ID = id
	return nil
}

func (db *DB) GetTarget(id int64) (*models.Target, error) {
	t := &models.Target{}
	err := db.conn.QueryRow(
		`SELECT id, name, type, hostname, port, username, password_encrypted, validate_certs, is_default, status, last_connected_at, created_at, updated_at,
		        COALESCE(storage_pool, ''), COALESCE(network_bridge, ''), COALESCE(datacenter, ''), COALESCE(datastore, ''), COALESCE(network, '')
		 FROM targets WHERE id = ?`, id,
	).Scan(&t.ID, &t.Name, &t.Type, &t.Hostname, &t.Port, &t.Username, &t.PasswordEncrypt, &t.ValidateCerts, &t.IsDefault, &t.Status, &t.LastConnectedAt, &t.CreatedAt, &t.UpdatedAt,
		&t.StoragePool, &t.NetworkBridge, &t.Datacenter, &t.Datastore, &t.Network)
	if err != nil {
		return nil, err
	}
	return t, nil
}

func (db *DB) ListTargets() ([]models.Target, error) {
	// V3-M13: Exclude password_encrypted from list query
	rows, err := db.conn.Query(
		`SELECT id, name, type, hostname, port, username, validate_certs, is_default, status, last_connected_at, created_at, updated_at,
		        COALESCE(storage_pool, ''), COALESCE(network_bridge, ''), COALESCE(datacenter, ''), COALESCE(datastore, ''), COALESCE(network, '')
		 FROM targets ORDER BY name`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	targets := []models.Target{}
	for rows.Next() {
		var t models.Target
		if err := rows.Scan(&t.ID, &t.Name, &t.Type, &t.Hostname, &t.Port, &t.Username, &t.ValidateCerts, &t.IsDefault, &t.Status, &t.LastConnectedAt, &t.CreatedAt, &t.UpdatedAt,
			&t.StoragePool, &t.NetworkBridge, &t.Datacenter, &t.Datastore, &t.Network); err != nil {
			return nil, err
		}
		targets = append(targets, t)
	}
	return targets, rows.Err()
}

func (db *DB) UpdateTarget(t *models.Target) error {
	res, err := db.conn.Exec(
		`UPDATE targets SET name=?, type=?, hostname=?, port=?, username=?, password_encrypted=?, validate_certs=?, is_default=?, updated_at=?,
		        storage_pool=?, network_bridge=?, datacenter=?, datastore=?, network=?
		 WHERE id=?`,
		t.Name, t.Type, t.Hostname, t.Port, t.Username, t.PasswordEncrypt, t.ValidateCerts, t.IsDefault, time.Now(),
		t.StoragePool, t.NetworkBridge, t.Datacenter, t.Datastore, t.Network, t.ID,
	)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("target %d not found", t.ID)
	}
	return nil
}

type DeleteTargetPreviewResult struct {
	Templates  int `json:"templates"`
	VMs        int `json:"vms"`
	Deployments int `json:"deployments"`
	Builds     int `json:"builds"`
	Executions int `json:"executions"`
}

func (db *DB) DeleteTargetPreview(id int64) (*DeleteTargetPreviewResult, error) {
	r := &DeleteTargetPreviewResult{}
	if err := db.conn.QueryRow(`SELECT COUNT(*) FROM templates WHERE target_id = ?`, id).Scan(&r.Templates); err != nil {
		return nil, fmt.Errorf("count templates: %w", err)
	}
	if err := db.conn.QueryRow(`SELECT COUNT(*) FROM managed_vms WHERE target_id = ?`, id).Scan(&r.VMs); err != nil {
		return nil, fmt.Errorf("count vms: %w", err)
	}
	if err := db.conn.QueryRow(`SELECT COUNT(*) FROM deployments WHERE target_id = ?`, id).Scan(&r.Deployments); err != nil {
		return nil, fmt.Errorf("count deployments: %w", err)
	}
	if err := db.conn.QueryRow(`SELECT COUNT(*) FROM template_builds WHERE target_id = ?`, id).Scan(&r.Builds); err != nil {
		return nil, fmt.Errorf("count builds: %w", err)
	}
	if err := db.conn.QueryRow(`SELECT COUNT(*) FROM action_executions WHERE vm_id IN (SELECT id FROM managed_vms WHERE target_id = ?)`, id).Scan(&r.Executions); err != nil {
		return nil, fmt.Errorf("count executions: %w", err)
	}
	return r, nil
}

func (db *DB) DeleteTarget(id int64) error {
	tx, err := db.conn.Begin()
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Delete child rows that reference this target (order matters for FK constraints)
	// 1. Action executions for VMs on this target
	if _, err := tx.Exec(`DELETE FROM action_executions WHERE vm_id IN (SELECT id FROM managed_vms WHERE target_id = ?)`, id); err != nil {
		return fmt.Errorf("delete action executions: %w", err)
	}
	// 2. Managed VMs
	if _, err := tx.Exec(`DELETE FROM managed_vms WHERE target_id = ?`, id); err != nil {
		return fmt.Errorf("delete managed vms: %w", err)
	}
	// 3. Deployment-action links
	if _, err := tx.Exec(`DELETE FROM deployment_actions WHERE deployment_id IN (SELECT id FROM deployments WHERE target_id = ?)`, id); err != nil {
		return fmt.Errorf("delete deployment actions: %w", err)
	}
	// 4. Deployments
	if _, err := tx.Exec(`DELETE FROM deployments WHERE target_id = ?`, id); err != nil {
		return fmt.Errorf("delete deployments: %w", err)
	}
	// 5. Template schedules
	if _, err := tx.Exec(`DELETE FROM template_schedules WHERE template_id IN (SELECT id FROM templates WHERE target_id = ?)`, id); err != nil {
		return fmt.Errorf("delete template schedules: %w", err)
	}
	// 6. Templates
	if _, err := tx.Exec(`DELETE FROM templates WHERE target_id = ?`, id); err != nil {
		return fmt.Errorf("delete templates: %w", err)
	}
	// 8. Factory builds
	if _, err := tx.Exec(`DELETE FROM template_builds WHERE target_id = ?`, id); err != nil {
		return fmt.Errorf("delete template builds: %w", err)
	}
	// 9. Target itself
	if _, err := tx.Exec(`DELETE FROM targets WHERE id = ?`, id); err != nil {
		return fmt.Errorf("delete target: %w", err)
	}

	return tx.Commit()
}

func (db *DB) UpdateTargetStatus(id int64, status string) error {
	_, err := db.conn.Exec(`UPDATE targets SET status=?, last_connected_at=?, updated_at=? WHERE id=?`, status, time.Now(), time.Now(), id)
	return err
}

// --- Templates ---

func (db *DB) CreateTemplate(t *models.Template) error {
	res, err := db.conn.Exec(
		`INSERT INTO templates (target_id, name, moref, os_type, os_name, guest_id, cpu, memory_mb, disk_gb, notes, icon, last_synced_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		t.TargetID, t.Name, t.Moref, t.OSType, t.OSName, t.GuestID, t.CPU, t.MemoryMB, t.DiskGB, t.Notes, t.Icon, t.LastSyncedAt,
	)
	if err != nil {
		return err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return fmt.Errorf("get last insert id: %w", err)
	}
	t.ID = id
	return nil
}

func (db *DB) GetTemplate(id int64) (*models.Template, error) {
	t := &models.Template{}
	err := db.conn.QueryRow(
		`SELECT t.id, t.target_id, t.name, t.moref, t.os_type, COALESCE(t.os_name, ''), COALESCE(t.guest_id, ''), t.cpu, t.memory_mb, t.disk_gb, COALESCE(t.notes, ''), t.icon, t.last_synced_at, t.created_at, tg.name, tg.type,
		        t.build_id, t.managed_by_forgemill, COALESCE(t.version, 0), COALESCE(t.iso_checksum, ''), t.built_at, COALESCE(t.lifecycle_status, ''), t.superseded_by, t.retain_until, COALESCE(t.platform, 'linux'), t.family_id
		 FROM templates t JOIN targets tg ON t.target_id = tg.id WHERE t.id = ?`, id,
	).Scan(&t.ID, &t.TargetID, &t.Name, &t.Moref, &t.OSType, &t.OSName, &t.GuestID, &t.CPU, &t.MemoryMB, &t.DiskGB, &t.Notes, &t.Icon, &t.LastSyncedAt, &t.CreatedAt, &t.TargetName, &t.TargetType,
		&t.BuildID, &t.ManagedByForgemill, &t.Version, &t.ISOChecksum, &t.BuiltAt, &t.LifecycleStatus, &t.SupersededBy, &t.RetainUntil, &t.Platform, &t.FamilyID)
	if err != nil {
		return nil, err
	}
	return t, nil
}

func (db *DB) ListTemplates() ([]models.Template, error) {
	rows, err := db.conn.Query(
		`SELECT t.id, t.target_id, t.name, t.moref, t.os_type, COALESCE(t.os_name, ''), COALESCE(t.guest_id, ''), t.cpu, t.memory_mb, t.disk_gb, COALESCE(t.notes, ''), t.icon, t.last_synced_at, t.created_at, tg.name, tg.type,
		        t.build_id, t.managed_by_forgemill, COALESCE(t.version, 0), COALESCE(t.iso_checksum, ''), t.built_at, COALESCE(t.lifecycle_status, ''), t.superseded_by, t.retain_until, COALESCE(t.platform, 'linux'), t.family_id
		 FROM templates t JOIN targets tg ON t.target_id = tg.id ORDER BY t.name`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	templates := []models.Template{}
	for rows.Next() {
		var t models.Template
		if err := rows.Scan(&t.ID, &t.TargetID, &t.Name, &t.Moref, &t.OSType, &t.OSName, &t.GuestID, &t.CPU, &t.MemoryMB, &t.DiskGB, &t.Notes, &t.Icon, &t.LastSyncedAt, &t.CreatedAt, &t.TargetName, &t.TargetType,
			&t.BuildID, &t.ManagedByForgemill, &t.Version, &t.ISOChecksum, &t.BuiltAt, &t.LifecycleStatus, &t.SupersededBy, &t.RetainUntil, &t.Platform, &t.FamilyID); err != nil {
			return nil, err
		}
		templates = append(templates, t)
	}
	return templates, rows.Err()
}

func (db *DB) ListTemplatesByTarget(targetID int64) ([]models.Template, error) {
	rows, err := db.conn.Query(
		`SELECT t.id, t.target_id, t.name, t.moref, t.os_type, COALESCE(t.os_name, ''), COALESCE(t.guest_id, ''), t.cpu, t.memory_mb, t.disk_gb, COALESCE(t.notes, ''), t.icon, t.last_synced_at, t.created_at, tg.name, tg.type,
		        t.build_id, t.managed_by_forgemill, COALESCE(t.version, 0), COALESCE(t.iso_checksum, ''), t.built_at, COALESCE(t.lifecycle_status, ''), t.superseded_by, t.retain_until, COALESCE(t.platform, 'linux'), t.family_id
		 FROM templates t JOIN targets tg ON t.target_id = tg.id WHERE t.target_id = ? ORDER BY t.name`, targetID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	templates := []models.Template{}
	for rows.Next() {
		var t models.Template
		if err := rows.Scan(&t.ID, &t.TargetID, &t.Name, &t.Moref, &t.OSType, &t.OSName, &t.GuestID, &t.CPU, &t.MemoryMB, &t.DiskGB, &t.Notes, &t.Icon, &t.LastSyncedAt, &t.CreatedAt, &t.TargetName, &t.TargetType,
			&t.BuildID, &t.ManagedByForgemill, &t.Version, &t.ISOChecksum, &t.BuiltAt, &t.LifecycleStatus, &t.SupersededBy, &t.RetainUntil, &t.Platform, &t.FamilyID); err != nil {
			return nil, err
		}
		templates = append(templates, t)
	}
	return templates, rows.Err()
}

func (db *DB) DeleteTemplatesByTarget(targetID int64) error {
	_, err := db.conn.Exec(`DELETE FROM templates WHERE target_id = ?`, targetID)
	return err
}

// TemplateDeletePreview holds counts of dependent resources for a template.
type TemplateDeletePreview struct {
	Deployments int `json:"deployments"`
	VMs         int `json:"vms"`
	Builds      int `json:"builds"`
}

func (db *DB) GetTemplateDeletePreview(id int64) (*TemplateDeletePreview, error) {
	var r TemplateDeletePreview
	if err := db.conn.QueryRow(`SELECT COUNT(*) FROM deployments WHERE template_id = ?`, id).Scan(&r.Deployments); err != nil {
		return nil, fmt.Errorf("count deployments: %w", err)
	}
	if err := db.conn.QueryRow(`SELECT COUNT(*) FROM managed_vms WHERE deployment_id IN (SELECT id FROM deployments WHERE template_id = ?)`, id).Scan(&r.VMs); err != nil {
		return nil, fmt.Errorf("count vms: %w", err)
	}
	if err := db.conn.QueryRow(`SELECT COUNT(*) FROM template_builds WHERE template_id = ?`, id).Scan(&r.Builds); err != nil {
		return nil, fmt.Errorf("count builds: %w", err)
	}
	return &r, nil
}

func (db *DB) DeleteTemplate(id int64, keepVMs bool) error {
	tx, err := db.conn.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Snapshot template name into deployments before unlinking, so it survives deletion
	if _, err := tx.Exec(`
		UPDATE deployments SET template_name = COALESCE(
			(SELECT name FROM templates WHERE id = ?),
			template_name
		) WHERE template_id = ?`, id, id); err != nil {
		return fmt.Errorf("snapshot template name: %w", err)
	}

	if !keepVMs {
		// Delete action executions for VMs deployed from this template
		if _, err := tx.Exec(`DELETE FROM action_executions WHERE vm_id IN (SELECT id FROM managed_vms WHERE deployment_id IN (SELECT id FROM deployments WHERE template_id = ?))`, id); err != nil {
			return fmt.Errorf("delete action executions: %w", err)
		}
		// Delete managed VMs from deployments of this template
		if _, err := tx.Exec(`DELETE FROM managed_vms WHERE deployment_id IN (SELECT id FROM deployments WHERE template_id = ?)`, id); err != nil {
			return fmt.Errorf("delete managed vms: %w", err)
		}
	}
	// Delete deployment actions before unlinking (while template_id is still set)
	if _, err := tx.Exec(`DELETE FROM deployment_actions WHERE deployment_id IN (SELECT id FROM deployments WHERE template_id = ?)`, id); err != nil {
		return fmt.Errorf("delete deployment actions: %w", err)
	}
	// Soft-unlink: null out template_id on deployments instead of deleting them.
	// Managed VMs and deployment history are preserved; template_name snapshot ensures
	// the UI can still show which template was used.
	if _, err := tx.Exec(`UPDATE deployments SET template_id = NULL WHERE template_id = ?`, id); err != nil {
		return fmt.Errorf("unlink deployments: %w", err)
	}
	// Delete template schedules
	if _, err := tx.Exec(`DELETE FROM template_schedules WHERE template_id = ?`, id); err != nil {
		return fmt.Errorf("delete template schedules: %w", err)
	}
	// Delete template builds
	if _, err := tx.Exec(`DELETE FROM template_builds WHERE template_id = ?`, id); err != nil {
		return fmt.Errorf("delete template builds: %w", err)
	}
	// Clear superseded_by references pointing to this template
	if _, err := tx.Exec(`UPDATE templates SET superseded_by = NULL WHERE superseded_by = ?`, id); err != nil {
		return fmt.Errorf("clear superseded refs: %w", err)
	}
	// Delete the template
	if _, err := tx.Exec(`DELETE FROM templates WHERE id = ?`, id); err != nil {
		return fmt.Errorf("delete template: %w", err)
	}

	return tx.Commit()
}

func (db *DB) CountDeploymentsByTarget(targetID int64) (int, error) {
	var count int
	err := db.conn.QueryRow(`SELECT COUNT(*) FROM deployments WHERE target_id = ?`, targetID).Scan(&count)
	return count, err
}

func (db *DB) CountManagedVMsByTarget(targetID int64) (int, error) {
	var count int
	err := db.conn.QueryRow(`SELECT COUNT(*) FROM managed_vms WHERE target_id = ?`, targetID).Scan(&count)
	return count, err
}

// --- Deployments ---

func (db *DB) CreateDeployment(d *models.Deployment) error {
	res, err := db.conn.Exec(
		`INSERT INTO deployments (template_id, target_id, vm_name, status, config_json, created_by, initial_username, initial_password_enc, template_name)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, COALESCE((SELECT name FROM templates WHERE id = ?), ''))`,
		d.TemplateID, d.TargetID, d.VMName, d.Status, d.ConfigJSON, d.CreatedBy, d.InitialUsername, d.InitialPwdEnc, d.TemplateID,
	)
	if err != nil {
		return err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return fmt.Errorf("get last insert id: %w", err)
	}
	d.ID = id
	d.CreatedAt = time.Now()
	return nil
}

func (db *DB) GetDeployment(id int64) (*models.Deployment, error) {
	d := &models.Deployment{}
	err := db.conn.QueryRow(
		`SELECT d.id, d.template_id, d.target_id, d.vm_name, d.status, d.config_json, d.started_at, d.completed_at, COALESCE(d.error_message, ''), d.created_by, d.created_at,
		        COALESCE(d.template_name, t.name, ''), COALESCE(tg.name, ''), COALESCE(d.initial_username, ''), COALESCE(d.initial_password_enc, ''),
		        (SELECT mv.id FROM managed_vms mv WHERE mv.deployment_id = d.id LIMIT 1)
		 FROM deployments d
		 LEFT JOIN templates t ON d.template_id = t.id
		 LEFT JOIN targets tg ON d.target_id = tg.id
		 WHERE d.id = ?`, id,
	).Scan(&d.ID, &d.TemplateID, &d.TargetID, &d.VMName, &d.Status, &d.ConfigJSON, &d.StartedAt, &d.CompletedAt, &d.ErrorMessage, &d.CreatedBy, &d.CreatedAt, &d.TemplateName, &d.TargetName, &d.InitialUsername, &d.InitialPwdEnc, &d.VMID)
	if err != nil {
		return nil, err
	}
	return d, nil
}

func (db *DB) UpdateDeploymentStatus(id int64, status string, errMsg string) error {
	now := time.Now()
	// V3-M15: Add state transition guards to prevent invalid status changes
	var validFrom string
	switch status {
	case "running":
		validFrom = "'pending'"
		res, err := db.conn.Exec(`UPDATE deployments SET status=?, started_at=? WHERE id=? AND status IN (`+validFrom+`)`, status, now, id)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return fmt.Errorf("invalid state transition to %s", status)
		}
		return nil
	case "completed":
		validFrom = "'running'"
		res, err := db.conn.Exec(`UPDATE deployments SET status=?, completed_at=?, error_message=? WHERE id=? AND status IN (`+validFrom+`)`, status, now, errMsg, id)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return fmt.Errorf("invalid state transition to %s", status)
		}
		return nil
	case "failed":
		validFrom = "'pending','running'"
		res, err := db.conn.Exec(`UPDATE deployments SET status=?, completed_at=?, error_message=? WHERE id=? AND status IN (`+validFrom+`)`, status, now, errMsg, id)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return fmt.Errorf("invalid state transition to %s", status)
		}
		return nil
	case "cancelled":
		validFrom = "'pending','running'"
		res, err := db.conn.Exec(`UPDATE deployments SET status=?, completed_at=?, error_message=? WHERE id=? AND status IN (`+validFrom+`)`, status, now, errMsg, id)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return fmt.Errorf("invalid state transition to %s", status)
		}
		return nil
	default:
		return fmt.Errorf("unknown deployment status: %s", status)
	}
}

type DeploymentFilter struct {
	Status   string
	TargetID int64
	Search   string
	Page     int
	PerPage  int
}

type PaginatedDeployments struct {
	Data    []models.Deployment `json:"data"`
	Total   int                 `json:"total"`
	Page    int                 `json:"page"`
	PerPage int                 `json:"per_page"`
}

func (db *DB) ListDeployments(f DeploymentFilter) (*PaginatedDeployments, error) {
	if f.Page < 1 {
		f.Page = 1
	}
	if f.PerPage < 1 {
		f.PerPage = 20
	}
	// V5-M1: Cap PerPage to prevent memory exhaustion via large page sizes
	if f.PerPage > 100 {
		f.PerPage = 100
	}

	where := "1=1"
	args := []any{}
	if f.Status != "" {
		where += " AND d.status = ?"
		args = append(args, f.Status)
	}
	if f.TargetID > 0 {
		where += " AND d.target_id = ?"
		args = append(args, f.TargetID)
	}
	if f.Search != "" {
		where += " AND (d.vm_name LIKE ? OR COALESCE(d.template_name, '') LIKE ? OR EXISTS (SELECT 1 FROM templates t2 WHERE t2.id = d.template_id AND t2.name LIKE ?) OR EXISTS (SELECT 1 FROM targets tg2 WHERE tg2.id = d.target_id AND tg2.name LIKE ?))"
		searchPattern := "%" + escapeLike(f.Search) + "%"
		args = append(args, searchPattern, searchPattern, searchPattern, searchPattern)
	}

	var total int
	countQuery := fmt.Sprintf(`SELECT COUNT(*) FROM deployments d WHERE %s`, where)
	if err := db.conn.QueryRow(countQuery, args...).Scan(&total); err != nil {
		return nil, err
	}

	offset := (f.Page - 1) * f.PerPage
	query := fmt.Sprintf(
		`SELECT d.id, d.template_id, d.target_id, d.vm_name, d.status, d.config_json, d.started_at, d.completed_at, COALESCE(d.error_message, ''), d.created_by, d.created_at,
		        COALESCE(d.template_name, t.name, ''), COALESCE(tg.name, '')
		 FROM deployments d
		 LEFT JOIN templates t ON d.template_id = t.id
		 LEFT JOIN targets tg ON d.target_id = tg.id
		 WHERE %s ORDER BY d.created_at DESC LIMIT ? OFFSET ?`, where)
	args = append(args, f.PerPage, offset)

	rows, err := db.conn.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	deployments := []models.Deployment{}
	for rows.Next() {
		var d models.Deployment
		if err := rows.Scan(&d.ID, &d.TemplateID, &d.TargetID, &d.VMName, &d.Status, &d.ConfigJSON, &d.StartedAt, &d.CompletedAt, &d.ErrorMessage, &d.CreatedBy, &d.CreatedAt, &d.TemplateName, &d.TargetName); err != nil {
			return nil, err
		}
		deployments = append(deployments, d)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return &PaginatedDeployments{Data: deployments, Total: total, Page: f.Page, PerPage: f.PerPage}, nil
}

func (db *DB) GetRecentDeployments(limit int) ([]models.Deployment, error) {
	rows, err := db.conn.Query(
		`SELECT d.id, d.template_id, d.target_id, d.vm_name, d.status, d.config_json, d.started_at, d.completed_at, COALESCE(d.error_message, ''), d.created_by, d.created_at,
		        COALESCE(d.template_name, t.name, ''), COALESCE(tg.name, '')
		 FROM deployments d
		 LEFT JOIN templates t ON d.template_id = t.id
		 LEFT JOIN targets tg ON d.target_id = tg.id
		 ORDER BY d.created_at DESC LIMIT ?`, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	deployments := []models.Deployment{}
	for rows.Next() {
		var d models.Deployment
		if err := rows.Scan(&d.ID, &d.TemplateID, &d.TargetID, &d.VMName, &d.Status, &d.ConfigJSON, &d.StartedAt, &d.CompletedAt, &d.ErrorMessage, &d.CreatedBy, &d.CreatedAt, &d.TemplateName, &d.TargetName); err != nil {
			return nil, err
		}
		deployments = append(deployments, d)
	}
	return deployments, rows.Err()
}

// ClearDeploymentHistory deletes all completed/failed deployment records and their logs.
// Active deployments (status = 'running' or 'pending') are kept.
func (db *DB) ClearDeploymentHistory() (int64, error) {
	tx, err := db.conn.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	// Delete deployment logs for completed/failed deployments
	if _, err := tx.Exec(`DELETE FROM deployment_logs WHERE deployment_id IN (SELECT id FROM deployments WHERE status NOT IN ('running', 'pending'))`); err != nil {
		return 0, fmt.Errorf("delete deployment logs: %w", err)
	}
	// Delete deployment actions
	if _, err := tx.Exec(`DELETE FROM deployment_actions WHERE deployment_id IN (SELECT id FROM deployments WHERE status NOT IN ('running', 'pending'))`); err != nil {
		return 0, fmt.Errorf("delete deployment actions: %w", err)
	}
	// Delete the deployments
	res, err := tx.Exec(`DELETE FROM deployments WHERE status NOT IN ('running', 'pending')`)
	if err != nil {
		return 0, fmt.Errorf("delete deployments: %w", err)
	}
	count, _ := res.RowsAffected()
	return count, tx.Commit()
}

// --- Deployment Logs ---

func (db *DB) AddDeploymentLog(log *models.DeploymentLog) error {
	res, err := db.conn.Exec(
		`INSERT INTO deployment_logs (deployment_id, level, message) VALUES (?, ?, ?)`,
		log.DeploymentID, log.Level, log.Message,
	)
	if err != nil {
		return err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return fmt.Errorf("get last insert id: %w", err)
	}
	log.ID = id
	return nil
}

func (db *DB) GetDeploymentLogs(deploymentID int64) ([]models.DeploymentLog, error) {
	rows, err := db.conn.Query(
		`SELECT id, deployment_id, timestamp, level, message FROM deployment_logs WHERE deployment_id = ? ORDER BY timestamp`, deploymentID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	logs := []models.DeploymentLog{}
	for rows.Next() {
		var l models.DeploymentLog
		if err := rows.Scan(&l.ID, &l.DeploymentID, &l.Timestamp, &l.Level, &l.Message); err != nil {
			return nil, err
		}
		logs = append(logs, l)
	}
	return logs, rows.Err()
}

// --- Template Sources ---

func (db *DB) CreateTemplateSource(ts *models.TemplateSource) error {
	res, err := db.conn.Exec(
		`INSERT INTO template_sources (name, os_type, iso_url, checksum_url, packer_config, auto_refresh, refresh_interval_days, target_id)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		ts.Name, ts.OSType, ts.ISOURL, ts.ChecksumURL, ts.PackerConfig, ts.AutoRefresh, ts.RefreshIntervalDays, ts.TargetID,
	)
	if err != nil {
		return err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return fmt.Errorf("get last insert id: %w", err)
	}
	ts.ID = id
	return nil
}

func (db *DB) GetTemplateSource(id int64) (*models.TemplateSource, error) {
	ts := &models.TemplateSource{}
	err := db.conn.QueryRow(
		`SELECT ts.id, ts.name, ts.os_type, ts.iso_url, ts.checksum_url, ts.packer_config, ts.auto_refresh, ts.refresh_interval_days, ts.last_built_at, ts.target_id, ts.created_at, COALESCE(tg.name, '')
		 FROM template_sources ts LEFT JOIN targets tg ON ts.target_id = tg.id WHERE ts.id = ?`, id,
	).Scan(&ts.ID, &ts.Name, &ts.OSType, &ts.ISOURL, &ts.ChecksumURL, &ts.PackerConfig, &ts.AutoRefresh, &ts.RefreshIntervalDays, &ts.LastBuiltAt, &ts.TargetID, &ts.CreatedAt, &ts.TargetName)
	if err != nil {
		return nil, err
	}
	return ts, nil
}

func (db *DB) ListTemplateSources() ([]models.TemplateSource, error) {
	rows, err := db.conn.Query(
		`SELECT ts.id, ts.name, ts.os_type, ts.iso_url, ts.checksum_url, ts.packer_config, ts.auto_refresh, ts.refresh_interval_days, ts.last_built_at, ts.target_id, ts.created_at, COALESCE(tg.name, '')
		 FROM template_sources ts LEFT JOIN targets tg ON ts.target_id = tg.id ORDER BY ts.name`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	sources := []models.TemplateSource{}
	for rows.Next() {
		var ts models.TemplateSource
		if err := rows.Scan(&ts.ID, &ts.Name, &ts.OSType, &ts.ISOURL, &ts.ChecksumURL, &ts.PackerConfig, &ts.AutoRefresh, &ts.RefreshIntervalDays, &ts.LastBuiltAt, &ts.TargetID, &ts.CreatedAt, &ts.TargetName); err != nil {
			return nil, err
		}
		sources = append(sources, ts)
	}
	return sources, rows.Err()
}

func (db *DB) UpdateTemplateSource(ts *models.TemplateSource) error {
	_, err := db.conn.Exec(
		`UPDATE template_sources SET name=?, os_type=?, iso_url=?, checksum_url=?, packer_config=?, auto_refresh=?, refresh_interval_days=?, target_id=? WHERE id=?`,
		ts.Name, ts.OSType, ts.ISOURL, ts.ChecksumURL, ts.PackerConfig, ts.AutoRefresh, ts.RefreshIntervalDays, ts.TargetID, ts.ID,
	)
	return err
}

func (db *DB) DeleteTemplateSource(id int64) error {
	_, err := db.conn.Exec(`DELETE FROM template_sources WHERE id = ?`, id)
	return err
}

// --- API Keys ---

func (db *DB) CreateAPIKey(k *models.APIKey) error {
	res, err := db.conn.Exec(
		`INSERT INTO api_keys (user_id, name, key_hash, prefix, expires_at) VALUES (?, ?, ?, ?, ?)`,
		k.UserID, k.Name, k.KeyHash, k.Prefix, k.ExpiresAt,
	)
	if err != nil {
		return err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return fmt.Errorf("get last insert id: %w", err)
	}
	k.ID = id
	return nil
}

func (db *DB) ListAPIKeys(userID int64) ([]models.APIKey, error) {
	rows, err := db.conn.Query(
		`SELECT k.id, k.user_id, k.name, k.prefix, k.last_used_at, k.expires_at, k.created_at, u.username
		 FROM api_keys k JOIN users u ON k.user_id = u.id WHERE k.user_id = ? ORDER BY k.created_at DESC`, userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	keys := []models.APIKey{}
	for rows.Next() {
		var k models.APIKey
		if err := rows.Scan(&k.ID, &k.UserID, &k.Name, &k.Prefix, &k.LastUsedAt, &k.ExpiresAt, &k.CreatedAt, &k.Username); err != nil {
			return nil, err
		}
		keys = append(keys, k)
	}
	return keys, rows.Err()
}

func (db *DB) ListAllAPIKeys() ([]models.APIKey, error) {
	rows, err := db.conn.Query(
		`SELECT k.id, k.user_id, k.name, k.prefix, k.last_used_at, k.expires_at, k.created_at, u.username
		 FROM api_keys k JOIN users u ON k.user_id = u.id ORDER BY k.created_at DESC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	keys := []models.APIKey{}
	for rows.Next() {
		var k models.APIKey
		if err := rows.Scan(&k.ID, &k.UserID, &k.Name, &k.Prefix, &k.LastUsedAt, &k.ExpiresAt, &k.CreatedAt, &k.Username); err != nil {
			return nil, err
		}
		keys = append(keys, k)
	}
	return keys, rows.Err()
}

func (db *DB) GetAPIKeyByPrefix(prefix string) (*models.APIKey, error) {
	k := &models.APIKey{}
	err := db.conn.QueryRow(
		`SELECT k.id, k.user_id, k.name, k.key_hash, k.prefix, k.last_used_at, k.expires_at, k.created_at, u.username
		 FROM api_keys k JOIN users u ON k.user_id = u.id WHERE k.prefix = ?`, prefix,
	).Scan(&k.ID, &k.UserID, &k.Name, &k.KeyHash, &k.Prefix, &k.LastUsedAt, &k.ExpiresAt, &k.CreatedAt, &k.Username)
	if err != nil {
		return nil, err
	}
	return k, nil
}

func (db *DB) GetAllAPIKeysByPrefix(prefix string) ([]models.APIKey, error) {
	rows, err := db.conn.Query(
		`SELECT k.id, k.user_id, k.name, k.key_hash, k.prefix, k.last_used_at, k.expires_at, k.created_at
		 FROM api_keys k WHERE k.prefix = ?`, prefix,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	keys := []models.APIKey{}
	for rows.Next() {
		var k models.APIKey
		if err := rows.Scan(&k.ID, &k.UserID, &k.Name, &k.KeyHash, &k.Prefix, &k.LastUsedAt, &k.ExpiresAt, &k.CreatedAt); err != nil {
			return nil, err
		}
		keys = append(keys, k)
	}
	return keys, rows.Err()
}

func (db *DB) UpdateAPIKeyLastUsed(id int64) error {
	_, err := db.conn.Exec(`UPDATE api_keys SET last_used_at = ? WHERE id = ?`, time.Now(), id)
	return err
}

func (db *DB) GetAPIKeyByID(id int64) (*models.APIKey, error) {
	k := &models.APIKey{}
	err := db.conn.QueryRow(
		`SELECT id, user_id, name, prefix, last_used_at, expires_at, created_at FROM api_keys WHERE id = ?`, id,
	).Scan(&k.ID, &k.UserID, &k.Name, &k.Prefix, &k.LastUsedAt, &k.ExpiresAt, &k.CreatedAt)
	if err != nil {
		return nil, err
	}
	return k, nil
}

func (db *DB) CountAPIKeysByUser(userID int64) (int, error) {
	var count int
	err := db.conn.QueryRow(`SELECT COUNT(*) FROM api_keys WHERE user_id = ?`, userID).Scan(&count)
	return count, err
}

func (db *DB) DeleteAPIKey(id int64) error {
	_, err := db.conn.Exec(`DELETE FROM api_keys WHERE id = ?`, id)
	return err
}

// --- Webhooks ---

func (db *DB) CreateWebhook(wh *models.Webhook) error {
	res, err := db.conn.Exec(
		`INSERT INTO webhooks (name, url, events, secret, is_active) VALUES (?, ?, ?, ?, ?)`,
		wh.Name, wh.URL, wh.Events, wh.Secret, wh.IsActive,
	)
	if err != nil {
		return err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return fmt.Errorf("get last insert id: %w", err)
	}
	wh.ID = id
	return nil
}

func (db *DB) GetWebhook(id int64) (*models.Webhook, error) {
	wh := &models.Webhook{}
	err := db.conn.QueryRow(
		`SELECT id, name, url, events, secret, is_active, created_at FROM webhooks WHERE id = ?`, id,
	).Scan(&wh.ID, &wh.Name, &wh.URL, &wh.Events, &wh.Secret, &wh.IsActive, &wh.CreatedAt)
	if err != nil {
		return nil, err
	}
	return wh, nil
}

func (db *DB) ListWebhooks() ([]models.Webhook, error) {
	// V3-M13: Exclude secret from list query
	rows, err := db.conn.Query(`SELECT id, name, url, events, is_active, created_at FROM webhooks ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	webhooks := []models.Webhook{}
	for rows.Next() {
		var wh models.Webhook
		if err := rows.Scan(&wh.ID, &wh.Name, &wh.URL, &wh.Events, &wh.IsActive, &wh.CreatedAt); err != nil {
			return nil, err
		}
		webhooks = append(webhooks, wh)
	}
	return webhooks, rows.Err()
}

func (db *DB) ListActiveWebhooks() ([]models.Webhook, error) {
	rows, err := db.conn.Query(`SELECT id, name, url, events, secret, is_active, created_at FROM webhooks WHERE is_active = TRUE ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	webhooks := []models.Webhook{}
	for rows.Next() {
		var wh models.Webhook
		if err := rows.Scan(&wh.ID, &wh.Name, &wh.URL, &wh.Events, &wh.Secret, &wh.IsActive, &wh.CreatedAt); err != nil {
			return nil, err
		}
		webhooks = append(webhooks, wh)
	}
	return webhooks, rows.Err()
}

func (db *DB) UpdateWebhook(wh *models.Webhook) error {
	_, err := db.conn.Exec(
		`UPDATE webhooks SET name=?, url=?, events=?, secret=?, is_active=? WHERE id=?`,
		wh.Name, wh.URL, wh.Events, wh.Secret, wh.IsActive, wh.ID,
	)
	return err
}

func (db *DB) DeleteWebhook(id int64) error {
	_, err := db.conn.Exec(`DELETE FROM webhooks WHERE id = ?`, id)
	return err
}

// --- Managed VMs ---

func (db *DB) CreateManagedVM(vm *models.ManagedVM) error {
	res, err := db.conn.Exec(
		`INSERT INTO managed_vms (deployment_id, target_id, vm_name, vm_ref, power_state, ip_address, cpu, memory_mb, disk_gb, os_type, last_synced_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		vm.DeploymentID, vm.TargetID, vm.VMName, vm.VMRef, vm.PowerState, vm.IPAddress, vm.CPU, vm.MemoryMB, vm.DiskGB, vm.OSType, vm.LastSyncedAt,
	)
	if err != nil {
		if isUniqueConstraintError(err) {
			return fmt.Errorf("VM with ref %q already registered on this target", vm.VMRef)
		}
		return err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return fmt.Errorf("get last insert id: %w", err)
	}
	vm.ID = id
	return nil
}

// UpsertManagedVM inserts a new managed VM or updates the existing record when
// the (vm_ref, target_id) pair already exists (e.g. Proxmox reuses a VMID).
func (db *DB) UpsertManagedVM(vm *models.ManagedVM) error {
	_, err := db.conn.Exec(
		`INSERT INTO managed_vms (deployment_id, target_id, vm_name, vm_ref, power_state, ip_address, cpu, memory_mb, disk_gb, os_type, last_synced_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(vm_ref, target_id) DO UPDATE SET
		   deployment_id = excluded.deployment_id,
		   vm_name = excluded.vm_name,
		   power_state = excluded.power_state,
		   ip_address = excluded.ip_address,
		   cpu = excluded.cpu,
		   memory_mb = excluded.memory_mb,
		   disk_gb = excluded.disk_gb,
		   os_type = excluded.os_type,
		   last_synced_at = excluded.last_synced_at`,
		vm.DeploymentID, vm.TargetID, vm.VMName, vm.VMRef, vm.PowerState, vm.IPAddress, vm.CPU, vm.MemoryMB, vm.DiskGB, vm.OSType, vm.LastSyncedAt,
	)
	if err != nil {
		return err
	}
	// Retrieve the ID whether this was an insert or update
	err = db.conn.QueryRow(`SELECT id FROM managed_vms WHERE vm_ref = ? AND target_id = ?`, vm.VMRef, vm.TargetID).Scan(&vm.ID)
	if err != nil {
		return fmt.Errorf("get upserted VM id: %w", err)
	}
	return nil
}

func (db *DB) GetManagedVM(id int64) (*models.ManagedVM, error) {
	vm := &models.ManagedVM{}
	err := db.conn.QueryRow(
		`SELECT v.id, v.deployment_id, v.target_id, v.vm_name, v.vm_ref, v.power_state, v.ip_address, v.cpu, v.memory_mb, v.disk_gb, v.os_type, COALESCE(v.platform, 'linux'), v.last_synced_at, v.created_at, COALESCE(t.name, ''),
		        COALESCE(d.template_name, tmpl.name, '')
		 FROM managed_vms v
		 LEFT JOIN targets t ON v.target_id = t.id
		 LEFT JOIN deployments d ON v.deployment_id = d.id
		 LEFT JOIN templates tmpl ON d.template_id = tmpl.id
		 WHERE v.id = ?`, id,
	).Scan(&vm.ID, &vm.DeploymentID, &vm.TargetID, &vm.VMName, &vm.VMRef, &vm.PowerState, &vm.IPAddress, &vm.CPU, &vm.MemoryMB, &vm.DiskGB, &vm.OSType, &vm.Platform, &vm.LastSyncedAt, &vm.CreatedAt, &vm.TargetName, &vm.TemplateName)
	if err != nil {
		return nil, err
	}
	return vm, nil
}

func (db *DB) ListManagedVMs() ([]models.ManagedVM, error) {
	rows, err := db.conn.Query(
		`SELECT v.id, v.deployment_id, v.target_id, v.vm_name, v.vm_ref, v.power_state, v.ip_address, v.cpu, v.memory_mb, v.disk_gb, v.os_type, COALESCE(v.platform, 'linux'), v.last_synced_at, v.created_at, COALESCE(t.name, ''),
		        COALESCE(d.template_name, tmpl.name, '')
		 FROM managed_vms v
		 LEFT JOIN targets t ON v.target_id = t.id
		 LEFT JOIN deployments d ON v.deployment_id = d.id
		 LEFT JOIN templates tmpl ON d.template_id = tmpl.id
		 ORDER BY v.vm_name`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	vms := []models.ManagedVM{}
	for rows.Next() {
		var vm models.ManagedVM
		if err := rows.Scan(&vm.ID, &vm.DeploymentID, &vm.TargetID, &vm.VMName, &vm.VMRef, &vm.PowerState, &vm.IPAddress, &vm.CPU, &vm.MemoryMB, &vm.DiskGB, &vm.OSType, &vm.Platform, &vm.LastSyncedAt, &vm.CreatedAt, &vm.TargetName, &vm.TemplateName); err != nil {
			return nil, err
		}
		vms = append(vms, vm)
	}
	return vms, rows.Err()
}

func (db *DB) UpdateManagedVMState(id int64, powerState string, ipAddress string) error {
	now := time.Now()
	_, err := db.conn.Exec(`UPDATE managed_vms SET power_state=?, ip_address=?, last_synced_at=? WHERE id=?`, powerState, ipAddress, now, id)
	return err
}

func (db *DB) DeleteManagedVM(id int64) error {
	_, err := db.conn.Exec(`DELETE FROM managed_vms WHERE id = ?`, id)
	return err
}

func (db *DB) UpdateManagedVMResources(id int64, cpu, memoryMB, diskGB int) error {
	_, err := db.conn.Exec(`UPDATE managed_vms SET cpu=?, memory_mb=?, disk_gb=?, last_synced_at=? WHERE id=?`, cpu, memoryMB, diskGB, time.Now(), id)
	return err
}

func (db *DB) UpdateManagedVMOSType(id int64, osType string) error {
	_, err := db.conn.Exec(`UPDATE managed_vms SET os_type=?, last_synced_at=? WHERE id=?`, osType, time.Now(), id)
	return err
}

func (db *DB) UpdateManagedVMName(id int64, name string) error {
	_, err := db.conn.Exec(`UPDATE managed_vms SET vm_name=?, last_synced_at=? WHERE id=?`, name, time.Now(), id)
	return err
}

// GetManagedVMHostKeyFP retrieves the stored SSH host key fingerprint for TOFU verification.
func (db *DB) GetManagedVMHostKeyFP(id int64) (string, error) {
	var fp string
	err := db.conn.QueryRow(`SELECT COALESCE(host_key_fp, '') FROM managed_vms WHERE id = ?`, id).Scan(&fp)
	return fp, err
}

// UpdateManagedVMHostKeyFP stores an SSH host key fingerprint (TOFU).
func (db *DB) UpdateManagedVMHostKeyFP(id int64, fingerprint string) error {
	_, err := db.conn.Exec(`UPDATE managed_vms SET host_key_fp = ? WHERE id = ?`, fingerprint, id)
	return err
}

// GetTargetSSHHostKeyFP retrieves the stored SSH host key fingerprint for a target (TOFU verification).
func (db *DB) GetTargetSSHHostKeyFP(id int64) (string, error) {
	var fp string
	err := db.conn.QueryRow(`SELECT COALESCE(ssh_host_key_fp, '') FROM targets WHERE id = ?`, id).Scan(&fp)
	return fp, err
}

// UpdateTargetSSHHostKeyFP stores an SSH host key fingerprint for a target (TOFU).
func (db *DB) UpdateTargetSSHHostKeyFP(id int64, fingerprint string) error {
	_, err := db.conn.Exec(`UPDATE targets SET ssh_host_key_fp = ? WHERE id = ?`, fingerprint, id)
	return err
}

func (db *DB) ListManagedVMsByTarget(targetID int64) ([]models.ManagedVM, error) {
	rows, err := db.conn.Query(
		`SELECT v.id, v.deployment_id, v.target_id, v.vm_name, v.vm_ref, v.power_state, v.ip_address, v.cpu, v.memory_mb, v.disk_gb, v.os_type, COALESCE(v.platform, 'linux'), v.last_synced_at, v.created_at, COALESCE(t.name, '')
		 FROM managed_vms v LEFT JOIN targets t ON v.target_id = t.id WHERE v.target_id = ? ORDER BY v.vm_name`, targetID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	vms := []models.ManagedVM{}
	for rows.Next() {
		var vm models.ManagedVM
		if err := rows.Scan(&vm.ID, &vm.DeploymentID, &vm.TargetID, &vm.VMName, &vm.VMRef, &vm.PowerState, &vm.IPAddress, &vm.CPU, &vm.MemoryMB, &vm.DiskGB, &vm.OSType, &vm.Platform, &vm.LastSyncedAt, &vm.CreatedAt, &vm.TargetName); err != nil {
			return nil, err
		}
		vms = append(vms, vm)
	}
	return vms, rows.Err()
}

func isUniqueConstraintError(err error) bool {
	return strings.Contains(err.Error(), "UNIQUE constraint failed")
}

// --- VM Snapshots ---

func (db *DB) CreateVMSnapshot(s *models.VMSnapshot) error {
	res, err := db.conn.Exec(
		`INSERT INTO vm_snapshots (vm_id, snapshot_ref, name, description) VALUES (?, ?, ?, ?)`,
		s.VMID, s.SnapshotRef, s.Name, s.Description,
	)
	if err != nil {
		return err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return fmt.Errorf("get last insert id: %w", err)
	}
	s.ID = id
	return nil
}

func (db *DB) ListVMSnapshots(vmID int64) ([]models.VMSnapshot, error) {
	rows, err := db.conn.Query(
		`SELECT id, vm_id, snapshot_ref, name, description, created_at FROM vm_snapshots WHERE vm_id = ? ORDER BY created_at`, vmID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	snapshots := []models.VMSnapshot{}
	for rows.Next() {
		var s models.VMSnapshot
		if err := rows.Scan(&s.ID, &s.VMID, &s.SnapshotRef, &s.Name, &s.Description, &s.CreatedAt); err != nil {
			return nil, err
		}
		snapshots = append(snapshots, s)
	}
	return snapshots, rows.Err()
}

func (db *DB) GetVMSnapshot(id int64) (*models.VMSnapshot, error) {
	s := &models.VMSnapshot{}
	err := db.conn.QueryRow(
		`SELECT id, vm_id, snapshot_ref, name, description, created_at FROM vm_snapshots WHERE id = ?`, id,
	).Scan(&s.ID, &s.VMID, &s.SnapshotRef, &s.Name, &s.Description, &s.CreatedAt)
	if err != nil {
		return nil, err
	}
	return s, nil
}

func (db *DB) DeleteVMSnapshot(id int64) error {
	_, err := db.conn.Exec(`DELETE FROM vm_snapshots WHERE id = ?`, id)
	return err
}

// --- Blueprints ---

func (db *DB) CreateBlueprint(b *models.Blueprint) error {
	res, err := db.conn.Exec(
		`INSERT INTO blueprints (name, description, template_id, target_id, config_json, created_by)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		b.Name, b.Description, b.TemplateID, b.TargetID, b.ConfigJSON, b.CreatedBy,
	)
	if err != nil {
		return err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return fmt.Errorf("get last insert id: %w", err)
	}
	b.ID = id
	return nil
}

func (db *DB) GetBlueprint(id int64) (*models.Blueprint, error) {
	b := &models.Blueprint{}
	err := db.conn.QueryRow(
		`SELECT b.id, b.name, b.description, b.template_id, b.target_id, b.config_json, b.created_by, b.created_at, b.updated_at,
		        COALESCE(t.name, ''), COALESCE(tg.name, '')
		 FROM blueprints b
		 LEFT JOIN templates t ON b.template_id = t.id
		 LEFT JOIN targets tg ON b.target_id = tg.id
		 WHERE b.id = ?`, id,
	).Scan(&b.ID, &b.Name, &b.Description, &b.TemplateID, &b.TargetID, &b.ConfigJSON, &b.CreatedBy, &b.CreatedAt, &b.UpdatedAt, &b.TemplateName, &b.TargetName)
	if err != nil {
		return nil, err
	}
	return b, nil
}

func (db *DB) ListBlueprints() ([]models.Blueprint, error) {
	rows, err := db.conn.Query(
		`SELECT b.id, b.name, b.description, b.template_id, b.target_id, b.config_json, b.created_by, b.created_at, b.updated_at,
		        COALESCE(t.name, ''), COALESCE(tg.name, '')
		 FROM blueprints b
		 LEFT JOIN templates t ON b.template_id = t.id
		 LEFT JOIN targets tg ON b.target_id = tg.id
		 ORDER BY b.name`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	blueprints := []models.Blueprint{}
	for rows.Next() {
		var b models.Blueprint
		if err := rows.Scan(&b.ID, &b.Name, &b.Description, &b.TemplateID, &b.TargetID, &b.ConfigJSON, &b.CreatedBy, &b.CreatedAt, &b.UpdatedAt, &b.TemplateName, &b.TargetName); err != nil {
			return nil, err
		}
		blueprints = append(blueprints, b)
	}
	return blueprints, rows.Err()
}

func (db *DB) UpdateBlueprint(b *models.Blueprint) error {
	_, err := db.conn.Exec(
		`UPDATE blueprints SET name=?, description=?, template_id=?, target_id=?, config_json=?, updated_at=? WHERE id=?`,
		b.Name, b.Description, b.TemplateID, b.TargetID, b.ConfigJSON, time.Now(), b.ID,
	)
	return err
}

func (db *DB) DeleteBlueprint(id int64) error {
	_, err := db.conn.Exec(`DELETE FROM blueprints WHERE id = ?`, id)
	return err
}

// --- Bulk Deployments ---

func (db *DB) CreateBulkDeployment(b *models.BulkDeployment) error {
	res, err := db.conn.Exec(
		`INSERT INTO bulk_deployments (name, status, total_vms, parallel, created_by) VALUES (?, ?, ?, ?, ?)`,
		b.Name, b.Status, b.TotalVMs, b.Parallel, b.CreatedBy,
	)
	if err != nil {
		return err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return fmt.Errorf("get last insert id: %w", err)
	}
	b.ID = id
	return nil
}

func (db *DB) GetBulkDeployment(id int64) (*models.BulkDeployment, error) {
	b := &models.BulkDeployment{}
	err := db.conn.QueryRow(
		`SELECT id, name, status, total_vms, completed_vms, failed_vms, parallel, created_by, created_at, completed_at
		 FROM bulk_deployments WHERE id = ?`, id,
	).Scan(&b.ID, &b.Name, &b.Status, &b.TotalVMs, &b.CompletedVMs, &b.FailedVMs, &b.Parallel, &b.CreatedBy, &b.CreatedAt, &b.CompletedAt)
	if err != nil {
		return nil, err
	}
	return b, nil
}

func (db *DB) ListBulkDeployments() ([]models.BulkDeployment, error) {
	rows, err := db.conn.Query(
		`SELECT id, name, status, total_vms, completed_vms, failed_vms, parallel, created_by, created_at, completed_at
		 FROM bulk_deployments ORDER BY created_at DESC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	bulks := []models.BulkDeployment{}
	for rows.Next() {
		var b models.BulkDeployment
		if err := rows.Scan(&b.ID, &b.Name, &b.Status, &b.TotalVMs, &b.CompletedVMs, &b.FailedVMs, &b.Parallel, &b.CreatedBy, &b.CreatedAt, &b.CompletedAt); err != nil {
			return nil, err
		}
		bulks = append(bulks, b)
	}
	return bulks, rows.Err()
}

// SEC-2: ListBulkDeploymentsByUser returns bulk deployments owned by a specific user.
func (db *DB) ListBulkDeploymentsByUser(userID int64) ([]models.BulkDeployment, error) {
	rows, err := db.conn.Query(
		`SELECT id, name, status, total_vms, completed_vms, failed_vms, parallel, created_by, created_at, completed_at
		 FROM bulk_deployments WHERE created_by = ? ORDER BY created_at DESC`, userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	bulks := []models.BulkDeployment{}
	for rows.Next() {
		var b models.BulkDeployment
		if err := rows.Scan(&b.ID, &b.Name, &b.Status, &b.TotalVMs, &b.CompletedVMs, &b.FailedVMs, &b.Parallel, &b.CreatedBy, &b.CreatedAt, &b.CompletedAt); err != nil {
			return nil, err
		}
		bulks = append(bulks, b)
	}
	return bulks, rows.Err()
}

func (db *DB) UpdateBulkDeploymentProgress(id int64, completed, failed int) error {
	_, err := db.conn.Exec(
		`UPDATE bulk_deployments SET completed_vms=?, failed_vms=? WHERE id=?`, completed, failed, id,
	)
	return err
}

func (db *DB) UpdateBulkDeploymentStatus(id int64, status string) error {
	if status == "completed" || status == "failed" {
		_, err := db.conn.Exec(`UPDATE bulk_deployments SET status=?, completed_at=? WHERE id=?`, status, time.Now(), id)
		return err
	}
	_, err := db.conn.Exec(`UPDATE bulk_deployments SET status=? WHERE id=?`, status, id)
	return err
}

func (db *DB) ListDeploymentsByBulk(bulkID int64) ([]models.Deployment, error) {
	rows, err := db.conn.Query(
		`SELECT d.id, d.template_id, d.target_id, d.vm_name, d.status, d.config_json, d.started_at, d.completed_at, COALESCE(d.error_message, ''), d.created_by, d.created_at,
		        COALESCE(d.template_name, t.name, ''), COALESCE(tg.name, '')
		 FROM deployments d
		 LEFT JOIN templates t ON d.template_id = t.id
		 LEFT JOIN targets tg ON d.target_id = tg.id
		 WHERE d.bulk_deployment_id = ? ORDER BY d.id`, bulkID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	deployments := []models.Deployment{}
	for rows.Next() {
		var d models.Deployment
		if err := rows.Scan(&d.ID, &d.TemplateID, &d.TargetID, &d.VMName, &d.Status, &d.ConfigJSON, &d.StartedAt, &d.CompletedAt, &d.ErrorMessage, &d.CreatedBy, &d.CreatedAt, &d.TemplateName, &d.TargetName); err != nil {
			return nil, err
		}
		deployments = append(deployments, d)
	}
	return deployments, rows.Err()
}

func (db *DB) CreateDeploymentWithBulk(d *models.Deployment) error {
	res, err := db.conn.Exec(
		`INSERT INTO deployments (template_id, target_id, vm_name, status, config_json, created_by, bulk_deployment_id, template_name)
		 VALUES (?, ?, ?, ?, ?, ?, ?, COALESCE((SELECT name FROM templates WHERE id = ?), ''))`,
		d.TemplateID, d.TargetID, d.VMName, d.Status, d.ConfigJSON, d.CreatedBy, d.BulkDeploymentID, d.TemplateID,
	)
	if err != nil {
		return err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return fmt.Errorf("get last insert id: %w", err)
	}
	d.ID = id
	return nil
}

// --- Auth Sources ---

func (db *DB) CreateAuthSource(a *models.AuthSource) error {
	res, err := db.conn.Exec(
		`INSERT INTO auth_sources (name, type, config_json, is_default, enabled) VALUES (?, ?, ?, ?, ?)`,
		a.Name, a.Type, a.ConfigJSON, a.IsDefault, a.Enabled,
	)
	if err != nil {
		return err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return fmt.Errorf("get last insert id: %w", err)
	}
	a.ID = id
	return nil
}

func (db *DB) GetAuthSource(id int64) (*models.AuthSource, error) {
	a := &models.AuthSource{}
	err := db.conn.QueryRow(
		`SELECT id, name, type, config_json, is_default, enabled, created_at FROM auth_sources WHERE id = ?`, id,
	).Scan(&a.ID, &a.Name, &a.Type, &a.ConfigJSON, &a.IsDefault, &a.Enabled, &a.CreatedAt)
	if err != nil {
		return nil, err
	}
	return a, nil
}

func (db *DB) ListAuthSources() ([]models.AuthSource, error) {
	rows, err := db.conn.Query(`SELECT id, name, type, config_json, is_default, enabled, created_at FROM auth_sources ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	sources := []models.AuthSource{}
	for rows.Next() {
		var a models.AuthSource
		if err := rows.Scan(&a.ID, &a.Name, &a.Type, &a.ConfigJSON, &a.IsDefault, &a.Enabled, &a.CreatedAt); err != nil {
			return nil, err
		}
		sources = append(sources, a)
	}
	return sources, rows.Err()
}

func (db *DB) UpdateAuthSource(a *models.AuthSource) error {
	_, err := db.conn.Exec(
		`UPDATE auth_sources SET name=?, type=?, config_json=?, is_default=?, enabled=? WHERE id=?`,
		a.Name, a.Type, a.ConfigJSON, a.IsDefault, a.Enabled, a.ID,
	)
	return err
}

func (db *DB) DeleteAuthSource(id int64) error {
	_, err := db.conn.Exec(`DELETE FROM auth_sources WHERE id = ?`, id)
	return err
}

func (db *DB) GetDefaultLDAPSource() (*models.AuthSource, error) {
	a := &models.AuthSource{}
	err := db.conn.QueryRow(
		`SELECT id, name, type, config_json, is_default, enabled, created_at FROM auth_sources WHERE type = 'ldap' AND enabled = TRUE ORDER BY is_default DESC LIMIT 1`,
	).Scan(&a.ID, &a.Name, &a.Type, &a.ConfigJSON, &a.IsDefault, &a.Enabled, &a.CreatedAt)
	if err != nil {
		return nil, err
	}
	return a, nil
}

func (db *DB) GetUserByExternalID(externalID string) (*models.User, error) {
	u := &models.User{}
	err := db.conn.QueryRow(
		`SELECT id, username, password_hash, display_name, role, is_active, last_login_at, created_at, auth_source_id, external_id, COALESCE(token_version, 0) FROM users WHERE external_id = ?`,
		externalID,
	).Scan(&u.ID, &u.Username, &u.PasswordHash, &u.DisplayName, &u.Role, &u.IsActive, &u.LastLoginAt, &u.CreatedAt, &u.AuthSourceID, &u.ExternalID, &u.TokenVersion)
	if err != nil {
		return nil, err
	}
	return u, nil
}

func (db *DB) CreateLDAPUser(u *models.User) error {
	res, err := db.conn.Exec(
		`INSERT INTO users (username, password_hash, display_name, role, is_active, auth_source_id, external_id) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		u.Username, u.PasswordHash, u.DisplayName, u.Role, u.IsActive, u.AuthSourceID, u.ExternalID,
	)
	if err != nil {
		return err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return fmt.Errorf("get last insert id: %w", err)
	}
	u.ID = id
	return nil
}

// --- Template Builds ---

func (db *DB) CreateTemplateBuild(b *models.TemplateBuild) error {
	res, err := db.conn.Exec(
		`INSERT INTO template_builds (os_definition_id, target_id, status, template_name, config_json, created_by, version, previous_build_id, auto_triggered)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		b.OSDefinitionID, b.TargetID, b.Status, b.TemplateName, b.ConfigJSON, b.CreatedBy, b.Version, b.PreviousBuildID, b.AutoTriggered,
	)
	if err != nil {
		return err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return fmt.Errorf("get last insert id: %w", err)
	}
	b.ID = id
	return nil
}

func (db *DB) GetTemplateBuild(id int64) (*models.TemplateBuild, error) {
	b := &models.TemplateBuild{}
	err := db.conn.QueryRow(
		`SELECT b.id, b.os_definition_id, b.target_id, b.status, b.template_name, COALESCE(b.config_json, ''),
		        COALESCE(b.iso_url, ''), COALESCE(b.iso_checksum, ''), COALESCE(b.packer_template, ''), COALESCE(b.autoinstall_config, ''), COALESCE(b.packer_log, ''),
		        b.started_at, b.completed_at, COALESCE(b.error_message, ''), b.created_by, b.created_at,
		        COALESCE(t.name, ''), b.template_id, COALESCE(b.version, 0), b.previous_build_id, b.auto_triggered
		 FROM template_builds b
		 LEFT JOIN targets t ON b.target_id = t.id
		 WHERE b.id = ?`, id,
	).Scan(&b.ID, &b.OSDefinitionID, &b.TargetID, &b.Status, &b.TemplateName, &b.ConfigJSON,
		&b.ISOURL, &b.ISOChecksum, &b.PackerTemplate, &b.AutoinstallConfig, &b.PackerLog,
		&b.StartedAt, &b.CompletedAt, &b.ErrorMessage, &b.CreatedBy, &b.CreatedAt,
		&b.TargetName, &b.TemplateID, &b.Version, &b.PreviousBuildID, &b.AutoTriggered)
	if err != nil {
		return nil, err
	}
	return b, nil
}

func (db *DB) ListTemplateBuilds() ([]models.TemplateBuild, error) {
	rows, err := db.conn.Query(
		`SELECT b.id, b.os_definition_id, b.target_id, b.status, b.template_name, COALESCE(b.config_json, ''),
		        COALESCE(b.iso_url, ''), COALESCE(b.iso_checksum, ''), COALESCE(b.packer_template, ''), COALESCE(b.autoinstall_config, ''), COALESCE(b.packer_log, ''),
		        b.started_at, b.completed_at, COALESCE(b.error_message, ''), b.created_by, b.created_at,
		        COALESCE(t.name, ''), b.template_id, COALESCE(b.version, 0), b.previous_build_id, b.auto_triggered
		 FROM template_builds b
		 LEFT JOIN targets t ON b.target_id = t.id
		 ORDER BY b.created_at DESC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	builds := []models.TemplateBuild{}
	for rows.Next() {
		var b models.TemplateBuild
		if err := rows.Scan(&b.ID, &b.OSDefinitionID, &b.TargetID, &b.Status, &b.TemplateName, &b.ConfigJSON,
			&b.ISOURL, &b.ISOChecksum, &b.PackerTemplate, &b.AutoinstallConfig, &b.PackerLog,
			&b.StartedAt, &b.CompletedAt, &b.ErrorMessage, &b.CreatedBy, &b.CreatedAt,
			&b.TargetName, &b.TemplateID, &b.Version, &b.PreviousBuildID, &b.AutoTriggered); err != nil {
			return nil, err
		}
		builds = append(builds, b)
	}
	return builds, rows.Err()
}

func (db *DB) UpdateBuildStatus(id int64, status string, errMsg string) error {
	_, err := db.conn.Exec(`UPDATE template_builds SET status=?, error_message=? WHERE id=?`, status, errMsg, id)
	return err
}

// CleanupStaleBuilds marks any builds stuck in active states as failed on server restart.
func (db *DB) CleanupStaleBuilds() (int, error) {
	result, err := db.conn.Exec("UPDATE template_builds SET status = 'failed', error_message = 'build interrupted by server restart' WHERE status IN ('building', 'downloading', 'pending')")
	if err != nil {
		return 0, err
	}
	n, _ := result.RowsAffected()
	return int(n), nil
}

func (db *DB) UpdateBuildStarted(id int64) error {
	_, err := db.conn.Exec(`UPDATE template_builds SET started_at=? WHERE id=?`, time.Now(), id)
	return err
}

func (db *DB) UpdateBuildCompleted(id int64) error {
	_, err := db.conn.Exec(`UPDATE template_builds SET completed_at=? WHERE id=?`, time.Now(), id)
	return err
}

func (db *DB) UpdateBuildLog(id int64, log string) error {
	_, err := db.conn.Exec(`UPDATE template_builds SET packer_log=? WHERE id=?`, log, id)
	return err
}

func (db *DB) UpdateBuildGenerated(id int64, packerHCL, autoinstall, isoURL, isoChecksum string) error {
	_, err := db.conn.Exec(
		`UPDATE template_builds SET packer_template=?, autoinstall_config=?, iso_url=?, iso_checksum=? WHERE id=?`,
		packerHCL, autoinstall, isoURL, isoChecksum, id,
	)
	return err
}

func (db *DB) DeleteTemplateBuild(id int64) error {
	_, err := db.conn.Exec(`DELETE FROM template_builds WHERE id = ?`, id)
	return err
}

// --- Template Lineage ---

func (db *DB) LinkBuildToTemplate(buildID int64, templateID int64, isoChecksum string, version int) error {
	// V3-M16: Wrap multi-step operation in a transaction
	tx, err := db.conn.Begin()
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	now := time.Now()
	_, err = tx.Exec(
		`UPDATE templates SET build_id=?, managed_by_forgemill=TRUE, version=?, iso_checksum=?, built_at=?, lifecycle_status='active' WHERE id=?`,
		buildID, version, isoChecksum, now, templateID,
	)
	if err != nil {
		return err
	}
	_, err = tx.Exec(`UPDATE template_builds SET template_id=?, version=? WHERE id=?`, templateID, version, buildID)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (db *DB) GetTemplateByName(name string, targetID int64) (*models.Template, error) {
	t := &models.Template{}
	err := db.conn.QueryRow(
		`SELECT t.id, t.target_id, t.name, t.moref, t.os_type, COALESCE(t.os_name, ''), COALESCE(t.guest_id, ''), t.cpu, t.memory_mb, t.disk_gb, COALESCE(t.notes, ''), t.icon, t.last_synced_at, t.created_at, tg.name, tg.type,
		        t.build_id, t.managed_by_forgemill, COALESCE(t.version, 0), COALESCE(t.iso_checksum, ''), t.built_at, COALESCE(t.lifecycle_status, ''), t.superseded_by, t.retain_until, COALESCE(t.platform, 'linux'), t.family_id
		 FROM templates t JOIN targets tg ON t.target_id = tg.id WHERE t.name = ? AND t.target_id = ? ORDER BY t.build_id IS NULL DESC, t.id DESC LIMIT 1`, name, targetID,
	).Scan(&t.ID, &t.TargetID, &t.Name, &t.Moref, &t.OSType, &t.OSName, &t.GuestID, &t.CPU, &t.MemoryMB, &t.DiskGB, &t.Notes, &t.Icon, &t.LastSyncedAt, &t.CreatedAt, &t.TargetName, &t.TargetType,
		&t.BuildID, &t.ManagedByForgemill, &t.Version, &t.ISOChecksum, &t.BuiltAt, &t.LifecycleStatus, &t.SupersededBy, &t.RetainUntil, &t.Platform, &t.FamilyID)
	if err != nil {
		return nil, err
	}
	return t, nil
}

func (db *DB) ListManagedTemplates() ([]models.Template, error) {
	rows, err := db.conn.Query(
		`SELECT t.id, t.target_id, t.name, t.moref, t.os_type, COALESCE(t.os_name, ''), COALESCE(t.guest_id, ''), t.cpu, t.memory_mb, t.disk_gb, COALESCE(t.notes, ''), t.icon, t.last_synced_at, t.created_at, tg.name, tg.type,
		        t.build_id, t.managed_by_forgemill, COALESCE(t.version, 0), COALESCE(t.iso_checksum, ''), t.built_at, COALESCE(t.lifecycle_status, ''), t.superseded_by, t.retain_until, COALESCE(t.platform, 'linux'), t.family_id
		 FROM templates t JOIN targets tg ON t.target_id = tg.id WHERE t.managed_by_forgemill = TRUE AND t.lifecycle_status = 'active' ORDER BY t.name`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	templates := []models.Template{}
	for rows.Next() {
		var t models.Template
		if err := rows.Scan(&t.ID, &t.TargetID, &t.Name, &t.Moref, &t.OSType, &t.OSName, &t.GuestID, &t.CPU, &t.MemoryMB, &t.DiskGB, &t.Notes, &t.Icon, &t.LastSyncedAt, &t.CreatedAt, &t.TargetName, &t.TargetType,
			&t.BuildID, &t.ManagedByForgemill, &t.Version, &t.ISOChecksum, &t.BuiltAt, &t.LifecycleStatus, &t.SupersededBy, &t.RetainUntil, &t.Platform, &t.FamilyID); err != nil {
			return nil, err
		}
		templates = append(templates, t)
	}
	return templates, rows.Err()
}

func (db *DB) SupersedeTemplate(oldID int64, newID int64) error {
	_, err := db.conn.Exec(`UPDATE templates SET lifecycle_status='superseded', superseded_by=? WHERE id=?`, newID, oldID)
	return err
}

// SupersedeAllActiveInFamily marks every active template in a family as superseded
// by newTemplateID, except for newTemplateID itself. This ensures only one active
// template exists per family after any build completes (whether StartBuild or
// RebuildTemplate).
func (db *DB) SupersedeAllActiveInFamily(familyID int64, newTemplateID int64) error {
	_, err := db.conn.Exec(
		`UPDATE templates SET lifecycle_status='superseded', superseded_by=?
		 WHERE family_id=? AND id!=? AND lifecycle_status='active'`,
		newTemplateID, familyID, newTemplateID,
	)
	return err
}

// GetMaxVersionInFamily returns the highest version number currently in the
// templates table for a given family. Returns 0 when no templates exist.
// Use this instead of family.LatestVersion to compute the next version — it
// stays accurate even when templates are deleted externally.
func (db *DB) GetMaxVersionInFamily(familyID int64) (int, error) {
	var maxVer int
	err := db.conn.QueryRow(
		`SELECT COALESCE(MAX(version), 0) FROM templates WHERE family_id=?`, familyID,
	).Scan(&maxVer)
	return maxVer, err
}

// --- Template Families ---

// GetOrCreateFamilyByOS looks up or creates a family by OS definition + target.
// This is the canonical method — family identity is (os_definition_id, target_id).
// The baseName is derived from osDefID (e.g., "ubuntu-2404" → "ubuntu-24.04-template").
func (db *DB) GetOrCreateFamilyByOS(osDefID string, targetID int64) (*models.TemplateFamily, error) {
	// Try to get existing family by OS + target (the canonical key)
	family, err := db.getFamilyByOSAndTarget(osDefID, targetID)
	if err == nil {
		return family, nil
	}
	if err != sql.ErrNoRows {
		return nil, fmt.Errorf("query existing family: %w", err)
	}

	// Derive base_name from OS definition ID
	baseName := deriveBaseName(osDefID)

	// Create new family
	res, err := db.conn.Exec(
		`INSERT INTO template_families (base_name, target_id, os_definition_id, latest_version) VALUES (?, ?, ?, ?)`,
		baseName, targetID, osDefID, 0,
	)
	if err != nil {
		return nil, fmt.Errorf("create family: %w", err)
	}

	id, err := res.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("get family id: %w", err)
	}

	return &models.TemplateFamily{
		ID:             id,
		BaseName:       baseName,
		TargetID:       targetID,
		OSDefinitionID: osDefID,
		LatestVersion:  0,
		CreatedAt:      time.Now(),
	}, nil
}

// deriveBaseName converts an OS definition ID to a human-readable base name.
// Examples: "ubuntu-2404" → "ubuntu-24.04-template", "ubuntu-2204" → "ubuntu-22.04-template"
func deriveBaseName(osDefID string) string {
	// Handle known patterns
	if len(osDefID) >= 11 && osDefID[:6] == "ubuntu" {
		// "ubuntu-XXYY" → "ubuntu-XX.YY-template"
		suffix := osDefID[7:] // e.g., "2404" or "2204"
		if len(suffix) == 4 {
			return fmt.Sprintf("ubuntu-%s.%s-template", suffix[:2], suffix[2:])
		}
	}
	// Fallback: use the ID directly with "-template" suffix
	return osDefID + "-template"
}

func (db *DB) getFamilyByBaseNameAndTarget(baseName string, targetID int64) (*models.TemplateFamily, error) {
	f := &models.TemplateFamily{}
	err := db.conn.QueryRow(
		`SELECT id, base_name, target_id, os_definition_id, latest_version, created_at
		 FROM template_families WHERE base_name = ? AND target_id = ?`,
		baseName, targetID,
	).Scan(&f.ID, &f.BaseName, &f.TargetID, &f.OSDefinitionID, &f.LatestVersion, &f.CreatedAt)
	return f, err
}

// getFamilyByOSAndTarget looks up a family by OS definition + target combination.
// This is the canonical lookup — family identity is (os_definition_id, target_id),
// not (base_name, target_id).
func (db *DB) getFamilyByOSAndTarget(osDefID string, targetID int64) (*models.TemplateFamily, error) {
	f := &models.TemplateFamily{}
	err := db.conn.QueryRow(
		`SELECT id, base_name, target_id, os_definition_id, latest_version, created_at
		 FROM template_families WHERE os_definition_id = ? AND target_id = ?`,
		osDefID, targetID,
	).Scan(&f.ID, &f.BaseName, &f.TargetID, &f.OSDefinitionID, &f.LatestVersion, &f.CreatedAt)
	return f, err
}

func (db *DB) GetFamily(id int64) (*models.TemplateFamily, error) {
	f := &models.TemplateFamily{}
	err := db.conn.QueryRow(
		`SELECT id, base_name, target_id, os_definition_id, latest_version, created_at
		 FROM template_families WHERE id = ?`,
		id,
	).Scan(&f.ID, &f.BaseName, &f.TargetID, &f.OSDefinitionID, &f.LatestVersion, &f.CreatedAt)
	return f, err
}

func (db *DB) UpdateFamilyLatestVersion(familyID int64, version int) error {
	_, err := db.conn.Exec(`UPDATE template_families SET latest_version = ? WHERE id = ?`, version, familyID)
	return err
}

func (db *DB) GetTemplatesByFamily(familyID int64) ([]models.Template, error) {
	rows, err := db.conn.Query(
		`SELECT t.id, t.target_id, t.name, t.moref, t.os_type, COALESCE(t.os_name, ''), COALESCE(t.guest_id, ''), t.cpu, t.memory_mb, t.disk_gb, COALESCE(t.notes, ''), t.icon, t.last_synced_at, t.created_at, tg.name, tg.type,
		        t.build_id, t.managed_by_forgemill, COALESCE(t.version, 0), COALESCE(t.iso_checksum, ''), t.built_at, COALESCE(t.lifecycle_status, ''), t.superseded_by, t.retain_until, COALESCE(t.platform, 'linux'), t.family_id
		 FROM templates t JOIN targets tg ON t.target_id = tg.id WHERE t.family_id = ? ORDER BY t.version DESC`, familyID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	templates := []models.Template{}
	for rows.Next() {
		var t models.Template
		if err := rows.Scan(&t.ID, &t.TargetID, &t.Name, &t.Moref, &t.OSType, &t.OSName, &t.GuestID, &t.CPU, &t.MemoryMB, &t.DiskGB, &t.Notes, &t.Icon, &t.LastSyncedAt, &t.CreatedAt, &t.TargetName, &t.TargetType,
			&t.BuildID, &t.ManagedByForgemill, &t.Version, &t.ISOChecksum, &t.BuiltAt, &t.LifecycleStatus, &t.SupersededBy, &t.RetainUntil, &t.Platform, &t.FamilyID); err != nil {
			return nil, err
		}
		templates = append(templates, t)
	}
	return templates, rows.Err()
}

func (db *DB) ListTemplateFamilies() ([]models.TemplateFamily, error) {
	rows, err := db.conn.Query(`SELECT id, base_name, target_id, os_definition_id, latest_version, created_at FROM template_families ORDER BY base_name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	families := []models.TemplateFamily{}
	for rows.Next() {
		var f models.TemplateFamily
		if err := rows.Scan(&f.ID, &f.BaseName, &f.TargetID, &f.OSDefinitionID, &f.LatestVersion, &f.CreatedAt); err != nil {
			return nil, err
		}
		families = append(families, f)
	}
	return families, rows.Err()
}

func (db *DB) GetTemplateHistory(templateID int64) ([]models.TemplateHistory, error) {
	// Use family-based query instead of chain-walking
	rows, err := db.conn.Query(
		`SELECT t.id, t.name, COALESCE(t.version, 0), COALESCE(t.lifecycle_status, ''), t.build_id, t.built_at, COALESCE(t.iso_checksum, ''), t.superseded_by
		 FROM templates t
		 WHERE t.family_id = (SELECT family_id FROM templates WHERE id = ?)
		 AND t.managed_by_forgemill = TRUE
		 ORDER BY t.version DESC`, templateID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	history := []models.TemplateHistory{}
	for rows.Next() {
		var h models.TemplateHistory
		if err := rows.Scan(&h.TemplateID, &h.TemplateName, &h.Version, &h.Status, &h.BuildID, &h.BuiltAt, &h.ISOChecksum, &h.SupersededBy); err != nil {
			return nil, err
		}
		history = append(history, h)
	}
	return history, rows.Err()
}

func (db *DB) DeleteSupersededTemplates(templateID int64) (int64, error) {
	res, err := db.conn.Exec(`DELETE FROM templates WHERE superseded_by = ? AND lifecycle_status = 'superseded'`, templateID)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func (db *DB) GetCompletedBuildsByTemplate(templateName string) ([]models.TemplateBuild, error) {
	rows, err := db.conn.Query(
		`SELECT b.id, b.os_definition_id, b.target_id, b.status, b.template_name, COALESCE(b.config_json, ''),
		        COALESCE(b.iso_url, ''), COALESCE(b.iso_checksum, ''), COALESCE(b.packer_template, ''), COALESCE(b.autoinstall_config, ''), COALESCE(b.packer_log, ''),
		        b.started_at, b.completed_at, COALESCE(b.error_message, ''), b.created_by, b.created_at,
		        COALESCE(t.name, ''), b.template_id, COALESCE(b.version, 0), b.previous_build_id, b.auto_triggered
		 FROM template_builds b
		 LEFT JOIN targets t ON b.target_id = t.id
		 WHERE b.status = 'completed' AND b.template_name = ?
		 ORDER BY b.created_at DESC`, templateName,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	builds := []models.TemplateBuild{}
	for rows.Next() {
		var b models.TemplateBuild
		if err := rows.Scan(&b.ID, &b.OSDefinitionID, &b.TargetID, &b.Status, &b.TemplateName, &b.ConfigJSON,
			&b.ISOURL, &b.ISOChecksum, &b.PackerTemplate, &b.AutoinstallConfig, &b.PackerLog,
			&b.StartedAt, &b.CompletedAt, &b.ErrorMessage, &b.CreatedBy, &b.CreatedAt,
			&b.TargetName, &b.TemplateID, &b.Version, &b.PreviousBuildID, &b.AutoTriggered); err != nil {
			return nil, err
		}
		builds = append(builds, b)
	}
	return builds, rows.Err()
}

func (db *DB) UpdateBuildAutoTriggered(id int64, previousBuildID *int64) error {
	_, err := db.conn.Exec(`UPDATE template_builds SET auto_triggered=TRUE, previous_build_id=? WHERE id=?`, previousBuildID, id)
	return err
}

// --- Template Schedules ---

func (db *DB) CreateTemplateSchedule(s *models.TemplateSchedule) error {
	res, err := db.conn.Exec(
		`INSERT INTO template_schedules (template_id, build_config_json, strategy, interval_days, check_interval_hours, next_check_at, enabled)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		s.TemplateID, s.BuildConfigJSON, s.Strategy, s.IntervalDays, s.CheckIntervalHours, s.NextCheckAt, s.Enabled,
	)
	if err != nil {
		return err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return fmt.Errorf("get last insert id: %w", err)
	}
	s.ID = id
	return nil
}

func (db *DB) GetTemplateSchedule(id int64) (*models.TemplateSchedule, error) {
	s := &models.TemplateSchedule{}
	err := db.conn.QueryRow(
		`SELECT id, template_id, build_config_json, strategy, interval_days, check_interval_hours,
		        last_checked_at, last_rebuilt_at, next_check_at, enabled, created_at
		 FROM template_schedules WHERE id = ?`, id,
	).Scan(&s.ID, &s.TemplateID, &s.BuildConfigJSON, &s.Strategy, &s.IntervalDays, &s.CheckIntervalHours,
		&s.LastCheckedAt, &s.LastRebuiltAt, &s.NextCheckAt, &s.Enabled, &s.CreatedAt)
	if err != nil {
		return nil, err
	}
	return s, nil
}

func (db *DB) ListTemplateSchedules() ([]models.TemplateSchedule, error) {
	rows, err := db.conn.Query(
		`SELECT id, template_id, build_config_json, strategy, interval_days, check_interval_hours,
		        last_checked_at, last_rebuilt_at, next_check_at, enabled, created_at
		 FROM template_schedules ORDER BY id`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	schedules := []models.TemplateSchedule{}
	for rows.Next() {
		var s models.TemplateSchedule
		if err := rows.Scan(&s.ID, &s.TemplateID, &s.BuildConfigJSON, &s.Strategy, &s.IntervalDays, &s.CheckIntervalHours,
			&s.LastCheckedAt, &s.LastRebuiltAt, &s.NextCheckAt, &s.Enabled, &s.CreatedAt); err != nil {
			return nil, err
		}
		schedules = append(schedules, s)
	}
	return schedules, rows.Err()
}

func (db *DB) ListDueSchedules() ([]models.TemplateSchedule, error) {
	now := time.Now()
	rows, err := db.conn.Query(
		`SELECT id, template_id, build_config_json, strategy, interval_days, check_interval_hours,
		        last_checked_at, last_rebuilt_at, next_check_at, enabled, created_at
		 FROM template_schedules WHERE enabled = TRUE AND (next_check_at IS NULL OR next_check_at <= ?)
		 ORDER BY id`, now,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	schedules := []models.TemplateSchedule{}
	for rows.Next() {
		var s models.TemplateSchedule
		if err := rows.Scan(&s.ID, &s.TemplateID, &s.BuildConfigJSON, &s.Strategy, &s.IntervalDays, &s.CheckIntervalHours,
			&s.LastCheckedAt, &s.LastRebuiltAt, &s.NextCheckAt, &s.Enabled, &s.CreatedAt); err != nil {
			return nil, err
		}
		schedules = append(schedules, s)
	}
	return schedules, rows.Err()
}

func (db *DB) UpdateTemplateSchedule(s *models.TemplateSchedule) error {
	_, err := db.conn.Exec(
		`UPDATE template_schedules SET build_config_json=?, strategy=?, interval_days=?, check_interval_hours=?, enabled=? WHERE id=?`,
		s.BuildConfigJSON, s.Strategy, s.IntervalDays, s.CheckIntervalHours, s.Enabled, s.ID,
	)
	return err
}

func (db *DB) UpdateScheduleChecked(id int64, nextCheckAt time.Time) error {
	now := time.Now()
	_, err := db.conn.Exec(`UPDATE template_schedules SET last_checked_at=?, next_check_at=? WHERE id=?`, now, nextCheckAt, id)
	return err
}

func (db *DB) UpdateScheduleRebuilt(id int64, nextCheckAt time.Time) error {
	now := time.Now()
	_, err := db.conn.Exec(`UPDATE template_schedules SET last_checked_at=?, last_rebuilt_at=?, next_check_at=? WHERE id=?`, now, now, nextCheckAt, id)
	return err
}

func (db *DB) DeleteTemplateSchedule(id int64) error {
	_, err := db.conn.Exec(`DELETE FROM template_schedules WHERE id = ?`, id)
	return err
}

func (db *DB) CountTemplatesNeedingUpdate() (int, error) {
	// This is a placeholder count — the actual check requires fetching remote checksums.
	// Returns count of managed templates (for dashboard purposes).
	var count int
	err := db.conn.QueryRow(`SELECT COUNT(*) FROM templates WHERE managed_by_forgemill = TRUE AND lifecycle_status = 'active'`).Scan(&count)
	return count, err
}

func (db *DB) CountScheduledBuildsToday() (int, error) {
	var count int
	err := db.conn.QueryRow(
		`SELECT COUNT(*) FROM template_schedules WHERE enabled = TRUE AND DATE(next_check_at) = DATE('now')`,
	).Scan(&count)
	return count, err
}

func (db *DB) GetScheduleByTemplateID(templateID int64) (*models.TemplateSchedule, error) {
	s := &models.TemplateSchedule{}
	err := db.conn.QueryRow(
		`SELECT id, template_id, build_config_json, strategy, interval_days, check_interval_hours,
		        last_checked_at, last_rebuilt_at, next_check_at, enabled, created_at
		 FROM template_schedules WHERE template_id = ?`, templateID,
	).Scan(&s.ID, &s.TemplateID, &s.BuildConfigJSON, &s.Strategy, &s.IntervalDays, &s.CheckIntervalHours,
		&s.LastCheckedAt, &s.LastRebuiltAt, &s.NextCheckAt, &s.Enabled, &s.CreatedAt)
	if err != nil {
		return nil, err
	}
	return s, nil
}

// --- App Settings ---

func (db *DB) GetAllSettings() (map[string]string, error) {
	rows, err := db.conn.Query(`SELECT key, value FROM app_settings ORDER BY key`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	settings := make(map[string]string)
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		settings[k] = v
	}
	return settings, rows.Err()
}

func (db *DB) SetSetting(key, value string) error {
	_, err := db.conn.Exec(
		`INSERT INTO app_settings (key, value, updated_at) VALUES (?, ?, CURRENT_TIMESTAMP)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = CURRENT_TIMESTAMP`,
		key, value,
	)
	return err
}

// --- Stats ---

type Stats struct {
	TotalTargets         int `json:"total_targets"`
	TotalTemplates       int `json:"total_templates"`
	TotalDeployments     int `json:"total_deployments"`
	TotalVMs             int `json:"total_vms"`
	TotalActions         int `json:"total_actions"`
	DeploymentsToday     int `json:"deployments_today"`
	RunningDeploys       int `json:"running_deploys"`
	ManagedTemplates     int `json:"managed_templates"`
	ScheduledBuildsToday int `json:"scheduled_builds_today"`
}

func (db *DB) GetStats() (*Stats, error) {
	s := &Stats{}
	if err := db.conn.QueryRow(`SELECT COUNT(*) FROM targets`).Scan(&s.TotalTargets); err != nil {
		return nil, fmt.Errorf("count targets: %w", err)
	}
	if err := db.conn.QueryRow(`SELECT COUNT(*) FROM templates`).Scan(&s.TotalTemplates); err != nil {
		return nil, fmt.Errorf("count templates: %w", err)
	}
	if err := db.conn.QueryRow(`SELECT COUNT(*) FROM deployments`).Scan(&s.TotalDeployments); err != nil {
		return nil, fmt.Errorf("count deployments: %w", err)
	}
	if err := db.conn.QueryRow(`SELECT COUNT(*) FROM deployments WHERE DATE(created_at) = DATE('now')`).Scan(&s.DeploymentsToday); err != nil {
		return nil, fmt.Errorf("count deployments today: %w", err)
	}
	if err := db.conn.QueryRow(`SELECT COUNT(*) FROM deployments WHERE status = 'running'`).Scan(&s.RunningDeploys); err != nil {
		return nil, fmt.Errorf("count running deploys: %w", err)
	}
	if err := db.conn.QueryRow(`SELECT COUNT(*) FROM managed_vms`).Scan(&s.TotalVMs); err != nil {
		return nil, fmt.Errorf("count vms: %w", err)
	}
	if err := db.conn.QueryRow(`SELECT COUNT(*) FROM actions`).Scan(&s.TotalActions); err != nil {
		return nil, fmt.Errorf("count actions: %w", err)
	}
	if err := db.conn.QueryRow(`SELECT COUNT(*) FROM templates WHERE managed_by_forgemill = TRUE AND lifecycle_status = 'active'`).Scan(&s.ManagedTemplates); err != nil {
		return nil, fmt.Errorf("count managed templates: %w", err)
	}
	if err := db.conn.QueryRow(`SELECT COUNT(*) FROM template_schedules WHERE enabled = TRUE AND DATE(next_check_at) = DATE('now')`).Scan(&s.ScheduledBuildsToday); err != nil {
		return nil, fmt.Errorf("count scheduled builds today: %w", err)
	}
	return s, nil
}

// --- Audit Logging ---

type AuditLogFilter struct {
	ActorID  *int64
	Action   string
	Since    *time.Time
	Until    *time.Time
	Page     int
	PageSize int
}

type PaginatedAuditLogs struct {
	Logs       []models.AuditLog `json:"logs"`
	Total      int64             `json:"total"`
	Page       int               `json:"page"`
	PageSize   int               `json:"page_size"`
	TotalPages int               `json:"total_pages"`
}

// DeleteOldAuditLogs removes audit log entries older than the specified number of days.
// Returns the number of rows deleted.
func (db *DB) DeleteOldAuditLogs(retentionDays int) (int64, error) {
	cutoff := time.Now().AddDate(0, 0, -retentionDays)
	result, err := db.conn.Exec(`DELETE FROM audit_logs WHERE created_at < ?`, cutoff)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func (db *DB) CreateAuditLog(entry *models.AuditLog) error {
	_, err := db.conn.Exec(
		`INSERT INTO audit_logs (actor, actor_id, action, resource_type, resource_id, metadata, ip_address) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		entry.Actor, entry.ActorID, entry.Action, entry.ResourceType, entry.ResourceID, entry.Metadata, entry.IPAddress,
	)
	return err
}

func (db *DB) ListAuditLogs(f AuditLogFilter) (*PaginatedAuditLogs, error) {
	if f.PageSize <= 0 {
		f.PageSize = 50
	}
	if f.PageSize > 200 {
		f.PageSize = 200
	}
	if f.Page <= 0 {
		f.Page = 1
	}

	where := []string{"1=1"}
	args := []interface{}{}
	if f.ActorID != nil {
		where = append(where, "actor_id = ?")
		args = append(args, *f.ActorID)
	}
	if f.Action != "" {
		where = append(where, "action LIKE ?")
		args = append(args, escapeLike(f.Action)+"%")
	}
	if f.Since != nil {
		where = append(where, "created_at >= ?")
		args = append(args, *f.Since)
	}
	if f.Until != nil {
		where = append(where, "created_at <= ?")
		args = append(args, *f.Until)
	}

	clause := strings.Join(where, " AND ")

	var total int64
	err := db.conn.QueryRow("SELECT COUNT(*) FROM audit_logs WHERE "+clause, args...).Scan(&total)
	if err != nil {
		return nil, err
	}

	offset := (f.Page - 1) * f.PageSize
	args = append(args, f.PageSize, offset)
	rows, err := db.conn.Query(
		"SELECT id, actor, actor_id, action, resource_type, resource_id, metadata, ip_address, created_at FROM audit_logs WHERE "+clause+" ORDER BY created_at DESC LIMIT ? OFFSET ?",
		args...,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	logs := []models.AuditLog{}
	for rows.Next() {
		var l models.AuditLog
		var metadataStr string
		if err := rows.Scan(&l.ID, &l.Actor, &l.ActorID, &l.Action, &l.ResourceType, &l.ResourceID, &metadataStr, &l.IPAddress, &l.CreatedAt); err != nil {
			return nil, err
		}
		l.Metadata = json.RawMessage(metadataStr)
		logs = append(logs, l)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	totalPages := int((total + int64(f.PageSize) - 1) / int64(f.PageSize))
	if totalPages == 0 {
		totalPages = 1
	}

	return &PaginatedAuditLogs{Logs: logs, Total: total, Page: f.Page, PageSize: f.PageSize, TotalPages: totalPages}, nil
}

// --- User Preferences ---

func (db *DB) GetUserPreferences(userID int64) (map[string]string, error) {
	rows, err := db.conn.Query(`SELECT key, value FROM user_preferences WHERE user_id = ?`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	prefs := make(map[string]string)
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		prefs[k] = v
	}
	return prefs, rows.Err()
}

func (db *DB) SetUserPreference(userID int64, key, value string) error {
	_, err := db.conn.Exec(
		`INSERT INTO user_preferences (user_id, key, value, updated_at)
		 VALUES (?, ?, ?, CURRENT_TIMESTAMP)
		 ON CONFLICT(user_id, key) DO UPDATE SET value = excluded.value, updated_at = CURRENT_TIMESTAMP`,
		userID, key, value,
	)
	return err
}
