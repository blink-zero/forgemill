package provider

import "context"

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
	GetConsoleURL(ctx context.Context, vmID string) (string, error)

	ListVMs(ctx context.Context) ([]VMInfo, error)
	GetResources(ctx context.Context) (*Resources, error)
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
	Node        string `json:"node,omitempty"`
	CPUType     string `json:"cpu_type,omitempty"`
	SCSIType    string `json:"scsi_type,omitempty"`
	CloudInit   bool   `json:"cloud_init,omitempty"`
	DiskFormat  string `json:"disk_format,omitempty"`
}

type DeploySpec struct {
	TemplateName string
	VMName       string
	Datacenter   string
	Cluster      string
	Datastore    string
	Folder       string
	Network      string
	CPU          int
	MemoryMB     int
	DiskGB       int
	IPAddress    string
	Netmask      string
	Gateway      string
	DNS          []string
	Hostname     string
	DomainName   string
	OSType       string // "linux" or "windows"
	LinkedClone  bool   // PV-P5: support for linked clones
	PasswordHash     string // SHA-512 crypt hash for cloud-init credential injection
	PlainPassword    string // BUG-03: Plaintext password for Proxmox cipassword
	SSHPublicKey     string // Optional SSH public key to inject
	UserDataOverride string // Pre-merged cloud-init userdata (when actions are selected)
	DiskProvisioning string // "thin", "thick", "thick_eager_zero", or "" (inherit from template)
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
}

type Resources struct {
	Datastores    []ResourceItem    `json:"datastores"`
	Networks      []ResourceItem    `json:"networks"`
	Folders       []ResourceItem    `json:"folders"`
	Clusters      []ResourceItem    `json:"clusters"`
	Datacenters   []ResourceItem    `json:"datacenters"`
	ISOStorages   []ResourceItem    `json:"iso_storages,omitempty"`
	ResourcePools []ResourceItem    `json:"resource_pools"`          // PV-X6
	Platform      string            `json:"platform"`
	Defaults      map[string]string `json:"defaults,omitempty"`
}

type ResourceItem struct {
	Name string `json:"name"`
	ID   string `json:"id"`
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
}

type VMInfo struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	PowerState string `json:"power_state"`
	IPAddress  string `json:"ip_address"`
	CPU        int    `json:"cpu"`
	MemoryMB   int    `json:"memory_mb"`
	GuestID    string `json:"guest_id"`
}
