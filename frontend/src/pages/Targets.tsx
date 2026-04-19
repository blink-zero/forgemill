import { useEffect, useState, useRef } from "react";
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
import { getErrorMessage } from "@/lib/utils";
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
    message: string;
    templatesFound?: number;
  } | null>(null);
  const [editTarget, setEditTarget] = useState<Target | null>(null);
  const [openMenu, setOpenMenu] = useState<number | null>(null);
  const menuRef = useRef<HTMLDivElement>(null);

  // Close dropdown on outside click
  useEffect(() => {
    if (!openMenu) return;
    const handleClick = (e: MouseEvent) => {
      // Only close if clicking outside the menu
      if (menuRef.current && !menuRef.current.contains(e.target as Node)) {
        setOpenMenu(null);
      }
    };
    // Use setTimeout to avoid closing immediately on the same click that opened the menu
    const timer = setTimeout(() => {
      document.addEventListener("click", handleClick);
    }, 0);
    return () => {
      clearTimeout(timer);
      document.removeEventListener("click", handleClick);
    };
  }, [openMenu]);

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

  const runTest = async (id: number, name: string) => {
    setActionModal({ targetName: name, targetId: id, phase: "testing", message: "Testing connection..." });
    setTesting(id);
    try {
      const res = await targetApi.test(id);
      loadTargets();
      setActionModal({ targetName: name, targetId: id, phase: "test-success", message: res.data.message || "Connection successful" });
    } catch {
      setActionModal({ targetName: name, targetId: id, phase: "test-failed", message: "Connection test failed. Check credentials and hostname." });
    } finally {
      setTesting(null);
    }
  };

  const handleTest = (target: Target) => runTest(target.id, target.name);

  const runSync = async (id: number, name: string) => {
    setActionModal({ targetName: name, targetId: id, phase: "syncing", message: "Syncing templates..." });
    setSyncing(id);
    try {
      const res = await targetApi.sync(id);
      loadTargets();
      setActionModal({ targetName: name, targetId: id, phase: "sync-done", message: `Sync complete`, templatesFound: res.data.templates_found });
    } catch {
      setActionModal({ targetName: name, targetId: id, phase: "test-failed", message: "Sync failed. Check target connection." });
    } finally {
      setSyncing(null);
    }
  };

  const handleSync = (target: Target) => runSync(target.id, target.name);

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
      ) : viewMode === "table" ? (
        <div className="rounded-md border">
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b bg-muted/50">
                <SortableTh label="Name" field="name" currentField={tgtSortField} currentDir={tgtSortDir} onSort={tgtToggleSort} />
                <SortableTh label="Hostname" field="hostname" currentField={tgtSortField} currentDir={tgtSortDir} onSort={tgtToggleSort} className="hidden sm:table-cell" />
                <SortableTh label="Type" field="type" currentField={tgtSortField} currentDir={tgtSortDir} onSort={tgtToggleSort} className="hidden md:table-cell" />
                <th className="text-left px-4 py-2 font-medium">Status</th>
                <th className="text-right px-4 py-2 font-medium">Actions</th>
              </tr>
            </thead>
            <tbody>
              {targetsSorted.map((t) => (
                <tr key={t.id} className="border-b last:border-0 hover:bg-muted/30 transition-colors">
                  <td className="px-4 py-2.5">
                    <div className="flex items-center gap-2">
                      <ProviderIcon type={t.type} size={18} />
                      <span className="font-medium">{t.name}</span>
                    </div>
                  </td>
                  <td className="px-4 py-2.5 text-muted-foreground font-mono text-xs hidden sm:table-cell">{t.hostname}:{t.port}</td>
                  <td className="px-4 py-2.5 text-muted-foreground hidden md:table-cell">{providerLabel(t.type)}</td>
                  <td className="px-4 py-2.5">
                    <Badge variant={t.status === "connected" ? "success" : t.status === "error" ? "destructive" : "secondary"}>
                      {t.status}
                    </Badge>
                  </td>
                  <td className="px-4 py-2.5 text-right">
                    <div className="flex items-center justify-end gap-1">
                      <Button variant="ghost" size="sm" onClick={() => handleTest(t)} disabled={testing === t.id} title="Test">
                        <Wifi className="h-3.5 w-3.5" />
                      </Button>
                      <Button variant="ghost" size="sm" onClick={() => handleSync(t)} disabled={syncing === t.id} title="Sync">
                        <RefreshCw className={`h-3.5 w-3.5 ${syncing === t.id ? "animate-spin" : ""}`} />
                      </Button>
                      <Button variant="ghost" size="sm" onClick={() => handleEditClick(t)} title="Edit">
                        <Pencil className="h-3.5 w-3.5" />
                      </Button>
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
        ) : (
        <div className="space-y-4">
          {targets.map((t) => (
            <Card key={t.id}>
              <CardContent className="flex items-center justify-between p-4">
                <div className="flex items-center gap-4">
                  <ProviderIcon type={t.type} size={32} />
                  <div>
                    <p className="font-medium">{t.name}</p>
                    <p className="text-sm text-muted-foreground">
                      {t.hostname}:{t.port} &middot; {providerLabel(t.type)} &middot; {t.username}
                      <span className="inline-flex items-center gap-0.5 ml-2 text-green-500"><ShieldCheck className="h-3 w-3" /> encrypted</span>
                    </p>
                  </div>
                </div>
                <div className="flex items-center gap-2">
                  <Badge variant={t.status === "connected" ? "success" : t.status === "error" ? "destructive" : "secondary"}>
                    {t.status}
                  </Badge>
                  <Button variant="outline" size="sm" onClick={() => handleTest(t)} disabled={testing === t.id}>
                    <Wifi className="h-3.5 w-3.5 mr-1" />
                    {testing === t.id ? "Testing..." : "Test"}
                  </Button>
                  <Button variant="outline" size="sm" onClick={() => handleSync(t)} disabled={syncing === t.id}>
                    <RefreshCw className={`h-3.5 w-3.5 mr-1 ${syncing === t.id ? "animate-spin" : ""}`} />
                    {syncing === t.id ? "Syncing..." : "Sync"}
                  </Button>
                  <div className="relative" ref={openMenu === t.id ? menuRef : undefined}>
                    <Button variant="ghost" size="icon" onClick={() => setOpenMenu(openMenu === t.id ? null : t.id)} aria-label="More actions">
                      <Pencil className="h-4 w-4" />
                    </Button>
                    {openMenu === t.id && (
                      <div className="absolute right-0 top-full mt-1 z-20 bg-popover border rounded-md shadow-lg py-1 min-w-[140px]">
                        <button
                          className="w-full flex items-center gap-2 px-3 py-2 text-sm hover:bg-accent transition-colors"
                          onClick={() => { setOpenMenu(null); handleEditClick(t); }}
                        >
                          <Pencil className="h-4 w-4" />
                          Edit
                        </button>
                        <button
                          className="w-full flex items-center gap-2 px-3 py-2 text-sm text-destructive hover:bg-accent transition-colors"
                          onClick={() => { setOpenMenu(null); handleDeleteClick(t); }}
                        >
                          <Trash2 className="h-4 w-4" />
                          Delete
                        </button>
                      </div>
                    )}
                  </div>
                </div>
              </CardContent>
            </Card>
          ))}
        </div>
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

            <div className="flex justify-end gap-2 pt-2">
              {actionModal.phase === "test-success" && (
                <Button size="sm" onClick={() => runSync(actionModal.targetId, actionModal.targetName)}>
                  <RefreshCw className="h-4 w-4 mr-1.5" />
                  Sync Templates
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
