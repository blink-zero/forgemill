package service

import (
	"context"
	"testing"

	"github.com/forgemill/forgemill/internal/crypto"
	"github.com/forgemill/forgemill/internal/db/models"
	"github.com/forgemill/forgemill/internal/provider"
)

// tofuProbe is a provider that records the TOFU wiring the service gives it.
type tofuProbe struct {
	provider.Provider
	targetID int64
	store    provider.TargetHostKeyStore
}

func (t *tofuProbe) SetTOFU(targetID int64, store provider.TargetHostKeyStore) {
	t.targetID, t.store = targetID, store
}

var _ provider.HostKeyTrusting = (*tofuProbe)(nil)

func TestGetProviderWiresHostKeyTOFUForProvidersThatSupportIt(t *testing.T) {
	database := newTestDB(t)
	enc, err := crypto.NewEncryptor(nicTestKey)
	if err != nil {
		t.Fatal(err)
	}
	encPW, _ := enc.Encrypt("secret")
	// The targets table only allows the in-tree types, so stand in for the
	// Proxmox factory for the duration of the test.
	target := &models.Target{Name: "pve", Type: "proxmox", Hostname: "pve.example.com", Port: 8006, Username: "root@pam", PasswordEncrypt: encPW}
	if err := database.CreateTarget(target); err != nil {
		t.Fatal(err)
	}

	probe := &tofuProbe{}
	orig := provider.GetProviderFactory("proxmox")
	provider.RegisterProvider("proxmox", func(string, int, string, string, bool) provider.Provider { return probe })
	t.Cleanup(func() { provider.RegisterProvider("proxmox", orig) })

	svc := NewTargetService(database, enc)
	p, err := svc.getProvider(target.ID)
	if err != nil {
		t.Fatalf("getProvider: %v", err)
	}
	if p != probe {
		t.Fatalf("expected the registered provider back, got %T", p)
	}
	if probe.targetID != target.ID {
		t.Errorf("SetTOFU target ID = %d, want %d", probe.targetID, target.ID)
	}
	if probe.store == nil {
		t.Fatal("SetTOFU must receive the fingerprint store")
	}
	// The store the provider got is the real targets table.
	if err := probe.store.UpdateTargetSSHHostKeyFP(target.ID, "SHA256:abc"); err != nil {
		t.Fatal(err)
	}
	if fp, err := probe.store.GetTargetSSHHostKeyFP(target.ID); err != nil || fp != "SHA256:abc" {
		t.Errorf("store round-trip: fp=%q err=%v", fp, err)
	}
}

func TestGetProviderLeavesProvidersWithoutTOFUAlone(t *testing.T) {
	// The vSphere provider does not implement HostKeyTrusting; the wiring is
	// a type assertion, so it must simply be skipped.
	database := newTestDB(t)
	enc, _ := crypto.NewEncryptor(nicTestKey)
	encPW, _ := enc.Encrypt("secret")
	target := &models.Target{Name: "vc", Type: "vcenter", Hostname: "vc.example.com", Port: 443, Username: "u", PasswordEncrypt: encPW}
	if err := database.CreateTarget(target); err != nil {
		t.Fatal(err)
	}
	p, err := NewTargetService(database, enc).getProvider(target.ID)
	if err != nil || p == nil {
		t.Fatalf("getProvider: %v", err)
	}
	if _, ok := p.(provider.HostKeyTrusting); ok {
		t.Error("vcenter provider unexpectedly claims TOFU support")
	}
	_ = context.Background()
}
