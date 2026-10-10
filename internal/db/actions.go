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

func scanTags(raw sql.NullString) []string {
	if !raw.Valid || raw.String == "" {
		return nil
	}
	var tags []string
	if err := json.Unmarshal([]byte(raw.String), &tags); err != nil {
		return nil
	}
	return tags
}

func marshalTags(tags []string) sql.NullString {
	if len(tags) == 0 {
		return sql.NullString{}
	}
	data, err := json.Marshal(tags)
	if err != nil {
		return sql.NullString{}
	}
	return sql.NullString{String: string(data), Valid: true}
}

// actionColumns is the one column list every action read uses.
const actionColumns = `id, name, description, category, script, script_type, platform, builtin, parameters, tags, version, created_at, updated_at,
	COALESCE(status, 'active'), COALESCE(source, 'user'), created_by, review_json, COALESCE(reviewed_at, ''), draft_meta_json`

func scanAction(r rowScanner) (models.Action, error) {
	var a models.Action
	var builtin int
	var paramsRaw, tagsRaw, reviewRaw, metaRaw sql.NullString
	if err := r.Scan(&a.ID, &a.Name, &a.Description, &a.Category, &a.Script, &a.ScriptType, &a.Platform, &builtin, &paramsRaw, &tagsRaw, &a.Version, &a.CreatedAt, &a.UpdatedAt,
		&a.Status, &a.Source, &a.CreatedBy, &reviewRaw, &a.ReviewedAt, &metaRaw); err != nil {
		return a, err
	}
	a.Builtin = builtin == 1
	a.Parameters = scanActionParameters(paramsRaw)
	a.Tags = scanTags(tagsRaw)
	if reviewRaw.Valid && reviewRaw.String != "" {
		a.Review = json.RawMessage(reviewRaw.String)
	}
	if metaRaw.Valid && metaRaw.String != "" {
		a.DraftMeta = json.RawMessage(metaRaw.String)
	}
	return a, nil
}

// ListActions returns runnable (active) actions only — what the deploy
// picker, the VM Actions tab, export and the MCP should see.
func (db *DB) ListActions() ([]models.Action, error) {
	return db.listActions(`WHERE COALESCE(status, 'active') = 'active'`)
}

// ListActionsWithDrafts returns every action, drafts included (the Actions
// page, where drafts are shown under their own filter).
func (db *DB) ListActionsWithDrafts() ([]models.Action, error) {
	return db.listActions(``)
}

func (db *DB) listActions(where string) ([]models.Action, error) {
	rows, err := db.conn.Query(`SELECT ` + actionColumns + ` FROM actions ` + where + ` ORDER BY category, name`)
	if err != nil {
		return nil, fmt.Errorf("list actions: %w", err)
	}
	defer rows.Close()
	var actions []models.Action
	for rows.Next() {
		a, err := scanAction(rows)
		if err != nil {
			return nil, fmt.Errorf("scan action: %w", err)
		}
		actions = append(actions, a)
	}
	return actions, rows.Err()
}

func (db *DB) GetAction(id int64) (*models.Action, error) {
	a, err := scanAction(db.conn.QueryRow(`SELECT `+actionColumns+` FROM actions WHERE id = ?`, id))
	if err != nil {
		return nil, fmt.Errorf("get action: %w", err)
	}
	return &a, nil
}

// SetActionReview stores the last check of an action.
func (db *DB) SetActionReview(id int64, review json.RawMessage) error {
	_, err := db.conn.Exec(`UPDATE actions SET review_json = ?, reviewed_at = ? WHERE id = ?`, string(review), time.Now().UTC().Format(time.DateTime), id)
	return err
}

// PublishAction turns a draft into a runnable action. Versioning starts
// here: the published content is version 1.
func (db *DB) PublishAction(id int64) error {
	res, err := db.conn.Exec(`UPDATE actions SET status = 'active', version = 1, updated_at = ? WHERE id = ? AND status = 'draft'`, time.Now().UTC().Format(time.DateTime), id)
	if err != nil {
		return fmt.Errorf("publish action: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("action %d is not a draft", id)
	}
	return nil
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
	if a.Status == "" {
		a.Status = models.ActionStatusActive
	}
	if a.Source == "" {
		a.Source = models.ActionSourceUser
	}
	paramsJSON := marshalParameters(a.Parameters)
	tagsJSON := marshalTags(a.Tags)
	result, err := db.conn.Exec(
		`INSERT INTO actions (name, description, category, script, script_type, platform, builtin, parameters, tags, version, created_at, updated_at, status, source, created_by, review_json, reviewed_at, draft_meta_json)
		 VALUES (?, ?, ?, ?, ?, ?, 0, ?, ?, 1, ?, ?, ?, ?, ?, ?, ?, ?)`,
		a.Name, a.Description, a.Category, a.Script, a.ScriptType, a.Platform, paramsJSON, tagsJSON, now, now,
		a.Status, a.Source, a.CreatedBy, nullString(string(a.Review)), nullString(a.ReviewedAt), nullString(string(a.DraftMeta)),
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
	var paramsRaw, tagsRaw sql.NullString
	err = tx.QueryRow(`SELECT name, description, category, script, script_type, platform, parameters, tags, version, COALESCE(status, 'active') FROM actions WHERE id = ? AND builtin = 0`, a.ID).
		Scan(&current.Name, &current.Description, &current.Category, &current.Script, &current.ScriptType, &current.Platform, &paramsRaw, &tagsRaw, &current.Version, &current.Status)
	if err != nil {
		return fmt.Errorf("load current action: %w", err)
	}
	current.Parameters = scanActionParameters(paramsRaw)
	current.Tags = scanTags(tagsRaw)

	// Drafts are overwritten in place: versioning starts at publish.
	newVersion := current.Version
	if current.Status != models.ActionStatusDraft {
		if _, err := tx.Exec(
			`INSERT INTO action_versions (action_id, version, name, description, category, script, script_type, platform, parameters, tags, changed_by) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			a.ID, current.Version, current.Name, current.Description, current.Category, current.Script, current.ScriptType, current.Platform, marshalParameters(current.Parameters), marshalTags(current.Tags), changedBy,
		); err != nil {
			return fmt.Errorf("snapshot superseded action version: %w", err)
		}
		newVersion = current.Version + 1
	}

	now := time.Now().UTC().Format(time.DateTime)
	if a.ScriptType == "" {
		a.ScriptType = "bash"
	}
	if a.Platform == "" {
		a.Platform = "linux"
	}
	paramsJSON := marshalParameters(a.Parameters)
	tagsJSON := marshalTags(a.Tags)
	// A content change invalidates the stored check unless the caller
	// brings a fresh one (an AI regenerate does); draft notes follow the
	// same rule.
	if _, err := tx.Exec(
		`UPDATE actions SET name = ?, description = ?, category = ?, script = ?, script_type = ?, platform = ?, parameters = ?, tags = ?, version = ?, updated_at = ?,
		 review_json = ?, reviewed_at = ?, draft_meta_json = ? WHERE id = ? AND builtin = 0`,
		a.Name, a.Description, a.Category, a.Script, a.ScriptType, a.Platform, paramsJSON, tagsJSON, newVersion, now,
		nullString(string(a.Review)), nullString(a.ReviewedAt), nullString(string(a.DraftMeta)), a.ID,
	); err != nil {
		return fmt.Errorf("update action: %w", err)
	}
	a.Status = current.Status

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
		`SELECT id, action_id, version, name, description, category, script, script_type, platform, parameters, tags, changed_by, created_at
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
		var paramsRaw, tagsRaw sql.NullString
		if err := rows.Scan(&v.ID, &v.ActionID, &v.Version, &v.Name, &v.Description, &v.Category, &v.Script, &v.ScriptType, &v.Platform, &paramsRaw, &tagsRaw, &v.ChangedBy, &v.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan action version: %w", err)
		}
		v.Parameters = scanActionParameters(paramsRaw)
		v.Tags = scanTags(tagsRaw)
		versions = append(versions, v)
	}
	return versions, rows.Err()
}

// GetActionVersion returns one specific past version's content.
func (db *DB) GetActionVersion(actionID int64, version int) (*models.ActionVersion, error) {
	var v models.ActionVersion
	var paramsRaw, tagsRaw sql.NullString
	err := db.conn.QueryRow(
		`SELECT id, action_id, version, name, description, category, script, script_type, platform, parameters, tags, changed_by, created_at
		 FROM action_versions WHERE action_id = ? AND version = ?`,
		actionID, version,
	).Scan(&v.ID, &v.ActionID, &v.Version, &v.Name, &v.Description, &v.Category, &v.Script, &v.ScriptType, &v.Platform, &paramsRaw, &tagsRaw, &v.ChangedBy, &v.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("get action version: %w", err)
	}
	v.Parameters = scanActionParameters(paramsRaw)
	v.Tags = scanTags(tagsRaw)
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
		Tags:        target.Tags,
	}
	if err := db.UpdateAction(restored, changedBy); err != nil {
		return nil, fmt.Errorf("apply rollback: %w", err)
	}
	restored.Builtin = current.Builtin
	return restored, nil
}

// DeleteAction removes a saved action. action_executions.action_id has no
// ON DELETE CASCADE, so with foreign_keys=ON the plain DELETE would fail
// whenever the action has execution history. Unlike VM deletion, execution
// rows already carry their own denormalized action_name/script — so instead
// of discarding that history, this soft-unlinks it (action_id set to NULL)
// the same way DeleteTemplate soft-unlinks deployments from a deleted
// template, and the execution stays fully readable afterward.
func (db *DB) DeleteAction(id int64) error {
	tx, err := db.conn.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`UPDATE action_executions SET action_id = NULL WHERE action_id = ?`, id); err != nil {
		return fmt.Errorf("unlink action executions: %w", err)
	}
	// deployment_actions references actions without ON DELETE, so a custom
	// action that any deployment ran could never be deleted (FK failure →
	// 500). Executions keep their own action_name snapshot; the deployment
	// link rows carry nothing but the FK, so they are removed with the action.
	if _, err := tx.Exec(`DELETE FROM deployment_actions WHERE action_id = ?`, id); err != nil {
		return fmt.Errorf("unlink deployment actions: %w", err)
	}
	result, err := tx.Exec(`DELETE FROM actions WHERE id = ? AND builtin = 0`, id)
	if err != nil {
		return fmt.Errorf("delete action: %w", err)
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return fmt.Errorf("action not found or is builtin")
	}
	return tx.Commit()
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
		`SELECT a.id, a.name, a.description, a.category, a.script, a.script_type, a.platform, a.builtin, a.parameters, a.tags, a.created_at, a.updated_at
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
		var paramsRaw, tagsRaw sql.NullString
		if err := rows.Scan(&a.ID, &a.Name, &a.Description, &a.Category, &a.Script, &a.ScriptType, &a.Platform, &builtin, &paramsRaw, &tagsRaw, &a.CreatedAt, &a.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan deployment action: %w", err)
		}
		a.Builtin = builtin == 1
		a.Parameters = scanActionParameters(paramsRaw)
		a.Tags = scanTags(tagsRaw)
		actions = append(actions, a)
	}
	return actions, nil
}

func nullString(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}
