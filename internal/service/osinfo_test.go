package service

import (
	"testing"

	"github.com/forgemill/forgemill/internal/provider"
)

func TestChooseOSTypeNeverDowngrades(t *testing.T) {
	cases := []struct {
		name, current string
		status        provider.VMStatus
		want          string
	}{
		{"proxmox generic over template name", "Ubuntu 24.04 LTS", provider.VMStatus{GuestID: "linux"}, ""},
		{"vsphere generic id over template name", "Ubuntu 24.04 LTS", provider.VMStatus{GuestID: "otherLinux64Guest"}, ""},
		{"agent pretty name over template name", "Ubuntu 24.04 LTS", provider.VMStatus{GuestID: "linux", GuestOS: "Ubuntu 24.04.1 LTS"}, "Ubuntu 24.04.1 LTS"},
		{"agent name over generic", "linux", provider.VMStatus{GuestID: "linux", GuestOS: "Debian GNU/Linux 12 (bookworm)"}, "Debian GNU/Linux 12 (bookworm)"},
		{"specific id over empty", "", provider.VMStatus{GuestID: "ubuntu64Guest"}, "ubuntu64Guest"},
		{"generic over empty", "", provider.VMStatus{GuestID: "linux"}, "linux"},
		{"generic over generic switches family", "linux", provider.VMStatus{GuestID: "windows"}, "windows"},
		{"nothing new", "Ubuntu 24.04 LTS", provider.VMStatus{}, ""},
		{"same value", "linux", provider.VMStatus{GuestID: "linux"}, ""},
		{"tools full name beats configured id", "ubuntu64Guest", provider.VMStatus{GuestID: "ubuntu64Guest", GuestOS: "Ubuntu Linux (64-bit)"}, "Ubuntu Linux (64-bit)"},
	}
	for _, tc := range cases {
		if got := chooseOSType(tc.current, &tc.status); got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
}
