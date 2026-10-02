package proxmox

import (
	"errors"
	"fmt"
	"testing"

	"github.com/forgemill/forgemill/internal/provider"
)

func TestNormalizeNICAdapterTypeDefaultsToVirtio(t *testing.T) {
	got, err := normalizeNICAdapterType("")
	if err != nil || got != "virtio" {
		t.Fatalf("got %q, %v; want virtio", got, err)
	}
	if nicAdapterTypes[0] != "virtio" {
		t.Fatalf("default (first entry) must stay virtio, got %q", nicAdapterTypes[0])
	}
}

func TestNormalizeNICAdapterTypeAcceptsKnownModelsAndRejectsOthers(t *testing.T) {
	for in, want := range map[string]string{"VIRTIO": "virtio", " e1000 ": "e1000", "vmxnet3": "vmxnet3", "rtl8139": "rtl8139"} {
		got, err := normalizeNICAdapterType(in)
		if err != nil || got != want {
			t.Errorf("normalizeNICAdapterType(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, in := range []string{"pcnet", "virtio-net", "sriov"} {
		if _, err := normalizeNICAdapterType(in); !errors.Is(err, provider.ErrInvalidAdapterType) {
			t.Errorf("normalizeNICAdapterType(%q) = %v, want ErrInvalidAdapterType", in, err)
		}
	}
}

func TestNextFreeNetSlotPicksLowestGap(t *testing.T) {
	cases := []struct {
		name   string
		config map[string]interface{}
		want   int
	}{
		{"empty", map[string]interface{}{}, 0},
		{"net0 taken", map[string]interface{}{"net0": "virtio=AA,bridge=vmbr0"}, 1},
		{"net0+net1 taken", map[string]interface{}{"net0": "x", "net1": "y", "scsi0": "z"}, 2},
		{"gap at net1", map[string]interface{}{"net0": "x", "net2": "y"}, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := nextFreeNetSlot(tc.config)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Errorf("got net%d, want net%d", got, tc.want)
			}
		})
	}
}

func TestNextFreeNetSlotErrorsWhenFull(t *testing.T) {
	full := map[string]interface{}{}
	for i := 0; i < maxNetSlots; i++ {
		full[fmt.Sprintf("net%d", i)] = "x"
	}
	if _, err := nextFreeNetSlot(full); err == nil {
		t.Fatal("expected an error when every slot is taken")
	}
}

func TestBuildNetConfig(t *testing.T) {
	cases := []struct {
		model, bridge string
		vlan          int
		down          bool
		want          string
	}{
		{"virtio", "vmbr0", 0, false, "virtio,bridge=vmbr0"},
		{"virtio", "vmbr0", 20, false, "virtio,bridge=vmbr0,tag=20"},
		{"e1000", "vmbr1", 0, true, "e1000,bridge=vmbr1,link_down=1"},
		{"vmxnet3", "vmbr0", 4094, true, "vmxnet3,bridge=vmbr0,tag=4094,link_down=1"},
		{"virtio", "vmbr0", -5, false, "virtio,bridge=vmbr0"},
	}
	for _, tc := range cases {
		if got := buildNetConfig(tc.model, tc.bridge, tc.vlan, tc.down); got != tc.want {
			t.Errorf("buildNetConfig(%q,%q,%d,%v) = %q, want %q", tc.model, tc.bridge, tc.vlan, tc.down, got, tc.want)
		}
	}
}

func TestBuildNet0ConfigStillMatchesDeployShape(t *testing.T) {
	// DeployVM's net0 builder is now a thin wrapper; its output must not drift.
	if got, want := buildNet0Config("vmbr0", 150), "virtio,bridge=vmbr0,tag=150"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestParseNetConfigRoundTripsProxmoxFormat(t *testing.T) {
	nc := parseNetConfig("virtio=BC:24:11:ab:cd:ef,bridge=vmbr0,tag=20,firewall=1,link_down=1")
	if nc.Model != "virtio" || nc.MAC != "BC:24:11:AB:CD:EF" || nc.Bridge != "vmbr0" || nc.VLANTag != 20 || !nc.LinkDown {
		t.Errorf("unexpected parse: %+v", nc)
	}
	// Our own freshly-built value (no MAC yet) parses too.
	nc = parseNetConfig(buildNetConfig("e1000", "vmbr1", 0, false))
	if nc.Model != "e1000" || nc.MAC != "" || nc.Bridge != "vmbr1" || nc.VLANTag != 0 || nc.LinkDown {
		t.Errorf("unexpected parse of built value: %+v", nc)
	}
}
