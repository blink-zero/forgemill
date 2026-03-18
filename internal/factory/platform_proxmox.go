package factory

import (
	"bytes"
	"strings"
	"text/template"
)

// ProxmoxPlatform implements Platform for Proxmox VE targets.
type ProxmoxPlatform struct{}

func init() {
	RegisterPlatform(&ProxmoxPlatform{})
}

func (p *ProxmoxPlatform) Types() []string      { return []string{"proxmox"} }
func (p *ProxmoxPlatform) InterfaceName() string { return "ens18" }

func (p *ProxmoxPlatform) InstallerHints() InstallerHints {
	return InstallerHints{
		CloudInitDatasources: "[NoCloud, ConfigDrive, None]",
		CloudInitExtraLines:  nil, // Proxmox needs no extra cloud.cfg lines
		PlatformServices:     []string{"qemu-guest-agent"},
	}
}

func (p *ProxmoxPlatform) ProvisionerPackages(osFamily string) []string {
	return []string{"qemu-guest-agent"}
}

func (p *ProxmoxPlatform) AdjustTemplateData(data *TemplateData, targetType string, osDef *OSDefinition) {
	// Proxmox-specific defaults — TODO: make these configurable per-target
	if data.StoragePool == "" {
		if data.Datastore != "" {
			data.StoragePool = data.Datastore
		} else {
			data.StoragePool = "local-lvm" // Common Proxmox default
		}
	}
	if data.Bridge == "" {
		if data.Network != "" {
			data.Bridge = data.Network
		} else {
			data.Bridge = "vmbr0" // Common Proxmox default
		}
	}
	if data.ISOStorage == "" {
		data.ISOStorage = "local"
	}
	data.ISODownloadPVE = true

	// Phase 3: Resolve boot commands and provisioner commands from OS definition.
	// Proxmox always uses HTTP delivery for installer configs.
	if osDef != nil {
		data.OSFamily = osDef.Family
		data.InstallMethod = osDef.InstallMethod
		data.UseHTTPDelivery = true

		// Phase 4b: Copy provisioner commands and inject platform-specific packages.
		// Platform packages (qemu-guest-agent) are installed after the first package manager command.
		if len(osDef.ProvisionerCmds) > 0 {
			// Make a copy to avoid modifying the original
			cmds := make([]string, len(osDef.ProvisionerCmds))
			copy(cmds, osDef.ProvisionerCmds)

			// Determine platform package command based on OS family
			var platformPkgCmd string
			if osDef.Family == "rhel" {
				platformPkgCmd = "sudo dnf install -y qemu-guest-agent || true"
			} else {
				platformPkgCmd = "sudo apt-get install -y qemu-guest-agent || true"
			}
			platformEnableCmd := "sudo systemctl enable qemu-guest-agent 2>/dev/null || true"

			// Find the first package manager command and insert after it
			inserted := false
			for i, cmd := range cmds {
				if !inserted && (strings.HasPrefix(cmd, "sudo apt-get up") || strings.HasPrefix(cmd, "sudo dnf makecache") || strings.HasPrefix(cmd, "sudo dnf -y update")) {
					// Insert after this command
					cmds = append(cmds[:i+1], append([]string{platformPkgCmd, platformEnableCmd}, cmds[i+1:]...)...)
					inserted = true
					break
				}
			}
			if !inserted {
				// No package manager update found, prepend the package install
				cmds = append([]string{platformPkgCmd, platformEnableCmd}, cmds...)
			}
			data.ProvisionerCmds = cmds
		}

		// Boot command selection:
		// - Kickstart uses CD delivery (OEMDRV label on additional_iso_files CD)
		// - Autoinstall uses hardcoded "yes<enter>" fallback in HCL template
		if osDef.InstallMethod == "kickstart" {
			// Proxmox uses HTTP delivery — use HTTP boot command
			if len(osDef.BootCommandHTTP) > 0 {
				data.BootCommand = osDef.BootCommandHTTP
			} else if len(osDef.BootCommandCD) > 0 {
				// Fallback to CD command if HTTP not defined
				data.BootCommand = osDef.BootCommandCD
			}
		}
		// For autoinstall, BootCommand stays empty — HCL template uses hardcoded "yes<enter>"
	}
}

func (p *ProxmoxPlatform) GenerateHCL(data TemplateData) (string, error) {
	tmpl, err := template.New("packer").Funcs(template.FuncMap{
		"hclEscape": hclEscape,
	}).Parse(proxmoxHCLTemplateStr)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

const proxmoxHCLTemplateStr = `packer {
  required_plugins {
    proxmox = {
      source  = "github.com/hashicorp/proxmox"
      version = "~> 1"
    }
  }
}

source "proxmox-iso" "template" {
  proxmox_url              = "https://{{.TargetHostname}}:{{.TargetPort}}/api2/json"
  username                 = "{{.TargetUsername}}"
  password                 = "{{.TargetPassword}}"
  insecure_skip_tls_verify = {{.InsecureConnection}}
  task_timeout             = "30m"
  node                     = "{{.Node}}"

  vm_id                = 0
  vm_name              = "{{.TemplateName}}"
  template_description = "Built by Forgemill"
  os                   = "{{.ProxmoxOS}}"
  cpu_type             = "host"
  cores                = {{.CPU}}
  memory               = {{.MemoryMB}}

  scsi_controller = "virtio-scsi-single"
  disks {
    type         = "scsi"
    disk_size    = "{{.DiskGB}}G"
    storage_pool = "{{.StoragePool}}"
  }

  network_adapters {
    model  = "virtio"
    bridge = "{{.Bridge}}"
  }

  boot_iso {
    type             = "ide"
    iso_url          = "{{.ISOURL}}"
    iso_checksum     = "{{.ISOChecksum}}"
    iso_storage_pool = "{{.ISOStorage}}"{{if .ISODownloadPVE}}
    iso_download_pve = true{{end}}
    unmount          = true
  }

  # Attach installer config as a CD-ROM (no HTTP server needed)
  additional_iso_files {
    iso_storage_pool = "{{.ISOStorage}}"
    unmount          = true
    cd_content = {
{{- if eq .InstallMethod "kickstart"}}
      "ks.cfg" = file("${path.root}/ks.cfg")
{{- else}}
      "meta-data" = ""
      "user-data" = file("${path.root}/autoinstall.yaml")
{{- end}}
    }
{{- if eq .InstallMethod "kickstart"}}
    cd_label     = "OEMDRV"
{{- else}}
    cd_label     = "cidata"
{{- end}}
  }

  boot         = "order=scsi0;ide0;ide1;net0"
{{- if .BootCommand}}
  boot_command = [
{{- range .BootCommand}}
    "{{hclEscape .}}",
{{- end}}
  ]
{{- else}}
  boot_command = [
    "yes<enter>"
  ]
{{- end}}

  boot_wait    = "120s"
  ssh_username     = "forgemill"
  ssh_password     = "{{.SSHPassword}}"
  ssh_timeout      = "30m"
  qemu_agent           = true

  serials      = ["socket"]

  cloud_init              = true
  cloud_init_storage_pool = "{{.StoragePool}}"
}

build {
  sources = ["source.proxmox-iso.template"]

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
  // Fallback: hardcoded Ubuntu provisioner for Proxmox
  provisioner "shell" {
    inline = [
      "sudo apt-get update || true",
      "sudo apt-get install -y cloud-init cloud-initramfs-growroot qemu-guest-agent || echo 'WARNING: apt install failed - skipping'",
      "sudo systemctl enable qemu-guest-agent 2>/dev/null || true",
      "sudo rm -f /etc/netplan/*.yaml",
      "sudo rm -f /etc/cloud/cloud.cfg.d/subiquity-disable-cloudinit-networking.cfg",
      "sudo rm -f /etc/cloud/cloud.cfg.d/99-installer.cfg",
      "sudo sed -i 's/autoinstall ds=nocloud//g' /etc/default/grub",
      "sudo update-grub",
      "sudo cloud-init clean --logs --seed",
      "sudo rm -rf /var/lib/cloud/",
      "sudo truncate -s 0 /etc/machine-id",
      "sudo rm -f /var/lib/dbus/machine-id",
      "sudo passwd -l forgemill"
    ]
  }
{{- end}}
}
`
