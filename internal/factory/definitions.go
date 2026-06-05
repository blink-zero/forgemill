package factory

import "context"

// OSDefinition describes an operating system available for template building.
type OSDefinition struct {
	ID             string   `json:"id"`
	Name           string   `json:"name"`
	Family         string   `json:"family"`
	Version        string   `json:"version"`
	Arch           string   `json:"arch"`
	ISOURLPattern  string   `json:"iso_url_pattern"`
	ISOChecksumURL string   `json:"iso_checksum_url"`
	GuestOSType    string   `json:"guest_os_type"`
	ProxmoxOSType  string   `json:"proxmox_os_type"`
	MinDiskGB      int      `json:"min_disk_gb"`
	MinMemoryMB    int      `json:"min_memory_mb"`
	MinCPU         int      `json:"min_cpu"`
	BootCommand    []string `json:"boot_command"`
	InstallMethod  string   `json:"install_method"`

	// Extended fields for OS modularity (Phase 1 - OS expandability refactor)
	// BootCommandCD is used when the platform delivers installer config via virtual CD-ROM
	// (e.g., vSphere nocloud for Ubuntu autoinstall). If empty, platform falls back to HTTP.
	BootCommandCD []string `json:"boot_command_cd,omitempty"`
	// BootCommandHTTP is used when the platform delivers installer config via HTTP server
	// (e.g., Proxmox for all OSes, vSphere for kickstart/preseed).
	BootCommandHTTP []string `json:"boot_command_http,omitempty"`
	// ProvisionerCmds are OS-specific shell commands run by Packer's provisioner block
	// before converting the VM to a template. Replaces hardcoded apt-get/cloud-init commands.
	ProvisionerCmds []string `json:"provisioner_cmds,omitempty"`

	// CustomChecksumFetcher, when non-nil, replaces the default resolveChecksum()
	// call in engine.go for this OS only. Lets a single OS opt in to retry logic,
	// longer TLS timeouts, hard-coded hashes, etc. without affecting the shared
	// fetch path used by every other OS.
	//
	// Currently set only by Ubuntu 26.04 (see os_ubuntu_2604.go) — releases.ubuntu.com
	// is round-robin DNS across multiple mirrors and at least one of them was
	// regularly exceeding Go's default 10 s TLSHandshakeTimeout during early-release
	// traffic. The same retry approach could be lifted to the default fetcher (or
	// adopted by 22.04 / 24.04 entries) once it has soaked here without surprises —
	// it was deliberately scoped narrow to avoid any risk to templates that build
	// reliably today.
	//
	// This field is not serialised to JSON because it is a runtime-only behaviour
	// hook, not part of the OS metadata exposed to the API.
	CustomChecksumFetcher func(ctx context.Context, isoURL string) (string, error) `json:"-"`
}

// PrereqStatus reports whether required tools are available.
type PrereqStatus struct {
	PackerInstalled bool   `json:"packer_installed"`
	PackerVersion   string `json:"packer_version"`
}

// GetDefinition returns an OS definition by ID or nil if not found.
func GetDefinition(id string) *OSDefinition {
	return getRegisteredDefinition(id)
}

// ListDefinitions returns all registered OS definitions.
func ListDefinitions() []OSDefinition {
	return listRegisteredDefinitions()
}
