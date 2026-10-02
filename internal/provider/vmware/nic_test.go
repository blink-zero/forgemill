package vmware

import (
	"errors"
	"testing"

	"github.com/forgemill/forgemill/internal/provider"
)

func TestNormalizeNICAdapterTypeDefaultsToVmxnet3(t *testing.T) {
	for _, in := range []string{"", "   "} {
		got, err := normalizeNICAdapterType(in)
		if err != nil {
			t.Fatalf("normalizeNICAdapterType(%q): %v", in, err)
		}
		if got != "vmxnet3" {
			t.Errorf("normalizeNICAdapterType(%q) = %q, want vmxnet3", in, got)
		}
	}
}

func TestNormalizeNICAdapterTypeAcceptsKnownModelsCaseInsensitively(t *testing.T) {
	cases := map[string]string{"vmxnet3": "vmxnet3", "VMXNET3": "vmxnet3", " E1000 ": "e1000", "e1000e": "e1000e"}
	if nicAdapterTypes[0] != "vmxnet3" {
		t.Fatalf("default adapter (first entry) must stay vmxnet3, got %q", nicAdapterTypes[0])
	}
	for in, want := range cases {
		got, err := normalizeNICAdapterType(in)
		if err != nil {
			t.Fatalf("normalizeNICAdapterType(%q): %v", in, err)
		}
		if got != want {
			t.Errorf("normalizeNICAdapterType(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNormalizeNICAdapterTypeRejectsUnknownModel(t *testing.T) {
	for _, in := range []string{"virtio", "pcnet32", "sriov", "vmxnet"} {
		_, err := normalizeNICAdapterType(in)
		if !errors.Is(err, provider.ErrInvalidAdapterType) {
			t.Errorf("normalizeNICAdapterType(%q) = %v, want ErrInvalidAdapterType", in, err)
		}
	}
}

func TestDatacenterPathOf(t *testing.T) {
	cases := map[string]string{
		"/DC1/vm/web/web-01":             "/DC1",
		"/ha-datacenter/vm/web-01":       "/ha-datacenter",
		"/Prod DC/vm/folder/sub/vm-name": "/Prod DC",
	}
	for in, want := range cases {
		got, err := datacenterPathOf(in)
		if err != nil {
			t.Fatalf("datacenterPathOf(%q): %v", in, err)
		}
		if got != want {
			t.Errorf("datacenterPathOf(%q) = %q, want %q", in, got, want)
		}
	}
	for _, in := range []string{"", "/"} {
		if _, err := datacenterPathOf(in); err == nil {
			t.Errorf("datacenterPathOf(%q) expected an error", in)
		}
	}
}
