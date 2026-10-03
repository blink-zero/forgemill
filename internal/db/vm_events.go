package db

import (
	"database/sql"
	"time"

	"github.com/forgemill/forgemill/internal/db/models"
)

// AddVMEvent records a provider/service event for a VM. vm_id is not a
// foreign key on purpose: events must survive the VM row being deleted
// (a destroy's own warnings are the most useful ones).
func (db *DB) AddVMEvent(vmID, targetID int64, level, message string) error {
	_, err := db.conn.Exec(`INSERT INTO vm_events (vm_id, target_id, level, message) VALUES (?, ?, ?, ?)`, vmID, targetID, level, message)
	return err
}

// ListVMEvents returns the newest events for one VM, newest first.
func (db *DB) ListVMEvents(vmID int64, limit int) ([]models.VMEvent, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := db.conn.Query(`SELECT id, vm_id, COALESCE(target_id, 0), level, message, created_at FROM vm_events WHERE vm_id = ? ORDER BY created_at DESC, id DESC LIMIT ?`, vmID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanVMEvents(rows)
}

// ListRecentVMEvents returns the newest events across all VMs (for the
// diagnostics view), optionally only warn/error.
func (db *DB) ListRecentVMEvents(limit int, problemsOnly bool) ([]models.VMEvent, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	q := `SELECT id, vm_id, COALESCE(target_id, 0), level, message, created_at FROM vm_events`
	if problemsOnly {
		q += ` WHERE level IN ('warn', 'error')`
	}
	q += ` ORDER BY created_at DESC, id DESC LIMIT ?`
	rows, err := db.conn.Query(q, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanVMEvents(rows)
}

// PruneVMEvents deletes events older than the retention window.
func (db *DB) PruneVMEvents(olderThan time.Duration) (int64, error) {
	res, err := db.conn.Exec(`DELETE FROM vm_events WHERE created_at < ?`, time.Now().Add(-olderThan).UTC().Format("2006-01-02 15:04:05"))
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func scanVMEvents(rows *sql.Rows) ([]models.VMEvent, error) {
	events := []models.VMEvent{}
	for rows.Next() {
		var e models.VMEvent
		if err := rows.Scan(&e.ID, &e.VMID, &e.TargetID, &e.Level, &e.Message, &e.CreatedAt); err != nil {
			return nil, err
		}
		events = append(events, e)
	}
	return events, rows.Err()
}
