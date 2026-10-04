package db

import (
	"testing"

	"github.com/forgemill/forgemill/internal/db/models"
)

func TestVMCredentialUpsertGetDeleteAndCascade(t *testing.T) {
	d := openTestDB(t)
	if _, err := d.conn.Exec(`INSERT INTO targets (name, type, hostname, username, password_encrypted) VALUES ('t', 'esxi', 'h', 'u', 'p')`); err != nil {
		t.Fatal(err)
	}
	vm := &models.ManagedVM{TargetID: 1, VMRef: "vm-1", VMName: "a"}
	if err := d.CreateManagedVM(vm); err != nil {
		t.Fatal(err)
	}
	got, err := d.GetVMCredential(vm.ID)
	if err != nil || got != nil {
		t.Fatalf("unset credential: got %+v err %v, want nil, nil", got, err)
	}
	if err := d.SetVMCredential(&models.VMCredential{VMID: vm.ID, Username: "root", Kind: "password", SecretEnc: "enc1"}); err != nil {
		t.Fatal(err)
	}
	if err := d.SetVMCredential(&models.VMCredential{VMID: vm.ID, Username: "ops", Kind: "private_key", SecretEnc: "enc2"}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	got, err = d.GetVMCredential(vm.ID)
	if err != nil || got == nil || got.Username != "ops" || got.Kind != "private_key" || got.SecretEnc != "enc2" {
		t.Fatalf("after upsert: %+v err %v", got, err)
	}
	if err := d.DeleteVMCredential(vm.ID); err != nil {
		t.Fatal(err)
	}
	if got, _ = d.GetVMCredential(vm.ID); got != nil {
		t.Fatalf("still present after delete: %+v", got)
	}
	if err := d.DeleteVMCredential(vm.ID); err != nil {
		t.Fatalf("second delete should be a no-op: %v", err)
	}
	// Removing the VM removes its credential.
	if err := d.SetVMCredential(&models.VMCredential{VMID: vm.ID, Username: "root", Kind: "password", SecretEnc: "enc"}); err != nil {
		t.Fatal(err)
	}
	if err := d.DeleteManagedVM(vm.ID); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := d.conn.QueryRow(`SELECT COUNT(*) FROM vm_credentials`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("credential survived VM delete: %d rows", n)
	}
}
