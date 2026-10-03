package proxmox

import (
	"encoding/json"
	"testing"
)

// Proxmox returns the same key as a number or a numeric string depending on
// version; the accessors must read both and never panic on the unexpected.
func TestQemuConfigAccessorsCoerceEveryShapeProxmoxUses(t *testing.T) {
	var cfg qemuConfig
	if err := json.Unmarshal([]byte(`{
		"name": "web-01", "ostype": "l26", "cores": 2, "sockets": "2", "memory": "4096",
		"agent": "1", "net0": "virtio=BC:24:11:AA:BB:01,bridge=vmbr0,tag=20",
		"scsi0": "local-lvm:vm-100-disk-0,size=32G", "bogus": true, "nested": {"a": 1}
	}`), &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Str("name") != "web-01" || cfg.Str("ostype") != "l26" {
		t.Errorf("Str: %q %q", cfg.Str("name"), cfg.Str("ostype"))
	}
	if cfg.Int("cores") != 2 || cfg.Int("sockets") != 2 || cfg.Int("memory") != 4096 || cfg.Int("agent") != 1 {
		t.Errorf("Int: cores=%d sockets=%d memory=%d agent=%d", cfg.Int("cores"), cfg.Int("sockets"), cfg.Int("memory"), cfg.Int("agent"))
	}
	if !cfg.Has("net0") || cfg.Has("net1") || !cfg.Has("scsi0") {
		t.Error("Has: slot presence wrong")
	}
	// Wrong-typed and absent keys degrade to zero values, never panic.
	if cfg.Str("cores") != "" || cfg.Str("bogus") != "" || cfg.Str("nested") != "" || cfg.Str("missing") != "" {
		t.Error("Str on non-string values must be empty")
	}
	if cfg.Int("name") != 0 || cfg.Int("bogus") != 0 || cfg.Int("missing") != 0 || cfg.Int("net0") != 0 {
		t.Error("Int on non-numeric values must be 0")
	}
	// "agent": "enabled=1,fstrim_cloned_disks=1" is not a bare number — stays 0, as before.
	cfg["agent"] = "enabled=1,fstrim_cloned_disks=1"
	if cfg.Int("agent") != 0 {
		t.Error("structured agent value must still read as 0 (unchanged behaviour)")
	}
}
