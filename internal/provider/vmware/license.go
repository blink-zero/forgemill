package vmware

import (
	"context"
	"fmt"
	"strings"

	"github.com/forgemill/forgemill/internal/provider"
	"github.com/vmware/govmomi/license"
	"github.com/vmware/govmomi/vim25/types"
)

// HostCapabilities reads the active license. On a standalone host the free
// "vSphere Hypervisor" SKU makes the API read-only for us; vCenter and any
// paid/evaluation license allow writes. Errors reading the license are not
// fatal to the caller — a target that cannot report is assumed capable.
func (p *Provider) HostCapabilities(ctx context.Context) (*provider.HostCapabilities, error) {
	client, err := p.getClient(ctx)
	if err != nil {
		return nil, err
	}
	infos, err := license.NewManager(client.Client).List(ctx)
	if err != nil {
		return nil, fmt.Errorf("read license: %w", err)
	}
	caps := &provider.HostCapabilities{WritesAllowed: true}
	for _, info := range infos {
		if caps.LicenseEdition == "" || isFreeLicense(info) {
			caps.LicenseEdition = info.Name
		}
		if p.esxiMode && isFreeLicense(info) {
			caps.WritesAllowed = false
			caps.Note = provider.LicenseRestrictedMessage
		}
	}
	return caps, nil
}

// isFreeLicense recognises the free vSphere Hypervisor SKU. VMware has named
// it "VMware vSphere Hypervisor" / "VMware vSphere N Hypervisor" and keyed it
// with "free"/"hypervisor" editions across releases; paid editions are
// Standard / Enterprise Plus / Essentials and never say "Hypervisor".
func isFreeLicense(info types.LicenseManagerLicenseInfo) bool {
	edition := strings.ToLower(info.EditionKey)
	name := strings.ToLower(info.Name)
	switch {
	case strings.Contains(edition, "free"), strings.Contains(edition, "hypervisor"):
		return true
	case strings.Contains(name, "hypervisor") && !strings.Contains(name, "evaluation"):
		return true
	}
	return false
}
