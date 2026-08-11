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
