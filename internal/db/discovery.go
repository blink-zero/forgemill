package db

import (
	"fmt"
	"strings"
	"time"

	"github.com/forgemill/forgemill/internal/db/models"
)

// vmOrigin defaults a ManagedVM's origin to "deployed" when the caller
// didn't set one (every pre-existing insert path).
func vmOrigin(vm *models.ManagedVM) string {
	if vm.Origin == "" {
		return models.VMOriginDeployed
	}
	return vm.Origin
}

// UpdateTargetUnmanaged records how many unmanaged, non-ignored VMs the last
// sync saw on a target.
func (db *DB) UpdateTargetUnmanaged(targetID int64, n int) error {
	_, err := db.conn.Exec(`UPDATE targets SET unmanaged_vms = ?, unmanaged_checked_at = ? WHERE id = ?`, n, time.Now().UTC(), targetID)
	return err
}

// ListIgnoredVMs returns the discovery ignore list for a target.
func (db *DB) ListIgnoredVMs(targetID int64) ([]models.IgnoredVM, error) {
	rows, err := db.conn.Query(`SELECT target_id, vm_ref, vm_name, ignored_by, created_at FROM target_ignored_vms WHERE target_id = ? ORDER BY vm_name, vm_ref`, targetID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.IgnoredVM{}
	for rows.Next() {
		var i models.IgnoredVM
		if err := rows.Scan(&i.TargetID, &i.VMRef, &i.VMName, &i.IgnoredBy, &i.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, i)
	}
	return out, rows.Err()
}

// IgnoreVMs adds refs to a target's ignore list (idempotent; names refresh).
func (db *DB) IgnoreVMs(targetID int64, refs map[string]string, by *int64) error {
	tx, err := db.conn.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for ref, name := range refs {
		if strings.TrimSpace(ref) == "" {
			continue
		}
		if _, err := tx.Exec(`INSERT INTO target_ignored_vms (target_id, vm_ref, vm_name, ignored_by) VALUES (?, ?, ?, ?)
			ON CONFLICT(target_id, vm_ref) DO UPDATE SET vm_name = excluded.vm_name`, targetID, ref, name, by); err != nil {
			return fmt.Errorf("ignore %s: %w", ref, err)
		}
	}
	return tx.Commit()
}

// UnignoreVMs removes refs from a target's ignore list; unknown refs are a no-op.
func (db *DB) UnignoreVMs(targetID int64, refs []string) error {
	tx, err := db.conn.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, ref := range refs {
		if _, err := tx.Exec(`DELETE FROM target_ignored_vms WHERE target_id = ? AND vm_ref = ?`, targetID, ref); err != nil {
			return err
		}
	}
	return tx.Commit()
}
