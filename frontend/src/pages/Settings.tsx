import { useEffect, useState, Fragment } from "react";
import { users as usersApi, settings as settingsApi, webhooks as webhooksApi, apiKeys as apiKeysApi, auditLogs as auditLogsApi, factoryApi } from "@/api/client";
import type { AuditLog, PaginatedAuditLogs } from "@/api/client";
import { useAuth } from "@/hooks/useAuth";
import { useTimezone, COMMON_TIMEZONES } from "@/hooks/useTimezone";
import type { User, Webhook, APIKey, PrereqStatus } from "@/types";
import { Card, CardHeader, CardTitle, CardContent } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Plus, X, Globe, KeyRound, Trash2, AlertTriangle, Webhook as WebhookIcon, Copy, Send, Pencil, Check, Info, UserCheck, UserX, LogOut as LogOutIcon, MoreHorizontal, Search } from "lucide-react";
import { DropdownMenu, DropdownMenuItem, DropdownMenuSeparator } from "@/components/ui/dropdown-menu";
import { ForgemillLogo } from "@/components/ForgemillLogo";
import { Select } from "@/components/ui/select";
import { useToast } from "@/components/ui/toast";
import { PageHeader } from "@/components/ui/page-header";
import { useConfirm } from "@/components/ui/confirm-dialog";
import { getErrorMessage, cn } from "@/lib/utils";

const WEBHOOK_EVENTS = [
  "deploy.started",
  "deploy.completed",
  "deploy.failed",
  "template.rebuild_started",
  "template.rebuild_completed",
  "template.update_available",
  "template.version_superseded",
  "execution.completed",
] as const;

export default function SettingsPage() {
  const { toast } = useToast();
  const { confirm: showConfirm } = useConfirm();
  const { user: currentUser } = useAuth();
  const { timezone, setTimezone, formatDateTime } = useTimezone();
  const [tab, setTab] = useState<"users" | "webhooks" | "apikeys" | "preferences" | "auditlog" | "about">("users");
  const [appVersion, setAppVersion] = useState({ version: "loading...", commit: "", date: "" });
  const [packerStatus, setPackerStatus] = useState<PrereqStatus | null>(null);
  const [usersList, setUsersList] = useState<User[]>([]);
  const [showForm, setShowForm] = useState(false);
  const [form, setForm] = useState({ username: "", password: "", display_name: "", role: "user" });
  const [formError, setFormError] = useState("");
  const [pwdUserId, setPwdUserId] = useState<number | null>(null);
  const [newPwd, setNewPwd] = useState("");
  const [pwdMsg, setPwdMsg] = useState("");
  const [editingNameUserId, setEditingNameUserId] = useState<number | null>(null);
  const [editingNameValue, setEditingNameValue] = useState("");
  const [userSearch, setUserSearch] = useState("");
  const [userStatusFilter, setUserStatusFilter] = useState<"all" | "active" | "disabled">("all");
  const isAdmin = currentUser?.role === "admin";

  // Webhooks state
  const [webhooksList, setWebhooksList] = useState<Webhook[]>([]);
  const [showWebhookForm, setShowWebhookForm] = useState(false);
  const [editingWebhook, setEditingWebhook] = useState<Webhook | null>(null);
  const [webhookForm, setWebhookForm] = useState({ name: "", url: "", events: [] as string[], secret: "", is_active: true });
  const [webhookFormError, setWebhookFormError] = useState("");

  // API Keys state
  const [apiKeysList, setApiKeysList] = useState<APIKey[]>([]);
  const [showApiKeyForm, setShowApiKeyForm] = useState(false);
  const [apiKeyForm, setApiKeyForm] = useState({ name: "", expires_at: "" });
  const [apiKeyFormError, setApiKeyFormError] = useState("");
  const [revealedKey, setRevealedKey] = useState<string | null>(null);
  const [keyCopied, setKeyCopied] = useState(false);

  // Audit log state
  const [auditData, setAuditData] = useState<PaginatedAuditLogs | null>(null);
  const [auditLoading, setAuditLoading] = useState(false);
  const [auditPage, setAuditPage] = useState(1);
  const [auditActionFilter, setAuditActionFilter] = useState("");
  const [auditSince, setAuditSince] = useState("");
  const [auditUntil, setAuditUntil] = useState("");
  const [auditRetentionDays, setAuditRetentionDays] = useState<number>(90);
  const [auditRetentionSaving, setAuditRetentionSaving] = useState(false);

  const refreshUsers = () => {
    usersApi.list().then((res) => setUsersList(res.data || [])).catch((e) => {
      toast(getErrorMessage(e, "Failed to load users"), "error");
    });
  };

  useEffect(() => {
    if (isAdmin) refreshUsers();
  }, [isAdmin]);

  useEffect(() => {
    fetch("/api/version").then(r => r.json()).then(setAppVersion).catch(() => {
      // Version fetch is non-critical, silently ignore
    });
  }, []);

  useEffect(() => {
    if (tab !== "about" || packerStatus) return;
    factoryApi.prerequisites().then((res) => setPackerStatus(res.data)).catch(() => {
      // Non-critical, silently ignore
    });
  }, [tab, packerStatus]);

  const refreshWebhooks = () => {
    webhooksApi.list().then((res) => setWebhooksList(res.data || [])).catch((e) => {
      toast(getErrorMessage(e, "Failed to load webhooks"), "error");
    });
  };

  const refreshApiKeys = () => {
    apiKeysApi.list().then((res) => setApiKeysList(res.data || [])).catch((e) => {
      toast(getErrorMessage(e, "Failed to load API keys"), "error");
    });
  };

  useEffect(() => {
    if (isAdmin && tab === "webhooks") refreshWebhooks();
  }, [isAdmin, tab]);

  useEffect(() => {
    if (tab === "apikeys") refreshApiKeys();
  }, [tab]);

  const refreshAuditLogs = (page?: number) => {
    setAuditLoading(true);
    const params: Record<string, string | number> = { page: page ?? auditPage, page_size: 25 };
    if (auditActionFilter) params.action = auditActionFilter;
    if (auditSince) params.since = new Date(auditSince).toISOString();
    if (auditUntil) params.until = new Date(auditUntil + "T23:59:59").toISOString();
    auditLogsApi.list(params).then((res) => setAuditData(res.data)).catch((e) => {
      toast(getErrorMessage(e, "Failed to load audit logs"), "error");
    }).finally(() => setAuditLoading(false));
  };

  useEffect(() => {
    if (isAdmin && tab === "auditlog") {
      refreshAuditLogs();
      settingsApi.get().then((res) => {
        const days = (res.data as Record<string, string>)["audit_retention_days"];
        if (days !== undefined) setAuditRetentionDays(Number(days));
      }).catch((e) => {
        toast(getErrorMessage(e, "Failed to load settings"), "error");
      });
    }
  }, [isAdmin, tab, auditPage]);

  const handleCreate = async () => {
    setFormError("");
    if (form.password.length < 12) {
      setFormError("Password must be at least 12 characters");
      return;
    }
    try {
      await usersApi.create(form);
      setShowForm(false);
      setForm({ username: "", password: "", display_name: "", role: "user" });
      refreshUsers();
    } catch (e: any) {
      const msg = e?.response?.data?.error || e?.message || "Failed to create user";
      setFormError(msg);
    }
  };

  const handleChangePassword = async (userId: number) => {
    setPwdMsg("");
    if (newPwd.length < 12) {
      setPwdMsg("Password must be at least 12 characters");
      return;
    }
    try {
      await usersApi.changePassword(userId, newPwd);
      setPwdMsg("Password updated ✓");
      setNewPwd("");
      setTimeout(() => { setPwdUserId(null); setPwdMsg(""); }, 1500);
    } catch (e: any) {
      setPwdMsg(e?.response?.data?.error || "Failed to change password");
    }
  };

  const handleDeleteUser = async (user: User) => {
    const ok = await showConfirm({ title: "Delete User", message: `Delete user "${user.username}"? This cannot be undone.`, confirmLabel: "Delete", variant: "destructive" });
    if (!ok) return;
    try {
      await usersApi.delete(user.id);
      toast(`User "${user.username}" deleted.`);
      refreshUsers();
    } catch (e: any) {
      toast(getErrorMessage(e, "Failed to delete user"), "error");
    }
  };

  const handleToggleActive = async (user: User) => {
    const next = !user.is_active;
    const ok = await showConfirm({
      title: next ? "Enable User" : "Disable User",
      message: next
        ? `Enable "${user.username}"? They will be able to log in again.`
        : `Disable "${user.username}"? They will be signed out of all sessions and unable to log in.`,
      confirmLabel: next ? "Enable" : "Disable",
      variant: next ? undefined : "destructive",
    });
    if (!ok) return;
    try {
      await usersApi.setActive(user.id, next);
      toast(next ? `User "${user.username}" enabled.` : `User "${user.username}" disabled and signed out.`);
      refreshUsers();
    } catch (e: any) {
      toast(getErrorMessage(e, "Failed to update status"), "error");
    }
  };

  const handleForceLogout = async (user: User) => {
    const ok = await showConfirm({
      title: "Force Logout",
      message: `Sign "${user.username}" out of all active sessions? They will need to log in again.`,
      confirmLabel: "Sign Out",
    });
    if (!ok) return;
    try {
      await usersApi.forceLogout(user.id);
      toast(`Signed "${user.username}" out of all sessions.`);
      refreshUsers();
    } catch (e: any) {
      toast(getErrorMessage(e, "Failed to force logout"), "error");
    }
  };

  const handleSaveDisplayName = async (userId: number) => {
    const name = editingNameValue.trim();
    try {
      await usersApi.update(userId, { display_name: name });
      setUsersList(usersList.map((u) => (u.id === userId ? { ...u, display_name: name } : u)));
      setEditingNameUserId(null);
      toast("Display name updated");
    } catch (e: any) {
      toast(getErrorMessage(e, "Failed to update display name"), "error");
    }
  };

  const resetWebhookForm = () => {
    setWebhookForm({ name: "", url: "", events: [], secret: "", is_active: true });
    setWebhookFormError("");
    setEditingWebhook(null);
    setShowWebhookForm(false);
  };

  const handleWebhookSubmit = async () => {
    setWebhookFormError("");
    if (!webhookForm.name.trim() || !webhookForm.url.trim() || webhookForm.events.length === 0) {
      setWebhookFormError("Name, URL, and at least one event are required.");
      return;
    }
    try {
      const payload = { name: webhookForm.name, url: webhookForm.url, events: webhookForm.events.join(","), secret: webhookForm.secret || undefined, is_active: webhookForm.is_active };
      if (editingWebhook) {
        await webhooksApi.update(editingWebhook.id, payload);
        toast("Webhook updated.");
      } else {
        await webhooksApi.create(payload as { name: string; url: string; events: string; is_active: boolean });
        toast("Webhook created.");
      }
      resetWebhookForm();
      refreshWebhooks();
    } catch (e: any) {
      setWebhookFormError(e?.response?.data?.error || e?.message || "Failed to save webhook");
    }
  };

  const handleToggleWebhook = async (wh: Webhook) => {
    try {
      await webhooksApi.update(wh.id, { is_active: !wh.is_active });
      refreshWebhooks();
    } catch (e) {
      toast(getErrorMessage(e, "Failed to toggle webhook"), "error");
    }
  };

  const handleDeleteWebhook = async (wh: Webhook) => {
    const ok = await showConfirm({ title: "Delete Webhook", message: `Delete webhook "${wh.name}"? This cannot be undone.`, confirmLabel: "Delete", variant: "destructive" });
    if (!ok) return;
    try {
      await webhooksApi.delete(wh.id);
      toast("Webhook deleted.");
      refreshWebhooks();
    } catch (e) {
      toast(getErrorMessage(e, "Failed to delete webhook"), "error");
    }
  };

  const handleTestWebhook = async (wh: Webhook) => {
    try {
      const res = await webhooksApi.test(wh.id);
      if (res.data.success) {
        toast(`Test payload delivered (HTTP ${res.data.status_code}).`);
      } else {
        toast(`Webhook responded with HTTP ${res.data.status_code}.`, "error");
      }
    } catch (e) {
      toast(getErrorMessage(e, "Failed to send test payload"), "error");
    }
  };

  const startEditWebhook = (wh: Webhook) => {
    setEditingWebhook(wh);
    setWebhookForm({ name: wh.name, url: wh.url, events: wh.events.split(",").filter(Boolean), secret: "", is_active: wh.is_active });
    setWebhookFormError("");
    setShowWebhookForm(true);
  };

  const handleCreateApiKey = async () => {
    setApiKeyFormError("");
    if (!apiKeyForm.name.trim()) {
      setApiKeyFormError("Name is required.");
      return;
    }
    try {
      const payload: { name: string; expires_at?: string } = { name: apiKeyForm.name };
      if (apiKeyForm.expires_at) payload.expires_at = new Date(apiKeyForm.expires_at).toISOString();
      const res = await apiKeysApi.create(payload);
      setRevealedKey(res.data.key);
      setKeyCopied(false);
      setShowApiKeyForm(false);
      setApiKeyForm({ name: "", expires_at: "" });
      refreshApiKeys();
    } catch (e: any) {
      setApiKeyFormError(e?.response?.data?.error || e?.message || "Failed to create API key");
    }
  };

  const handleDeleteApiKey = async (key: APIKey) => {
    const ok = await showConfirm({ title: "Delete API Key", message: `Delete API key "${key.name}" (${key.prefix}...)? This cannot be undone.`, confirmLabel: "Delete", variant: "destructive" });
    if (!ok) return;
    try {
      await apiKeysApi.delete(key.id);
      toast("API key deleted.");
      refreshApiKeys();
    } catch (e) {
      toast(getErrorMessage(e, "Failed to delete API key"), "error");
    }
  };

  const copyToClipboard = async (text: string) => {
    try {
      // navigator.clipboard requires HTTPS or localhost — use fallback for HTTP
      if (navigator.clipboard && window.isSecureContext) {
        await navigator.clipboard.writeText(text);
      } else {
        const textarea = document.createElement("textarea");
        textarea.value = text;
        textarea.style.position = "fixed";
        textarea.style.opacity = "0";
        document.body.appendChild(textarea);
        textarea.select();
        document.execCommand("copy");
        document.body.removeChild(textarea);
      }
      setKeyCopied(true);
      toast("Copied to clipboard");
      setTimeout(() => setKeyCopied(false), 2000);
    } catch (e) {
      toast(getErrorMessage(e, "Failed to copy to clipboard"), "error");
    }
  };

  const tabItems: { key: typeof tab; label: string; adminOnly?: boolean }[] = [
    { key: "users", label: "Users" },
    { key: "webhooks", label: "Webhooks", adminOnly: true },
    { key: "apikeys", label: "API Keys" },
    { key: "preferences", label: "Preferences" },
    { key: "auditlog", label: "Audit Log", adminOnly: true },
    { key: "about", label: "About" },
  ];

  return (
    <div className="space-y-6">
      <PageHeader
        title="Settings"
        description="Manage users, webhooks, API keys, and display preferences."
      />

      <div className="flex gap-1 border-b border-border overflow-x-auto scrollbar-none">
        {tabItems.filter((t) => !t.adminOnly || isAdmin).map((t) => (
          <button
            key={t.key}
            onClick={() => setTab(t.key)}
            className={`px-4 py-2 text-sm font-medium border-b-2 transition-colors whitespace-nowrap shrink-0 ${
              tab === t.key ? "border-primary text-primary" : "border-transparent text-muted-foreground hover:text-foreground"
            }`}
          >
            {t.label}
          </button>
        ))}
      </div>

      {tab === "users" && (() => {
        const search = userSearch.trim().toLowerCase();
        const filteredUsers = usersList.filter((u) => {
          if (userStatusFilter === "active" && !u.is_active) return false;
          if (userStatusFilter === "disabled" && u.is_active) return false;
          if (search) {
            return (
              u.username.toLowerCase().includes(search) ||
              (u.display_name || "").toLowerCase().includes(search) ||
              u.role.toLowerCase().includes(search)
            );
          }
          return true;
        });
        const activeCount = usersList.filter((u) => u.is_active).length;
        const disabledCount = usersList.length - activeCount;

        return (
        <div className="space-y-4">
          <div className="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-3">
            <div className="flex flex-wrap items-center gap-2 flex-1">
              <div className="relative w-full sm:w-72">
                <Search className="absolute left-3 top-1/2 -translate-y-1/2 h-4 w-4 text-muted-foreground" />
                <Input
                  placeholder="Search username, name, or role..."
                  value={userSearch}
                  onChange={(e) => setUserSearch(e.target.value)}
                  className="pl-9"
                />
              </div>
              <Select
                value={userStatusFilter}
                onChange={(e) => setUserStatusFilter(e.target.value as typeof userStatusFilter)}
                className="w-auto"
                aria-label="Filter by status"
              >
                <option value="all">All ({usersList.length})</option>
                <option value="active">Active ({activeCount})</option>
                <option value="disabled">Disabled ({disabledCount})</option>
              </Select>
            </div>
            {isAdmin && (
              <Button onClick={() => { setShowForm(!showForm); setFormError(""); }} className="shrink-0">
                {showForm ? <><X className="h-4 w-4 mr-2" />Cancel</> : <><Plus className="h-4 w-4 mr-2" />Add User</>}
              </Button>
            )}
          </div>

          {showForm && isAdmin && (
            <Card>
              <CardHeader><CardTitle>New User</CardTitle></CardHeader>
              <CardContent>
                <div className="grid gap-4 sm:grid-cols-2">
                  <div className="space-y-2">
                    <Label>Username</Label>
                    <Input value={form.username} onChange={(e) => setForm({ ...form, username: e.target.value })} />
                  </div>
                  <div className="space-y-2">
                    <Label>Password <span className="text-xs text-muted-foreground">(min 12 chars, stored as bcrypt hash)</span></Label>
                    <Input type="password" value={form.password} onChange={(e) => setForm({ ...form, password: e.target.value })} />
                  </div>
                  <div className="space-y-2">
                    <Label>Display Name</Label>
                    <Input value={form.display_name} onChange={(e) => setForm({ ...form, display_name: e.target.value })} />
                  </div>
                  <div className="space-y-2">
                    <Label>Role</Label>
                    <Select
                      value={form.role}
                      onChange={(e) => setForm({ ...form, role: e.target.value })}
                    >
                      <option value="admin">Admin</option>
                      <option value="user">User</option>
                      <option value="viewer">Viewer</option>
                    </Select>
                  </div>
                  <div className="sm:col-span-2 space-y-2">
                    {formError && <p className="text-sm text-destructive">{formError}</p>}
                    <Button onClick={handleCreate}>Create User</Button>
                  </div>
                </div>
              </CardContent>
            </Card>
          )}

          <Card>
            <CardContent className="p-0">
              <table className="w-full text-sm">
                <thead>
                  <tr className="border-b border-border bg-muted/30">
                    <th className="text-left px-4 py-3 font-medium text-muted-foreground text-xs uppercase tracking-wider">User</th>
                    <th className="text-left px-4 py-3 font-medium text-muted-foreground text-xs uppercase tracking-wider">Role</th>
                    <th className="text-left px-4 py-3 font-medium text-muted-foreground text-xs uppercase tracking-wider">Status</th>
                    <th className="text-left px-4 py-3 font-medium text-muted-foreground text-xs uppercase tracking-wider">Last Login</th>
                    <th className="w-12"></th>
                  </tr>
                </thead>
                <tbody>
                  {filteredUsers.length === 0 && (
                    <tr data-no-hover>
                      <td colSpan={5} className="px-4 py-10 text-center text-sm text-muted-foreground">
                        {usersList.length === 0 ? "No users yet" : "No users match your filters"}
                      </td>
                    </tr>
                  )}
                  {filteredUsers.map((u) => {
                    const isSelf = currentUser?.id === u.id;
                    const displayName = u.display_name || u.username;
                    const initials = displayName.split(/[\s._-]+/).filter(Boolean).slice(0, 2).map((s) => s[0]?.toUpperCase() || "").join("") || u.username.slice(0, 2).toUpperCase();
                    const isEditingPwd = pwdUserId === u.id;
                    const isEditingName = editingNameUserId === u.id;
                    return (
                      <Fragment key={u.id}>
                        <tr className={cn("border-b border-border", !u.is_active && "opacity-60")}>
                          <td className="px-4 py-3">
                            <div className="flex items-center gap-3">
                              <div className="flex h-9 w-9 shrink-0 items-center justify-center rounded-full bg-primary/15 text-[11px] font-semibold text-primary">
                                {initials}
                              </div>
                              <div className="min-w-0">
                                {isEditingName ? (
                                  <div className="flex items-center gap-1.5">
                                    <Input
                                      value={editingNameValue}
                                      onChange={(e) => setEditingNameValue(e.target.value)}
                                      onKeyDown={(e) => {
                                        if (e.key === "Enter") handleSaveDisplayName(u.id);
                                        if (e.key === "Escape") setEditingNameUserId(null);
                                      }}
                                      autoFocus
                                      className="h-8 text-sm max-w-[200px]"
                                      placeholder="Display name"
                                    />
                                    <Button size="sm" variant="ghost" className="h-7 w-7 p-0" onClick={() => handleSaveDisplayName(u.id)} title="Save">
                                      <Check className="h-3.5 w-3.5" />
                                    </Button>
                                    <Button size="sm" variant="ghost" className="h-7 w-7 p-0" onClick={() => setEditingNameUserId(null)} title="Cancel">
                                      <X className="h-3.5 w-3.5" />
                                    </Button>
                                  </div>
                                ) : (
                                  <>
                                    <div className="flex items-center gap-1.5 group/name">
                                      <span className="font-medium text-foreground truncate">{u.display_name || u.username}</span>
                                      {isSelf && <Badge variant="outline" className="text-[10px] px-1.5 py-0 shrink-0">You</Badge>}
                                      {(isAdmin || isSelf) && (
                                        <button
                                          onClick={() => { setEditingNameUserId(u.id); setEditingNameValue(u.display_name || ""); }}
                                          className="opacity-0 group-hover/name:opacity-100 text-muted-foreground hover:text-foreground transition-opacity"
                                          title="Edit display name"
                                          aria-label="Edit display name"
                                        >
                                          <Pencil className="h-3 w-3" />
                                        </button>
                                      )}
                                    </div>
                                    <div className="text-xs text-muted-foreground font-mono truncate">@{u.username}</div>
                                  </>
                                )}
                              </div>
                            </div>
                          </td>
                          <td className="px-4 py-3">
                            {isAdmin && !isSelf ? (
                              <select
                                className="text-sm border border-input rounded-md px-2 py-1 bg-background text-foreground hover:border-ring focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring [&>option]:bg-background [&>option]:text-foreground"
                                value={u.role}
                                onChange={async (e) => {
                                  const newRole = e.target.value;
                                  try {
                                    await usersApi.updateRole(u.id, newRole);
                                    setUsersList(usersList.map(x => x.id === u.id ? { ...x, role: newRole as User["role"] } : x));
                                    toast(`Role updated to ${newRole}`);
                                  } catch (err) {
                                    toast(getErrorMessage(err, "Failed to update role"), "error");
                                  }
                                }}
                              >
                                <option value="admin">Admin</option>
                                <option value="user">User</option>
                                <option value="viewer">Viewer</option>
                              </select>
                            ) : (
                              <Badge variant="secondary" className="capitalize">{u.role}</Badge>
                            )}
                          </td>
                          <td className="px-4 py-3">
                            <Badge variant={u.is_active ? "success" : "destructive"}>
                              {u.is_active ? "Active" : "Disabled"}
                            </Badge>
                          </td>
                          <td className="px-4 py-3 text-muted-foreground text-sm">
                            {u.last_login_at ? formatDateTime(u.last_login_at) : <span className="italic">Never</span>}
                          </td>
                          <td className="px-2 py-3 text-right">
                            <DropdownMenu
                              trigger={
                                <Button variant="ghost" size="sm" className="h-8 w-8 p-0" aria-label="User actions">
                                  <MoreHorizontal className="h-4 w-4" />
                                </Button>
                              }
                            >
                              {(isAdmin || isSelf) && (
                                <DropdownMenuItem
                                  icon={<KeyRound />}
                                  onClick={() => { setPwdUserId(isEditingPwd ? null : u.id); setNewPwd(""); setPwdMsg(""); }}
                                >
                                  {isEditingPwd ? "Cancel password change" : "Change password"}
                                </DropdownMenuItem>
                              )}
                              {(isAdmin || isSelf) && (
                                <DropdownMenuItem
                                  icon={<Pencil />}
                                  onClick={() => { setEditingNameUserId(u.id); setEditingNameValue(u.display_name || ""); }}
                                >
                                  Edit display name
                                </DropdownMenuItem>
                              )}
                              {isAdmin && !isSelf && (
                                <>
                                  <DropdownMenuSeparator />
                                  <DropdownMenuItem icon={<LogOutIcon />} onClick={() => handleForceLogout(u)}>
                                    Force sign out
                                  </DropdownMenuItem>
                                  <DropdownMenuItem
                                    icon={u.is_active ? <UserX /> : <UserCheck />}
                                    onClick={() => handleToggleActive(u)}
                                  >
                                    {u.is_active ? "Disable user" : "Enable user"}
                                  </DropdownMenuItem>
                                  <DropdownMenuSeparator />
                                  <DropdownMenuItem icon={<Trash2 />} destructive onClick={() => handleDeleteUser(u)}>
                                    Delete user
                                  </DropdownMenuItem>
                                </>
                              )}
                            </DropdownMenu>
                          </td>
                        </tr>
                        {isEditingPwd && (
                          <tr data-no-hover>
                            <td colSpan={5} className="px-4 py-3 bg-muted/30 border-b border-border">
                              <div className="flex items-center gap-2 max-w-xl">
                                <div className="text-xs text-muted-foreground whitespace-nowrap">New password for <span className="font-mono text-foreground">@{u.username}</span>:</div>
                                <Input
                                  type="password"
                                  placeholder="Min 12 characters"
                                  value={newPwd}
                                  onChange={(e) => setNewPwd(e.target.value)}
                                  className="h-8 text-sm flex-1"
                                  autoFocus
                                  onKeyDown={(e) => { if (e.key === "Enter") handleChangePassword(u.id); }}
                                />
                                <Button size="sm" onClick={() => handleChangePassword(u.id)}>Save</Button>
                                <Button size="sm" variant="ghost" onClick={() => { setPwdUserId(null); setNewPwd(""); setPwdMsg(""); }}>Cancel</Button>
                              </div>
                              {pwdMsg && (
                                <p className={`text-xs mt-1.5 ${pwdMsg.includes("✓") ? "text-success" : "text-destructive"}`}>{pwdMsg}</p>
                              )}
                            </td>
                          </tr>
                        )}
                      </Fragment>
                    );
                  })}
                </tbody>
              </table>
            </CardContent>
          </Card>
        </div>
        );
      })()}

      {tab === "webhooks" && isAdmin && (
        <div className="space-y-4">
          <div className="flex justify-end">
            <Button onClick={() => { if (showWebhookForm && !editingWebhook) { resetWebhookForm(); } else { resetWebhookForm(); setShowWebhookForm(true); } }}>
              {showWebhookForm ? <><X className="h-4 w-4 mr-2" />Cancel</> : <><Plus className="h-4 w-4 mr-2" />Add Webhook</>}
            </Button>
          </div>

          {showWebhookForm && (
            <Card>
              <CardHeader><CardTitle>{editingWebhook ? "Edit Webhook" : "New Webhook"}</CardTitle></CardHeader>
              <CardContent>
                <div className="grid gap-4 sm:grid-cols-2">
                  <div className="space-y-2">
                    <Label>Name</Label>
                    <Input value={webhookForm.name} onChange={(e) => setWebhookForm({ ...webhookForm, name: e.target.value })} placeholder="e.g. Slack notifications" />
                  </div>
                  <div className="space-y-2">
                    <Label>URL</Label>
                    <Input value={webhookForm.url} onChange={(e) => setWebhookForm({ ...webhookForm, url: e.target.value })} placeholder="https://example.com/webhook" />
                  </div>
                  <div className="space-y-2">
                    <Label>Secret <span className="text-xs text-muted-foreground">(optional, for HMAC signature)</span></Label>
                    <Input value={webhookForm.secret} onChange={(e) => setWebhookForm({ ...webhookForm, secret: e.target.value })} placeholder={editingWebhook ? "Leave blank to keep existing" : "Optional shared secret"} />
                  </div>
                  <div className="space-y-2">
                    <Label>Active</Label>
                    <div className="flex items-center gap-2 h-9">
                      <button
                        type="button"
                        onClick={() => setWebhookForm({ ...webhookForm, is_active: !webhookForm.is_active })}
                        className={`relative inline-flex h-5 w-9 shrink-0 cursor-pointer rounded-full border-2 border-transparent transition-colors ${webhookForm.is_active ? "bg-primary" : "bg-muted"}`}
                      >
                        <span className={`pointer-events-none inline-block h-4 w-4 rounded-full bg-background shadow-lg ring-0 transition-transform ${webhookForm.is_active ? "translate-x-4" : "translate-x-0"}`} />
                      </button>
                      <span className="text-sm text-muted-foreground">{webhookForm.is_active ? "Enabled" : "Disabled"}</span>
                    </div>
                  </div>
                  <div className="sm:col-span-2 space-y-2">
                    <Label>Events</Label>
                    <div className="grid gap-2 sm:grid-cols-2">
                      {WEBHOOK_EVENTS.map((ev) => (
                        <label key={ev} className="flex items-center gap-2 text-sm cursor-pointer">
                          <input
                            type="checkbox"
                            checked={webhookForm.events.includes(ev)}
                            onChange={(e) => {
                              const next = e.target.checked ? [...webhookForm.events, ev] : webhookForm.events.filter((x) => x !== ev);
                              setWebhookForm({ ...webhookForm, events: next });
                            }}
                            className="rounded border-input"
                          />
                          <span className="font-mono text-xs">{ev}</span>
                        </label>
                      ))}
                    </div>
                  </div>
                  <div className="sm:col-span-2 space-y-2">
                    {webhookFormError && <p className="text-sm text-destructive">{webhookFormError}</p>}
                    <Button onClick={handleWebhookSubmit}>{editingWebhook ? "Update Webhook" : "Create Webhook"}</Button>
                  </div>
                </div>
              </CardContent>
            </Card>
          )}

          <Card>
            <CardContent className="p-0">
              {webhooksList.length === 0 ? (
                <div className="p-8 text-center text-muted-foreground text-sm">
                  <WebhookIcon className="h-8 w-8 mx-auto mb-2 opacity-50" />
                  No webhooks configured yet.
                </div>
              ) : (
                <table className="w-full text-sm">
                  <thead>
                    <tr className="border-b">
                      <th className="text-left p-4 font-medium text-muted-foreground">Name</th>
                      <th className="text-left p-4 font-medium text-muted-foreground">URL</th>
                      <th className="text-left p-4 font-medium text-muted-foreground">Events</th>
                      <th className="text-left p-4 font-medium text-muted-foreground">Status</th>
                      <th className="text-left p-4 font-medium text-muted-foreground">Actions</th>
                    </tr>
                  </thead>
                  <tbody>
                    {webhooksList.map((wh) => (
                      <tr key={wh.id} className="border-b">
                        <td className="p-4 font-medium">{wh.name}</td>
                        <td className="p-4 text-muted-foreground font-mono text-xs max-w-[200px] truncate" title={wh.url}>{wh.url}</td>
                        <td className="p-4"><Badge variant="secondary">{wh.events.split(",").length} event{wh.events.split(",").length !== 1 ? "s" : ""}</Badge></td>
                        <td className="p-4">
                          <button onClick={() => handleToggleWebhook(wh)} className="cursor-pointer">
                            <Badge variant={wh.is_active ? "success" : "destructive"}>
                              {wh.is_active ? "Active" : "Inactive"}
                            </Badge>
                          </button>
                        </td>
                        <td className="p-4">
                          <div className="flex items-center gap-1">
                            <Button size="sm" variant="ghost" onClick={() => handleTestWebhook(wh)} title="Send test payload">
                              <Send className="h-3 w-3" />
                            </Button>
                            <Button size="sm" variant="ghost" onClick={() => startEditWebhook(wh)} title="Edit">
                              <Pencil className="h-3 w-3" />
                            </Button>
                            <Button size="sm" variant="ghost" onClick={() => handleDeleteWebhook(wh)} title="Delete" className="text-destructive hover:text-destructive">
                              <Trash2 className="h-3 w-3" />
                            </Button>
                          </div>
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              )}
            </CardContent>
          </Card>
        </div>
      )}

      {tab === "apikeys" && (
        <div className="space-y-4">
          <Card className="border-blue-500/20 bg-blue-500/5">
            <CardContent className="p-4">
              <div className="flex items-start gap-3">
                <AlertTriangle className="h-5 w-5 text-blue-500 shrink-0 mt-0.5" />
                <div className="text-sm">
                  <p className="font-medium text-blue-500">API Key Authentication</p>
                  <p className="text-muted-foreground mt-1">
                    API keys authenticate as Bearer tokens. Use <code className="bg-muted px-1 py-0.5 rounded text-xs font-mono">Authorization: Bearer fm_xxxxx</code> in your requests.
                  </p>
                </div>
              </div>
            </CardContent>
          </Card>

          {revealedKey && (
            <Card className="border-yellow-500/30 bg-yellow-500/5">
              <CardHeader><CardTitle className="text-base flex items-center gap-2"><KeyRound className="h-4 w-4" />API Key Created</CardTitle></CardHeader>
              <CardContent className="space-y-3">
                <p className="text-sm text-muted-foreground">Copy this key now. It will not be shown again.</p>
                <div className="flex items-center gap-2">
                  <code className="flex-1 bg-muted px-3 py-2 rounded font-mono text-sm break-all">{revealedKey}</code>
                  <Button size="sm" variant="outline" onClick={() => copyToClipboard(revealedKey)}>
                    {keyCopied ? <><Check className="h-3 w-3 mr-1" />Copied</> : <><Copy className="h-3 w-3 mr-1" />Copy</>}
                  </Button>
                </div>
                <Button size="sm" variant="ghost" onClick={() => setRevealedKey(null)}>Dismiss</Button>
              </CardContent>
            </Card>
          )}

          <div className="flex justify-end">
            <Button onClick={() => { setShowApiKeyForm(!showApiKeyForm); setApiKeyFormError(""); setApiKeyForm({ name: "", expires_at: "" }); }}>
              {showApiKeyForm ? <><X className="h-4 w-4 mr-2" />Cancel</> : <><Plus className="h-4 w-4 mr-2" />Create API Key</>}
            </Button>
          </div>

          {showApiKeyForm && (
            <Card>
              <CardHeader><CardTitle>New API Key</CardTitle></CardHeader>
              <CardContent>
                <div className="grid gap-4 sm:grid-cols-2">
                  <div className="space-y-2">
                    <Label>Name</Label>
                    <Input value={apiKeyForm.name} onChange={(e) => setApiKeyForm({ ...apiKeyForm, name: e.target.value })} placeholder="e.g. CI/CD Pipeline" />
                  </div>
                  <div className="space-y-2">
                    <Label>Expires <span className="text-xs text-muted-foreground">(optional)</span></Label>
                    <Input type="date" value={apiKeyForm.expires_at} onChange={(e) => setApiKeyForm({ ...apiKeyForm, expires_at: e.target.value })} />
                  </div>
                  <div className="sm:col-span-2 space-y-2">
                    {apiKeyFormError && <p className="text-sm text-destructive">{apiKeyFormError}</p>}
                    <Button onClick={handleCreateApiKey}>Create Key</Button>
                  </div>
                </div>
              </CardContent>
            </Card>
          )}

          <Card>
            <CardContent className="p-0">
              {apiKeysList.length === 0 ? (
                <div className="p-8 text-center text-muted-foreground text-sm">
                  <KeyRound className="h-8 w-8 mx-auto mb-2 opacity-50" />
                  No API keys created yet.
                </div>
              ) : (
                <table className="w-full text-sm">
                  <thead>
                    <tr className="border-b">
                      <th className="text-left p-4 font-medium text-muted-foreground">Name</th>
                      <th className="text-left p-4 font-medium text-muted-foreground">Prefix</th>
                      <th className="text-left p-4 font-medium text-muted-foreground">Owner</th>
                      <th className="text-left p-4 font-medium text-muted-foreground">Last Used</th>
                      <th className="text-left p-4 font-medium text-muted-foreground">Expires</th>
                      <th className="text-left p-4 font-medium text-muted-foreground">Created</th>
                      <th className="text-left p-4 font-medium text-muted-foreground">Actions</th>
                    </tr>
                  </thead>
                  <tbody>
                    {apiKeysList.map((k) => (
                      <tr key={k.id} className="border-b">
                        <td className="p-4 font-medium">{k.name}</td>
                        <td className="p-4 font-mono text-xs text-muted-foreground">{k.prefix}...</td>
                        <td className="p-4 text-muted-foreground">{k.username || "-"}</td>
                        <td className="p-4 text-muted-foreground">{k.last_used_at ? formatDateTime(k.last_used_at) : "Never"}</td>
                        <td className="p-4 text-muted-foreground">{k.expires_at ? formatDateTime(k.expires_at) : "Never"}</td>
                        <td className="p-4 text-muted-foreground">{formatDateTime(k.created_at)}</td>
                        <td className="p-4">
                          <Button size="sm" variant="ghost" className="text-destructive hover:text-destructive hover:bg-destructive/10" onClick={() => handleDeleteApiKey(k)}>
                            <Trash2 className="h-3 w-3" />
                          </Button>
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              )}
            </CardContent>
          </Card>
        </div>
      )}

      {tab === "preferences" && (
        <div className="space-y-4">
          <Card>
            <CardHeader>
              <CardTitle className="flex items-center gap-2"><Globe className="h-5 w-5" />Display Timezone</CardTitle>
            </CardHeader>
            <CardContent className="space-y-4">
              <p className="text-sm text-muted-foreground">
                All dates and times in the UI will be displayed in your selected timezone.
                This is stored locally in your browser.
              </p>
              <div className="space-y-2">
                <Label>Timezone</Label>
                <Select
                  value={timezone}
                  onChange={(e) => setTimezone(e.target.value)}
                >
                  {COMMON_TIMEZONES.map((tz) => (
                    <option key={tz} value={tz}>{tz.replace(/_/g, " ")}</option>
                  ))}
                </Select>
              </div>
              <div className="rounded-md bg-muted p-3 text-sm">
                <span className="text-muted-foreground">Preview: </span>
                <span className="font-medium">{formatDateTime(new Date())}</span>
              </div>
            </CardContent>
          </Card>

          <Card>
            <CardHeader>
              <CardTitle className="flex items-center gap-2"><Trash2 className="h-5 w-5" />Data Management</CardTitle>
            </CardHeader>
            <CardContent className="space-y-4">
              <div className="flex items-start justify-between gap-4">
                <div>
                  <p className="text-sm font-medium">Deployment History</p>
                  <p className="text-xs text-muted-foreground">
                    Clear all completed and failed deployment records. Active deployments will not be affected.
                  </p>
                </div>
                <Button
                  variant="destructive"
                  size="sm"
                  className="shrink-0"
                  onClick={async () => {
                    const ok = await showConfirm({ title: "Clear Deployment History", message: "This will remove all completed and failed deployment records. VMs deployed from these records will lose their stored credentials. This cannot be undone.", confirmLabel: "Clear History", variant: "destructive" });
                    if (!ok) return;
                    try {
                      const res = await settingsApi.clearDeploymentHistory();
                      const count = (res.data as { deleted: number }).deleted;
                      toast(`Cleared ${count} deployment record${count !== 1 ? "s" : ""}.`);
                    } catch (e) {
                      toast(getErrorMessage(e, "Failed to clear deployment history"), "error");
                    }
                  }}
                >
                  <Trash2 className="h-3.5 w-3.5 mr-1.5" />
                  Clear History
                </Button>
              </div>
            </CardContent>
          </Card>
        </div>
      )}

      {tab === "auditlog" && isAdmin && (
        <div className="space-y-4">
          <Card>
            <CardHeader>
              <CardTitle className="flex items-center gap-2"><Trash2 className="h-5 w-5" />Retention Policy</CardTitle>
            </CardHeader>
            <CardContent>
              <div className="flex items-center gap-4">
                <p className="text-sm text-muted-foreground flex-1">
                  Audit log entries older than this will be automatically deleted daily. Set to <strong>0</strong> to retain indefinitely.
                </p>
                <div className="flex items-center gap-2 shrink-0">
                  <Input
                    type="number"
                    min={0}
                    max={3650}
                    value={auditRetentionDays}
                    onChange={(e) => setAuditRetentionDays(Number(e.target.value))}
                    className="h-8 w-24 text-sm"
                  />
                  <span className="text-sm text-muted-foreground">days</span>
                  <Button size="sm" disabled={auditRetentionSaving} onClick={async () => {
                    setAuditRetentionSaving(true);
                    try {
                      await settingsApi.update({ audit_retention_days: String(auditRetentionDays) });
                      toast("Retention policy saved");
                    } catch (e) {
                      toast(getErrorMessage(e, "Failed to save retention policy"), "error");
                    } finally {
                      setAuditRetentionSaving(false);
                    }
                  }}>Save</Button>
                </div>
              </div>
            </CardContent>
          </Card>

          <Card>
            <CardContent className="p-4">
              <div className="flex flex-wrap items-end gap-4">
                <div className="space-y-1">
                  <Label className="text-xs text-muted-foreground">Action</Label>
                  <Input
                    placeholder="Filter by action..."
                    value={auditActionFilter}
                    onChange={(e) => setAuditActionFilter(e.target.value)}
                    className="h-8 w-48 text-xs"
                  />
                </div>
                <div className="space-y-1">
                  <Label className="text-xs text-muted-foreground">Since</Label>
                  <Input
                    type="date"
                    value={auditSince}
                    onChange={(e) => setAuditSince(e.target.value)}
                    className="h-8 w-40 text-xs"
                  />
                </div>
                <div className="space-y-1">
                  <Label className="text-xs text-muted-foreground">Until</Label>
                  <Input
                    type="date"
                    value={auditUntil}
                    onChange={(e) => setAuditUntil(e.target.value)}
                    className="h-8 w-40 text-xs"
                  />
                </div>
                <Button size="sm" onClick={() => { setAuditPage(1); refreshAuditLogs(1); }}>
                  Filter
                </Button>
                {(auditActionFilter || auditSince || auditUntil) && (
                  <Button size="sm" variant="ghost" onClick={() => { setAuditActionFilter(""); setAuditSince(""); setAuditUntil(""); setAuditPage(1); setTimeout(() => refreshAuditLogs(1), 0); }}>
                    Clear
                  </Button>
                )}
              </div>
            </CardContent>
          </Card>

          <Card>
            <CardContent className="p-0">
              {auditLoading ? (
                <div className="p-8 text-center text-muted-foreground text-sm">Loading...</div>
              ) : !auditData || auditData.logs.length === 0 ? (
                <div className="p-8 text-center text-muted-foreground text-sm">
                  No audit events recorded yet.
                </div>
              ) : (
                <table className="w-full text-sm">
                  <thead>
                    <tr className="border-b">
                      <th className="text-left p-4 font-medium text-muted-foreground">Timestamp</th>
                      <th className="text-left p-4 font-medium text-muted-foreground">Actor</th>
                      <th className="text-left p-4 font-medium text-muted-foreground">Action</th>
                      <th className="text-left p-4 font-medium text-muted-foreground">Resource</th>
                      <th className="text-left p-4 font-medium text-muted-foreground">Details</th>
                      <th className="text-left p-4 font-medium text-muted-foreground">IP</th>
                    </tr>
                  </thead>
                  <tbody>
                    {auditData.logs.map((log) => (
                      <tr key={log.id} className="border-b">
                        <td className="p-4 text-muted-foreground whitespace-nowrap">{formatDateTime(log.created_at)}</td>
                        <td className="p-4 font-medium">{log.actor}</td>
                        <td className="p-4"><Badge variant="secondary">{log.action}</Badge></td>
                        <td className="p-4 text-muted-foreground">{log.resource_type ? `${log.resource_type}${log.resource_id ? `:${log.resource_id}` : ""}` : "-"}</td>
                        <td className="p-4 text-muted-foreground font-mono text-xs max-w-xs truncate" title={log.metadata && Object.keys(log.metadata).length > 0 ? JSON.stringify(log.metadata) : undefined}>{log.metadata && Object.keys(log.metadata).length > 0 ? Object.entries(log.metadata).map(([k, v]) => `${k}=${v}`).join(", ") : "-"}</td>
                        <td className="p-4 text-muted-foreground font-mono text-xs">{log.ip_address || "-"}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              )}
            </CardContent>
          </Card>

          {auditData && auditData.total_pages > 1 && (
            <div className="flex items-center justify-between">
              <p className="text-sm text-muted-foreground">
                Page {auditData.page} of {auditData.total_pages} ({auditData.total} total)
              </p>
              <div className="flex gap-2">
                <Button
                  size="sm"
                  variant="outline"
                  disabled={auditData.page <= 1}
                  onClick={() => setAuditPage(auditData.page - 1)}
                >
                  Previous
                </Button>
                <Button
                  size="sm"
                  variant="outline"
                  disabled={auditData.page >= auditData.total_pages}
                  onClick={() => setAuditPage(auditData.page + 1)}
                >
                  Next
                </Button>
              </div>
            </div>
          )}
        </div>
      )}

      {tab === "about" && (
        <div className="space-y-6">
        <Card>
          <CardHeader>
            <CardTitle className="flex items-center gap-3">
              <ForgemillLogo size={40} className="shrink-0" />
              <div>
                <div>Forgemill</div>
                <p className="text-sm font-normal text-muted-foreground">Infrastructure, forged to order.</p>
              </div>
            </CardTitle>
          </CardHeader>
          <CardContent className="space-y-4 text-sm">
            <p className="text-muted-foreground">
              A self-hosted platform for building, deploying, and managing virtual machines across VMware vCenter, ESXi, and Proxmox VE — from a single interface.
            </p>

            <div className="grid gap-3 sm:grid-cols-2">
              <div className="rounded-md border p-3 space-y-1">
                <p className="text-xs text-muted-foreground font-medium uppercase tracking-wider">Version</p>
                <p className="text-foreground font-mono">{appVersion.version}</p>
                {appVersion.commit && appVersion.commit !== "unknown" && (
                  <p className="text-xs text-muted-foreground font-mono">{appVersion.commit.slice(0, 7)}</p>
                )}
              </div>
              <div className="rounded-md border p-3 space-y-1">
                <p className="text-xs text-muted-foreground font-medium uppercase tracking-wider">License</p>
                <p className="text-foreground">MIT</p>
              </div>
              <div className="rounded-md border p-3 space-y-1">
                <p className="text-xs text-muted-foreground font-medium uppercase tracking-wider">Stack</p>
                <p className="text-foreground">Go · React · SQLite</p>
              </div>
              <div className="rounded-md border p-3 space-y-1">
                <p className="text-xs text-muted-foreground font-medium uppercase tracking-wider">Runtime</p>
                <p className="text-foreground">Docker (single container)</p>
              </div>
              <div className="rounded-md border p-3 space-y-1 sm:col-span-2">
                <p className="text-xs text-muted-foreground font-medium uppercase tracking-wider">Packer</p>
                {packerStatus === null ? (
                  <p className="text-muted-foreground text-xs">Checking...</p>
                ) : packerStatus.packer_installed ? (
                  <p className="text-foreground font-mono text-sm">{packerStatus.packer_version}</p>
                ) : (
                  <p className="text-warning text-sm">Not installed — required for Template Factory</p>
                )}
              </div>
            </div>
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle className="text-base">Features</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="grid gap-3 sm:grid-cols-2 text-sm">
              <div className="flex items-start gap-2">
                <span className="text-green-500 mt-0.5">✓</span>
                <div>
                  <p className="font-medium">Multi-Hypervisor</p>
                  <p className="text-xs text-muted-foreground">vCenter, ESXi standalone, Proxmox VE</p>
                </div>
              </div>
              <div className="flex items-start gap-2">
                <span className="text-green-500 mt-0.5">✓</span>
                <div>
                  <p className="font-medium">Template Factory</p>
                  <p className="text-xs text-muted-foreground">Build templates from ISO with Packer</p>
                </div>
              </div>
              <div className="flex items-start gap-2">
                <span className="text-green-500 mt-0.5">✓</span>
                <div>
                  <p className="font-medium">VM Lifecycle</p>
                  <p className="text-xs text-muted-foreground">Deploy, power, snapshot, resize, console</p>
                </div>
              </div>
              <div className="flex items-start gap-2">
                <span className="text-green-500 mt-0.5">✓</span>
                <div>
                  <p className="font-medium">Post-Deploy Actions</p>
                  <p className="text-xs text-muted-foreground">SSH execution with live terminal streaming</p>
                </div>
              </div>
              <div className="flex items-start gap-2">
                <span className="text-green-500 mt-0.5">✓</span>
                <div>
                  <p className="font-medium">Credential Management</p>
                  <p className="text-xs text-muted-foreground">AES-256-GCM encrypted, auto-generated</p>
                </div>
              </div>
              <div className="flex items-start gap-2">
                <span className="text-green-500 mt-0.5">✓</span>
                <div>
                  <p className="font-medium">ISO Update Detection</p>
                  <p className="text-xs text-muted-foreground">Checksum comparison against upstream mirrors</p>
                </div>
              </div>
            </div>
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle className="text-base">Links</CardTitle>
          </CardHeader>
          <CardContent className="space-y-2 text-sm">
            <a href="https://github.com/blink-zero/forgemill" target="_blank" rel="noopener noreferrer" className="flex items-center gap-2 text-primary hover:underline">
              GitHub Repository →
            </a>
            <a href="https://github.com/blink-zero/forgemill/issues" target="_blank" rel="noopener noreferrer" className="flex items-center gap-2 text-primary hover:underline">
              Report an Issue →
            </a>
          </CardContent>
        </Card>
        </div>
      )}
    </div>
  );
}
