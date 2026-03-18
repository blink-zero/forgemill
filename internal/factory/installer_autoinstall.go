package factory

import (
	"bytes"
	"fmt"
	"text/template"
)

// AutoinstallInstaller generates Ubuntu autoinstall (cloud-config) YAML.
type AutoinstallInstaller struct{}

func init() {
	RegisterInstaller(&AutoinstallInstaller{})
}

func (a *AutoinstallInstaller) Method() string   { return "autoinstall" }
func (a *AutoinstallInstaller) Filename() string { return "autoinstall.yaml" }

func (a *AutoinstallInstaller) GenerateConfig(params InstallerParams) (string, error) {
	passwordHash, err := sha512Crypt(params.Password)
	if err != nil {
		return "", fmt.Errorf("generate password hash: %w", err)
	}

	useDHCP := params.BuildIP == ""

	data := autoinstallData{
		Hostname:             params.Hostname,
		Username:             params.Username,
		PasswordHash:         passwordHash,
		PlainPassword:        params.Password,
		SSHPublicKey:         params.SSHPublicKey,
		Timezone:             params.Timezone,
		Locale:               params.Locale,
		Keyboard:             params.Keyboard,
		Packages:             params.ExtraPackages,
		InterfaceName:        params.InterfaceName,
		BuildIP:              params.BuildIP,
		Netmask:              params.Netmask,
		Gateway:              params.Gateway,
		PrefixLen:            netmaskToPrefixLen(params.Netmask),
		UseDHCP:              useDHCP,
		CloudInitDatasources: params.CloudInitDatasources,
		CloudInitExtraLines:  params.CloudInitExtraLines,
	}

	tmpl, err := template.New("autoinstall").Parse(autoinstallTemplate)
	if err != nil {
		return "", fmt.Errorf("parse autoinstall template: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("execute autoinstall template: %w", err)
	}

	return buf.String(), nil
}

// autoinstallData holds parameters for generating autoinstall configuration.
type autoinstallData struct {
	Hostname      string
	Username      string
	PasswordHash  string
	PlainPassword string
	SSHPublicKey  string
	Timezone      string
	Locale        string
	Keyboard      string
	Packages      []string
	InterfaceName string // NIC name varies by platform (ens192 for VMware, ens18 for Proxmox)
	// Build-time networking (optional static IP; DHCP if empty)
	BuildIP   string // Static IP for build-time SSH access
	Netmask   string // e.g. "255.255.255.0" -> /24
	Gateway   string
	PrefixLen int  // CIDR prefix length (e.g. 24)
	UseDHCP   bool // true when no BuildIP is specified
	// Platform-injectable fields — no TargetType conditionals needed.
	// New hypervisors just implement Platform.InstallerHints().
	CloudInitDatasources string   // e.g. "[NoCloud, ConfigDrive, None]"
	CloudInitExtraLines  []string // e.g. ["disable_vmware_customization: false"]
}

// netmaskToPrefixLen converts a dotted netmask to CIDR prefix length.
func netmaskToPrefixLen(netmask string) int {
	switch netmask {
	case "255.255.255.0":
		return 24
	case "255.255.0.0":
		return 16
	case "255.0.0.0":
		return 8
	case "255.255.255.128":
		return 25
	case "255.255.255.192":
		return 26
	case "255.255.255.224":
		return 27
	case "255.255.255.240":
		return 28
	default:
		return 24
	}
}

// autoinstallTemplate follows VMware's official packer-examples-for-vsphere pattern.
// Key design decisions (from VMware guidance):
//   - early-commands stops SSH so Packer can't connect during install phase
//   - Network config is inline (static or DHCP) — no late-command hacks
//   - apt fallback: offline-install prevents subiquity from aborting when apt
//     mirrors are unreachable during install (the default is fallback: abort)
//   - apt fallback: offline-install ensures the build doesn't abort on mirror failure
//   - Provisioner handles final cleanup before template conversion
//
// IMPORTANT: This template contains ZERO platform conditionals. All platform-specific
// values (datasources, extra cloud.cfg lines) are injected via InstallerParams from
// Platform.InstallerHints(). To add a new hypervisor, implement InstallerHints() on
// the new Platform — do NOT add if/else blocks here.
const autoinstallTemplate = `#cloud-config
autoinstall:
  version: 1
  locale: "{{.Locale}}"
  keyboard:
    layout: "{{.Keyboard}}"
  refresh-installer:
    update: false
  updates: security
  apt:
    geoip: false
    mirror-selection:
      primary:
        - uri: "http://archive.ubuntu.com/ubuntu"
      fallback: offline-install
  source:
    search_drivers: false
    id: ubuntu-server
  network:
    network:
      version: 2
      ethernets:
{{- if .UseDHCP}}
        {{.InterfaceName}}:
          dhcp4: true
{{- else}}
        {{.InterfaceName}}:
          dhcp4: false
          addresses:
            - {{.BuildIP}}/{{.PrefixLen}}
          routes:
            - to: default
              via: {{.Gateway}}
          nameservers:
            addresses:
              - 8.8.8.8
              - 1.1.1.1
{{- end}}
  identity:
    hostname: "{{.Hostname}}"
    username: "{{.Username}}"
    password: "{{.PasswordHash}}"
  ssh:
    install-server: true
    allow-pw: true{{if .SSHPublicKey}}
    authorized-keys:
      - "{{.SSHPublicKey}}"{{end}}
  storage:
    layout:
      name: lvm
      sizing-policy: all
  packages:
    - openssh-server
    - cloud-init
    - cloud-initramfs-growroot
    - curl
    - wget{{range .Packages}}
    - {{.}}{{end}}
  early-commands:
    # Stop SSH during install so Packer doesn't connect prematurely
    - sudo systemctl stop ssh
    - systemctl stop snapd.service snapd.socket snapd.seeded.service || true
    - snap set system refresh.hold="2099-01-01T00:00:00+00:00" || true
    # Prevent unattended-upgrades from running during install (belt-and-suspenders
    # alongside updates: none — handles edge cases where subiquity ignores the key)
    - systemctl stop unattended-upgrades || true
    - systemctl disable unattended-upgrades || true
  late-commands:
    - 'echo "{{.Username}} ALL=(ALL) NOPASSWD:ALL" > /target/etc/sudoers.d/{{.Username}}'
    - 'chmod 440 /target/etc/sudoers.d/{{.Username}}'
    - curtin in-target --target=/target -- systemctl disable snapd.service snapd.socket || true
    - curtin in-target --target=/target -- systemctl enable ssh || true{{if .SSHPublicKey}}
    - "mkdir -p /target/home/{{.Username}}/.ssh && echo '{{.SSHPublicKey}}' > /target/home/{{.Username}}/.ssh/authorized_keys"
    - "chmod 700 /target/home/{{.Username}}/.ssh && chmod 600 /target/home/{{.Username}}/.ssh/authorized_keys"
    - curtin in-target --target=/target -- chown -R {{.Username}}:{{.Username}} /home/{{.Username}}/.ssh{{end}}
    - sed -i -e 's/^#\?PasswordAuthentication.*/PasswordAuthentication yes/g' /target/etc/ssh/sshd_config
    # Ubuntu 24.04 uses sshd_config.d drop-in files that override the main config
    - sed -i -e 's/^#\?PasswordAuthentication.*/PasswordAuthentication yes/g' /target/etc/ssh/sshd_config.d/*.conf 2>/dev/null || true
    - 'echo "PasswordAuthentication yes" > /target/etc/ssh/sshd_config.d/99-forgemill.conf'
    # Cloud-init template preparation
    - "mkdir -p /target/etc/cloud/cloud.cfg.d"
    # Set datasource_list — overwrite 90_dpkg.cfg (Ubuntu default) to prioritise the
    # correct datasource for this hypervisor. Must overwrite 90_dpkg.cfg because it
    # sorts AFTER any 90-*.cfg file in ASCII (underscore > hyphen), and cloud-init's
    # last-wins merge would override our list.
    - 'echo "datasource_list: {{.CloudInitDatasources}}" > /target/etc/cloud/cloud.cfg.d/90_dpkg.cfg'
{{- range .CloudInitExtraLines}}
    # Platform-specific cloud.cfg line (injected by Platform.InstallerHints)
    - 'grep -q "{{.}}" /target/etc/cloud/cloud.cfg || echo "{{.}}" >> /target/etc/cloud/cloud.cfg'
{{- end}}
    - |
      cat > /target/etc/cloud/cloud.cfg.d/91-growpart.cfg << 'GROWEOF'
      growpart:
        mode: auto
        devices: [/]
        ignore_growroot_disabled: false
      resize_rootfs: true
      GROWEOF
    - curtin in-target --target=/target -- update-grub || true
  user-data:
    disable_root: true
    timezone: "{{.Timezone}}"
    # Preserve the build user's password through cloud-init first boot
    chpasswd:
      expire: false
      list:
        - {{.Username}}:{{.PlainPassword}}
    ssh_pwauth: true
`
