import axios from "axios";
import type {
  User,
  Target,
  Template,
  TemplateDetailInfo,
  TemplateFamily,
  Deployment,
  DeployResponse,
  DashboardData,
  Resources,
  PaginatedResponse,
  ManagedVM,
  VMSnapshot,
  Blueprint,
  BulkDeployment,
  AuthSource,
  OSDefinition,
  TemplateBuild,
  PrereqStatus,
  TemplateSchedule,
  UpdateAvailable,
  TemplateHistory,
  UpdateCheckResult,
  Action,
  ActionExecution,
  ExecuteRequest,
  Webhook,
  APIKey,
  APIKeyCreateResponse,
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

api.interceptors.response.use(
  (res) => res,
  (err) => {
    if (err.response?.status === 401) {
      localStorage.removeItem("forgemill_token");
      if (window.location.pathname !== "/login") {
        window.location.href = "/login";
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
  getTypes: () => api.get<{ types: any[] }>("/targets/types"),
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
  changePassword: (id: number, password: string) =>
    api.put<{ status: string }>(`/users/${id}/password`, { password }),
  updateRole: (id: number, role: string) =>
    api.put<{ role: string }>(`/users/${id}/role`, { role }),
  delete: (id: number) => api.delete(`/users/${id}`),
};

export const vms = {
  list: () => api.get<ManagedVM[]>("/vms"),
  get: (id: number) => api.get<ManagedVM>(`/vms/${id}`),
  register: (data: Partial<ManagedVM>) => api.post<ManagedVM>("/vms", data),
  delete: (id: number, force?: boolean) =>
    api.delete(`/vms/${id}${force ? "?force=true" : ""}`),
  power: (id: number, action: string) =>
    api.post<{ status: string }>(`/vms/${id}/power/${action}`),
  syncAll: () =>
    api.post<{ synced: number; orphaned: number; errors?: string[] }>("/vms/sync-all"),
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
  listDisks: (id: number) =>
    api.get<{ key: number; label: string; size_gb: number }[]>(`/vms/${id}/disks`),
  expandDisk: (id: number, key: number, data: { new_size_gb: number }) =>
    api.put(`/vms/${id}/disks/${key}/expand`, data),
  console: (id: number) => api.get<{ url: string }>(`/vms/${id}/console`),
  credentials: (id: number) => api.get<{ username: string; password: string }>(`/vms/${id}/credentials`),
  resetHostKey: (id: number) => api.post(`/vms/${id}/reset-host-key`),
};

export const blueprints = {
  list: () => api.get<Blueprint[]>("/blueprints"),
  get: (id: number) => api.get<Blueprint>(`/blueprints/${id}`),
  create: (data: Partial<Blueprint>) => api.post<Blueprint>("/blueprints", data),
  update: (id: number, data: Partial<Blueprint>) => api.put<Blueprint>(`/blueprints/${id}`, data),
  delete: (id: number) => api.delete(`/blueprints/${id}`),
  deploy: (id: number, data: { vm_name: string }) =>
    api.post<Deployment>(`/blueprints/${id}/deploy`, data),
};

export const bulkDeploy = {
  list: () => api.get<BulkDeployment[]>("/deploy/bulk"),
  get: (id: number) => api.get<BulkDeployment>(`/deploy/bulk/${id}`),
  create: (data: Record<string, unknown>) => api.post<BulkDeployment>("/deploy/bulk", data),
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
  create: (data: { name: string; expires_at?: string }) =>
    api.post<APIKeyCreateResponse>("/api-keys", data),
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

export const auditLogs = {
  list: (params?: { page?: number; page_size?: number; action?: string; since?: string; until?: string; actor_id?: number }) =>
    api.get<PaginatedAuditLogs>("/audit-logs", { params }),
};

export const preferences = {
  get: () => api.get<Record<string, string>>("/preferences"),
  set: (key: string, value: string) => api.put("/preferences", { key, value }),
};

export default api;
