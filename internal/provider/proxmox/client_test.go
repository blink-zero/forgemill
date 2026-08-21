package proxmox

import (
	"context"
	"testing"

	"github.com/forgemill/forgemill/internal/provider"
)

func TestValidateDeploySpecIsANoOp(t *testing.T) {
	// Proxmox's DeployVM passes Network/Datastore straight through as literal
	// API field values, unlike vCenter/ESXi's inventory-path finder — so
	// there's no separate resolver here to fall out of sync with
	// GetResources. This pins that intentional no-op so a future change
	// doesn't silently start rejecting valid Proxmox deploys.
	p := &Provider{}
	spec := &provider.DeploySpec{Network: "vmbr0", Datastore: "local-lvm"}
	if errs := p.ValidateDeploySpec(context.Background(), spec); errs != nil {
		t.Errorf("expected no errors, got %v", errs)
	}
}

func TestBuildNet0ConfigUntaggedWhenNoVLAN(t *testing.T) {
	got := buildNet0Config("vmbr0", 0)
	want := "virtio,bridge=vmbr0"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestBuildNet0ConfigAppendsTagWhenVLANSet(t *testing.T) {
	got := buildNet0Config("vmbr0", 150)
	want := "virtio,bridge=vmbr0,tag=150"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestBuildNet0ConfigIgnoresNegativeVLAN(t *testing.T) {
	// Defense in depth: validateDeployRequest already rejects this before it
	// gets here, but the builder itself should never emit a nonsensical tag.
	got := buildNet0Config("vmbr0", -1)
	want := "virtio,bridge=vmbr0"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
