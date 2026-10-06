package db

import (
	"database/sql"
	"errors"
	"time"

	"github.com/forgemill/forgemill/internal/db/models"
)

// GetVMCredential returns the explicitly set credential for a VM, or nil
// (no error) when none has been set.
func (db *DB) GetVMCredential(vmID int64) (*models.VMCredential, error) {
	c := &models.VMCredential{}
	err := db.conn.QueryRow(`SELECT vm_id, username, kind, secret_enc, COALESCE(sudo_password_enc, ''), set_by, updated_at FROM vm_credentials WHERE vm_id = ?`, vmID).
		Scan(&c.VMID, &c.Username, &c.Kind, &c.SecretEnc, &c.SudoPasswordEnc, &c.SetBy, &c.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return c, nil
}

// SetVMCredential inserts or replaces the credential for a VM.
func (db *DB) SetVMCredential(c *models.VMCredential) error {
	c.UpdatedAt = time.Now()
	_, err := db.conn.Exec(`INSERT INTO vm_credentials (vm_id, username, kind, secret_enc, sudo_password_enc, set_by, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(vm_id) DO UPDATE SET username = excluded.username, kind = excluded.kind, secret_enc = excluded.secret_enc, sudo_password_enc = excluded.sudo_password_enc, set_by = excluded.set_by, updated_at = excluded.updated_at`,
		c.VMID, c.Username, c.Kind, c.SecretEnc, c.SudoPasswordEnc, c.SetBy, c.UpdatedAt)
	return err
}

// DeleteVMCredential removes the explicit credential; the VM falls back to
// its deployment credentials, if any. Deleting a missing row is not an error.
func (db *DB) DeleteVMCredential(vmID int64) error {
	_, err := db.conn.Exec(`DELETE FROM vm_credentials WHERE vm_id = ?`, vmID)
	return err
}
