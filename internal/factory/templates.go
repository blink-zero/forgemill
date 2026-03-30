package factory

import (
	"fmt"
	"regexp"
	"strings"
)

// Input validation patterns for Packer build config fields.
// validPath and validNetwork allow characters commonly found in vCenter resource names
// (parentheses, brackets, etc.) while blocking HCL injection chars (", \, $, `).
var (
	validName    = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,63}$`)
	validPath    = regexp.MustCompile(`^[a-zA-Z0-9/][a-zA-Z0-9._ /\-()[\]@#&+,]{0,127}$`)
	validNetwork = regexp.MustCompile(`^[/a-zA-Z0-9][a-zA-Z0-9._ /\-()[\]@#&+,]{0,255}$`)
)

// ValidateBuildConfig validates all user-supplied fields in a BuildConfig.
func ValidateBuildConfig(cfg *BuildConfig) error {
	if !validName.MatchString(cfg.TemplateName) {
		return fmt.Errorf("invalid template name: must be 1-64 alphanumeric characters with dashes, dots, or underscores")
	}
	if cfg.Datacenter != "" && !validPath.MatchString(cfg.Datacenter) {
		return fmt.Errorf("invalid datacenter name")
	}
	if cfg.Datastore != "" && !validPath.MatchString(cfg.Datastore) {
		return fmt.Errorf("invalid datastore name")
	}
	if cfg.Network != "" && !validNetwork.MatchString(cfg.Network) {
		return fmt.Errorf("invalid network name")
	}
	if cfg.Folder != "" && !validPath.MatchString(cfg.Folder) {
		return fmt.Errorf("invalid folder path")
	}
	if cfg.Cluster != "" && !validPath.MatchString(cfg.Cluster) {
		return fmt.Errorf("invalid cluster name")
	}
	if cfg.Node != "" && !validName.MatchString(cfg.Node) {
		return fmt.Errorf("invalid node name")
	}
	if cfg.StoragePool != "" && !validName.MatchString(cfg.StoragePool) {
		return fmt.Errorf("invalid storage pool name")
	}
	if cfg.Bridge != "" && !validName.MatchString(cfg.Bridge) {
		return fmt.Errorf("invalid bridge name")
	}
	if cfg.ISOStorage != "" && !validName.MatchString(cfg.ISOStorage) {
		return fmt.Errorf("invalid ISO storage name")
	}
	// ISSUE-03 fix: Validate Host field to match the same pattern as other name fields.
	// Without this, cfg.Host bypasses the regex validation that all other fields go through.
	if cfg.Host != "" && !validName.MatchString(cfg.Host) {
		return fmt.Errorf("invalid host name")
	}
	if cfg.CPU < 1 || cfg.CPU > 128 {
		return fmt.Errorf("CPU must be between 1 and 128")
	}
	if cfg.MemoryMB < 512 || cfg.MemoryMB > 524288 {
		return fmt.Errorf("memory must be between 512MB and 512GB")
	}
	if cfg.DiskGB < 10 || cfg.DiskGB > 16384 {
		return fmt.Errorf("disk must be between 10GB and 16TB")
	}
	return nil
}

// BuildConfig holds all parameters needed to generate Packer HCL.
type BuildConfig struct {
	TemplateName string `json:"template_name"`
	CPU          int    `json:"cpu"`
	MemoryMB     int    `json:"memory_mb"`
	DiskGB       int    `json:"disk_gb"`

	// VMware-specific
	Datacenter string `json:"datacenter,omitempty"`
	Cluster    string `json:"cluster,omitempty"`
	Host       string `json:"host,omitempty"` // ESXi standalone host
	Datastore  string `json:"datastore,omitempty"`
	Folder     string `json:"folder,omitempty"`
	Network    string `json:"network,omitempty"`

	// Build networking (for SSH during build)
	BuildIP string `json:"build_ip,omitempty"`
	Netmask string `json:"netmask,omitempty"`
	Gateway string `json:"gateway,omitempty"`

	// Proxmox-specific
	Node        string `json:"node,omitempty"`
	StoragePool string `json:"storage_pool,omitempty"`
	Bridge      string `json:"bridge,omitempty"`
	ISOStorage  string `json:"iso_storage,omitempty"`
}

// TemplateData is fed into the Packer HCL Go templates.
type TemplateData struct {
	TemplateName string
	GuestOSType  string
	ProxmoxOS    string
	CPU          int
	MemoryMB     int
	DiskGB       int
	DiskMB       int
	ISOURL       string
	ISOChecksum  string

	// Target connection
	TargetHostname string
	TargetPort     int
	TargetUsername string
	TargetPassword string
	// V3-M10: Propagate TLS validation setting from target to Packer templates
	InsecureConnection bool

	// SSH build credentials (random per build)
	SSHPassword string

	// VMware
	Datacenter string
	Cluster    string
	Host       string // ESXi standalone host (used instead of cluster)
	Datastore  string
	Folder     string
	Network    string

	// Proxmox
	Node           string
	StoragePool    string
	Bridge         string
	ISOStorage     string
	ISODownloadPVE bool

	// Build-time SSH host (IP the VM will have during build)
	SSHHost string

	// ESXi standalone mode (no convert_to_template — vCenter-only feature)
	ESXiStandalone bool

	// Extended fields for OS modularity (Phase 1 - OS expandability refactor)
	// These enable dynamic HCL generation based on OS definition rather than hardcoded values.
	OSFamily          string   // e.g. "ubuntu", "rhel", "debian" — from OSDefinition.Family
	InstallMethod     string   // e.g. "autoinstall", "kickstart", "preseed" — from OSDefinition.InstallMethod
	InstallerFilename string   // e.g. "autoinstall.yaml", "ks.cfg" — from Installer.Filename()
	BootCommand       []string // Resolved boot commands for this platform's delivery type
	ProvisionerCmds   []string // OS-specific provisioner shell commands
	UseHTTPDelivery   bool     // true = serve installer config via HTTP; false = via CD-ROM
}

// hclEscape escapes a string for safe inclusion in HCL quoted strings.
// I1: Prevents HCL injection through values like passwords containing ", \, or ${}.
func hclEscape(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	s = strings.ReplaceAll(s, `${`, `$${`)
	// HIGH-01: Escape newline/CR/tab to prevent HCL string breakout injection
	s = strings.ReplaceAll(s, "\n", `\n`)
	s = strings.ReplaceAll(s, "\r", `\r`)
	s = strings.ReplaceAll(s, "\t", `\t`)
	return s
}

// GeneratePackerHCL creates the Packer HCL content for the given target type and template data.
// It applies centralized HCL escaping for defense-in-depth, then delegates to the
// registered Platform implementation for template rendering.
func GeneratePackerHCL(targetType string, data TemplateData) (string, error) {
	// I1 + V3-L8: HCL-escape ALL interpolated string values for defense-in-depth
	data.TargetPassword = hclEscape(data.TargetPassword)
	data.SSHPassword = hclEscape(data.SSHPassword)
	data.TargetUsername = hclEscape(data.TargetUsername)
	data.TargetHostname = hclEscape(data.TargetHostname)
	data.TemplateName = hclEscape(data.TemplateName)
	data.Datacenter = hclEscape(data.Datacenter)
	data.Cluster = hclEscape(data.Cluster)
	data.Host = hclEscape(data.Host)
	data.Datastore = hclEscape(data.Datastore)
	data.Folder = hclEscape(data.Folder)
	data.Network = hclEscape(data.Network)
	data.Node = hclEscape(data.Node)
	data.StoragePool = hclEscape(data.StoragePool)
	data.Bridge = hclEscape(data.Bridge)
	data.ISOStorage = hclEscape(data.ISOStorage)
	data.GuestOSType = hclEscape(data.GuestOSType)
	data.ProxmoxOS = hclEscape(data.ProxmoxOS)
	data.ISOURL = hclEscape(data.ISOURL)
	data.ISOChecksum = hclEscape(data.ISOChecksum)

	plat, err := GetPlatform(targetType)
	if err != nil {
		return "", fmt.Errorf("unsupported target type: %s", targetType)
	}

	return plat.GenerateHCL(data)
}
