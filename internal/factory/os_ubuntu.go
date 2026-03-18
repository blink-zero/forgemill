package factory

// ubuntuBootCommandHTTP is used when the installer config is served via HTTP.
// Used by: Proxmox (always), vSphere (fallback for non-nocloud installers).
// The {{ .HTTPIP }}:{{ .HTTPPort }} placeholders are resolved by Packer at runtime.
var ubuntuBootCommandHTTP = []string{
	"c<wait>",
	"linux /casper/vmlinuz --- autoinstall ds='nocloud-net;s=http://{{ .HTTPIP }}:{{ .HTTPPort }}/'",
	"<enter><wait>",
	"initrd /casper/initrd",
	"<enter><wait>",
	"boot",
	"<enter>",
}

// ubuntuBootCommandCD is used when the installer config is delivered via virtual CD-ROM.
// Used by: vSphere with nocloud CD mount (cidata label).
// Note: ds="nocloud" (no URL) tells cloud-init to read from attached CD.
var ubuntuBootCommandCD = []string{
	"<wait3s>c<wait3s>",
	`linux /casper/vmlinuz --- autoinstall ds="nocloud"`,
	"<enter><wait>",
	"initrd /casper/initrd",
	"<enter><wait>",
	"boot",
	"<enter>",
}

// ubuntuProvisionerCmds are run by Packer's shell provisioner before converting to template.
// These prepare the VM for cloning: install cloud-init, clean up installer artifacts,
// reset machine-id, and lock the build account.
var ubuntuProvisionerCmds = []string{
	// Ensure critical packages are installed
	"sudo apt-get update || true",
	"sudo apt-get install -y cloud-init cloud-initramfs-growroot || echo 'WARNING: apt install failed - skipping'",
	// Clean up networking for template (cloud-init will manage on clone)
	"sudo rm -f /etc/netplan/*.yaml",
	// Remove subiquity/installer configs that interfere with VMware datasource
	"sudo rm -f /etc/cloud/cloud.cfg.d/subiquity-disable-cloudinit-networking.cfg",
	"sudo rm -f /etc/cloud/cloud.cfg.d/99-installer.cfg",
	// Remove autoinstall boot params from GRUB
	"sudo sed -i 's/autoinstall ds=nocloud//g' /etc/default/grub",
	"sudo update-grub",
	// Reset cloud-init so it runs fresh on first clone boot
	"sudo cloud-init clean --logs || true",
	"sudo rm -rf /var/lib/cloud/",
	// Remove machine-id so each clone gets a unique one (important for DHCP)
	"sudo truncate -s 0 /etc/machine-id",
	"sudo rm -f /var/lib/dbus/machine-id",
	"sudo ln -s /etc/machine-id /var/lib/dbus/machine-id || true",
	// Lock the build account password
	"sudo passwd -l forgemill",
}

func init() {
	RegisterOSDefinition(OSDefinition{
		ID:             "ubuntu-2404",
		Name:           "Ubuntu 24.04 LTS (Noble Numbat)",
		Family:         "ubuntu",
		Version:        "24.04",
		Arch:           "amd64",
		ISOURLPattern:  "https://releases.ubuntu.com/24.04/ubuntu-24.04.4-live-server-amd64.iso",
		ISOChecksumURL: "https://releases.ubuntu.com/24.04/SHA256SUMS",
		GuestOSType:    "ubuntu64Guest",
		ProxmoxOSType:  "l26",
		MinDiskGB:      20,
		MinMemoryMB:    2048,
		MinCPU:         2,
		BootCommand:    ubuntuBootCommandHTTP, // Legacy field, kept for backward compat
		InstallMethod:  "autoinstall",
		// Extended fields (Phase 2)
		BootCommandCD:   ubuntuBootCommandCD,
		BootCommandHTTP: ubuntuBootCommandHTTP,
		ProvisionerCmds: ubuntuProvisionerCmds,
	})

	RegisterOSDefinition(OSDefinition{
		ID:             "ubuntu-2204",
		Name:           "Ubuntu 22.04 LTS (Jammy Jellyfish)",
		Family:         "ubuntu",
		Version:        "22.04",
		Arch:           "amd64",
		ISOURLPattern:  "https://releases.ubuntu.com/22.04/ubuntu-22.04.5-live-server-amd64.iso",
		ISOChecksumURL: "https://releases.ubuntu.com/22.04/SHA256SUMS",
		GuestOSType:    "ubuntu64Guest",
		ProxmoxOSType:  "l26",
		MinDiskGB:      20,
		MinMemoryMB:    2048,
		MinCPU:         2,
		BootCommand:    ubuntuBootCommandHTTP, // Legacy field, kept for backward compat
		InstallMethod:  "autoinstall",
		// Extended fields (Phase 2)
		BootCommandCD:   ubuntuBootCommandCD,
		BootCommandHTTP: ubuntuBootCommandHTTP,
		ProvisionerCmds: ubuntuProvisionerCmds,
	})
}
