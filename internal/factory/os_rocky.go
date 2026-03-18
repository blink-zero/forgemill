package factory

// rockyBootCommandCD is used when the kickstart config is delivered via virtual CD-ROM.
// Uses GRUB2 edit mode: up arrow to select, 'e' to edit, navigate to kernel line,
// append kickstart params, then Ctrl+X to boot.
// Reference: github.com/vmware-samples/packer-examples-for-vsphere
// Note: CD is labeled OEMDRV for auto-discovery, but we also specify inst.ks for reliability
// Rocky 9 EFI boot command - edit GRUB to add kickstart parameter
// Reference: github.com/vmware-samples/packer-examples-for-vsphere
var rockyBootCommandCD = []string{
	"<up>",
	"e",
	"<down><down><end><wait>",
	" text inst.ks=cdrom:/ks.cfg",
	"<enter><wait><leftCtrlOn>x<leftCtrlOff>",
}

// rockyBootCommandHTTP is used when the kickstart config is served via HTTP.
// The {{ .HTTPIP }}:{{ .HTTPPort }} placeholders are resolved by Packer at runtime.
var rockyBootCommandHTTP = []string{
	"<up>",
	"e",
	"<down><down><end><wait>",
	" text inst.ks=http://{{ .HTTPIP }}:{{ .HTTPPort }}/ks.cfg",
	"<enter><wait><leftCtrlOn>x<leftCtrlOff>",
}

// rockyProvisionerCmds are run by Packer's shell provisioner before converting to template.
// These prepare the VM for cloning: install cloud-init, clean up kickstart artifacts,
// reset machine-id, remove SSH host keys, and lock the build account.
var rockyProvisionerCmds = []string{
	// Refresh package cache
	"sudo dnf makecache || true",
	// Ensure cloud-init is installed for template cloning
	"sudo dnf install -y cloud-init || true",
	// Clean up kickstart artifacts
	"sudo rm -f /root/anaconda-ks.cfg /root/original-ks.cfg",
	// Remove kickstart boot params from GRUB
	`sudo sed -i 's/inst\.text//g; s/inst\.ks=[^ ]*//g' /etc/default/grub`,
	"sudo grub2-mkconfig -o /boot/grub2/grub.cfg || true",
	// Reset cloud-init so it runs fresh on first clone boot
	"sudo cloud-init clean --logs || true",
	"sudo rm -rf /var/lib/cloud/",
	// Remove machine-id so each clone gets a unique one (important for DHCP)
	"sudo truncate -s 0 /etc/machine-id",
	"sudo rm -f /var/lib/dbus/machine-id",
	"sudo ln -s /etc/machine-id /var/lib/dbus/machine-id || true",
	// Remove SSH host keys so each clone generates fresh ones
	"sudo rm -f /etc/ssh/ssh_host_*",
	// Truncate logs for clean template
	"sudo truncate -s 0 /var/log/messages /var/log/secure /var/log/cron || true",
	// Lock the build account password
	"sudo passwd -l forgemill",
}

func init() {
	// Rocky Linux 9 - use minimal ISO with network repos for additional packages
	// GuestOSType aligned with VMware packer-examples-for-vsphere
	RegisterOSDefinition(OSDefinition{
		ID:             "rocky-9",
		Name:           "Rocky Linux 9 (Blue Onyx)",
		Family:         "rhel",
		Version:        "9",
		Arch:           "x86_64",
		ISOURLPattern:  "https://download.rockylinux.org/pub/rocky/9/isos/x86_64/Rocky-9-latest-x86_64-minimal.iso",
		ISOChecksumURL: "https://download.rockylinux.org/pub/rocky/9/isos/x86_64/CHECKSUM",
		GuestOSType:    "other5xLinux64Guest", // VMware packer-examples uses this
		ProxmoxOSType:  "l26",
		MinDiskGB:      20,
		MinMemoryMB:    2048,
		MinCPU:         2,
		BootCommand:    rockyBootCommandCD, // Legacy field, kept for backward compat
		InstallMethod:  "kickstart",
		// Extended fields
		BootCommandCD:   rockyBootCommandCD,
		BootCommandHTTP: rockyBootCommandHTTP,
		ProvisionerCmds: rockyProvisionerCmds,
	})

	// Rocky Linux 8 - use minimal ISO with network repos for additional packages
	// GuestOSType aligned with VMware packer-examples-for-vsphere
	RegisterOSDefinition(OSDefinition{
		ID:             "rocky-8",
		Name:           "Rocky Linux 8 (Green Obsidian)",
		Family:         "rhel",
		Version:        "8",
		Arch:           "x86_64",
		ISOURLPattern:  "https://download.rockylinux.org/pub/rocky/8/isos/x86_64/Rocky-8-latest-x86_64-minimal.iso",
		ISOChecksumURL: "https://download.rockylinux.org/pub/rocky/8/isos/x86_64/CHECKSUM",
		GuestOSType:    "other4xLinux64Guest", // VMware packer-examples uses this
		ProxmoxOSType:  "l26",
		MinDiskGB:      20,
		MinMemoryMB:    2048,
		MinCPU:         2,
		BootCommand:    rockyBootCommandCD, // Legacy field, kept for backward compat
		InstallMethod:  "kickstart",
		// Extended fields
		BootCommandCD:   rockyBootCommandCD,
		BootCommandHTTP: rockyBootCommandHTTP,
		ProvisionerCmds: rockyProvisionerCmds,
	})
}
