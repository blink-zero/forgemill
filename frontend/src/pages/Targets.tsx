import { useEffect, useState } from "react";
import { targets as targetApi } from "@/api/client";
import type { Target } from "@/types";
import { Card, CardHeader, CardTitle, CardContent } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { useToast } from "@/components/ui/toast";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Plus, Trash2, RefreshCw, Wifi, X, ShieldCheck, Info, AlertTriangle, Box, Monitor, Rocket, Wrench, Terminal, Pencil, Loader2, MoreHorizontal } from "lucide-react";
import { Select } from "@/components/ui/select";
import ProviderIcon, { providerLabel } from "@/components/ProviderIcon";
import { getErrorMessage, timeAgo } from "@/lib/utils";
import { DropdownMenu, DropdownMenuItem, DropdownMenuSeparator } from "@/components/ui/dropdown-menu";
import { Star } from "lucide-react";
import { useProviders, useProvider } from "@/context/ProviderContext";
import { ViewToggle } from "@/components/ui/view-toggle";
import { usePreference } from "@/context/PreferencesContext";
import { SortableTh } from "@/components/ui/sortable-th";
import { useTableSort } from "@/hooks/useTableSort";
import { PageHeader } from "@/components/ui/page-header";
import { PermissionsHelp } from "@/components/ui/permissions-help";

// Extracted form component using provider metadata from context
interface TargetFormProps {
  form: { name: string; type: string; hostname: string; port: number; username: string; password: string; validate_certs: boolean };
  setForm: (form: TargetFormProps["form"]) => void;
  onSubmit: () => void;
  onCancel?: () => void;
  submitLabel: string;
  title: string;
  isEdit?: boolean;
}

function TargetForm({ form, setForm, onSubmit, onCancel, submitLabel, title, isEdit }: TargetFormProps) {
  const { providers } = useProviders();
  const currentProvider = useProvider(form.type);

  const handleTypeChange = (newType: string) => {
    const provider = providers.find(p => p.id === newType);
    if (provider) {
      setForm({
        ...form,
        type: newType,
        port: provider.defaults.port,
        username: provider.defaults.username,
      });
    }
  };

  return (
    <Card>
      <CardHeader><CardTitle>{title}</CardTitle></CardHeader>
      <CardContent>
        <div className="grid gap-4 sm:grid-cols-2">
          <div className="space-y-2">
            <Label>Name</Label>
            <Input
              value={form.name}
              onChange={(e) => setForm({ ...form, name: e.target.value })}
              placeholder={currentProvider?.defaults.name_placeholder || "Target name"}
            />
          </div>
          <div className="space-y-2">
            <Label>Type</Label>
            <Select value={form.type} onChange={(e) => handleTypeChange(e.target.value)} disabled={isEdit}>
              {providers.map((p) => (
                <option key={p.id} value={p.id}>{p.name}</option>
              ))}
            </Select>
            {currentProvider && (
              <p className="text-xs text-muted-foreground">{currentProvider.description}</p>
            )}
          </div>
          <div className="space-y-2">
            <Label>Hostname</Label>
            <Input
              value={form.hostname}
              onChange={(e) => setForm({ ...form, hostname: e.target.value })}
              placeholder={currentProvider?.defaults.hostname_placeholder || "hostname.example.com"}
            />
          </div>
          <div className="space-y-2">
            <Label>Port</Label>
            <Input
              type="number"
              value={form.port}
              onChange={(e) => setForm({ ...form, port: Number(e.target.value) })}
            />
            {currentProvider?.hints.port && (
              <p className="text-xs text-muted-foreground">{currentProvider.hints.port}</p>
            )}
          </div>
          <div className="space-y-2">
            <div className="flex items-center gap-1.5">
              <Label>Username</Label>
              <PermissionsHelp providerType={form.type} />
            </div>
            <Input
              value={form.username}
              onChange={(e) => setForm({ ...form, username: e.target.value })}
              placeholder={currentProvider?.defaults.username || "username"}
            />
            {currentProvider?.hints.username && (
              <p className="text-xs text-muted-foreground">{currentProvider.hints.username}</p>
            )}
          </div>
          <div className="space-y-2">
            <Label>Password</Label>
            <Input
              type="password"
              value={form.password}
              onChange={(e) => setForm({ ...form, password: e.target.value })}
            />
          </div>
          <div className="flex items-center gap-2 sm:col-span-2">
            <input
              type="checkbox"
              id="validate_certs"
              checked={form.validate_certs}
              onChange={(e) => setForm({ ...form, validate_certs: e.target.checked })}
              className="rounded"
            />
            <Label htmlFor="validate_certs">Validate SSL/TLS certificates</Label>
            <span className="text-xs text-muted-foreground ml-1">(disable only for self-signed certs)</span>
          </div>
          <div className="sm:col-span-2 flex items-start gap-2 rounded-md border border-border bg-muted/50 p-3">
            <ShieldCheck className="h-4 w-4 text-green-500 mt-0.5 shrink-0" />
            <p className="text-xs text-muted-foreground">
              Credentials are encrypted at rest using AES-256-GCM and are never logged or exposed via the API.
              They are only used to communicate directly with your hypervisor over HTTPS.
            </p>
          </div>
          <div className="sm:col-span-2 flex gap-2">
            <Button onClick={onSubmit}>{submitLabel}</Button>
            {onCancel && <Button variant="outline" onClick={onCancel}>Cancel</Button>}
          </div>
        </div>
      </CardContent>
    </Card>
  );
}

export default function Targets() {
  const { toast } = useToast();
  const [targets, setTargets] = useState<Target[]>([]);
  const [loading, setLoading] = useState(true);
  const [showForm, setShowForm] = useState(false);
  const [testing, setTesting] = useState<number | null>(null);
  const [syncing, setSyncing] = useState<number | null>(null);
  const [deleteModal, setDeleteModal] = useState<{ target: Target; preview: { templates: number; vms: number; deployments: number; builds: number; executions: number } } | null>(null);
  const [deleting, setDeleting] = useState(false);
  const [actionModal, setActionModal] = useState<{
    targetName: string;
    targetId: number;
    phase: "testing" | "test-success" | "test-failed" | "syncing" | "sync-done";
    targetType?: string;
    message: string;
    templatesFound?: number;
  } | null>(null);
  const [editTarget, setEditTarget] = useState<Target | null>(null);

  const viewMode = usePreference("view_mode", "cards");
  const { sorted: targetsSorted, sortField: tgtSortField, sortDir: tgtSortDir, toggleSort: tgtToggleSort } = useTableSort(targets, "name");

  const [form, setForm] = useState({
    name: "", type: "vcenter", hostname: "", port: 443, username: "", password: "", validate_certs: true,
  });

  const loadTargets = () => {
    targetApi.list().then((res) => setTargets(res.data || [])).finally(() => setLoading(false));
  };

  useEffect(() => { loadTargets(); }, []);

  const handleCreate = async () => {
    try {
      const res = await targetApi.create({ ...form, type: form.type as Target["type"], port: Number(form.port) });
      setShowForm(false);
      setForm({ name: "", type: "vcenter", hostname: "", port: 443, username: "", password: "", validate_certs: true });
      loadTargets();
      // Auto-test immediately after adding
      const newTarget = res.data;
      runTest(newTarget.id, newTarget.name);
    } catch (e) {
      toast(getErrorMessage(e, "Failed to create target"), "error");
    }
  };

  const handleEditClick = (target: Target) => {
    setEditTarget(target);
    setShowForm(false);
    setForm({
      name: target.name,
      type: target.type,
      hostname: target.hostname,
      port: target.port,
      username: target.username,
      password: "",
      validate_certs: target.validate_certs,
    });
  };

  const handleUpdate = async () => {
    if (!editTarget) return;
    try {
      await targetApi.update(editTarget.id, {
        name: form.name,
        hostname: form.hostname,
        port: Number(form.port),
        username: form.username,
        password: form.password || undefined, // only send if changed
        validate_certs: form.validate_certs,
      });
      toast("Target updated");
      setEditTarget(null);
      setForm({ name: "", type: "vcenter", hostname: "", port: 443, username: "", password: "", validate_certs: true });
      loadTargets();
    } catch (e) {
      toast(getErrorMessage(e, "Failed to update target"), "error");
    }
  };

  const handleCancelEdit = () => {
    setEditTarget(null);
    setForm({ name: "", type: "vcenter", hostname: "", port: 443, username: "", password: "", validate_certs: true });
  };

  const handleDeleteClick = async (target: Target) => {
    try {
      const res = await targetApi.deletePreview(target.id);
      setDeleteModal({ target, preview: res.data });
    } catch (e) {
      toast(getErrorMessage(e, "Failed to load delete preview"), "error");
    }
  };

  const handleDeleteConfirm = async () => {
    if (!deleteModal) return;
    setDeleting(true);
    try {
      await targetApi.delete(deleteModal.target.id);
      setDeleteModal(null);
      loadTargets();
    } catch (e) {
      toast(getErrorMessage(e, "Failed to delete target"), "error");
    } finally {
      setDeleting(false);
    }
  };

  const runTest = async (id: number, name: string, type?: string) => {
    setActionModal({ targetName: name, targetId: id, phase: "testing", message: "Testing connection...", targetType: type });
    setTesting(id);
    try {
      const res = await targetApi.test(id);
      loadTargets();
      setActionModal({ targetName: name, targetId: id, phase: "test-success", message: res.data.message || "Connection successful", targetType: type });
    } catch (e) {
      setActionModal({
        targetName: name,
        targetId: id,
        phase: "test-failed",
        message: getErrorMessage(e, "Connection test failed. Check credentials and hostname."),
        targetType: type,
      });
    } finally {
      setTesting(null);
    }
  };

  const handleTest = (target: Target) => runTest(target.id, target.name, target.type);

  const runSync = async (id: number, name: string, type?: string) => {
    setActionModal({ targetName: name, targetId: id, phase: "syncing", message: "Syncing templates...", targetType: type });
    setSyncing(id);
    try {
      const res = await targetApi.sync(id);
      loadTargets();
      setActionModal({ targetName: name, targetId: id, phase: "sync-done", message: `Sync complete`, templatesFound: res.data.templates_found, targetType: type });
    } catch (e) {
      setActionModal({
        targetName: name,
        targetId: id,
        phase: "test-failed",
        message: getErrorMessage(e, "Sync failed. Check target connection."),
        targetType: type,
      });
    } finally {
      setSyncing(null);
    }
  };

  const handleSync = (target: Target) => runSync(target.id, target.name, target.type);

  if (loading) {
    return <div className="flex items-center justify-center h-64"><Loader2 className="h-8 w-8 animate-spin text-primary" /></div>;
  }

  return (
    <div className="space-y-6">
      <PageHeader
        title={
          <span className="flex items-center gap-2">
            Targets
            {targets.length > 0 && <Badge variant="outline">{targets.length}</Badge>}
          </span>
        }
        description="Hypervisor connections — manage your vCenter, ESXi, and Proxmox targets. Templates and VMs are built and deployed through these."
        actions={
          <>
            <ViewToggle />
            <Button onClick={() => { setShowForm(!showForm); setEditTarget(null); }}>
              {showForm ? <><X className="h-4 w-4 mr-2" />Cancel</> : <><Plus className="h-4 w-4 mr-2" />Add Target</>}
            </Button>
          </>
        }
      />

      {showForm && (
        <TargetForm
          form={form}
          setForm={setForm}
          onSubmit={handleCreate}
          submitLabel="Create Target"
          title="New Target"
        />
      )}
      {editTarget && (
        <TargetForm
          form={form}
          setForm={setForm}
          onSubmit={handleUpdate}
          onCancel={handleCancelEdit}
          submitLabel="Update Target"
          title={`Edit ${editTarget.name}`}
          isEdit
        />
      )}

      {targets.length === 0 && !showForm ? (
        <div className="text-center py-16 text-muted-foreground">
          <Terminal className="h-12 w-12 mx-auto mb-4 opacity-50" />
          <p className="font-medium">No targets configured</p>
          <p className="text-sm mt-1">Add a vCenter, ESXi, or Proxmox target to get started</p>
        </div>
      ) : (
        <>
          {/* Per-target action menu (reused by table and card views) */}
          {(() => null)()}
          {viewMode === "table" ? (
            <div className="rounded-md border">
              <table className="w-full text-sm">
                <thead>
                  <tr className="border-b bg-muted/50">
                    <SortableTh label="Name" field="name" currentField={tgtSortField} currentDir={tgtSortDir} onSort={tgtToggleSort} />
                    <SortableTh label="Hostname" field="hostname" currentField={tgtSortField} currentDir={tgtSortDir} onSort={tgtToggleSort} className="hidden sm:table-cell" />
                    <SortableTh label="Type" field="type" currentField={tgtSortField} currentDir={tgtSortDir} onSort={tgtToggleSort} className="hidden md:table-cell" />
                    <th className="text-left px-4 py-2 font-medium">Status</th>
                    <th className="text-left px-4 py-2 font-medium hidden lg:table-cell">Last connected</th>
                    <th className="text-right px-4 py-2 font-medium w-32">Actions</th>
                  </tr>
                </thead>
                <tbody>
                  {targetsSorted.map((t) => (
                    <tr key={t.id} className="border-b last:border-0 hover:bg-muted/30 transition-colors">
                      <td className="px-4 py-2.5">
                        <div className="flex items-center gap-2">
                          <ProviderIcon type={t.type} size={18} />
                          <span className="font-medium">{t.name}</span>
                          {t.is_default && (
                            <span title="Default target" className="inline-flex items-center gap-0.5 text-[10px] font-medium px-1.5 py-0.5 rounded bg-primary/15 text-primary">
                              <Star className="h-2.5 w-2.5" />
                              default
                            </span>
                          )}
                        </div>
                      </td>
                      <td className="px-4 py-2.5 text-muted-foreground font-mono text-xs hidden sm:table-cell">{t.hostname}:{t.port}</td>
                      <td className="px-4 py-2.5 text-muted-foreground hidden md:table-cell">{providerLabel(t.type)}</td>
                      <td className="px-4 py-2.5">
                        <Badge variant={t.status === "connected" ? "success" : t.status === "error" ? "destructive" : "secondary"}>
                          {t.status}
                        </Badge>
                      </td>
                      <td className="px-4 py-2.5 text-xs text-muted-foreground hidden lg:table-cell" title={t.last_connected_at ? new Date(t.last_connected_at).toLocaleString() : undefined}>
                        {timeAgo(t.last_connected_at)}
                      </td>
                      <td className="px-2 py-2.5 text-right">
                        <div className="flex items-center justify-end gap-0.5">
                          <Button variant="ghost" size="sm" className="h-7 w-7 p-0" onClick={() => handleTest(t)} disabled={testing === t.id} title="Test connection">
                            <Wifi className={`h-3.5 w-3.5 ${testing === t.id ? "animate-pulse" : ""}`} />
                          </Button>
                          <DropdownMenu
                            trigger={
                              <Button variant="ghost" size="sm" className="h-7 w-7 p-0" aria-label="Target actions">
                                <MoreHorizontal className="h-4 w-4" />
                              </Button>
                            }
                          >
                            <DropdownMenuItem
                              icon={<RefreshCw className={syncing === t.id ? "animate-spin" : ""} />}
                              disabled={syncing === t.id}
                              onClick={() => handleSync(t)}
                            >
                              Sync templates
                            </DropdownMenuItem>
                            <DropdownMenuItem icon={<Pencil />} onClick={() => handleEditClick(t)}>
                              Edit target
                            </DropdownMenuItem>
                            <DropdownMenuSeparator />
                            <DropdownMenuItem icon={<Trash2 />} destructive onClick={() => handleDeleteClick(t)}>
                              Delete target
                            </DropdownMenuItem>
                          </DropdownMenu>
                        </div>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          ) : (
            <div className="space-y-4">
              {targetsSorted.map((t) => (
                <Card key={t.id}>
                  <CardContent className="flex items-center justify-between p-4 gap-4">
                    <div className="flex items-center gap-4 min-w-0">
                      <ProviderIcon type={t.type} size={32} />
                      <div className="min-w-0">
                        <div className="flex items-center gap-2 flex-wrap">
                          <p className="font-medium truncate">{t.name}</p>
                          {t.is_default && (
                            <span title="Default target" className="inline-flex items-center gap-0.5 text-[10px] font-medium px-1.5 py-0.5 rounded bg-primary/15 text-primary">
                              <Star className="h-2.5 w-2.5" />
                              default
                            </span>
                          )}
                        </div>
                        <p className="text-sm text-muted-foreground truncate">
                          <span className="font-mono">{t.hostname}:{t.port}</span>
                          <span className="mx-1.5">·</span>
                          {providerLabel(t.type)}
                          <span className="mx-1.5">·</span>
                          {t.username}
                          <span className="inline-flex items-center gap-0.5 ml-2 text-green-500"><ShieldCheck className="h-3 w-3" /> encrypted</span>
                        </p>
                        <p className="text-[11px] text-muted-foreground/80 mt-0.5" title={t.last_connected_at ? new Date(t.last_connected_at).toLocaleString() : undefined}>
                          Last connected {timeAgo(t.last_connected_at)}
                        </p>
                      </div>
                    </div>
                    <div className="flex items-center gap-2 shrink-0">
                      <Badge variant={t.status === "connected" ? "success" : t.status === "error" ? "destructive" : "secondary"}>
                        {t.status}
                      </Badge>
                      <Button variant="outline" size="sm" onClick={() => handleTest(t)} disabled={testing === t.id}>
                        <Wifi className={`h-3.5 w-3.5 mr-1 ${testing === t.id ? "animate-pulse" : ""}`} />
                        {testing === t.id ? "Testing..." : "Test"}
                      </Button>
                      <DropdownMenu
                        trigger={
                          <Button variant="ghost" size="icon" aria-label="Target actions">
                            <MoreHorizontal className="h-4 w-4" />
                          </Button>
                        }
                      >
                        <DropdownMenuItem
                          icon={<RefreshCw className={syncing === t.id ? "animate-spin" : ""} />}
                          disabled={syncing === t.id}
                          onClick={() => handleSync(t)}
                        >
                          Sync templates
                        </DropdownMenuItem>
                        <DropdownMenuItem icon={<Pencil />} onClick={() => handleEditClick(t)}>
                          Edit target
                        </DropdownMenuItem>
                        <DropdownMenuSeparator />
                        <DropdownMenuItem icon={<Trash2 />} destructive onClick={() => handleDeleteClick(t)}>
                          Delete target
                        </DropdownMenuItem>
                      </DropdownMenu>
                    </div>
                  </CardContent>
                </Card>
              ))}
            </div>
          )}
        </>
      )}
      {/* Test/Sync Modal */}
      {actionModal && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50" onClick={() => !["testing", "syncing"].includes(actionModal.phase) && setActionModal(null)}>
          <div className="bg-card border rounded-lg shadow-xl max-w-sm w-full mx-4 p-6 space-y-4" onClick={(e) => e.stopPropagation()}>
            <div className="flex items-center gap-3">
              {(actionModal.phase === "testing" || actionModal.phase === "syncing") && (
                <div className="h-10 w-10 rounded-full bg-primary/10 flex items-center justify-center shrink-0">
                  <RefreshCw className="h-5 w-5 text-primary animate-spin" />
                </div>
              )}
              {actionModal.phase === "test-success" && (
                <div className="h-10 w-10 rounded-full bg-green-500/10 flex items-center justify-center shrink-0">
                  <Wifi className="h-5 w-5 text-green-500" />
                </div>
              )}
              {actionModal.phase === "test-failed" && (
                <div className="h-10 w-10 rounded-full bg-destructive/10 flex items-center justify-center shrink-0">
                  <X className="h-5 w-5 text-destructive" />
                </div>
              )}
              {actionModal.phase === "sync-done" && (
                <div className="h-10 w-10 rounded-full bg-green-500/10 flex items-center justify-center shrink-0">
                  <RefreshCw className="h-5 w-5 text-green-500" />
                </div>
              )}
              <div>
                <h3 className="text-lg font-semibold">{actionModal.targetName}</h3>
                <p className="text-sm text-muted-foreground">{actionModal.message}</p>
              </div>
            </div>

            {actionModal.phase === "sync-done" && actionModal.templatesFound !== undefined && (
              <div className="rounded-md border bg-green-500/5 border-green-500/30 p-3 text-center">
                <p className="text-2xl font-bold text-green-500">{actionModal.templatesFound}</p>
                <p className="text-xs text-muted-foreground">template{actionModal.templatesFound !== 1 ? "s" : ""} found</p>
              </div>
            )}

            {actionModal.phase === "test-failed" && (
              <div className="rounded-md border border-border bg-muted/30 p-3 space-y-2">
                <div className="flex items-center gap-1.5">
                  <Info className="h-3.5 w-3.5 text-muted-foreground" />
                  <p className="text-xs font-semibold">Troubleshooting</p>
                </div>
                <ul className="text-xs text-muted-foreground space-y-1 list-disc pl-4">
                  {actionModal.targetType === "vcenter" && (
                    <>
                      <li>Confirm the user can sign in to vSphere directly at <span className="font-mono">https://{"<host>"}/ui</span></li>
                      <li>Username should be <span className="font-mono">user@vsphere.local</span> or your AD UPN</li>
                      <li>Port 443 must be reachable from the Forgemill container</li>
                      <li>If using a self-signed cert, untick "Validate SSL/TLS certificates"</li>
                      <li>Required role: see the <span className="font-mono">?</span> next to Username on the create form</li>
                    </>
                  )}
                  {actionModal.targetType === "esxi" && (
                    <>
                      <li>Confirm the user can sign in to the ESXi host UI at <span className="font-mono">https://{"<host>"}/ui</span></li>
                      <li>If lockdown mode is enabled, only Exception users can connect via API</li>
                      <li>Port 443 must be reachable from the Forgemill container</li>
                      <li>If using a self-signed cert, untick "Validate SSL/TLS certificates"</li>
                      <li>Default username is normally <span className="font-mono">root</span></li>
                    </>
                  )}
                  {actionModal.targetType === "proxmox" && (
                    <>
                      <li>Default API port is <span className="font-mono">8006</span> — Forgemill normalises 443/0 → 8006 automatically</li>
                      <li>Username format: <span className="font-mono">user@pam</span> or <span className="font-mono">user@pve</span></li>
                      <li>If using API tokens, paste the full token: <span className="font-mono">user@pve!tokenid=secret</span></li>
                      <li>If using a self-signed cert, untick "Validate SSL/TLS certificates"</li>
                      <li>Required perms: see the <span className="font-mono">?</span> next to Username on the create form</li>
                    </>
                  )}
                  {!actionModal.targetType && (
                    <>
                      <li>Verify hostname / port match the hypervisor's web UI</li>
                      <li>If using a self-signed cert, untick "Validate SSL/TLS certificates"</li>
                      <li>Confirm the username/password are correct by signing in to the hypervisor directly</li>
                    </>
                  )}
                </ul>
              </div>
            )}

            <div className="flex justify-end gap-2 pt-2">
              {actionModal.phase === "test-success" && (
                <Button size="sm" onClick={() => runSync(actionModal.targetId, actionModal.targetName, actionModal.targetType)}>
                  <RefreshCw className="h-4 w-4 mr-1.5" />
                  Sync Templates
                </Button>
              )}
              {actionModal.phase === "test-failed" && (
                <Button size="sm" variant="outline" onClick={() => runTest(actionModal.targetId, actionModal.targetName, actionModal.targetType)}>
                  <RefreshCw className="h-4 w-4 mr-1.5" />
                  Retry
                </Button>
              )}
              {!["testing", "syncing"].includes(actionModal.phase) && (
                <Button variant="outline" size="sm" onClick={() => setActionModal(null)}>
                  Close
                </Button>
              )}
            </div>
          </div>
        </div>
      )}

      {/* Delete Confirmation Modal */}
      {deleteModal && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50" onClick={() => !deleting && setDeleteModal(null)}>
          <div className="bg-card border rounded-lg shadow-xl max-w-md w-full mx-4 p-6 space-y-4" onClick={(e) => e.stopPropagation()}>
            <div className="flex items-center gap-3">
              <div className="h-10 w-10 rounded-full bg-destructive/10 flex items-center justify-center shrink-0">
                <AlertTriangle className="h-5 w-5 text-destructive" />
              </div>
              <div>
                <h3 className="text-lg font-semibold">Delete Target</h3>
                <p className="text-sm text-muted-foreground">
                  This will permanently delete <strong>{deleteModal.target.name}</strong> and all associated data.
                </p>
              </div>
            </div>

            {/* Impact Summary */}
            {(deleteModal.preview.templates > 0 || deleteModal.preview.vms > 0 || deleteModal.preview.deployments > 0 || deleteModal.preview.builds > 0 || deleteModal.preview.executions > 0) ? (
              <div className="rounded-md border border-destructive/30 bg-destructive/5 p-4 space-y-2">
                <p className="text-sm font-medium text-destructive">The following will be removed:</p>
                <div className="grid grid-cols-2 gap-2 text-sm">
                  {deleteModal.preview.templates > 0 && (
                    <div className="flex items-center gap-2 text-muted-foreground">
                      <Box className="h-4 w-4" />
                      <span>{deleteModal.preview.templates} template{deleteModal.preview.templates !== 1 ? "s" : ""}</span>
                    </div>
                  )}
                  {deleteModal.preview.vms > 0 && (
                    <div className="flex items-center gap-2 text-muted-foreground">
                      <Monitor className="h-4 w-4" />
                      <span>{deleteModal.preview.vms} VM{deleteModal.preview.vms !== 1 ? "s" : ""}</span>
                    </div>
                  )}
                  {deleteModal.preview.deployments > 0 && (
                    <div className="flex items-center gap-2 text-muted-foreground">
                      <Rocket className="h-4 w-4" />
                      <span>{deleteModal.preview.deployments} deployment{deleteModal.preview.deployments !== 1 ? "s" : ""}</span>
                    </div>
                  )}
                  {deleteModal.preview.builds > 0 && (
                    <div className="flex items-center gap-2 text-muted-foreground">
                      <Wrench className="h-4 w-4" />
                      <span>{deleteModal.preview.builds} build{deleteModal.preview.builds !== 1 ? "s" : ""}</span>
                    </div>
                  )}
                  {deleteModal.preview.executions > 0 && (
                    <div className="flex items-center gap-2 text-muted-foreground">
                      <Terminal className="h-4 w-4" />
                      <span>{deleteModal.preview.executions} execution{deleteModal.preview.executions !== 1 ? "s" : ""}</span>
                    </div>
                  )}
                </div>
              </div>
            ) : (
              <div className="rounded-md border bg-muted/50 p-4">
                <p className="text-sm text-muted-foreground">No associated data to remove. This target is clean.</p>
              </div>
            )}

            <div className="rounded-md border border-yellow-500/30 bg-yellow-500/5 px-3 py-2 flex items-start gap-2">
              <Info className="h-4 w-4 text-yellow-500 shrink-0 mt-0.5" />
              <p className="text-xs text-yellow-600 dark:text-yellow-400">
                This action cannot be undone. VMs on the hypervisor will not be affected — only Forgemill's records are removed.
              </p>
            </div>

            <div className="flex justify-end gap-3 pt-2">
              <Button variant="outline" onClick={() => setDeleteModal(null)} disabled={deleting}>
                Cancel
              </Button>
              <Button variant="destructive" onClick={handleDeleteConfirm} disabled={deleting}>
                {deleting ? (
                  <>
                    <RefreshCw className="h-4 w-4 mr-2 animate-spin" />
                    Deleting...
                  </>
                ) : (
                  <>
                    <Trash2 className="h-4 w-4 mr-2" />
                    Delete Target
                  </>
                )}
              </Button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
