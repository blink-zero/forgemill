package factory

import "fmt"

// Installer generates OS-specific unattended install configurations.
// Implementations: AutoinstallInstaller (Ubuntu), future: KickstartInstaller (RHEL/Rocky),
// PreseedInstaller (Debian), UnattendInstaller (Windows).
type Installer interface {
	// Method returns the install method name (e.g. "autoinstall", "kickstart", "preseed", "unattend").
	Method() string
	// Filename returns the config file name expected by the OS installer (e.g. "autoinstall.yaml", "ks.cfg").
	Filename() string
	// GenerateConfig produces the unattended install configuration content.
	GenerateConfig(params InstallerParams) (string, error)
}

// InstallerParams holds all parameters needed to generate an unattended install config.
type InstallerParams struct {
	TargetType    string   // "vcenter", "esxi", or "proxmox"
	OSFamily      string   // "debian", "rhel", etc.
	OSVersion     string   // "9", "8", "24.04", etc.
	Username      string   // build user name
	Password      string   // plaintext — installer hashes internally if needed
	SSHPublicKey  string   // optional SSH public key for authorized_keys
	Hostname      string   // VM hostname during build
	Timezone      string   // e.g. "UTC"
	Locale        string   // e.g. "en_US.UTF-8"
	Keyboard      string   // e.g. "us"
	ExtraPackages []string // platform-specific packages (open-vm-tools, qemu-guest-agent)
	BuildIP       string   // static IP for build-time SSH (empty = DHCP)
	Netmask       string   // e.g. "255.255.255.0"
	Gateway       string   // default gateway
	InterfaceName string   // NIC name (ens192, ens18, eth0)

	// Platform-injectable fields (populated from Platform.InstallerHints).
	// These eliminate TargetType conditionals from installer templates,
	// so adding new hypervisors never requires editing existing templates.
	CloudInitDatasources string   // pre-formatted datasource_list value, e.g. "[NoCloud, ConfigDrive, None]"
	CloudInitExtraLines  []string // extra lines for cloud.cfg (e.g. "disable_vmware_customization: false")
	PlatformServices     []string // services to enable in %post (e.g. ["qemu-guest-agent"] or ["vmtoolsd"])
}

// InstallerHints provides platform-specific configuration that installer templates
// need without requiring TargetType conditionals. Each Platform populates this
// so that new hypervisors can be added without modifying installer templates.
type InstallerHints struct {
	// CloudInitDatasources is the pre-formatted datasource_list value for cloud.cfg.
	// Example: "[NoCloud, ConfigDrive, None]" for Proxmox, "[VMware, OVF, None]" for vSphere.
	CloudInitDatasources string
	// CloudInitExtraLines are additional lines written into cloud.cfg.d/ config files.
	// Example: "disable_vmware_customization: false" for VMware.
	CloudInitExtraLines []string
	// PlatformServices are service names to enable during OS install %post/%late-commands.
	// Example: ["qemu-guest-agent"] for Proxmox, ["vmtoolsd"] for VMware.
	PlatformServices []string
}

var installerRegistry = map[string]Installer{}

// RegisterInstaller registers an Installer by its method name.
func RegisterInstaller(i Installer) {
	installerRegistry[i.Method()] = i
}

// GetInstaller returns the Installer for the given method, or an error if not found.
func GetInstaller(method string) (Installer, error) {
	i, ok := installerRegistry[method]
	if !ok {
		return nil, fmt.Errorf("no installer registered for method %q", method)
	}
	return i, nil
}
