package vmware

import (
	"reflect"
	"testing"

	"github.com/vmware/govmomi/vim25/types"

	"github.com/forgemill/forgemill/internal/provider"
)

func TestGuestAddressesForMatchesByMACCaseInsensitively(t *testing.T) {
	nets := []types.GuestNicInfo{
		{MacAddress: "00:50:56:aa:bb:01", IpAddress: []string{"10.0.0.1"}, DeviceConfigId: 4000},
		{MacAddress: "00:50:56:aa:bb:02", IpAddress: []string{"10.0.0.2", "fe80::2"}, DeviceConfigId: 4001},
	}
	got := guestAddressesFor("00:50:56:AA:BB:02", 4001, nets)
	if !reflect.DeepEqual(got, []string{"10.0.0.2", "fe80::2"}) {
		t.Errorf("got %v", got)
	}
}

func TestGuestAddressesForFallsBackToDeviceConfigID(t *testing.T) {
	// Some guests report no MAC (or a mangled one) but Tools still ties the
	// entry to the device key.
	nets := []types.GuestNicInfo{{MacAddress: "", IpAddress: []string{"192.168.1.9"}, DeviceConfigId: 4002}}
	if got := guestAddressesFor("00:50:56:AA:BB:03", 4002, nets); !reflect.DeepEqual(got, []string{"192.168.1.9"}) {
		t.Errorf("got %v", got)
	}
	if got := guestAddressesFor("00:50:56:AA:BB:04", 4003, nets); got != nil {
		t.Errorf("expected no addresses for an unmatched adapter, got %v", got)
	}
}

func TestSortAddressesPutsIPv4FirstAndLinkLocalLast(t *testing.T) {
	in := []string{"fe80::1", "2001:db8::10", "169.254.3.3", "10.20.10.11"}
	want := []string{"10.20.10.11", "169.254.3.3", "2001:db8::10", "fe80::1"}
	if got := provider.SortAddresses(in); !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
	// Input must not be mutated — callers hand over the guest's slice.
	if in[0] != "fe80::1" {
		t.Error("SortAddresses mutated its input")
	}
}
