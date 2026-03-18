# Adding a New OS or Hypervisor to the Template Factory

## Architecture Overview

The Template Factory uses a plugin-style registry system with three extension points.
Adding new OSes or hypervisors requires **no changes to existing files** — just new ones.

```
┌─────────────────────────┐     ┌──────────────────────────────┐
│  OS Definition          │     │  Platform                    │
│  (os_ubuntu.go)         │     │  (platform_proxmox.go)       │
│                         │     │                              │
│  • ISO URL/checksum     │     │  • InterfaceName()           │
│  • BootCommandCD/HTTP   │     │  • ProvisionerPackages()     │
│  • ProvisionerCmds      │     │  • InstallerHints()  ←─ NEW  │
│  • InstallMethod        │     │  • AdjustTemplateData()      │
│  • Family/Version       │     │  • GenerateHCL()             │
└──────────┬──────────────┘     └──────────────┬───────────────┘
           │                                   │
           ▼                                   ▼
┌─────────────────────────┐     ┌──────────────────────────────┐
│  Installer              │     │  Engine (engine.go)          │
│  (installer_autoinstall)│     │                              │
│                         │◄────│  Wires OS + Platform +       │
│  • GenerateConfig()     │     │  Installer together          │
│  • Uses InstallerParams │     │  No hardcoded OS/platform    │
│    with platform hints  │     │  logic                       │
└─────────────────────────┘     └──────────────────────────────┘
```

### Key Design Principle: Platform-Injectable Installer Params

Installer templates (autoinstall.yaml, ks.cfg) contain **zero platform conditionals**.
All platform-specific values (cloud-init datasources, extra config lines, services)
are injected via `InstallerParams` from `Platform.InstallerHints()`. This means:

- Adding a new hypervisor **never** requires editing installer templates
- Adding a new OS **never** requires editing platform files (unless new install method)

---

## Adding a New Hypervisor

**Risk to existing templates: zero.** Each platform is fully self-contained.

### Step 1 — Create platform file

Create `internal/factory/platform_hyperv.go`:

```go
package factory

func init() {
    RegisterPlatform(&HyperVPlatform{})
}

type HyperVPlatform struct{}

func (h *HyperVPlatform) Types() []string      { return []string{"hyperv"} }
func (h *HyperVPlatform) InterfaceName() string { return "eth0" }

func (h *HyperVPlatform) ProvisionerPackages(osFamily string) []string {
    // Hyper-V guest tools package varies by OS family
    if osFamily == "rhel" {
        return []string{"hyperv-daemons"}
    }
    return []string{"linux-cloud-tools-common", "linux-tools-virtual"}
}

func (h *HyperVPlatform) InstallerHints() InstallerHints {
    return InstallerHints{
        CloudInitDatasources: "[Azure, None]",
        CloudInitExtraLines:  nil,
        PlatformServices:     []string{"hv_kvp_daemon", "hv_vss_daemon"},
    }
}

func (h *HyperVPlatform) AdjustTemplateData(data *TemplateData, targetType string, osDef *OSDefinition) {
    // Set platform defaults and resolve boot/provisioner commands from OS def
    // Follow the pattern in platform_proxmox.go or platform_vsphere.go
}

func (h *HyperVPlatform) GenerateHCL(data TemplateData) (string, error) {
    // Generate Packer HCL for hyperv-iso builder
    // See platform_proxmox.go for the template pattern
}
```

**That's it.** The `init()` function registers the platform automatically.
No existing files need modification.

### Files created: 1 | Files modified: 0

---

## Adding a New OS Version (Same Family)

**Risk to existing templates: zero.** New definition reuses existing boot commands,
provisioner commands, and installer.

### Example: Ubuntu 26.04

Add to `internal/factory/os_ubuntu.go`:

```go
RegisterOSDefinition(OSDefinition{
    ID:              "ubuntu-2604",
    Name:            "Ubuntu 26.04 LTS",
    Family:          "ubuntu",
    Version:         "26.04",
    Arch:            "amd64",
    ISOURLPattern:   "https://releases.ubuntu.com/26.04/ubuntu-26.04-live-server-amd64.iso",
    ISOChecksumURL:  "https://releases.ubuntu.com/26.04/SHA256SUMS",
    GuestOSType:     "ubuntu64Guest",
    ProxmoxOSType:   "l26",
    MinDiskGB:       20,
    MinMemoryMB:     2048,
    MinCPU:          2,
    BootCommand:     ubuntuBootCommandHTTP,
    InstallMethod:   "autoinstall",
    BootCommandCD:   ubuntuBootCommandCD,
    BootCommandHTTP: ubuntuBootCommandHTTP,
    ProvisionerCmds: ubuntuProvisionerCmds,
})
```

If the new version needs different boot/provisioner commands (e.g. Ubuntu changes its
casper path), create new command slices and reference those instead — the existing
versions stay unchanged.

### Files created: 0 | Files modified: 1 (add definition to existing os_ubuntu.go)

---

## Adding a New OS Family (New Install Method)

**Risk to existing templates: zero.** New installer file, new OS file.

### Example: Debian 12 (preseed)

#### Step 1 — Create OS definition

Create `internal/factory/os_debian.go`:

```go
package factory

var debianBootCommandCD = []string{
    // Debian-specific GRUB/isolinux boot sequence
}

var debianProvisionerCmds = []string{
    "sudo apt-get update || true",
    "sudo apt-get install -y cloud-init",
    // ... standard template cleanup
}

func init() {
    RegisterOSDefinition(OSDefinition{
        ID:              "debian-12",
        Name:            "Debian 12 (Bookworm)",
        Family:          "debian",
        Version:         "12",
        InstallMethod:   "preseed",
        BootCommandCD:   debianBootCommandCD,
        ProvisionerCmds: debianProvisionerCmds,
        // ... remaining fields
    })
}
```

#### Step 2 — Create installer

Create `internal/factory/installer_preseed.go`:

```go
package factory

type PreseedInstaller struct{}

func init() { RegisterInstaller(&PreseedInstaller{}) }

func (p *PreseedInstaller) Method() string   { return "preseed" }
func (p *PreseedInstaller) Filename() string { return "preseed.cfg" }

func (p *PreseedInstaller) GenerateConfig(params InstallerParams) (string, error) {
    // Use params.CloudInitDatasources, params.CloudInitExtraLines,
    // params.PlatformServices — these are populated automatically from
    // Platform.InstallerHints(). No TargetType conditionals needed.
}
```

#### Step 3 — Update HCL templates for new CD label/content

The platform HCL templates need to know the CD label and content file for the new
install method. This is the **one place** where an existing platform file needs a
minor addition:

In `platform_proxmox.go` and `platform_vsphere.go` HCL template:

```hcl
{{- if eq .InstallMethod "preseed"}}
    cd_label = "OEMDRV"
    cd_content = { "preseed.cfg" = file("${path.root}/preseed.cfg") }
{{- end}}
```

### Files created: 2 | Files modified: 2 (add cd_content case to HCL templates)

---

## Interface Reference

### Platform Interface

```go
type Platform interface {
    Types() []string                    // Target type strings (e.g. ["proxmox"])
    InterfaceName() string              // Default NIC name (e.g. "ens18")
    ProvisionerPackages(os string) []string  // Guest agent packages
    InstallerHints() InstallerHints     // Cloud-init datasources, services, extra config
    AdjustTemplateData(data *TemplateData, targetType string, osDef *OSDefinition)
    GenerateHCL(data TemplateData) (string, error)
}
```

### InstallerHints Struct

```go
type InstallerHints struct {
    CloudInitDatasources string     // e.g. "[NoCloud, ConfigDrive, None]"
    CloudInitExtraLines  []string   // e.g. ["disable_vmware_customization: false"]
    PlatformServices     []string   // e.g. ["qemu-guest-agent"] or ["vmtoolsd"]
}
```

### Installer Interface

```go
type Installer interface {
    Method() string                          // e.g. "autoinstall", "kickstart"
    Filename() string                        // e.g. "autoinstall.yaml", "ks.cfg"
    GenerateConfig(InstallerParams) (string, error)
}
```

### InstallerParams (platform-injected fields)

```go
type InstallerParams struct {
    // ... standard fields (Username, Password, Hostname, etc.)

    // Platform-injectable — populated from Platform.InstallerHints()
    CloudInitDatasources string     // Injected into installer templates
    CloudInitExtraLines  []string   // Injected into installer templates
    PlatformServices     []string   // Injected into installer templates
}
```

---

## Current Inventory

### Platforms
| Platform | Target Types | NIC | Guest Agent |
|----------|-------------|-----|-------------|
| vSphere | `vcenter`, `esxi` | ens192 | open-vm-tools |
| Proxmox | `proxmox` | ens18 | qemu-guest-agent |

### OS Definitions
| ID | Family | Install Method | Platforms Tested |
|----|--------|---------------|-----------------|
| ubuntu-2404 | ubuntu | autoinstall | Proxmox, vSphere |
| ubuntu-2204 | ubuntu | autoinstall | Proxmox, vSphere |
| rocky-9 | rhel | kickstart | Proxmox, vSphere |
| rocky-8 | rhel | kickstart | Proxmox, vSphere |

### Installers
| Method | Filename | OS Families |
|--------|----------|-------------|
| autoinstall | autoinstall.yaml | Ubuntu/Debian (subiquity) |
| kickstart | ks.cfg | RHEL/Rocky/Alma |

---

## File Reference

| File | Purpose |
|------|---------|
| `internal/factory/definitions.go` | `OSDefinition` struct |
| `internal/factory/os_registry.go` | OS definition registry |
| `internal/factory/os_ubuntu.go` | Ubuntu 22.04 + 24.04 definitions |
| `internal/factory/os_rocky.go` | Rocky Linux 8 + 9 definitions |
| `internal/factory/installer.go` | `Installer` + `InstallerParams` + `InstallerHints` |
| `internal/factory/installer_autoinstall.go` | Ubuntu autoinstall generator |
| `internal/factory/installer_kickstart.go` | RHEL/Rocky kickstart generator |
| `internal/factory/platform.go` | `Platform` interface + registry |
| `internal/factory/platform_vsphere.go` | vSphere/ESXi HCL + `InstallerHints()` |
| `internal/factory/platform_proxmox.go` | Proxmox HCL + `InstallerHints()` |
| `internal/factory/engine.go` | Build orchestration (wires everything together) |
| `internal/factory/templates.go` | `BuildConfig`, `TemplateData`, HCL escaping |
| `internal/factory/installer_hints_test.go` | Tests for all platform × OS combos |
