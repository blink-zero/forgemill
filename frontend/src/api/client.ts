import axios from "axios";
import type {
  User,
  Target,
  Template,
  TemplateDetailInfo,
  TemplateFamily,
  Deployment,
  DeployResponse,
  DeploymentLog,
  DashboardData,
  Resources,
  PaginatedResponse,
  ManagedVM,
  VMSnapshot,
  AuthSource,
  OSDefinition,
  TemplateBuild,
  PrereqStatus,
  TemplateSchedule,
  UpdateAvailable,
  TemplateHistory,
  UpdateCheckResult,
  Action,
  ActionParameter,
  ActionExportEntry,
  ActionImportResponse,
  ActionExecution,
  ExecuteRequest,
  Webhook,
  APIKey,
  APIKeyCreateResponse,
  NotificationListResponse,
  VMNIC,
  ProviderMetadata,
  VMDisk,
  Diagnostics,
  VMEvent,
} from "@/types";

const api = axios.create({
  baseURL: "/api",
  headers: { "Content-Type": "application/json" },
});

// V5-M6: Token is stored in localStorage which is accessible to any JS in the same origin.
// Accepted risk: Migrating to httpOnly cookies requires backend cookie management, CSRF
// protection changes, and WebSocket auth rework (subprotocol token delivery). CSP headers
// and V5-M5 (token version increment on logout) partially mitigate the XSS exfiltration risk.
// sessionStorage was considered but breaks multi-tab usage (each tab requires re-login).
api.interceptors.request.use((config) => {
  const token = localStorage.getItem("forgemill_token");
  if (token) {
    config.headers.Authorization = `Bearer ${token}`;
  }
  return config;
});

// Retry idempotent GETs that hit the per-IP rate limiter. A page load fires
// several requests at once and the last of them can be refused with a 429
// when the bucket is low; without this the failure is usually swallowed by
// a `.catch(() => {})` and a piece of UI quietly goes missing (the Add
// Network Adapter control depends on GET /targets/:id, for instance).
// Honours Retry-After, backs off 1s → 2s → 4s, three attempts max.
const MAX_429_RETRIES = 3;
const sleep = (ms: number) => new Promise<void>((resolve) => setTimeout(resolve, ms));

api.interceptors.response.use(
  (res) => res,
  async (err) => {
    if (err.response?.status === 401) {
      localStorage.removeItem("forgemill_token");
      if (window.location.pathname !== "/login") {
        window.location.href = "/login";
      }
      return Promise.reject(err);
    }
    const cfg = err.config as (typeof err.config & { __retry429?: number }) | undefined;
    if (err.response?.status === 429 && cfg && (cfg.method ?? "get").toLowerCase() === "get") {
      const attempt = cfg.__retry429 ?? 0;
      if (attempt < MAX_429_RETRIES) {
        const retryAfter = Number(err.response.headers?.["retry-after"]);
        const delay = Number.isFinite(retryAfter) && retryAfter > 0 ? retryAfter * 1000 : 1000 * 2 ** attempt;
        await sleep(delay);
        cfg.__retry429 = attempt + 1;
        return api.request(cfg);
      }
    }
    return Promise.reject(err);
  }
);

export const auth = {
  login: (username: string, password: string) =>
    api.post<{ token: string; user: User }>("/auth/login", { username, password }),
  me: () => api.get<User>("/auth/me"),
  logout: () => api.post("/auth/logout"),
};

export const dashboard = {
  get: () => api.get<DashboardData>("/dashboard"),
};

export const targets = {
  list: () => api.get<Target[]>("/targets"),
  get: (id: number) => api.get<Target>(`/targets/${id}`),
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  getTypes: () => api.get<{ types: ProviderMetadata[] }>("/targets/types"),
  create: (data: Partial<Target> & { password: string }) =>
    api.post<Target>("/targets", data),
  update: (id: number, data: Partial<Target> & { password?: string }) =>
    api.put<Target>(`/targets/${id}`, data),
  delete: (id: number) => api.delete(`/targets/${id}`),
  deletePreview: (id: number) => api.get<{ templates: number; vms: number; deployments: number; builds: number; executions: number }>(`/targets/${id}/delete-preview`),
  test: (id: number) =>
    api.post<{ success: boolean; message: string }>(`/targets/${id}/test`),
  sync: (id: number) =>
    api.post<{ templates_found: number }>(`/targets/${id}/sync`),
  resources: (id: number) => api.get<Resources>(`/targets/${id}/resources`),
};

export const templates = {
  list: () => api.get<Template[]>("/templates"),
  get: (id: number) => api.get<Template>(`/templates/${id}`),
  getDetail: (id: number) => api.get<TemplateDetailInfo>(`/templates/${id}/detail`),
  deletePreview: (id: number) => api.get<{ deployments: number; vms: number; builds: number }>(`/templates/${id}/delete-preview`),
  delete: (id: number, destroy: boolean, keepVMs?: boolean) => api.delete(`/templates/${id}?destroy=${destroy}${keepVMs ? "&keep_vms=true" : ""}`),
};

export const deploy = {
  start: (data: Record<string, unknown>) => api.post<DeployResponse>("/deploy", data),
  get: (id: number) => api.get<Deployment>(`/deploy/${id}`),
  cancel: (id: number) => api.post(`/deploy/${id}/cancel`),
  // Checks whether `data` (same shape as start()) would be accepted, without deploying anything.
  preflight: (data: Record<string, unknown>) => api.post<PreflightResult>("/deploy/preflight", data),
  manifest: (id: number) => api.get<DeploymentManifest>(`/deployments/${id}/manifest`),
  timeline: (id: number) => api.get<TimelineEvent[]>(`/deployments/${id}/timeline`),
};

export const history = {
  list: (params?: { page?: number; per_page?: number; status?: string; target_id?: number; search?: string }) =>
    api.get<PaginatedResponse<Deployment>>("/history", { params }),
  get: (id: number) => api.get<Deployment>(`/history/${id}`),
};

export const settings = {
  get: () => api.get("/settings"),
  update: (data: Record<string, unknown>) => api.put("/settings", data),
  clearDeploymentHistory: () => api.delete<{ deleted: number }>("/deployment-history"),
};

export const users = {
  list: () => api.get<User[]>("/users"),
  create: (data: { username: string; password: string; display_name: string; role: string }) =>
    api.post<User>("/users", data),
  update: (id: number, data: { display_name?: string }) =>
    api.patch<User>(`/users/${id}`, data),
  changePassword: (id: number, password: string) =>
    api.put<{ status: string }>(`/users/${id}/password`, { password }),
  updateRole: (id: number, role: string) =>
    api.put<{ role: string }>(`/users/${id}/role`, { role }),
  setActive: (id: number, active: boolean) =>
    api.put<{ is_active: boolean }>(`/users/${id}/active`, { active }),
  forceLogout: (id: number) =>
    api.post<{ status: string }>(`/users/${id}/force-logout`),
  delete: (id: number) => api.delete(`/users/${id}`),
};

export const vms = {
  list: () => api.get<ManagedVM[]>("/vms"),
  get: (id: number) => api.get<ManagedVM>(`/vms/${id}`),
  register: (data: Partial<ManagedVM>) => api.post<ManagedVM>("/vms", data),
  delete: (id: number, force?: boolean) =>
    api.delete(`/vms/${id}${force ? "?force=true" : ""}`),
  // Previews what delete(id, force) would do, without deleting anything.
  previewDelete: (id: number, force?: boolean) =>
    api.delete<DeletePreview>(`/vms/${id}?dry_run=true${force ? "&force=true" : ""}`),
  power: (id: number, action: string) =>
    api.post<{ status: string }>(`/vms/${id}/power/${action}`),
  syncAll: (dryRun?: boolean) =>
    api.post<SyncAllResult>(`/vms/sync-all${dryRun ? "?dry_run=true" : ""}`),
  sync: (id: number) => api.post<ManagedVM>(`/vms/${id}/sync`),
  listSnapshots: (id: number) => api.get<VMSnapshot[]>(`/vms/${id}/snapshots`),
  createSnapshot: (id: number, data: { name: string; description: string; memory: boolean }) =>
    api.post(`/vms/${id}/snapshots`, data),
  revertSnapshot: (id: number, snapId: number) =>
    api.post(`/vms/${id}/snapshots/${snapId}/revert`),
  deleteSnapshot: (id: number, snapId: number) =>
    api.delete(`/vms/${id}/snapshots/${snapId}`),
  resize: (id: number, data: { cpu: number; memory_mb: number }) =>
    api.put(`/vms/${id}/resize`, data),
  // Live from the hypervisor. Providers advertise AddDisk via
  // features.disk_attach; datastore defaults to the VM's first disk's;
  // provisioning only where disk_provisioning_types is published.
  listDisks: (id: number) => api.get<VMDisk[]>(`/vms/${id}/disks`),
  addDisk: (id: number, data: { size_gb: number; datastore?: string; provisioning?: string }) =>
    api.post<{ status: string; disk: VMDisk }>(`/vms/${id}/disks`, data),
  expandDisk: (id: number, key: number, data: { new_size_gb: number }) =>
    api.put(`/vms/${id}/disks/${key}/expand`, data),
  // Providers advertise support via features.nic_attach; adapter_type
  // defaults server-side (vmxnet3 on vSphere, virtio on Proxmox); connected
  // defaults to true; vlan_tag is Proxmox-only.
  // Live from the hypervisor, like listDisks.
  listNICs: (id: number) => api.get<VMNIC[]>(`/vms/${id}/nics`),
  // Recent operational events for the VM (provider warnings, attach results), newest first.
  listEvents: (id: number, limit = 100) => api.get<VMEvent[]>(`/vms/${id}/events`, { params: { limit } }),
  addNIC: (id: number, data: { network: string; adapter_type?: string; connected?: boolean; vlan_tag?: number }) =>
    api.post<{ status: string; nic: VMNIC }>(`/vms/${id}/nics`, data),
  console: (id: number) => api.get<{ url: string }>(`/vms/${id}/console`),
  credentials: (id: number) => api.get<{ username: string; password: string }>(`/vms/${id}/credentials`),
  resetHostKey: (id: number) => api.post(`/vms/${id}/reset-host-key`),
};

export const authSources = {
  list: () => api.get<AuthSource[]>("/auth-sources"),
  get: (id: number) => api.get<AuthSource>(`/auth-sources/${id}`),
  create: (data: Partial<AuthSource>) => api.post<AuthSource>("/auth-sources", data),
  update: (id: number, data: Partial<AuthSource>) => api.put<AuthSource>(`/auth-sources/${id}`, data),
  delete: (id: number) => api.delete(`/auth-sources/${id}`),
  test: (id: number) => api.post<{ success: boolean; message: string }>(`/auth-sources/${id}/test`),
};

export const factoryApi = {
  listOSDefinitions: () => api.get<OSDefinition[]>("/factory/os-definitions"),
  getOSDefinition: (id: string) => api.get<OSDefinition>(`/factory/os-definitions/${id}`),
  prerequisites: () => api.get<PrereqStatus>("/factory/prerequisites"),
  status: () => api.get<{ build_running: boolean }>("/factory/status"),
  startBuild: (data: Record<string, unknown>) =>
    api.post<TemplateBuild>("/factory/builds", data),
  listBuilds: () => api.get<TemplateBuild[]>("/factory/builds"),
  getBuild: (id: number) => api.get<TemplateBuild>(`/factory/builds/${id}`),
  cancelBuild: (id: number) => api.post(`/factory/builds/${id}/cancel`),
  deleteBuild: (id: number) => api.delete(`/factory/builds/${id}`),

  // Phase 5: Updates
  checkAllUpdates: () => api.get<UpdateAvailable[]>("/factory/updates"),
  checkTemplateUpdate: (templateId: number) =>
    api.get<UpdateCheckResult>(`/factory/updates/${templateId}`),
  rebuildTemplate: (templateId: number) =>
    api.post<TemplateBuild>(`/factory/updates/${templateId}/rebuild`),

  // Phase 5: Schedules
  listSchedules: () => api.get<TemplateSchedule[]>("/factory/schedules"),
  createSchedule: (data: Partial<TemplateSchedule>) =>
    api.post<TemplateSchedule>("/factory/schedules", data),
  getSchedule: (id: number) => api.get<TemplateSchedule>(`/factory/schedules/${id}`),
  updateSchedule: (id: number, data: Partial<TemplateSchedule>) =>
    api.put<TemplateSchedule>(`/factory/schedules/${id}`, data),
  deleteSchedule: (id: number) => api.delete(`/factory/schedules/${id}`),
  // Template families
  listTemplateFamilies: () => api.get<TemplateFamily[]>("/factory/families"),
  getFamilyHistory: (id: number) => api.get<Template[]>(`/factory/families/${id}/history`),
};

// Phase 5: Template history
export const templateHistory = {
  get: (id: number) => api.get<TemplateHistory[]>(`/templates/${id}/history`),
  cleanup: (id: number) => api.post<{ deleted: number }>(`/templates/${id}/cleanup`),
};

// Phase 2: SSH Action Execution
export const executions = {
  execute: (vmId: number, req: ExecuteRequest) =>
    api.post<ActionExecution>(`/vms/${vmId}/execute`, req),
  cancel: (executionId: number) =>
    api.post(`/executions/${executionId}/cancel`),
  list: (vmId: number) =>
    api.get<ActionExecution[]>(`/vms/${vmId}/executions`),
  get: (executionId: number) =>
    api.get<ActionExecution>(`/executions/${executionId}`),
};

// Post-deploy automation: Actions
export const actions = {
  list: () => api.get<Action[]>("/actions"),
  create: (data: Partial<Action>) => api.post<Action>("/actions", data),
  update: (id: number, data: Partial<Action>) => api.put<Action>(`/actions/${id}`, data),
  delete: (id: number) => api.delete(`/actions/${id}`),
  getForDeployment: (deploymentId: number) =>
    api.get<Action[]>(`/deployments/${deploymentId}/actions`),
  listVersions: (id: number) => api.get<ActionVersion[]>(`/actions/${id}/versions`),
  getVersion: (id: number, version: number) => api.get<ActionVersion>(`/actions/${id}/versions/${version}`),
  rollback: (id: number, version: number) => api.post<Action>(`/actions/${id}/rollback`, { version }),
  import: (actionsToImport: ActionExportEntry[]) =>
    api.post<ActionImportResponse>("/actions/import", { actions: actionsToImport }),
};

export const webhooks = {
  list: () => api.get<Webhook[]>("/webhooks"),
  get: (id: number) => api.get<Webhook>(`/webhooks/${id}`),
  create: (data: { name: string; url: string; events: string; secret?: string; is_active: boolean }) =>
    api.post<Webhook>("/webhooks", data),
  update: (id: number, data: { name?: string; url?: string; events?: string; secret?: string; is_active?: boolean }) =>
    api.put<Webhook>(`/webhooks/${id}`, data),
  delete: (id: number) => api.delete(`/webhooks/${id}`),
  test: (id: number) => api.post<{ success: boolean; status_code: number }>(`/webhooks/${id}/test`),
};

export const apiKeys = {
  list: () => api.get<APIKey[]>("/api-keys"),
  create: (data: {
    name: string;
    expires_at?: string;
    role?: string;
    scope?: string;
  }) => api.post<APIKeyCreateResponse>("/api-keys", data),
  delete: (id: number) => api.delete(`/api-keys/${id}`),
};

export interface AuditLog {
  id: number;
  actor: string;
  actor_id?: number;
  action: string;
  resource_type: string;
  resource_id: string;
  metadata: Record<string, unknown>;
  ip_address: string;
  created_at: string;
}

export interface PaginatedAuditLogs {
  logs: AuditLog[];
  total: number;
  page: number;
  page_size: number;
  total_pages: number;
}

// --- Deployment receipts (manifest/timeline/preflight) ---

export interface DeploymentManifest {
  deployment_id: number;
  what: string;
  target_id: number;
  target_name?: string;
  template_id?: number | null;
  template_name?: string;
  vm_name: string;
  vm_id?: number | null;
  triggered_by?: string;
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  inputs?: any;
  status: string;
  started_at: string | null;
  completed_at: string | null;
  error_message?: string;
  has_credentials: boolean;
  credentials_ref?: string;
  undo_options?: string[];
  audit_events?: AuditLog[];
  logs?: DeploymentLog[];
}

export interface TimelineEvent {
  timestamp: string;
  source: "log" | "audit";
  level?: string;
  action?: string;
  actor?: string;
  message: string;
}

export interface PreflightResult {
  valid: boolean;
  blockers?: string[];
  warnings?: string[];
}

// --- VM delete preview / sync-all orphan detail ---

export interface DeletePreview {
  vm_id: number;
  vm_name: string;
  force: boolean;
  would_delete_on_hypervisor: boolean;
  would_untrack_only: boolean;
  dependent_snapshots: number;
  dependent_executions: number;
}

export interface OrphanedVM {
  id: number;
  vm_name: string;
  vm_ref: string;
  target_id: number;
}

export interface SyncAllResult {
  synced: number;
  orphaned: number;
  orphaned_vms?: OrphanedVM[];
  errors?: string[];
}

// --- Action version history ---

export interface ActionVersion {
  id?: number;
  action_id: number;
  version: number;
  name: string;
  description: string;
  category: string;
  script: string;
  script_type: string;
  platform: string;
  parameters?: ActionParameter[];
  tags?: string[];
  changed_by?: number | null;
  created_at: string;
}

// Admin operational snapshot — Settings → Diagnostics.
export const diagnostics = {
  get: () => api.get<Diagnostics>("/diagnostics"),
};

export const auditLogs = {
  list: (params?: { page?: number; page_size?: number; action?: string; since?: string; until?: string; actor_id?: number }) =>
    api.get<PaginatedAuditLogs>("/audit-logs", { params }),
};

export const preferences = {
  get: () => api.get<Record<string, string>>("/preferences"),
  set: (key: string, value: string) => api.put("/preferences", { key, value }),
};

export const notifications = {
  list: (params?: { unread_only?: boolean; limit?: number }) => {
    const qs: Record<string, string> = {};
    if (params?.unread_only) qs.unread_only = "true";
    if (params?.limit) qs.limit = String(params.limit);
    return api.get<NotificationListResponse>("/notifications", { params: qs });
  },
  unreadCount: () => api.get<{ unread_count: number }>("/notifications/unread-count"),
  markRead: (id: number) => api.post<{ status: string }>(`/notifications/${id}/read`),
  markAllRead: () => api.post<{ marked_read: number }>("/notifications/read-all"),
  delete: (id: number) => api.delete(`/notifications/${id}`),
};

export default api;
