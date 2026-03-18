package factory

import (
	"bytes"
	"strings"
	"text/template"
)

// VSpherePlatform implements Platform for VMware vCenter and ESXi standalone targets.
type VSpherePlatform struct{}

func init() {
	RegisterPlatform(&VSpherePlatform{})
}

func (v *VSpherePlatform) Types() []string      { return []string{"vcenter", "esxi"} }
func (v *VSpherePlatform) InterfaceName() string { return "ens192" }

func (v *VSpherePlatform) InstallerHints() InstallerHints {
	return InstallerHints{
		CloudInitDatasources: "[VMware, OVF, None]",
		CloudInitExtraLines: []string{
			"disable_vmware_customization: false",
		},
		PlatformServices: []string{"vmtoolsd"},
	}
}

func (v *VSpherePlatform) ProvisionerPackages(osFamily string) []string {
	return []string{"open-vm-tools"}
}

func (v *VSpherePlatform) AdjustTemplateData(data *TemplateData, targetType string, osDef *OSDefinition) {
	if targetType == "esxi" {
		if data.Datacenter == "" {
			data.Datacenter = "ha-datacenter"
		}
		if data.Network == "" {
			data.Network = "VM Network"
		}
		data.ESXiStandalone = true
		data.Folder = ""
	}

	// Phase 3: Resolve boot commands and provisioner commands from OS definition.
	// vSphere prefers CD delivery (nocloud) if BootCommandCD is available.
	if osDef != nil {
		data.OSFamily = osDef.Family
		data.InstallMethod = osDef.InstallMethod

		// Phase 4b: Copy provisioner commands and inject platform-specific packages.
		// Platform packages (open-vm-tools) are installed after the first package manager command.
		if len(osDef.ProvisionerCmds) > 0 {
			// Make a copy to avoid modifying the original
			cmds := make([]string, len(osDef.ProvisionerCmds))
			copy(cmds, osDef.ProvisionerCmds)

			// Determine platform package command based on OS family
			var platformPkgCmd string
			if osDef.Family == "rhel" {
				platformPkgCmd = "sudo dnf install -y open-vm-tools || true"
			} else {
				platformPkgCmd = "sudo apt-get install -y open-vm-tools || true"
			}

			// Find the first package manager command and insert after it
			inserted := false
			for i, cmd := range cmds {
				if !inserted && (strings.HasPrefix(cmd, "sudo apt-get up") || strings.HasPrefix(cmd, "sudo dnf makecache") || strings.HasPrefix(cmd, "sudo dnf -y update")) {
					// Insert after this command
					cmds = append(cmds[:i+1], append([]string{platformPkgCmd}, cmds[i+1:]...)...)
					inserted = true
					break
				}
			}
			if !inserted {
				// No package manager update found, prepend the package install
				cmds = append([]string{platformPkgCmd}, cmds...)
			}
			data.ProvisionerCmds = cmds
		}

		if len(osDef.BootCommandCD) > 0 {
			data.BootCommand = osDef.BootCommandCD
			data.UseHTTPDelivery = false
		} else if len(osDef.BootCommandHTTP) > 0 {
			data.BootCommand = osDef.BootCommandHTTP
			data.UseHTTPDelivery = true
		}
		// Fallback: if neither is set, BootCommand stays empty and HCL uses hardcoded values
	}
}

func (v *VSpherePlatform) GenerateHCL(data TemplateData) (string, error) {
	tmpl, err := template.New("packer").Funcs(template.FuncMap{
		"hclEscape": hclEscape,
	}).Parse(vsphereHCLTemplateStr)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// vsphereHCLTemplateStr follows VMware's official packer-examples-for-vsphere pattern.
// Reference: github.com/vmware-samples/packer-examples-for-vsphere
const vsphereHCLTemplateStr = `packer {
  required_plugins {
    vsphere = {
      source  = "github.com/hashicorp/vsphere"
      version = "~> 1"
    }
  }
}

source "vsphere-iso" "template" {
  // vCenter/ESXi connection
  vcenter_server      = "{{.TargetHostname}}"
  username            = "{{.TargetUsername}}"
  password            = "{{.TargetPassword}}"
  insecure_connection = {{.InsecureConnection}}
  datacenter          = "{{.Datacenter}}"
{{- if .Host}}
  host                = "{{.Host}}"
{{- end}}
{{- if .Cluster}}
  cluster             = "{{.Cluster}}"
{{- end}}
  datastore           = "{{.Datastore}}"
{{- if .Folder}}
  folder              = "{{.Folder}}"
{{- end}}

  // Virtual machine settings
  vm_name              = "{{.TemplateName}}"
  guest_os_type        = "{{.GuestOSType}}"
{{- if or (eq .InstallMethod "kickstart") (eq .InstallMethod "preseed")}}
  firmware             = "efi"
{{- end}}
  CPUs                 = {{.CPU}}
  RAM                  = {{.MemoryMB}}
  disk_controller_type = ["pvscsi"]
  storage {
    disk_size             = {{.DiskMB}}
    disk_thin_provisioned = true
  }
  network_adapters {
    network      = "{{.Network}}"
    network_card = "vmxnet3"
  }

  // ISO and installer config delivery via CD-ROM (no HTTP server needed)
  iso_url      = "{{.ISOURL}}"
  iso_checksum = "{{.ISOChecksum}}"

{{- if eq .InstallMethod "kickstart"}}
  cd_content = {
    "ks.cfg" = file("${path.root}/ks.cfg")
  }
  cd_label = "OEMDRV"
{{- else if eq .InstallMethod "preseed"}}
  cd_content = {
    "preseed.cfg" = file("${path.root}/preseed.cfg")
  }
  cd_label = "OEMDRV"
{{- else}}
  cd_content = {
    "meta-data" = ""
    "user-data" = file("${path.root}/autoinstall.yaml")
  }
  cd_label = "cidata"
{{- end}}

  // Boot configuration - dynamic from OS definition (Phase 4a)
{{- if .BootCommand}}
  boot_command = [
{{- range .BootCommand}}
    "{{hclEscape .}}",
{{- end}}
  ]
{{- else}}
  // Fallback: hardcoded Ubuntu autoinstall boot command
  boot_command = [
    "<wait3s>c<wait3s>",
    "linux /casper/vmlinuz --- autoinstall ds=\"nocloud\"",
    "<enter><wait>",
    "initrd /casper/initrd",
    "<enter><wait>",
    "boot",
    "<enter>"
  ]
{{- end}}

{{- if or (eq .InstallMethod "kickstart") (eq .InstallMethod "preseed")}}
  boot_wait         = "10s"
{{- else}}
  boot_wait         = "5s"
{{- end}}
  ip_wait_timeout   = "30m"
  ip_settle_timeout = "5s"

  // SSH communicator
{{- if .SSHHost}}
  ssh_host         = "{{.SSHHost}}"
{{- end}}
  ssh_username     = "forgemill"
  ssh_password     = "{{.SSHPassword}}"
  ssh_timeout      = "30m"

  shutdown_command      = "sudo -S shutdown -P now"
  shutdown_timeout      = "15m"
{{- if not .ESXiStandalone}}
  convert_to_template   = true
{{- end}}
}

build {
  sources = ["source.vsphere-iso.template"]

  // Provisioner commands - dynamic from OS definition (Phase 4b)
{{- if .ProvisionerCmds}}
  provisioner "shell" {
    inline = [
{{- range .ProvisionerCmds}}
      "{{hclEscape .}}",
{{- end}}
    ]
  }
{{- else}}
  // Fallback: hardcoded Ubuntu provisioner for vSphere
  provisioner "shell" {
    inline = [
      "sudo apt-get update || true",
      "sudo apt-get install -y cloud-init cloud-initramfs-growroot open-vm-tools || echo 'WARNING: apt install failed - skipping'",
      "sudo rm -f /etc/netplan/*.yaml",
      "sudo rm -f /etc/cloud/cloud.cfg.d/subiquity-disable-cloudinit-networking.cfg",
      "sudo rm -f /etc/cloud/cloud.cfg.d/99-installer.cfg",
      "sudo sed -i 's/autoinstall ds=nocloud//g' /etc/default/grub",
      "sudo update-grub",
      "sudo cloud-init clean --logs || true",
      "sudo rm -rf /var/lib/cloud/",
      "sudo truncate -s 0 /etc/machine-id",
      "sudo rm -f /var/lib/dbus/machine-id",
      "sudo ln -s /etc/machine-id /var/lib/dbus/machine-id || true",
      "sudo passwd -l forgemill"
    ]
  }
{{- end}}
}
`
