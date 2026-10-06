package service

import (
	"strings"

	"github.com/forgemill/forgemill/internal/provider"
)

// What a sync may write into managed_vms.os_type.
//
// Sources, best first: the guest's own answer (agent / Tools), the
// hypervisor's guest id, the template the VM was deployed from. The rule
// that keeps a deployed "Ubuntu 24.04 LTS" from turning into "linux" on the
// first sync: a generic family name never replaces a specific one.

// genericOSNames are values that only say which family a guest belongs to.
var genericOSNames = map[string]bool{
	"linux": true, "windows": true, "other": true, "unknown": true,
	"l26": true, "l24": true, "other26xlinux64guest": true, "other26xlinuxguest": true,
	"otherlinux64guest": true, "otherlinuxguest": true, "other3xlinux64guest": true, "other4xlinux64guest": true,
	"other5xlinux64guest": true, "other6xlinux64guest": true, "otherguest": true, "otherguest64": true,
	"windows9_64guest": false, // specific enough: Windows 10/11 family
}

// osIsGeneric reports whether name only names a family.
func osIsGeneric(name string) bool {
	n := strings.ToLower(strings.TrimSpace(name))
	if n == "" {
		return true
	}
	if v, ok := genericOSNames[n]; ok {
		return v
	}
	return false
}

// chooseOSType returns what os_type should become after a sync that saw
// status, given its current value — or "" when nothing should change.
func chooseOSType(current string, status *provider.VMStatus) string {
	if status == nil {
		return ""
	}
	candidate := strings.TrimSpace(status.GuestOS)
	if candidate == "" {
		candidate = strings.TrimSpace(status.GuestID)
	}
	if candidate == "" || candidate == current {
		return ""
	}
	switch {
	case current == "":
		return candidate
	case !osIsGeneric(candidate):
		// The guest or hypervisor named a real OS; that wins — including
		// over a template name that may have been wrong.
		return candidate
	case osIsGeneric(current):
		// generic → generic: harmless, keep whatever the hypervisor says now
		return candidate
	}
	return "" // generic must not overwrite specific
}
