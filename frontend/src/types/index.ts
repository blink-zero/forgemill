export interface User {
  id: number;
  username: string;
  display_name: string;
  role: "admin" | "user" | "viewer";
  is_active: boolean;
  last_login_at: string | null;
  created_at: string;
}

export type NotificationLevel = "info" | "success" | "warning" | "error";

export interface Notification {
  id: number;
  user_id?: number;
  level: NotificationLevel;
  title: string;
  body?: string;
  link?: string;
  event?: string;
  is_read: boolean;
  created_at: string;
  read_at?: string | null;
}

export interface NotificationListResponse {
  notifications: Notification[];
  unread_count: number;
}

export interface Target {
  id: number;
  name: string;
  type: string;  // Dynamic: loaded from /api/targets/types
  hostname: string;
  port: number;
  username: string;
  validate_certs: boolean;
  is_default: boolean;
  status: string;
  last_connected_at: string | null;
  // From the last sync: VMs on this target Forgemill doesn't manage and nobody ignored.
  unmanaged_vms?: number;
  unmanaged_checked_at?: string | null;
  created_at: string;
  updated_at: string;
}

export interface Template {
  id: number;
  target_id: number;
  name: string;
  moref: string;
  os_type: string;
  os_name: string;
  guest_id: string;
  cpu: number;
  memory_mb: number;
  disk_gb: number;
  notes: string;
  icon: string;
  last_synced_at: string | null;
  created_at: string;
  target_name: string;
  target_type?: string;  // Dynamic: loaded from /api/targets/types
  build_id?: number;
  managed_by_forgemill: boolean;
  version: number;
  iso_checksum?: string;
  built_at?: string;
  lifecycle_status: string;
  superseded_by?: number;
  retain_until?: string;
  platform: "linux" | "windows";
  family_id?: number;
}

export interface TemplateDetailInfo {
  id: string;
  name: string;
  os_type: string;
  guest_id: string;
  cpu: number;
  memory_mb: number;
  disk_gb: number;
  moref: string;
  datastore: string;
  folder?: string;
  networks: string[];
  annotation: string;
  tools_status: string;
  hardware_version: string;
  firmware: string;
  created_at: string;
  platform: string;
  // Proxmox-specific
  node?: string;
  cpu_type?: string;
  scsi_type?: string;
  cloud_init?: boolean;
  disk_format?: string;
}

export interface TemplateFamily {
  id: number;
  base_name: string;
  target_id: number;
  os_definition_id: string;
  latest_version: number;
  created_at: string;
}

export interface Deployment {
  id: number;
  template_id: number | null;
  target_id: number;
  vm_name: string;
  status: "pending" | "running" | "completed" | "failed" | "cancelled";
  config_json: string;
  started_at: string | null;
  completed_at: string | null;
  error_message: string;
  created_by: number;
  created_at: string;
  bulk_deployment_id?: number;
  template_name: string;
  target_name: string;
  logs?: DeploymentLog[];
}

export interface DeployResponse extends Deployment {
  initial_username: string;
  initial_password: string;
  ssh_key_injected: boolean;
}

export interface DeploymentLog {
  id: number;
  deployment_id: number;
  timestamp: string;
  level: string;
  message: string;
}

export interface DashboardData {
  stats: {
    total_targets: number;
    total_templates: number;
    total_deployments: number;
    total_vms: number;
    total_actions: number;
    deployments_today: number;
    running_deploys: number;
    managed_templates: number;
    scheduled_builds_today: number;
  };
  recent_deployments: Deployment[];
  recent_executions: ActionExecution[];
  targets: Target[];
}

// ISSUE-06 fix: Added resource_pools to match backend provider.Resources struct.
export interface Resources {
  datastores: ResourceItem[];
  networks: ResourceItem[];
  folders: ResourceItem[];
  clusters: ResourceItem[];
  datacenters: ResourceItem[];
  resource_pools: ResourceItem[];
  hosts?: ResourceItem[];
  iso_storages?: ResourceItem[];
  platform?: string;
  defaults?: Record<string, string>;
}

export interface ResourceItem {
  name: string;
  id: string;
  path?: string;
}

export interface PaginatedResponse<T> {
  data: T[];
  total: number;
  page: number;
  per_page: number;
}

export interface TemplateSource {
  id: number;
  name: string;
  os_type: string;
  iso_url: string;
  checksum_url: string;
  packer_config: string;
  auto_refresh: boolean;
  refresh_interval_days: number;
  last_built_at: string | null;
  target_id: number;
  created_at: string;
  target_name: string;
}

export interface APIKey {
  id: number;
  user_id: number;
  name: string;
  prefix: string;
  last_used_at: string | null;
  expires_at: string | null;
  created_at: string;
  username: string;
  /** Per-key role override. null = inherit user's role. */
  role?: "viewer" | "user" | "admin" | null;
  /** Per-key scope. null/"full" = no restriction. */
  scope?: "read-only" | "action-only" | "deploy-only" | "full" | null;
}

export interface APIKeyCreateResponse {
  key: string;
  api_key: APIKey;
}

export interface Webhook {
  id: number;
  name: string;
  url: string;
  events: string;
  is_active: boolean;
  created_at: string;
}

export interface ManagedVM {
  id: number;
  deployment_id: number | null;
  target_id: number;
  vm_name: string;
  vm_ref: string;
  power_state: string;
  ip_address: string;
  cpu: number;
  memory_mb: number;
  disk_gb: number;
  os_type: string;
  platform: "linux" | "windows";
  host_key_fp?: string;
  last_synced_at: string | null;
  created_at: string;
  target_name: string;
  template_name?: string;

  // Lifecycle tracking — state_changed_at/last_powered_on_at/last_powered_off_at
  // are null until the first observed power-state transition. total_runtime_seconds
  // is cumulative and only grows while poweredOn (frozen otherwise, including
  // while suspended — never reset to zero).
  state_changed_at: string | null;
  last_powered_on_at: string | null;
  last_powered_off_at: string | null;
  total_runtime_seconds: number;
  // deployed (Forgemill created it) | adopted (discovered and taken under management) | registered (added by ref)
  origin?: "deployed" | "adopted" | "registered" | string;
  adopted_at?: string | null;
  adopted_by?: number | null;
}

export interface VMSnapshot {
  id: number;
  vm_id: number;
  snapshot_ref: string;
  name: string;
  description: string;
  created_at: string;
}

export interface AuthSource {
  id: number;
  name: string;
  type: "local" | "ldap" | "saml" | "oidc";
  config_json: string;
  is_default: boolean;
  enabled: boolean;
  created_at: string;
}

// --- Phase 4: Template Factory ---

export interface OSDefinition {
  id: string;
  name: string;
  family: string;
  version: string;
  arch: string;
  iso_url_pattern: string;
  iso_checksum_url: string;
  guest_os_type: string;
  proxmox_os_type: string;
  min_disk_gb: number;
  min_memory_mb: number;
  min_cpu: number;
  boot_command: string[];
  install_method: string;
}

export interface TemplateBuild {
  id: number;
  os_definition_id: string;
  target_id: number;
  status: "pending" | "downloading" | "building" | "converting" | "completed" | "failed" | "cancelled";
  template_name: string;
  config_json: string;
  iso_url: string;
  iso_checksum: string;
  packer_template: string;
  autoinstall_config: string;
  packer_log: string;
  started_at: string | null;
  completed_at: string | null;
  error_message: string;
  created_by: number;
  created_at: string;
  target_name: string;
  template_id?: number;
  version: number;
  previous_build_id?: number;
  auto_triggered: boolean;
}

export interface PrereqStatus {
  packer_installed: boolean;
  packer_version: string;
}

export interface BuildWSMessage {
  type: "progress" | "log" | "complete" | "error";
  data: Record<string, string>;
}

// --- Phase 5: Template Lifecycle ---

export interface TemplateSchedule {
  id: number;
  template_id: number;
  build_config_json: string;
  strategy: "interval" | "on_update" | "both";
  interval_days: number;
  check_interval_hours: number;
  last_checked_at: string | null;
  last_rebuilt_at: string | null;
  next_check_at: string | null;
  enabled: boolean;
  created_at: string;
}

export interface UpdateAvailable {
  template_id: number;
  template_name: string;
  os_definition_id: string;
  current_checksum: string;
  latest_checksum: string;
  current_version: number;
  iso_url: string;
}

export interface TemplateHistory {
  template_id: number;
  template_name: string;
  version: number;
  status: string;
  build_id?: number;
  built_at?: string;
  iso_checksum?: string;
  superseded_by?: number;
}

export interface UpdateCheckResult {
  update_available: boolean;
  update?: UpdateAvailable;
}

// --- Post-deploy automation ---

export interface ActionParameter {
  name: string;
  label: string;
  type: "string" | "number" | "select" | "boolean" | "password";
  required: boolean;
  default: string;
  placeholder: string;
  options: string[] | null;
  description: string;
}

export interface Action {
  id: number;
  name: string;
  description: string;
  category: "packages" | "scripts" | "security" | "monitoring" | "custom";
  script: string;
  script_type: "bash" | "powershell" | "python";
  platform: "linux" | "windows" | "any";
  builtin: boolean;
  parameters?: ActionParameter[];
  tags?: string[];
  version?: number;
  created_at: string;
  updated_at: string;
}

// --- Action import/export ---
//
// The exported JSON intentionally excludes id/builtin/version/timestamps —
// those are instance-specific and re-derived on import. script_type/platform
// are included for readability only; the import API ignores them (imported
// actions are always plain bash actions, same as anything made through the
// "Create Action" form).
export interface ActionExportEntry {
  name: string;
  description: string;
  category: Action["category"];
  script: string;
  script_type: Action["script_type"];
  platform: Action["platform"];
  parameters?: ActionParameter[];
  tags?: string[];
}

export interface ActionExportFile {
  schema_version: 1;
  exported_at: string;
  source: "forgemill";
  actions: ActionExportEntry[];
}

export interface ActionImportResult {
  index: number;
  name: string;
  status: "created" | "failed";
  id?: number;
  error?: string;
}

export interface ActionImportResponse {
  created: number;
  failed: number;
  results: ActionImportResult[];
}

// --- Phase 2: SSH Action Execution ---

export interface ActionExecution {
  id: number;
  vm_id: number;
  action_id: number | null;
  action_name: string;
  script: string;
  status: "pending" | "running" | "completed" | "failed" | "cancelled";
  exit_code: number | null;
  output: string;
  parameter_values?: Record<string, string>;
  started_at: string | null;
  completed_at: string | null;
  created_by: number;
  created_at: string;
}

export interface ExecuteRequest {
  action_id?: number;
  script?: string;
  timeout_seconds?: number;
  parameter_values?: Record<string, string>;
}

// A virtual network adapter as the hypervisor reports it, returned by
// POST /api/vms/:id/nics after a successful attach.
export interface VMDisk {
  key: number;
  label: string;
  size_gb: number;
  // Datastore (vSphere) / storage (Proxmox) the disk lives on.
  datastore?: string;
  // "thin" / "thick" on vSphere; the volume format (qcow2, raw) on Proxmox when known.
  provisioning?: string;
  // Backing file ("[ds] vm/vm_1.vmdk") or volume ("local-lvm:vm-100-disk-1").
  backing?: string;
  // Proxmox with disk hot-plug disabled: saved, attaches at next power cycle.
  pending?: boolean;
}

export interface VMNIC {
  key: number;
  label: string;
  adapter_type: string;
  network: string;
  mac_address: string;
  // connected = live link state (only while the VM runs); start_connected =
  // configured to connect at power-on. An off VM is false/true for a normal NIC.
  connected: boolean;
  start_connected?: boolean;
  vlan_tag?: number;
  // Proxmox with network hot-plug disabled: saved, attaches at next power cycle.
  pending?: boolean;
  // Guest-reported IPs on this adapter (IPv4 first); empty when guest
  // tools / the guest agent aren't reporting.
  addresses: string[];
}

// Provider metadata from backend
export interface ProviderDefaults {
  port: number;
  username: string;
  name_placeholder: string;
  hostname_placeholder: string;
}

export interface ProviderFeatures {
  folders: boolean;
  clusters: boolean;
  disk_provisioning: boolean;
  linked_clones: boolean;
  vlan_tagging: boolean;
  nic_attach: boolean;
  // The provider implements AddDisk (POST /vms/:id/disks).
  disk_attach: boolean;
}

export interface DeployField {
  key: string;
  label: string;
  resource: string;
  placeholder?: string;
}

export interface ProviderMetadata {
  id: string;
  name: string;
  description: string;
  icon: string;
  defaults: ProviderDefaults;
  hints: Record<string, string>;
  features: ProviderFeatures;
  deploy_fields: DeployField[];
  // Adapter models AddNIC accepts, first is the default. Absent when
  // features.nic_attach is false.
  nic_adapter_types?: string[];
  // Provisioning modes AddDisk accepts ("thin", "thick"), first is the
  // default. Absent when the provider can't choose per disk (Proxmox).
  disk_provisioning_types?: string[];
}

export interface VMEvent {
  id: number;
  vm_id: number;
  target_id?: number;
  level: "info" | "warn" | "error" | string;
  message: string;
  created_at: string;
}

export interface TargetSyncInfo {
  at: string;
  synced: number;
  orphaned: number;
  errors?: string[];
}

export interface Diagnostics {
  generated_at: string;
  build: { version: string; commit: string; date: string };
  targets: { id: number; name: string; type: string; status: string; last_connected_at: string | null; last_sync: TargetSyncInfo | null }[];
  recent_vm_events: VMEvent[];
  recent_failed_deployments: { id: number; vm_name: string; target_name: string; error_message: string; completed_at: string | null }[];
  recent_server_errors: { time: string; status: number; message: string; error: string }[];
  rate_limited_requests: number;
}

export interface DiscoveredVM {
  ref: string;
  name: string;
  power_state: string;
  ip_address?: string;
  cpu: number;
  memory_mb: number;
  disk_gb: number;
  guest_id?: string;
  host?: string;
  ignored: boolean;
}

export interface DiscoverResult {
  target_id: number;
  target_name: string;
  computed_at: string;
  managed: number;
  unmanaged: number;
  ignored: number;
  vms: DiscoveredVM[];
}

export interface AdoptResult {
  adopted: ManagedVM[];
  skipped: { ref: string; reason: string }[];
}

export interface IgnoredVM {
  target_id: number;
  vm_ref: string;
  vm_name: string;
  ignored_by?: number | null;
  created_at: string;
}

// SSH login Forgemill would use for a VM. `source` says where it comes from:
// set explicitly on the VM, or inherited from the deployment. A private key
// is never echoed back — only the fact that one is stored.
export interface VMCredentials {
  username: string;
  password?: string;
  kind: "password" | "private_key";
  source: "vm" | "deployment";
  has_private_key?: boolean;
  has_sudo_password?: boolean;
  set_at?: string | null;
  set_by?: number | null;
}

export interface SetVMCredentialsRequest {
  username: string;
  password?: string;
  private_key?: string;
  /** Handed to sudo when it asks. Optional for password logins (defaults to the login password). */
  sudo_password?: string;
  /** Save even if the live check fails or can't run. */
  force?: boolean;
}

// Live result of trying credentials on the VM: SSH login, then sudo.
export interface CredentialCheck {
  skipped: boolean;
  ssh_ok: boolean;
  sudo?: "nopasswd" | "password" | "needs_password" | "wrong_password" | "not_permitted" | "requiretty" | "probe_failed" | "unknown_failure" | string;
  ok: boolean;
  message: string;
  detail?: string;
  checked_via?: string;
}
