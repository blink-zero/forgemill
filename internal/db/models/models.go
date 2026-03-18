package models

import (
	"encoding/json"
	"time"
)

type User struct {
	ID             int64      `json:"id"`
	Username       string     `json:"username"`
	PasswordHash   string     `json:"-"`
	DisplayName    string     `json:"display_name"`
	Role           string     `json:"role"`
	IsActive       bool       `json:"is_active"`
	LastLoginAt    *time.Time `json:"last_login_at"`
	CreatedAt      time.Time  `json:"created_at"`
	AuthSourceID   *int64     `json:"auth_source_id,omitempty"`
	ExternalID     string     `json:"external_id,omitempty"`
	TokenVersion   int        `json:"token_version"`
}

type Target struct {
	ID              int64      `json:"id"`
	Name            string     `json:"name"`
	Type            string     `json:"type"`
	Hostname        string     `json:"hostname"`
	Port            int        `json:"port"`
	Username        string     `json:"username"`
	PasswordEncrypt string     `json:"-"`
	ValidateCerts   bool       `json:"validate_certs"`
	IsDefault       bool       `json:"is_default"`
	Status          string     `json:"status"`
	LastConnectedAt *time.Time `json:"last_connected_at"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
	// Proxmox-specific fields
	StoragePool   string `json:"storage_pool,omitempty"`
	NetworkBridge string `json:"network_bridge,omitempty"`
	// vSphere-specific fields
	Datacenter string `json:"datacenter,omitempty"`
	Datastore  string `json:"datastore,omitempty"`
	Network    string `json:"network,omitempty"`
}

type Template struct {
	ID                 int64      `json:"id"`
	TargetID           int64      `json:"target_id"`
	Name               string     `json:"name"`
	Moref              string     `json:"moref"`
	OSType             string     `json:"os_type"`
	OSName             string     `json:"os_name"`
	GuestID            string     `json:"guest_id"`
	CPU                int        `json:"cpu"`
	MemoryMB           int        `json:"memory_mb"`
	DiskGB             int        `json:"disk_gb"`
	Notes              string     `json:"notes"`
	Icon               string     `json:"icon"`
	LastSyncedAt       *time.Time `json:"last_synced_at"`
	CreatedAt          time.Time  `json:"created_at"`
	TargetName         string     `json:"target_name,omitempty"`
	TargetType         string     `json:"target_type,omitempty"`
	BuildID            *int64     `json:"build_id,omitempty"`
	ManagedByForgemill bool       `json:"managed_by_forgemill"`
	Version            int        `json:"version"`
	ISOChecksum        string     `json:"iso_checksum,omitempty"`
	BuiltAt            *time.Time `json:"built_at,omitempty"`
	LifecycleStatus    string     `json:"lifecycle_status"`
	SupersededBy       *int64     `json:"superseded_by,omitempty"`
	RetainUntil        *time.Time `json:"retain_until,omitempty"`
	Platform           string     `json:"platform"`
	FamilyID           *int64     `json:"family_id,omitempty"`
}

type TemplateFamily struct {
	ID             int64     `json:"id"`
	BaseName       string    `json:"base_name"`
	TargetID       int64     `json:"target_id"`
	OSDefinitionID string    `json:"os_definition_id"`
	LatestVersion  int       `json:"latest_version"`
	CreatedAt      time.Time `json:"created_at"`
}

type Deployment struct {
	ID               int64      `json:"id"`
	TemplateID       *int64     `json:"template_id"`
	TargetID         int64      `json:"target_id"`
	VMName           string     `json:"vm_name"`
	Status           string     `json:"status"`
	ConfigJSON       string     `json:"config_json,omitempty"`
	StartedAt        *time.Time `json:"started_at"`
	CompletedAt      *time.Time `json:"completed_at"`
	ErrorMessage     string     `json:"error_message,omitempty"`
	CreatedBy        int64      `json:"created_by"`
	CreatedAt        time.Time  `json:"created_at"`
	BulkDeploymentID *int64     `json:"bulk_deployment_id,omitempty"`
	InitialUsername  string     `json:"initial_username,omitempty"`
	InitialPwdEnc   string     `json:"-"`                          // encrypted, never serialised
	TemplateName     string     `json:"template_name,omitempty"`
	TargetName       string     `json:"target_name,omitempty"`
	VMID             *int64     `json:"vm_id,omitempty"`
	Logs             []DeploymentLog `json:"logs,omitempty"`
}

type DeploymentLog struct {
	ID           int64     `json:"id"`
	DeploymentID int64     `json:"deployment_id"`
	Timestamp    time.Time `json:"timestamp"`
	Level        string    `json:"level"`
	Message      string    `json:"message"`
}

type TemplateSource struct {
	ID                  int64      `json:"id"`
	Name                string     `json:"name"`
	OSType              string     `json:"os_type"`
	ISOURL              string     `json:"iso_url"`
	ChecksumURL         string     `json:"checksum_url"`
	PackerConfig        string     `json:"packer_config"`
	AutoRefresh         bool       `json:"auto_refresh"`
	RefreshIntervalDays int        `json:"refresh_interval_days"`
	LastBuiltAt         *time.Time `json:"last_built_at"`
	TargetID            int64      `json:"target_id"`
	CreatedAt           time.Time  `json:"created_at"`
	TargetName          string     `json:"target_name,omitempty"`
}

type APIKey struct {
	ID         int64      `json:"id"`
	UserID     int64      `json:"user_id"`
	Name       string     `json:"name"`
	KeyHash    string     `json:"-"`
	Prefix     string     `json:"prefix"`
	LastUsedAt *time.Time `json:"last_used_at"`
	ExpiresAt  *time.Time `json:"expires_at"`
	CreatedAt  time.Time  `json:"created_at"`
	Username   string     `json:"username,omitempty"`
}

type Webhook struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	URL       string    `json:"url"`
	Events    string    `json:"events"`
	Secret    string    `json:"-"`
	IsActive  bool      `json:"is_active"`
	CreatedAt time.Time `json:"created_at"`
}

// --- Phase 3 models ---

type ManagedVM struct {
	ID           int64      `json:"id"`
	DeploymentID *int64     `json:"deployment_id"`
	TargetID     int64      `json:"target_id"`
	VMName       string     `json:"vm_name"`
	VMRef        string     `json:"vm_ref"`
	PowerState   string     `json:"power_state"`
	IPAddress    string     `json:"ip_address"`
	CPU          int        `json:"cpu"`
	MemoryMB     int        `json:"memory_mb"`
	DiskGB       int        `json:"disk_gb"`
	OSType       string     `json:"os_type"`
	Platform     string     `json:"platform"`
	HostKeyFP    string     `json:"host_key_fp,omitempty"` // SSH host key fingerprint (TOFU)
	LastSyncedAt *time.Time `json:"last_synced_at"`
	CreatedAt    time.Time  `json:"created_at"`
	TargetName   string     `json:"target_name,omitempty"`
	TemplateName string     `json:"template_name,omitempty"`
}

type VMSnapshot struct {
	ID          int64     `json:"id"`
	VMID        int64     `json:"vm_id"`
	SnapshotRef string    `json:"snapshot_ref"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"created_at"`
}

type Blueprint struct {
	ID          int64     `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	TemplateID  *int64    `json:"template_id"`
	TargetID    *int64    `json:"target_id"`
	ConfigJSON  string    `json:"config_json"`
	CreatedBy   int64     `json:"created_by"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	TemplateName string   `json:"template_name,omitempty"`
	TargetName   string   `json:"target_name,omitempty"`
}

type BulkDeployment struct {
	ID           int64      `json:"id"`
	Name         string     `json:"name"`
	Status       string     `json:"status"`
	TotalVMs     int        `json:"total_vms"`
	CompletedVMs int        `json:"completed_vms"`
	FailedVMs    int        `json:"failed_vms"`
	Parallel     bool       `json:"parallel"`
	CreatedBy    int64      `json:"created_by"`
	CreatedAt    time.Time  `json:"created_at"`
	CompletedAt  *time.Time `json:"completed_at"`
	Deployments  []Deployment `json:"deployments,omitempty"`
}

type AuthSource struct {
	ID         int64     `json:"id"`
	Name       string    `json:"name"`
	Type       string    `json:"type"`
	ConfigJSON string    `json:"config_json"`
	IsDefault  bool      `json:"is_default"`
	Enabled    bool      `json:"enabled"`
	CreatedAt  time.Time `json:"created_at"`
}

// --- Phase 4 models ---

type TemplateBuild struct {
	ID                int64      `json:"id"`
	OSDefinitionID    string     `json:"os_definition_id"`
	TargetID          int64      `json:"target_id"`
	Status            string     `json:"status"`
	TemplateName      string     `json:"template_name"`
	ConfigJSON        string     `json:"config_json"`
	ISOURL            string     `json:"iso_url,omitempty"`
	ISOChecksum       string     `json:"iso_checksum,omitempty"`
	PackerTemplate    string     `json:"-"`
	AutoinstallConfig string     `json:"-"`
	PackerLog         string     `json:"packer_log,omitempty"`
	StartedAt         *time.Time `json:"started_at"`
	CompletedAt       *time.Time `json:"completed_at"`
	ErrorMessage      string     `json:"error_message,omitempty"`
	CreatedBy         int64      `json:"created_by"`
	CreatedAt         time.Time  `json:"created_at"`
	TargetName        string     `json:"target_name,omitempty"`
	TemplateID        *int64     `json:"template_id,omitempty"`
	Version           int        `json:"version"`
	PreviousBuildID   *int64     `json:"previous_build_id,omitempty"`
	AutoTriggered     bool       `json:"auto_triggered"`
}

// --- Phase 5 models ---

type TemplateSchedule struct {
	ID                 int64      `json:"id"`
	TemplateID         int64      `json:"template_id"`
	BuildConfigJSON    string     `json:"build_config_json"`
	Strategy           string     `json:"strategy"`
	IntervalDays       int        `json:"interval_days"`
	CheckIntervalHours int        `json:"check_interval_hours"`
	LastCheckedAt      *time.Time `json:"last_checked_at"`
	LastRebuiltAt      *time.Time `json:"last_rebuilt_at"`
	NextCheckAt        *time.Time `json:"next_check_at"`
	Enabled            bool       `json:"enabled"`
	CreatedAt          time.Time  `json:"created_at"`
}

type UpdateAvailable struct {
	TemplateID      int64  `json:"template_id"`
	TemplateName    string `json:"template_name"`
	OSDefinitionID  string `json:"os_definition_id"`
	CurrentChecksum string `json:"current_checksum"`
	LatestChecksum  string `json:"latest_checksum"`
	CurrentVersion  int    `json:"current_version"`
	ISOURL          string `json:"iso_url"`
}

type TemplateHistory struct {
	TemplateID   int64      `json:"template_id"`
	TemplateName string     `json:"template_name"`
	Version      int        `json:"version"`
	Status       string     `json:"status"`
	BuildID      *int64     `json:"build_id,omitempty"`
	BuiltAt      *time.Time `json:"built_at,omitempty"`
	ISOChecksum  string     `json:"iso_checksum,omitempty"`
	SupersededBy *int64     `json:"superseded_by,omitempty"`
}

// --- Post-deploy automation ---

type ActionParameter struct {
	Name        string   `json:"name"`        // env var name (uppercase, A-Z0-9_)
	Label       string   `json:"label"`       // display label
	Type        string   `json:"type"`        // "string", "number", "select", "boolean", "password"
	Required    bool     `json:"required"`    // must be filled before execution
	Default     string   `json:"default"`     // default value
	Placeholder string   `json:"placeholder"` // input placeholder
	Options     []string `json:"options"`     // for "select" type only
	Description string   `json:"description"` // help text
}

type Action struct {
	ID          int64             `json:"id"`
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Category    string            `json:"category"`
	Script      string            `json:"script"`
	ScriptType  string            `json:"script_type"`
	Platform    string            `json:"platform"`
	Builtin     bool              `json:"builtin"`
	Parameters  []ActionParameter `json:"parameters,omitempty"`
	CreatedAt   string            `json:"created_at"`
	UpdatedAt   string            `json:"updated_at"`
}

// --- Audit Logging ---

type AuditLog struct {
	ID           int64           `json:"id"`
	Actor        string          `json:"actor"`
	ActorID      *int64          `json:"actor_id,omitempty"`
	Action       string          `json:"action"`
	ResourceType string          `json:"resource_type"`
	ResourceID   string          `json:"resource_id"`
	Metadata     json.RawMessage `json:"metadata"`
	IPAddress    string          `json:"ip_address"`
	CreatedAt    time.Time       `json:"created_at"`
}

// --- Phase 2: SSH Action Execution ---

type ActionExecution struct {
	ID              int64             `json:"id"`
	VMID            int64             `json:"vm_id"`
	ActionID        *int64            `json:"action_id"`
	ActionName      string            `json:"action_name"`
	Script          string            `json:"script"`
	Status          string            `json:"status"`
	ExitCode        *int              `json:"exit_code"`
	Output          string            `json:"output"`
	ParameterValues map[string]string `json:"parameter_values,omitempty"`
	StartedAt       *time.Time        `json:"started_at"`
	CompletedAt     *time.Time        `json:"completed_at"`
	CreatedBy       int64             `json:"created_by"`
	CreatedAt       time.Time         `json:"created_at"`
}
