package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/forgemill/forgemill/internal/db/models"
)

func scanActionParameters(raw sql.NullString) []models.ActionParameter {
	if !raw.Valid || raw.String == "" {
		return nil
	}
	var params []models.ActionParameter
	if err := json.Unmarshal([]byte(raw.String), &params); err != nil {
		return nil
	}
	return params
}

func (db *DB) ListActions() ([]models.Action, error) {
	rows, err := db.conn.Query(`SELECT id, name, description, category, script, script_type, platform, builtin, parameters, version, created_at, updated_at FROM actions ORDER BY category, name`)
	if err != nil {
		return nil, fmt.Errorf("list actions: %w", err)
	}
	defer rows.Close()

	var actions []models.Action
	for rows.Next() {
		var a models.Action
		var builtin int
		var paramsRaw sql.NullString
		if err := rows.Scan(&a.ID, &a.Name, &a.Description, &a.Category, &a.Script, &a.ScriptType, &a.Platform, &builtin, &paramsRaw, &a.Version, &a.CreatedAt, &a.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan action: %w", err)
		}
		a.Builtin = builtin == 1
		a.Parameters = scanActionParameters(paramsRaw)
		actions = append(actions, a)
	}
	return actions, nil
}

func (db *DB) GetAction(id int64) (*models.Action, error) {
	var a models.Action
	var builtin int
	var paramsRaw sql.NullString
	err := db.conn.QueryRow(`SELECT id, name, description, category, script, script_type, platform, builtin, parameters, version, created_at, updated_at FROM actions WHERE id = ?`, id).
		Scan(&a.ID, &a.Name, &a.Description, &a.Category, &a.Script, &a.ScriptType, &a.Platform, &builtin, &paramsRaw, &a.Version, &a.CreatedAt, &a.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("get action: %w", err)
	}
	a.Builtin = builtin == 1
	a.Parameters = scanActionParameters(paramsRaw)
	return &a, nil
}

func marshalParameters(params []models.ActionParameter) sql.NullString {
	if len(params) == 0 {
		return sql.NullString{}
	}
	data, err := json.Marshal(params)
	if err != nil {
		return sql.NullString{}
	}
	return sql.NullString{String: string(data), Valid: true}
}

func (db *DB) CreateAction(a *models.Action) error {
	now := time.Now().UTC().Format(time.DateTime)
	if a.ScriptType == "" {
		a.ScriptType = "bash"
	}
	if a.Platform == "" {
		a.Platform = "linux"
	}
	paramsJSON := marshalParameters(a.Parameters)
	result, err := db.conn.Exec(
		`INSERT INTO actions (name, description, category, script, script_type, platform, builtin, parameters, version, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, 0, ?, 1, ?, ?)`,
		a.Name, a.Description, a.Category, a.Script, a.ScriptType, a.Platform, paramsJSON, now, now,
	)
	if err != nil {
		return fmt.Errorf("create action: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return fmt.Errorf("get action id: %w", err)
	}
	a.ID = id
	a.Version = 1
	a.CreatedAt = now
	a.UpdatedAt = now
	return nil
}

// UpdateAction overwrites the live action content. Before doing so, it
// snapshots the row's current (about-to-be-superseded) content into
// action_versions, so every prior version stays retrievable — including,
// on an action's very first edit, the content it was originally created
// with, captured retroactively with no separate backfill needed.
func (db *DB) UpdateAction(a *models.Action, changedBy *int64) error {
	tx, err := db.conn.Begin()
	if err != nil {
		return fmt.Errorf("begin update action: %w", err)
	}
	defer tx.Rollback()

	var current models.Action
	var paramsRaw sql.NullString
	err = tx.QueryRow(`SELECT name, description, category, script, script_type, platform, parameters, version FROM actions WHERE id = ? AND builtin = 0`, a.ID).
		Scan(&current.Name, &current.Description, &current.Category, &current.Script, &current.ScriptType, &current.Platform, &paramsRaw, &current.Version)
	if err != nil {
		return fmt.Errorf("load current action: %w", err)
	}
	current.Parameters = scanActionParameters(paramsRaw)

	if _, err := tx.Exec(
		`INSERT INTO action_versions (action_id, version, name, description, category, script, script_type, platform, parameters, changed_by) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		a.ID, current.Version, current.Name, current.Description, current.Category, current.Script, current.ScriptType, current.Platform, marshalParameters(current.Parameters), changedBy,
	); err != nil {
		return fmt.Errorf("snapshot superseded action version: %w", err)
	}

	now := time.Now().UTC().Format(time.DateTime)
	if a.ScriptType == "" {
		a.ScriptType = "bash"
	}
	if a.Platform == "" {
		a.Platform = "linux"
	}
	newVersion := current.Version + 1
	paramsJSON := marshalParameters(a.Parameters)
	if _, err := tx.Exec(
		`UPDATE actions SET name = ?, description = ?, category = ?, script = ?, script_type = ?, platform = ?, parameters = ?, version = ?, updated_at = ? WHERE id = ? AND builtin = 0`,
		a.Name, a.Description, a.Category, a.Script, a.ScriptType, a.Platform, paramsJSON, newVersion, now, a.ID,
	); err != nil {
		return fmt.Errorf("update action: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit update action: %w", err)
	}
	a.Version = newVersion
	a.UpdatedAt = now
	return nil
}

// ListActionVersionHistory returns only superseded (past) versions of an
// action, newest first. The current version lives on the action itself —
// callers that want the full history should combine this with GetAction.
func (db *DB) ListActionVersionHistory(actionID int64) ([]models.ActionVersion, error) {
	rows, err := db.conn.Query(
		`SELECT id, action_id, version, name, description, category, script, script_type, platform, parameters, changed_by, created_at
		 FROM action_versions WHERE action_id = ? ORDER BY version DESC`,
		actionID,
	)
	if err != nil {
		return nil, fmt.Errorf("list action version history: %w", err)
	}
	defer rows.Close()

	versions := []models.ActionVersion{}
	for rows.Next() {
		var v models.ActionVersion
		var paramsRaw sql.NullString
		if err := rows.Scan(&v.ID, &v.ActionID, &v.Version, &v.Name, &v.Description, &v.Category, &v.Script, &v.ScriptType, &v.Platform, &paramsRaw, &v.ChangedBy, &v.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan action version: %w", err)
		}
		v.Parameters = scanActionParameters(paramsRaw)
		versions = append(versions, v)
	}
	return versions, rows.Err()
}

// GetActionVersion returns one specific past version's content.
func (db *DB) GetActionVersion(actionID int64, version int) (*models.ActionVersion, error) {
	var v models.ActionVersion
	var paramsRaw sql.NullString
	err := db.conn.QueryRow(
		`SELECT id, action_id, version, name, description, category, script, script_type, platform, parameters, changed_by, created_at
		 FROM action_versions WHERE action_id = ? AND version = ?`,
		actionID, version,
	).Scan(&v.ID, &v.ActionID, &v.Version, &v.Name, &v.Description, &v.Category, &v.Script, &v.ScriptType, &v.Platform, &paramsRaw, &v.ChangedBy, &v.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("get action version: %w", err)
	}
	v.Parameters = scanActionParameters(paramsRaw)
	return &v, nil
}

// RollbackAction restores an action's content to a prior version. It never
// rewrites history: the current content is snapshotted (superseded) first,
// and the restored content becomes a brand new version number — a forward
// move, like a revert, not a reset.
func (db *DB) RollbackAction(actionID int64, targetVersion int, changedBy *int64) (*models.Action, error) {
	current, err := db.GetAction(actionID)
	if err != nil {
		return nil, fmt.Errorf("action not found: %w", err)
	}
	if current.Builtin {
		return nil, fmt.Errorf("cannot roll back builtin actions")
	}
	if targetVersion == current.Version {
		return nil, fmt.Errorf("action is already at version %d", targetVersion)
	}

	target, err := db.GetActionVersion(actionID, targetVersion)
	if err != nil {
		return nil, fmt.Errorf("version %d not found: %w", targetVersion, err)
	}

	restored := &models.Action{
		ID:          actionID,
		Name:        target.Name,
		Description: target.Description,
		Category:    target.Category,
		Script:      target.Script,
		ScriptType:  target.ScriptType,
		Platform:    target.Platform,
		Parameters:  target.Parameters,
	}
	if err := db.UpdateAction(restored, changedBy); err != nil {
		return nil, fmt.Errorf("apply rollback: %w", err)
	}
	restored.Builtin = current.Builtin
	return restored, nil
}

func (db *DB) DeleteAction(id int64) error {
	result, err := db.conn.Exec(`DELETE FROM actions WHERE id = ? AND builtin = 0`, id)
	if err != nil {
		return fmt.Errorf("delete action: %w", err)
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return fmt.Errorf("action not found or is builtin")
	}
	return nil
}

func (db *DB) SetDeploymentActions(deploymentID int64, actionIDs []int64) error {
	tx, err := db.conn.Begin()
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`DELETE FROM deployment_actions WHERE deployment_id = ?`, deploymentID); err != nil {
		return fmt.Errorf("clear deployment actions: %w", err)
	}

	for i, actionID := range actionIDs {
		if _, err := tx.Exec(`INSERT INTO deployment_actions (deployment_id, action_id, sort_order) VALUES (?, ?, ?)`, deploymentID, actionID, i); err != nil {
			return fmt.Errorf("insert deployment action: %w", err)
		}
	}

	return tx.Commit()
}

func (db *DB) GetDeploymentActions(deploymentID int64) ([]models.Action, error) {
	rows, err := db.conn.Query(
		`SELECT a.id, a.name, a.description, a.category, a.script, a.script_type, a.platform, a.builtin, a.parameters, a.created_at, a.updated_at
		 FROM actions a
		 JOIN deployment_actions da ON da.action_id = a.id
		 WHERE da.deployment_id = ?
		 ORDER BY da.sort_order`,
		deploymentID,
	)
	if err != nil {
		return nil, fmt.Errorf("get deployment actions: %w", err)
	}
	defer rows.Close()

	var actions []models.Action
	for rows.Next() {
		var a models.Action
		var builtin int
		var paramsRaw sql.NullString
		if err := rows.Scan(&a.ID, &a.Name, &a.Description, &a.Category, &a.Script, &a.ScriptType, &a.Platform, &builtin, &paramsRaw, &a.CreatedAt, &a.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan deployment action: %w", err)
		}
		a.Builtin = builtin == 1
		a.Parameters = scanActionParameters(paramsRaw)
		actions = append(actions, a)
	}
	return actions, nil
}
