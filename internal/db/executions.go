package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/forgemill/forgemill/internal/db/models"
)

func scanParameterValues(raw sql.NullString) map[string]string {
	if !raw.Valid || raw.String == "" {
		return nil
	}
	var vals map[string]string
	if err := json.Unmarshal([]byte(raw.String), &vals); err != nil {
		return nil
	}
	return vals
}

func marshalParameterValues(vals map[string]string) sql.NullString {
	if len(vals) == 0 {
		return sql.NullString{}
	}
	data, err := json.Marshal(vals)
	if err != nil {
		return sql.NullString{}
	}
	return sql.NullString{String: string(data), Valid: true}
}

func (db *DB) CreateExecution(e *models.ActionExecution) error {
	pvJSON := marshalParameterValues(e.ParameterValues)
	result, err := db.conn.Exec(
		`INSERT INTO action_executions (vm_id, action_id, action_name, script, status, parameter_values, created_by, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		e.VMID, e.ActionID, e.ActionName, e.Script, e.Status, pvJSON, e.CreatedBy, time.Now().UTC(),
	)
	if err != nil {
		return fmt.Errorf("create execution: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return fmt.Errorf("get execution id: %w", err)
	}
	e.ID = id
	return nil
}

func (db *DB) GetExecution(id int64) (*models.ActionExecution, error) {
	e := &models.ActionExecution{}
	var pvRaw sql.NullString
	err := db.conn.QueryRow(
		`SELECT id, vm_id, action_id, action_name, script, status, exit_code, output, parameter_values, started_at, completed_at, created_by, created_at
		 FROM action_executions WHERE id = ?`, id,
	).Scan(&e.ID, &e.VMID, &e.ActionID, &e.ActionName, &e.Script, &e.Status, &e.ExitCode, &e.Output, &pvRaw, &e.StartedAt, &e.CompletedAt, &e.CreatedBy, &e.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("get execution: %w", err)
	}
	e.ParameterValues = scanParameterValues(pvRaw)
	return e, nil
}

func (db *DB) ListVMExecutions(vmID int64) ([]models.ActionExecution, error) {
	rows, err := db.conn.Query(
		`SELECT id, vm_id, action_id, action_name, script, status, exit_code, output, parameter_values, started_at, completed_at, created_by, created_at
		 FROM action_executions WHERE vm_id = ? ORDER BY created_at DESC`, vmID,
	)
	if err != nil {
		return nil, fmt.Errorf("list vm executions: %w", err)
	}
	defer rows.Close()

	var execs []models.ActionExecution
	for rows.Next() {
		var e models.ActionExecution
		var pvRaw sql.NullString
		if err := rows.Scan(&e.ID, &e.VMID, &e.ActionID, &e.ActionName, &e.Script, &e.Status, &e.ExitCode, &e.Output, &pvRaw, &e.StartedAt, &e.CompletedAt, &e.CreatedBy, &e.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan execution: %w", err)
		}
		e.ParameterValues = scanParameterValues(pvRaw)
		execs = append(execs, e)
	}
	return execs, nil
}

func (db *DB) ListRecentExecutions(limit int) ([]models.ActionExecution, error) {
	rows, err := db.conn.Query(
		`SELECT id, vm_id, action_id, action_name, script, status, exit_code, output, parameter_values, started_at, completed_at, created_by, created_at
		 FROM action_executions ORDER BY created_at DESC LIMIT ?`, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("list recent executions: %w", err)
	}
	defer rows.Close()

	var execs []models.ActionExecution
	for rows.Next() {
		var e models.ActionExecution
		var pvRaw sql.NullString
		if err := rows.Scan(&e.ID, &e.VMID, &e.ActionID, &e.ActionName, &e.Script, &e.Status, &e.ExitCode, &e.Output, &pvRaw, &e.StartedAt, &e.CompletedAt, &e.CreatedBy, &e.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan execution: %w", err)
		}
		e.ParameterValues = scanParameterValues(pvRaw)
		execs = append(execs, e)
	}
	return execs, nil
}

func (db *DB) UpdateExecutionStatus(id int64, status string, exitCode *int, output string) error {
	var completedAt *time.Time
	if status == "completed" || status == "failed" || status == "cancelled" {
		now := time.Now().UTC()
		completedAt = &now
	}
	var startedAt *time.Time
	if status == "running" {
		now := time.Now().UTC()
		startedAt = &now
	}

	if startedAt != nil {
		_, err := db.conn.Exec(
			`UPDATE action_executions SET status = ?, exit_code = ?, output = ?, started_at = ? WHERE id = ?`,
			status, exitCode, output, startedAt, id,
		)
		if err != nil {
			return fmt.Errorf("update execution status: %w", err)
		}
		return nil
	}

	_, err := db.conn.Exec(
		`UPDATE action_executions SET status = ?, exit_code = ?, output = ?, completed_at = ? WHERE id = ?`,
		status, exitCode, output, completedAt, id,
	)
	if err != nil {
		return fmt.Errorf("update execution status: %w", err)
	}
	return nil
}

func (db *DB) AppendExecutionOutput(id int64, text string) error {
	_, err := db.conn.Exec(
		`UPDATE action_executions SET output = output || ? WHERE id = ?`,
		text, id,
	)
	if err != nil {
		return fmt.Errorf("append execution output: %w", err)
	}
	return nil
}

func (db *DB) CountRunningExecutionsForVM(vmID int64) (int, error) {
	var count int
	err := db.conn.QueryRow(
		`SELECT COUNT(*) FROM action_executions WHERE vm_id = ? AND status IN ('pending', 'running')`, vmID,
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count running executions: %w", err)
	}
	return count, nil
}

func (db *DB) CountRunningExecutionsTotal() (int, error) {
	var count int
	err := db.conn.QueryRow(
		`SELECT COUNT(*) FROM action_executions WHERE status IN ('pending', 'running')`,
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count total running executions: %w", err)
	}
	return count, nil
}
