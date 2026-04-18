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
}

export interface VMSnapshot {
  id: number;
  vm_id: number;
  snapshot_ref: string;
  name: string;
  description: string;
  created_at: string;
}

export interface Blueprint {
  id: number;
  name: string;
  description: string;
  template_id: number | null;
  target_id: number | null;
  config_json: string;
  created_by: number;
  created_at: string;
  updated_at: string;
  template_name: string;
  target_name: string;
}

export interface BulkDeployment {
  id: number;
  name: string;
  status: string;
  total_vms: number;
  completed_vms: number;
  failed_vms: number;
  parallel: boolean;
  created_by: number;
  created_at: string;
  completed_at: string | null;
  deployments: Deployment[];
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
  created_at: string;
  updated_at: string;
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
