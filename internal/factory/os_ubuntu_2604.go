package factory

// This file is intentionally isolated from os_ubuntu.go. The boot commands
// and provisioner commands below are duplicated rather than shared so that
// any future divergence in Ubuntu 26.04's installer behaviour can be
// adjusted here without risking regressions on 22.04 / 24.04.

// ubuntu2604BootCommandHTTP is used when the installer config is served via HTTP.
// Used by: Proxmox (always), vSphere (fallback for non-nocloud installers).
// The {{ .HTTPIP }}:{{ .HTTPPort }} placeholders are resolved by Packer at runtime.
var ubuntu2604BootCommandHTTP = []string{
	"c<wait>",
	"linux /casper/vmlinuz --- autoinstall ds='nocloud-net;s=http://{{ .HTTPIP }}:{{ .HTTPPort }}/'",
	"<enter><wait>",
	"initrd /casper/initrd",
	"<enter><wait>",
	"boot",
	"<enter>",
}

// ubuntu2604BootCommandCD is used when the installer config is delivered via virtual CD-ROM.
// Used by: vSphere with nocloud CD mount (cidata label).
// Note: ds="nocloud" (no URL) tells cloud-init to read from attached CD.
var ubuntu2604BootCommandCD = []string{
	"<wait3s>c<wait3s>",
	`linux /casper/vmlinuz --- autoinstall ds="nocloud"`,
	"<enter><wait>",
	"initrd /casper/initrd",
	"<enter><wait>",
	"boot",
	"<enter>",
}

// ubuntu2604ProvisionerCmds are run by Packer's shell provisioner before converting to template.
// These prepare the VM for cloning: install cloud-init, clean up installer artifacts,
// reset machine-id, and lock the build account.
var ubuntu2604ProvisionerCmds = []string{
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
		ID:             "ubuntu-2604",
		Name:           "Ubuntu 26.04 LTS (Resolute Raccoon)",
		Family:         "ubuntu",
		Version:        "26.04",
		Arch:           "amd64",
		ISOURLPattern:  "https://releases.ubuntu.com/26.04/ubuntu-26.04-live-server-amd64.iso",
		ISOChecksumURL: "https://releases.ubuntu.com/26.04/SHA256SUMS",
		GuestOSType:    "ubuntu64Guest",
		ProxmoxOSType:  "l26",
		MinDiskGB:      20,
		MinMemoryMB:    2048,
		MinCPU:         2,
		BootCommand:    ubuntu2604BootCommandHTTP, // Legacy field, kept for backward compat
		InstallMethod:  "autoinstall",
		// Extended fields (Phase 2)
		BootCommandCD:   ubuntu2604BootCommandCD,
		BootCommandHTTP: ubuntu2604BootCommandHTTP,
		ProvisionerCmds: ubuntu2604ProvisionerCmds,
	})
}
