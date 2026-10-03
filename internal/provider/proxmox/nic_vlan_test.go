package proxmox

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/forgemill/forgemill/internal/provider"
)

func TestAddNICRefusesVLANTagOnABridgeThatIsNotVLANAware(t *testing.T) {
	f := newFakePVE(t) // vmbr0, vmbr1; neither VLAN aware
	p := f.provider(t)

	_, err := p.AddNIC(context.Background(), "100", provider.NICSpec{Network: "vmbr0", Connected: true, VLANTag: 150})
	if !errors.Is(err, provider.ErrVLANUnsupportedOnNetwork) || !strings.Contains(err.Error(), "not VLAN aware") {
		t.Fatalf("want ErrVLANUnsupportedOnNetwork, got %v", err)
	}
	if len(f.puts) != 0 {
		t.Errorf("nothing may be written for a config Proxmox cannot boot: %v", f.puts)
	}

	f.vlanAware = []string{"vmbr0"}
	nic, err := p.AddNIC(context.Background(), "100", provider.NICSpec{Network: "vmbr0", Connected: true, VLANTag: 150})
	if err != nil || nic.VLANTag != 150 {
		t.Errorf("VLAN tag on a VLAN-aware bridge must work: nic=%+v err=%v", nic, err)
	}
}

func TestAddNICHotplugFailureRemovesThePendingConfigAndNamesTheReason(t *testing.T) {
	f := newFakePVE(t)
	f.hotplugFails = true
	p := f.provider(t)

	_, err := p.AddNIC(context.Background(), "100", provider.NICSpec{Network: "vmbr1", Connected: true})
	if err == nil || !strings.Contains(err.Error(), "hot-plug failed on the running VM") || !strings.Contains(err.Error(), "netdev_add") {
		t.Fatalf("want the hot-plug reason, got %v", err)
	}
	if len(f.deletes) != 1 || f.deletes[0] != "net1" {
		t.Errorf("the pending net1 must be removed so the VM can still boot, deletes=%v", f.deletes)
	}
	if _, present := f.config["net1"]; present {
		t.Error("net1 should be gone from the config after the revert")
	}
}

func TestProxmoxExtrasValidatorChecksBridgeVLANAndStorage(t *testing.T) {
	f := newFakePVE(t)
	f.vlanAware = []string{"vmbr1"}
	p := f.provider(t)
	ctx := context.Background()

	if err := p.ValidateNICSpec(ctx, "", provider.NICSpec{Network: "vmbr1", VLANTag: 20}); err != nil {
		t.Errorf("tagged NIC on a VLAN-aware bridge is valid: %v", err)
	}
	if err := p.ValidateNICSpec(ctx, "", provider.NICSpec{Network: "vmbr0", VLANTag: 20}); !errors.Is(err, provider.ErrVLANUnsupportedOnNetwork) {
		t.Errorf("tagged NIC on vmbr0 must be refused: %v", err)
	}
	if err := p.ValidateNICSpec(ctx, "", provider.NICSpec{Network: "vmbr9"}); !errors.Is(err, provider.ErrNetworkNotFound) {
		t.Errorf("unknown bridge: %v", err)
	}
	if err := p.ValidateDiskSpec(ctx, "", provider.DiskSpec{SizeGB: 5, Datastore: "local"}); err != nil {
		t.Errorf("known storage: %v", err)
	}
	if err := p.ValidateDiskSpec(ctx, "", provider.DiskSpec{SizeGB: 5, Datastore: "nope"}); !errors.Is(err, provider.ErrDatastoreNotFound) {
		t.Errorf("unknown storage: %v", err)
	}
}
