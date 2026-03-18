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
	rows, err := db.conn.Query(`SELECT id, name, description, category, script, script_type, platform, builtin, parameters, created_at, updated_at FROM actions ORDER BY category, name`)
	if err != nil {
		return nil, fmt.Errorf("list actions: %w", err)
	}
	defer rows.Close()

	var actions []models.Action
	for rows.Next() {
		var a models.Action
		var builtin int
		var paramsRaw sql.NullString
		if err := rows.Scan(&a.ID, &a.Name, &a.Description, &a.Category, &a.Script, &a.ScriptType, &a.Platform, &builtin, &paramsRaw, &a.CreatedAt, &a.UpdatedAt); err != nil {
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
	err := db.conn.QueryRow(`SELECT id, name, description, category, script, script_type, platform, builtin, parameters, created_at, updated_at FROM actions WHERE id = ?`, id).
		Scan(&a.ID, &a.Name, &a.Description, &a.Category, &a.Script, &a.ScriptType, &a.Platform, &builtin, &paramsRaw, &a.CreatedAt, &a.UpdatedAt)
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
		`INSERT INTO actions (name, description, category, script, script_type, platform, builtin, parameters, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, 0, ?, ?, ?)`,
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
	a.CreatedAt = now
	a.UpdatedAt = now
	return nil
}

func (db *DB) UpdateAction(a *models.Action) error {
	now := time.Now().UTC().Format(time.DateTime)
	if a.ScriptType == "" {
		a.ScriptType = "bash"
	}
	if a.Platform == "" {
		a.Platform = "linux"
	}
	paramsJSON := marshalParameters(a.Parameters)
	_, err := db.conn.Exec(
		`UPDATE actions SET name = ?, description = ?, category = ?, script = ?, script_type = ?, platform = ?, parameters = ?, updated_at = ? WHERE id = ? AND builtin = 0`,
		a.Name, a.Description, a.Category, a.Script, a.ScriptType, a.Platform, paramsJSON, now, a.ID,
	)
	if err != nil {
		return fmt.Errorf("update action: %w", err)
	}
	a.UpdatedAt = now
	return nil
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
