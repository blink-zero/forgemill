package provider

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"
)

// Sentinel errors providers wrap so callers can map them to a specific
// response (errors.Is) instead of matching on error text.
var (
	// ErrNotSupported: the operation isn't implemented for this provider's
	// platform. Callers surface a clear "not available for X targets" message.
	ErrNotSupported = errors.New("operation not supported by this provider")
	// ErrNetworkNotFound: the requested network/portgroup doesn't resolve on
	// the hypervisor.
	ErrNetworkNotFound = errors.New("network not found")
	// ErrInvalidAdapterType: the requested NIC adapter model isn't one the
	// provider will create.
	ErrInvalidAdapterType = errors.New("invalid adapter type")
	// ErrVLANUnsupportedOnNetwork: a VLAN tag was requested on a Proxmox bridge
	// that is not VLAN aware — Proxmox would accept the config and then fail
	// to hot-plug (and later to boot) the NIC.
	ErrVLANUnsupportedOnNetwork = errors.New("network is not VLAN aware")
	// ErrRequiresPowerOff: the change (CPU/memory resize without hot-add,
	// etc.) can only be made while the VM is powered off.
	ErrRequiresPowerOff = errors.New("VM must be powered off for this change")
	// ErrDatastoreNotFound: the datastore/storage named in a DiskSpec does not
	// exist on the target (or isn't visible to the VM's host/node).
	ErrDatastoreNotFound = errors.New("datastore not found")
	// ErrDatastoreNotAccessible: the datastore exists but the host the VM runs
	// on cannot reach it (a local datastore of another host, an unmounted
	// NFS export), so a disk cannot be created there for this VM.
	ErrDatastoreNotAccessible = errors.New("datastore not accessible from the VM's host")
)

// PV-X1: All Provider interface methods now accept context.Context for
// per-operation timeouts, cancellation propagation, and tracing support.
type Provider interface {
	Connect(ctx context.Context) error
	Disconnect() error
	TestConnection(ctx context.Context) error

	ListTemplates(ctx context.Context) ([]Template, error)
	GetTemplate(ctx context.Context, id string) (*Template, error)
	GetTemplateDetail(ctx context.Context, id string) (*TemplateDetail, error)

	DeployVM(ctx context.Context, spec *DeploySpec) (*DeployResult, error)
	GetDeployProgress(ctx context.Context, taskID string) (*Progress, error)

	PowerOn(ctx context.Context, vmID string) error
	PowerOff(ctx context.Context, vmID string) error
	Restart(ctx context.Context, vmID string) error
	Suspend(ctx context.Context, vmID string) error
	DeleteVM(ctx context.Context, vmID string) error
	GetVMStatus(ctx context.Context, vmID string) (*VMStatus, error)

	ListSnapshots(ctx context.Context, vmID string) ([]Snapshot, error)
	CreateSnapshot(ctx context.Context, vmID string, name string, description string, memory bool) error
	RevertSnapshot(ctx context.Context, vmID string, snapshotRef string) error
	DeleteSnapshot(ctx context.Context, vmID string, snapshotRef string) error
	ResizeVM(ctx context.Context, vmID string, cpu int, memoryMB int) error
	ListDisks(ctx context.Context, vmID string) ([]Disk, error)
	ExpandDisk(ctx context.Context, vmID string, diskKey int, newSizeGB int) error
	// AddDisk attaches an additional virtual disk to an existing VM and
	// returns it as the hypervisor reports it afterwards. No power cycle;
	// existing devices are untouched. A datastore/storage that doesn't
	// resolve returns ErrDatastoreNotFound (wrapped).
	AddDisk(ctx context.Context, vmID string, spec DiskSpec) (*Disk, error)
	// AddNIC attaches an additional virtual network adapter to an existing
	// VM and returns the adapter as the hypervisor reports it afterwards.
	// Providers that don't implement it return ErrNotSupported (wrapped);
	// a network that doesn't resolve returns ErrNetworkNotFound (wrapped).
	AddNIC(ctx context.Context, vmID string, spec NICSpec) (*NIC, error)
	// ListNICs returns the VM's virtual network adapters as the hypervisor
	// reports them right now, with guest-reported addresses joined in by
	// MAC where guest tools / the guest agent make them available.
	ListNICs(ctx context.Context, vmID string) ([]NIC, error)
	GetConsoleURL(ctx context.Context, vmID string) (string, error)

	ListVMs(ctx context.Context) ([]VMInfo, error)
	GetResources(ctx context.Context) (*Resources, error)

	// ValidateDeploySpec resolves every named resource in spec (datacenter,
	// cluster, folder, datastore, network, host) using the exact same
	// lookup DeployVM uses, without creating or reserving anything. It
	// exists so pre-flight validation can never give a false "would
	// succeed" for a value that a looser, list-based check would accept
	// but the real deploy's resolver would reject — e.g. vCenter networks,
	// where a bare display name only resolves if the object happens to sit
	// directly under the default inventory folder; nested ones need the
	// full path. Returns one error per resource that failed to resolve.
	ValidateDeploySpec(ctx context.Context, spec *DeploySpec) []error
}

// PV-X3: Canonical progress state constants for cross-provider consistency.
const (
	ProgressStateQueued  = "queued"
	ProgressStateRunning = "running"
	ProgressStateSuccess = "success"
	ProgressStateError   = "error"
)

// NormalizePowerState normalizes provider-specific power state strings (PV-X2).
func NormalizePowerState(state string) string {
	switch state {
	case "running":
		return "poweredOn"
	case "stopped":
		return "poweredOff"
	case "paused":
		return "suspended"
	default:
		return state
	}
}

type Template struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	OSType   string `json:"os_type"`
	GuestID  string `json:"guest_id"`
	CPU      int    `json:"cpu"`
	MemoryMB int    `json:"memory_mb"`
	DiskGB   int    `json:"disk_gb"`
	Moref    string `json:"moref"`
}

type TemplateDetail struct {
	Template
	Datastore   string   `json:"datastore"`
	Folder      string   `json:"folder,omitempty"`
	Networks    []string `json:"networks"`
	Annotation  string   `json:"annotation"`
	ToolsStatus string   `json:"tools_status"`
	HardwareVer string   `json:"hardware_version"`
	Firmware    string   `json:"firmware"`
	CreatedAt   string   `json:"created_at"`
	Platform    string   `json:"platform"`
	// Proxmox-specific fields
	Node       string `json:"node,omitempty"`
	CPUType    string `json:"cpu_type,omitempty"`
	SCSIType   string `json:"scsi_type,omitempty"`
	CloudInit  bool   `json:"cloud_init,omitempty"`
	DiskFormat string `json:"disk_format,omitempty"`
}

type DeploySpec struct {
	TemplateName     string
	VMName           string
	Datacenter       string
	Cluster          string
	Datastore        string
	Folder           string
	Network          string
	CPU              int
	MemoryMB         int
	DiskGB           int
	IPAddress        string
	Netmask          string
	Gateway          string
	DNS              []string
	Hostname         string
	DomainName       string
	OSType           string // "linux" or "windows"
	LinkedClone      bool   // PV-P5: support for linked clones
	PasswordHash     string // SHA-512 crypt hash for cloud-init credential injection
	PlainPassword    string // BUG-03: Plaintext password for Proxmox cipassword
	SSHPublicKey     string // Optional SSH public key to inject
	UserDataOverride string // Pre-merged cloud-init userdata (when actions are selected)
	DiskProvisioning string // "thin", "thick", "thick_eager_zero", or "" (inherit from template)
	Host             string // Optional: specific ESXi host within a vCenter cluster for placement
	VLANTag          int    // Optional 802.1Q VLAN tag (1-4094), 0 = untagged. Proxmox-only.
}

type DeployResult struct {
	TaskID string
	VMID   string
}

type Progress struct {
	Percent int    `json:"percent"`
	State   string `json:"state"`
	Message string `json:"message"`
}

type VMStatus struct {
	PowerState string `json:"power_state"`
	IPAddress  string `json:"ip_address"`
	HostName   string `json:"host_name"`
	CPU        int    `json:"cpu"`
	MemoryMB   int    `json:"memory_mb"`
	DiskGB     int    `json:"disk_gb"`
	GuestID    string `json:"guest_id"`
	// GuestOS is what the guest itself reports when an agent / Tools is
	// running — "Ubuntu 22.04.4 LTS", "Ubuntu Linux (64-bit)" — and beats
	// GuestID, which on Proxmox is only ever a family (l26 / win11).
	GuestOS string `json:"guest_os,omitempty"`
}

type Resources struct {
	Datastores    []ResourceItem    `json:"datastores"`
	Networks      []ResourceItem    `json:"networks"`
	Folders       []ResourceItem    `json:"folders"`
	Clusters      []ResourceItem    `json:"clusters"`
	Datacenters   []ResourceItem    `json:"datacenters"`
	ISOStorages   []ResourceItem    `json:"iso_storages,omitempty"`
	ResourcePools []ResourceItem    `json:"resource_pools"` // PV-X6
	Hosts         []ResourceItem    `json:"hosts,omitempty"`
	Platform      string            `json:"platform"`
	Defaults      map[string]string `json:"defaults,omitempty"`
}

type ResourceItem struct {
	Name string `json:"name"`
	ID   string `json:"id"`
	Path string `json:"path,omitempty"`
}

type Snapshot struct {
	Ref         string `json:"ref"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Created     string `json:"created"`
}

type Disk struct {
	Key    int    `json:"key"`
	Label  string `json:"label"`
	SizeGB int    `json:"size_gb"`
	// Datastore (vSphere) or storage (Proxmox) the disk lives on.
	Datastore string `json:"datastore,omitempty"`
	// Provisioning is "thin" or "thick" where the hypervisor reports it
	// (vSphere); Proxmox reports the volume format (qcow2, raw) if known.
	Provisioning string `json:"provisioning,omitempty"`
	// Backing is the disk file (vSphere "[ds] vm/vm_1.vmdk") or volume
	// (Proxmox "local-lvm:vm-100-disk-1").
	Backing string `json:"backing,omitempty"`
	// Pending (Proxmox): saved to the config but only attaches at the next
	// power cycle because the VM's hotplug setting excludes "disk".
	Pending bool `json:"pending,omitempty"`
}

// DiskSpec describes a virtual disk to attach with AddDisk.
type DiskSpec struct {
	SizeGB int
	// Datastore (vSphere) or storage (Proxmox) name, in the form
	// GetResources reports it. Empty = the same one as the VM's first disk.
	Datastore string
	// Provisioning is "thin" or "thick" on providers that publish
	// ProviderMetadata.DiskProvisioningTypes; empty = provider default.
	Provisioning string
}

// NICSpec describes a network adapter to attach with AddNIC.
type NICSpec struct {
	// Network is the network/portgroup name or full inventory path, in the
	// same form GetResources reports it (and DeploySpec.Network accepts).
	Network string
	// AdapterType is the provider-specific adapter model. Empty means the
	// provider's default (vmxnet3 on vSphere).
	AdapterType string
	// Connected controls whether the adapter is connected immediately (when
	// the VM is running) and at the next power-on.
	Connected bool
	// VLANTag is an optional 802.1Q tag (1-4094) for providers whose VLAN
	// membership is a NIC property (Proxmox). 0 = untagged. Ignored by
	// providers where VLAN is part of the network itself (vSphere).
	VLANTag int
}

// NIC is a virtual network adapter as the hypervisor reports it.
type NIC struct {
	Key         int    `json:"key"`
	Label       string `json:"label"`
	AdapterType string `json:"adapter_type"`
	Network     string `json:"network"`
	MACAddress  string `json:"mac_address"`
	// Connected is the live link state: true only while the VM is running
	// with the adapter attached. StartConnected is the configured intent —
	// the adapter connects when the VM powers on. A powered-off VM therefore
	// reports Connected=false, StartConnected=true for a normal adapter.
	Connected      bool `json:"connected"`
	StartConnected bool `json:"start_connected"`
	VLANTag        int  `json:"vlan_tag,omitempty"`
	// Pending: the hypervisor accepted the adapter but will only attach it
	// at the next power cycle (Proxmox with network hot-plug disabled).
	Pending bool `json:"pending,omitempty"`
	// Addresses are the guest-reported IPs on this adapter (IPv4 first),
	// empty when guest tools / the guest agent aren't reporting.
	Addresses []string `json:"addresses"`
}

// SortAddresses orders guest-reported addresses for display: IPv4 before
// IPv6, link-local last within each family, otherwise stable.
func SortAddresses(addrs []string) []string {
	rank := func(a string) int {
		isV6 := strings.Contains(a, ":")
		linkLocal := strings.HasPrefix(a, "169.254.") || strings.HasPrefix(strings.ToLower(a), "fe80:")
		r := 0
		if isV6 {
			r += 2
		}
		if linkLocal {
			r++
		}
		return r
	}
	out := append([]string(nil), addrs...)
	sort.SliceStable(out, func(i, j int) bool { return rank(out[i]) < rank(out[j]) })
	return out
}

// VMInfo is one entry of a target-wide listing. It carries the same fields
// GetVMStatus returns so a sync can be fed from a single listing instead
// of one status call per VM; a provider that can't fill a field from its
// listing leaves it zero and the caller falls back to GetVMStatus.
type VMInfo struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	PowerState string `json:"power_state"`
	IPAddress  string `json:"ip_address"`
	CPU        int    `json:"cpu"`
	MemoryMB   int    `json:"memory_mb"`
	DiskGB     int    `json:"disk_gb"`
	GuestID    string `json:"guest_id"`
	// Host is the node/host the VM lives on where the listing already says
	// so (Proxmox node); empty otherwise. Informational, shown by Discover.
	Host string `json:"host,omitempty"`
}

// TargetHostKeyStore persists one SSH host-key fingerprint per target for
// trust-on-first-use verification of SSH connections a provider opens to
// the hypervisor itself (Proxmox snippet uploads). The DB implements it.
type TargetHostKeyStore interface {
	GetTargetSSHHostKeyFP(targetID int64) (string, error)
	UpdateTargetSSHHostKeyFP(targetID int64, fingerprint string) error
}

// HostKeyTrusting is implemented by providers that talk SSH to the target
// and want TOFU host-key verification. The service wires it right after
// constructing the provider; without it the provider accepts any host key.
type HostKeyTrusting interface {
	SetTOFU(targetID int64, store TargetHostKeyStore)
}

// ExtrasValidator is implemented by providers that can check an extra NIC /
// disk request against the target without creating anything — the deploy
// preflight uses it so a bad network, a VLAN tag on a non-VLAN-aware bridge
// or an unknown datastore is a blocker before the clone, not a failed
// deployment after it. datacenter is the deploy's datacenter (vSphere).
type ExtrasValidator interface {
	ValidateNICSpec(ctx context.Context, datacenter string, spec NICSpec) error
	ValidateDiskSpec(ctx context.Context, datacenter string, spec DiskSpec) error
}

// HostCapabilities is what a target can do for Forgemill beyond reading its
// inventory. Standalone ESXi hosts on the free "vSphere Hypervisor" license
// make the vSphere API read-only for third-party clients: inventory, sync,
// discover and adopt work, but every write (clone/copy, create, power,
// reconfigure, snapshot, destroy) is rejected with RestrictedVersion.
type HostCapabilities struct {
	LicenseEdition string `json:"license_edition,omitempty"` // display name of the active license
	WritesAllowed  bool   `json:"writes_allowed"`            // false = inventory-only
	Note           string `json:"note,omitempty"`            // what the user should know when writes are off
	// EvaluationExpiresAt is set while the host runs on an evaluation
	// license that still has time left — after that moment it becomes
	// inventory-only. Nil for keyed hosts and for expired evaluations.
	EvaluationExpiresAt *time.Time `json:"evaluation_expires_at,omitempty"`
}

// CapabilityReporter is implemented by providers that can tell up front
// whether writes will be accepted; others are assumed fully capable.
type CapabilityReporter interface {
	HostCapabilities(ctx context.Context) (*HostCapabilities, error)
}

// ErrLicenseRestricted: the hypervisor refused a write because of its
// license (vSphere RestrictedVersion). Handlers turn it into the message
// below instead of a generic 500.
var ErrLicenseRestricted = errors.New("hypervisor license prohibits this operation")

// LicenseRestrictedMessage is shown when the hypervisor refuses a write with
// the license fault and nothing more specific is known (a VM operation on a
// host that wasn't re-tested, for instance). It names both causes rather
// than guessing one; the target's own note is specific.
const LicenseRestrictedMessage = "This ESXi host's license prohibits vSphere API write operations — it is on the free vSphere Hypervisor license or its evaluation has expired. Forgemill can read its inventory, sync, discover and adopt VMs, but cannot deploy, power, reconfigure, snapshot or destroy VMs on it. Assign a paid or VMUG license key to the host, or manage it through vCenter."

// FreeLicenseMessage is the target note when the free SKU was identified.
const FreeLicenseMessage = "This ESXi host is on the free vSphere Hypervisor license, which prohibits vSphere API write operations. Forgemill can read its inventory, sync, discover and adopt VMs, but cannot deploy, power, reconfigure, snapshot or destroy VMs on it. Assign a paid or VMUG license key to the host, or manage it through vCenter."

// EvaluationExpiredMessage: the host's 60-day evaluation ran out. ESXi keeps
// reporting "Evaluation Mode" but refuses every API write from then on.
const EvaluationExpiredMessage = "This ESXi host's evaluation license has expired, so the vSphere API refuses write operations. Forgemill can read its inventory, sync, discover and adopt VMs, but cannot deploy, power, reconfigure, snapshot or destroy VMs on it. Assign a license key to the host (a paid or VMUG key restores everything; the free vSphere Hypervisor key keeps it inventory-only) or manage it through vCenter."

// WriteProbeRefusedMessage: the host rejected Forgemill's test write with
// the license fault although the license itself didn't explain why.
func WriteProbeRefusedMessage(edition string) string {
	if edition == "" {
		edition = "unknown"
	}
	return "This ESXi host rejected a test write: its license (" + edition + ") prohibits vSphere API write operations. Forgemill can read its inventory, sync, discover and adopt VMs, but cannot deploy, power, reconfigure, snapshot or destroy VMs on it. Check the host's licensing (expired evaluation or free vSphere Hypervisor key), assign a paid or VMUG key, or manage it through vCenter."
}

// IsLicenseRestricted reports whether err is the license gate, by sentinel
// or by the fault text vSphere has used for it for years.
func IsLicenseRestricted(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, ErrLicenseRestricted) {
		return true
	}
	return strings.Contains(strings.ToLower(err.Error()), "license or esxi version prohibits")
}
