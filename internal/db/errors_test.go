package db

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/forgemill/forgemill/internal/db/models"
)

func openTestDB(t *testing.T) *DB {
	t.Helper()
	d, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

func TestUpdateUserRoleRejectsUnknownRoleWithSentinel(t *testing.T) {
	d := openTestDB(t)
	err := d.UpdateUserRole(1, "superuser")
	if !errors.Is(err, ErrInvalidRole) {
		t.Fatalf("expected ErrInvalidRole, got %v", err)
	}
	// The human-readable text the API relied on matching is preserved.
	if got := err.Error(); got != `invalid role "superuser": must be admin, user, or viewer` {
		t.Errorf("message changed: %q", got)
	}
}

func TestCreateManagedVMDuplicateRefIsSentinelViaSQLiteErrorCode(t *testing.T) {
	d := openTestDB(t)
	target := &models.Target{Name: "t", Type: "esxi", Hostname: "h", Port: 443, Username: "u", PasswordEncrypt: "x"}
	if err := d.CreateTarget(target); err != nil {
		t.Fatalf("create target: %v", err)
	}
	vm := &models.ManagedVM{TargetID: target.ID, VMName: "a", VMRef: "vm-1"}
	if err := d.CreateManagedVM(vm); err != nil {
		t.Fatalf("first create: %v", err)
	}
	dup := &models.ManagedVM{TargetID: target.ID, VMName: "b", VMRef: "vm-1"}
	err := d.CreateManagedVM(dup)
	if !errors.Is(err, ErrAlreadyRegistered) {
		t.Fatalf("expected ErrAlreadyRegistered from the UNIQUE violation, got %v", err)
	}
}

func TestIsUniqueConstraintErrorIgnoresOtherErrors(t *testing.T) {
	if isUniqueConstraintError(errors.New("UNIQUE constraint failed: looks like one but isn't a driver error")) {
		t.Error("text that merely resembles the driver message must not count")
	}
	if isUniqueConstraintError(nil) {
		t.Error("nil is not a constraint error")
	}
}
