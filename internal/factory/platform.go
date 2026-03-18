package factory

import "fmt"

// Platform generates Packer HCL templates and provides platform-specific
// configuration for a hypervisor target (vSphere/ESXi, Proxmox, etc.).
type Platform interface {
	// Types returns the target type strings this platform handles (e.g. ["vcenter", "esxi"] or ["proxmox"]).
	Types() []string
	// InterfaceName returns the default NIC name for this platform (e.g. "ens192", "ens18").
	InterfaceName() string
	// ProvisionerPackages returns packages the provisioner should install for the given OS family.
	ProvisionerPackages(osFamily string) []string
	// InstallerHints returns platform-specific configuration for installer templates
	// (cloud-init datasources, extra config lines, services to enable).
	// This eliminates TargetType conditionals from installer templates, so adding
	// new hypervisors never requires editing existing installer templates.
	InstallerHints() InstallerHints
	// AdjustTemplateData applies platform-specific defaults to the template data.
	// The osDef parameter allows platforms to resolve boot commands and provisioner
	// commands from the OS definition (Phase 3 - OS expandability).
	AdjustTemplateData(data *TemplateData, targetType string, osDef *OSDefinition)
	// GenerateHCL produces the Packer HCL template content.
	GenerateHCL(data TemplateData) (string, error)
}

var platformRegistry = map[string]Platform{}

// RegisterPlatform registers a Platform for each of its target types.
func RegisterPlatform(p Platform) {
	for _, t := range p.Types() {
		platformRegistry[t] = p
	}
}

// GetPlatform returns the Platform for the given target type, or an error if not found.
func GetPlatform(targetType string) (Platform, error) {
	p, ok := platformRegistry[targetType]
	if !ok {
		return nil, fmt.Errorf("no platform registered for target type %q", targetType)
	}
	return p, nil
}
