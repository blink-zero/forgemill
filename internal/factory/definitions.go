package factory

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
