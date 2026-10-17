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

// VMCredentialMeta is what the credential-export preview needs to count a
// VM's explicit login without decrypting anything.
type VMCredentialMeta struct {
	Kind            string
	HasSudoPassword bool
}

// ListVMCredentialMeta returns, per VM id, the kind of explicit credential
// stored and whether a separate sudo password exists. No secret material is
// read.
func (db *DB) ListVMCredentialMeta() (map[int64]VMCredentialMeta, error) {
	rows, err := db.conn.Query(`SELECT vm_id, kind, COALESCE(sudo_password_enc, '') != '' FROM vm_credentials`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]VMCredentialMeta{}
	for rows.Next() {
		var id int64
		var m VMCredentialMeta
		if err := rows.Scan(&id, &m.Kind, &m.HasSudoPassword); err != nil {
			return nil, err
		}
		out[id] = m
	}
	return out, rows.Err()
}

// ListDeploymentsWithInitialCredentials returns the ids of deployments that
// still hold the cloud-init login they were created with, so the export
// preview can count VMs whose only login is the inherited one.
func (db *DB) ListDeploymentsWithInitialCredentials() (map[int64]bool, error) {
	rows, err := db.conn.Query(`SELECT id FROM deployments WHERE COALESCE(initial_username, '') != '' AND COALESCE(initial_password_enc, '') != ''`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]bool{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = true
	}
	return out, rows.Err()
}
