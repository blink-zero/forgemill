import React, { useEffect, useRef, useState } from "react";
import { useSearchParams } from "react-router-dom";
import { actions as actionsApi, ai as aiApi } from "@/api/client";
import { ActionReviewPanel } from "@/pages/actions/ActionReviewPanel";
import { ActionDraftPanel } from "@/pages/actions/ActionDraftPanel";
import { useAIJob } from "@/hooks/useAIJob";
import type { ActionVersion } from "@/api/client";
import type { Action, ActionParameter, ActionExportEntry, ActionExportFile, ActionReview, AIStatus } from "@/types";
import { Card, CardHeader, CardTitle, CardContent } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Badge } from "@/components/ui/badge";
import { Plus, Trash2, Edit2, Package, Terminal, Shield, Activity, Puzzle, Search, X, Code2, ChevronDown, ChevronUp, Copy, Check, Loader2, ArrowUp, ArrowDown, Settings2, History, RotateCcw, Download, Upload, ShieldCheck, Sparkles, FileEdit, Rocket, AlertTriangle } from "lucide-react";
import { Select } from "@/components/ui/select";
import { Pagination } from "@/components/ui/pagination";
import { useAuth } from "@/hooks/useAuth";
import { useToast } from "@/components/ui/toast";
import { useConfirm } from "@/components/ui/confirm-dialog";
import { getErrorMessage } from "@/lib/utils";
import { ViewToggle } from "@/components/ui/view-toggle";
import { usePreference } from "@/context/PreferencesContext";
import { SortableTh } from "@/components/ui/sortable-th";
import { useTableSort } from "@/hooks/useTableSort";
import { PageHeader } from "@/components/ui/page-header";
import { usePageSize } from "@/hooks/usePageSize";

const categoryIcons: Record<string, typeof Package> = {
  packages: Package,
  scripts: Terminal,
  security: Shield,
  monitoring: Activity,
  custom: Puzzle,
};

const categoryLabels: Record<string, string> = {
  packages: "Packages",
  scripts: "Scripts",
  security: "Security",
  monitoring: "Monitoring",
  custom: "Custom",
};

const categoryColors: Record<string, string> = {
  packages: "bg-blue-100 text-blue-700 dark:bg-blue-900 dark:text-blue-300",
  scripts: "bg-purple-100 text-purple-700 dark:bg-purple-900 dark:text-purple-300",
  security: "bg-red-100 text-red-700 dark:bg-red-900 dark:text-red-300",
  monitoring: "bg-green-100 text-green-700 dark:bg-green-900 dark:text-green-300",
  custom: "bg-gray-100 text-gray-700 dark:bg-gray-800 dark:text-gray-300",
};

function actionToExportEntry(action: Action): ActionExportEntry {
  return {
    name: action.name,
    description: action.description,
    category: action.category,
    script: action.script,
    script_type: action.script_type,
    platform: action.platform,
    parameters: action.parameters,
    tags: action.tags,
  };
}

function slugify(name: string): string {
  return name.trim().toLowerCase().replace(/[^a-z0-9]+/g, "-").replace(/^-+|-+$/g, "") || "action";
}

// Triggers a browser download of the given actions as a single JSON file.
// Export runs entirely client-side against data the page already has
// loaded — nothing new is fetched from the server just to download it.
function downloadActionsFile(entries: ActionExportEntry[], filename: string) {
  const file: ActionExportFile = {
    schema_version: 1,
    exported_at: new Date().toISOString(),
    source: "forgemill",
    actions: entries,
  };
  const blob = new Blob([JSON.stringify(file, null, 2)], { type: "application/json" });
  const url = URL.createObjectURL(blob);
  const link = document.createElement("a");
  link.href = url;
  link.download = filename;
  document.body.appendChild(link);
  link.click();
  link.remove();
  URL.revokeObjectURL(url);
}

export default function ActionsPage() {
  const { toast } = useToast();
  const { confirm: showConfirm } = useConfirm();
  const { user } = useAuth();
  const isAdmin = user?.role === "admin";
  const [actionList, setActionList] = useState<Action[]>([]);
  const [loading, setLoading] = useState(true);
  const [showForm, setShowForm] = useState(false);
  const [editingId, setEditingId] = useState<number | null>(null);
  const [form, setForm] = useState<{ name: string; description: string; category: Action["category"]; script: string; parameters: ActionParameter[]; tags: string[] }>({ name: "", description: "", category: "custom", script: "", parameters: [], tags: [] });
  const [configError, setConfigError] = useState("");
  const [search, setSearch] = useState("");
  const [categoryFilter, setCategoryFilter] = useState<string>("all");
  const [expandedId, setExpandedId] = useState<number | null>(null);
  const [copiedId, setCopiedId] = useState<number | null>(null);
  const [versionsOpenId, setVersionsOpenId] = useState<number | null>(null);
  const [versions, setVersions] = useState<ActionVersion[]>([]);
  const [versionsLoading, setVersionsLoading] = useState(false);
  const [viewingVersion, setViewingVersion] = useState<number | null>(null);
  const [importing, setImporting] = useState(false);
  const importInputRef = useRef<HTMLInputElement>(null);

  const fetchActions = async () => {
    try {
      const res = await actionsApi.list(true);
      setActionList(res.data || []);
    } catch (e: unknown) {
      toast(getErrorMessage(e, "Failed to load actions"), "error");
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => { fetchActions(); }, []);

  // Script check: deterministic lint always; the model's review when AI
  // assistance is on (status tells us which to expect).
  const [aiStatus, setAIStatus] = useState<AIStatus | null>(null);
  const [review, setReview] = useState<ActionReview | null>(null);
  const [checking, setChecking] = useState(false);
  const scriptRef = useRef<HTMLTextAreaElement | null>(null);
  useEffect(() => { aiApi.status().then((res) => setAIStatus(res.data)).catch(() => setAIStatus(null)); }, []);
  const aiOn = Boolean(aiStatus?.enabled && aiStatus?.configured);
  const [showDraft, setShowDraft] = useState(false);
  // The action being edited when it is a draft (Publish / Discard instead of Update).
  const [editingDraft, setEditingDraft] = useState<Action | null>(null);
  const [draftPrompt, setDraftPrompt] = useState("");
  const [statusFilter, setStatusFilter] = useState<"all" | "drafts">("all");
  const [searchParams, setSearchParams] = useSearchParams();
  const draftCount = actionList.filter((a) => a.status === "draft").length;


  // With AI on, the check runs as a background job (polled, refresh-safe);
  // lint alone is instant and stays a plain request.
  const reviewJob = useAIJob("ai-review-job");
  useEffect(() => {
    if (reviewJob.job?.status === "done" && reviewJob.job.review) { setReview(reviewJob.job.review); setChecking(false); }
    if (reviewJob.job?.status === "failed" || reviewJob.error) { if (reviewJob.error) toast(reviewJob.error, "error"); setChecking(false); }
  }, [reviewJob.job, reviewJob.error, toast]);
  useEffect(() => { if (reviewJob.running) setChecking(true); }, [reviewJob.running]);

  const runCheck = async () => {
    if (!form.script.trim()) return;
    setChecking(true);
    let actionId = editingId;
    if (aiOn && !actionId) {
      // A model review needs somewhere to land: save the unsaved script as a
      // draft first (never runnable), so the result survives leaving the page.
      try {
        const res = await actionsApi.create({ ...form, name: form.name || "Untitled draft", status: "draft", parameters: form.parameters.length > 0 ? form.parameters : undefined });
        actionId = res.data.id;
        setEditingId(actionId);
        setEditingDraft(res.data);
        if (!form.name) setForm({ ...form, name: "Untitled draft" });
        fetchActions();
      } catch (e: unknown) {
        toast(getErrorMessage(e, "Could not save the draft before checking"), "error");
        setChecking(false);
        return;
      }
    }
    const input = { name: form.name, description: form.description, script: form.script, parameters: form.parameters, platform: "linux", ...(actionId ? { action_id: actionId } : {}) };
    if (aiOn) {
      setReview(null);
      await reviewJob.start(() => aiApi.startReviewJob(input));
      return;
    }
    try {
      const res = await aiApi.lintAction(input);
      setReview(res.data);
    } catch (e: unknown) {
      toast(getErrorMessage(e, "Check failed"), "error");
    } finally {
      setChecking(false);
    }
  };

  // A stored review is stale once the script changes; we don't hash in the
  // browser, so track "edited since the review arrived" instead.
  const [scriptHashHint, setScriptHashHint] = useState<string | undefined>(undefined);
  useEffect(() => { setScriptHashHint(review?.script_hash); }, [review]);

  const jumpToLine = (line: number) => {
    const ta = scriptRef.current;
    if (!ta) return;
    const lines = form.script.split("\n");
    const start = lines.slice(0, line - 1).reduce((n, l) => n + l.length + 1, 0);
    const end = start + (lines[line - 1]?.length ?? 0);
    ta.focus();
    ta.setSelectionRange(start, end);
    ta.scrollTop = Math.max(0, (line - 3) * 20); // approximate line height
  };

  const addSuggestedParameters = (params: ActionParameter[]) => {
    const have = new Set(form.parameters.map((p) => p.name));
    const add = params.filter((p) => !have.has(p.name)).map((p) => ({ name: p.name, label: p.label || p.name, type: p.type, required: Boolean(p.required), default: p.default || "", placeholder: p.placeholder || "", options: p.options ?? null, description: p.description || "" }));
    if (add.length === 0) return;
    setForm({ ...form, parameters: [...form.parameters, ...add] });
    toast(`${add.length} parameter${add.length === 1 ? "" : "s"} added`);
  };

  const validateScript = (val: string): boolean => {
    if (!val.trim()) {
      setConfigError("Script is empty");
      return false;
    }
    if (val.length > 65536) {
      setConfigError("Script exceeds maximum size (64KB)");
      return false;
    }
    setConfigError("");
    return true;
  };

  const handleSave = async () => {
    if (!form.name || !form.script) return;
    if (!validateScript(form.script)) return;
    const payload = {
      ...form,
      parameters: form.parameters.length > 0 ? form.parameters : undefined,
    };
    try {
      if (editingId) {
        await actionsApi.update(editingId, payload);
      } else {
        await actionsApi.create(payload);
      }
      if (editingDraft) {
        toast("Draft saved");
        fetchActions();
        return;
      }
      setShowForm(false);
      setEditingId(null);
      setForm({ name: "", description: "", category: "custom" as Action["category"], script: "", parameters: [], tags: [] });
      fetchActions();
    } catch (err: unknown) {
      const msg = (err as { response?: { data?: { error?: string } } })?.response?.data?.error || "Failed to save action";
      toast(msg, "error");
    }
  };

  const handleEdit = (action: Action) => {
    setForm({
      name: action.name,
      description: action.description,
      category: action.category,
      script: action.script || "",
      parameters: action.parameters || [],
      tags: action.tags || [],
    });
    setEditingId(action.id);
    setEditingDraft(action.status === "draft" ? action : null);
    setReview(action.review ?? null);
    setShowDraft(false);
    setDraftPrompt(action.draft_meta?.prompt || "");
    setShowForm(true);
    setConfigError("");
    window.scrollTo({ top: 0, behavior: "smooth" });
  };

  // Deep link from the bell: /actions?open=<id> opens that action in the editor.
  useEffect(() => {
    const open = searchParams.get("open");
    if (!open || actionList.length === 0) return;
    const target = actionList.find((a) => a.id === Number(open));
    const next = new URLSearchParams(searchParams);
    next.delete("open");
    setSearchParams(next, { replace: true });
    if (target) handleEdit(target);
    else toast("That action no longer exists", "error");
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [actionList, searchParams]);

  // The AI job saved a draft: load it into the editor (fresh from the server).
  const onDraftSaved = async (actionId: number) => {
    try {
      const res = await actionsApi.list(true);
      setActionList(res.data || []);
      const saved = (res.data || []).find((a) => a.id === actionId);
      if (saved) handleEdit(saved);
    } catch (e: unknown) {
      toast(getErrorMessage(e, "Draft saved, but it could not be loaded"), "error");
    }
  };

  const handlePublish = async () => {
    if (!editingId) return;
    // Save any edits first, then publish.
    if (!validateScript(form.script)) return;
    try {
      await actionsApi.update(editingId, { ...form, parameters: form.parameters.length > 0 ? form.parameters : undefined });
      await actionsApi.publish(editingId);
      toast(`"${form.name}" published — it can now be run`);
      setShowForm(false);
      setEditingId(null);
      setEditingDraft(null);
      setReview(null);
      fetchActions();
    } catch (err: unknown) {
      toast((err as { response?: { data?: { error?: string } } })?.response?.data?.error || "Failed to publish", "error");
    }
  };

  const handleDiscardDraft = async () => {
    if (!editingId) return;
    const ok = await showConfirm({ title: "Discard draft", message: `Delete the draft "${form.name}"? This cannot be undone.`, confirmLabel: "Discard", variant: "destructive" });
    if (!ok) return;
    try {
      await actionsApi.delete(editingId);
      setShowForm(false);
      setEditingId(null);
      setEditingDraft(null);
      setReview(null);
      fetchActions();
    } catch (err: unknown) {
      toast(getErrorMessage(err, "Failed to discard draft"), "error");
    }
  };

  const handleDelete = async (id: number) => {
    const ok = await showConfirm({ title: "Delete Action", message: "Delete this action? This cannot be undone.", confirmLabel: "Delete", variant: "destructive" });
    if (!ok) return;
    try {
      await actionsApi.delete(id);
      fetchActions();
    } catch (e) {
      toast(getErrorMessage(e, "Failed to delete action"), "error");
    }
  };

  const loadVersions = async (actionId: number) => {
    setVersionsLoading(true);
    try {
      const res = await actionsApi.listVersions(actionId);
      setVersions(res.data || []);
    } catch (e) {
      toast(getErrorMessage(e, "Failed to load version history"), "error");
      setVersions([]);
    } finally {
      setVersionsLoading(false);
    }
  };

  const toggleVersions = (actionId: number) => {
    if (versionsOpenId === actionId) {
      setVersionsOpenId(null);
      setViewingVersion(null);
      return;
    }
    setVersionsOpenId(actionId);
    setViewingVersion(null);
    loadVersions(actionId);
  };

  const handleRollback = async (actionId: number, version: number) => {
    const ok = await showConfirm({
      title: "Roll Back Action",
      message: `Restore this action to version ${version}? This creates a new version with that content — nothing already recorded is deleted or overwritten.`,
      confirmLabel: "Roll Back",
    });
    if (!ok) return;
    try {
      await actionsApi.rollback(actionId, version);
      toast(`Rolled back to version ${version}`);
      fetchActions();
      loadVersions(actionId);
      setViewingVersion(null);
    } catch (e) {
      toast(getErrorMessage(e, "Failed to roll back"), "error");
    }
  };

  const handleExportOne = (action: Action) => {
    downloadActionsFile([actionToExportEntry(action)], `forgemill-action-${slugify(action.name)}.json`);
  };

  const handleExportAll = () => {
    // Drafts are not exported: they haven't been published.
    downloadActionsFile(filtered.filter((a) => a.status !== "draft").map(actionToExportEntry), `forgemill-actions-${new Date().toISOString().slice(0, 10)}.json`);
  };

  const handleImportFile = async (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0];
    e.target.value = ""; // reset so re-selecting the same file fires onChange again

    if (!file) return;

    let parsed: unknown;
    try {
      parsed = JSON.parse(await file.text());
    } catch {
      toast("That file isn't valid JSON", "error");
      return;
    }

    const entries = (parsed as Partial<ActionExportFile> | null)?.actions;
    if (!Array.isArray(entries) || entries.length === 0) {
      toast('No actions found — expected a file exported from this page (with an "actions" array)', "error");
      return;
    }

    setImporting(true);
    try {
      const res = await actionsApi.import(entries);
      const { created, failed, results } = res.data;
      const firstError = results.find((r) => r.status === "failed");
      if (created > 0 && failed === 0) {
        toast(`Imported ${created} action${created === 1 ? "" : "s"}`);
      } else if (created > 0 && failed > 0) {
        toast(`Imported ${created}, ${failed} failed${firstError ? ` — ${firstError.name || "unnamed"}: ${firstError.error}` : ""}`, "error");
      } else {
        toast(`Import failed${firstError ? `: ${firstError.error}` : ""}`, "error");
      }
      if (created > 0) fetchActions();
    } catch (err: unknown) {
      toast(getErrorMessage(err, "Failed to import actions"), "error");
    } finally {
      setImporting(false);
    }
  };

  const filtered = actionList.filter((a) => {
    const q = search.toLowerCase();
    const matchSearch = !search ||
      a.name.toLowerCase().includes(q) ||
      (a.description || "").toLowerCase().includes(q) ||
      (a.tags || []).some((t) => t.toLowerCase().includes(q));
    const matchCategory = categoryFilter === "all" || a.category === categoryFilter;
    const matchStatus = statusFilter === "drafts" ? a.status === "draft" : true;
    return matchSearch && matchCategory && matchStatus;
  });
  const viewMode = usePreference("view_mode", "cards");
  const { sorted: actionsSorted, sortField: actSortField, sortDir: actSortDir, toggleSort: actToggleSort } = useTableSort(filtered, "name");

  const categories = Array.from(new Set(actionList.map((a) => a.category)));

  // Paginate the sorted+filtered list so both views share the same slice
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = usePageSize("actions", 25);
  const paginated = actionsSorted.slice((page - 1) * pageSize, page * pageSize);

  // Reset page on filter, sort, or page-size change
  useEffect(() => { setPage(1); }, [search, categoryFilter, pageSize, actSortField, actSortDir]);

  const grouped = paginated.reduce<Record<string, Action[]>>((acc, a) => {
    (acc[a.category] = acc[a.category] || []).push(a);
    return acc;
  }, {});

  if (loading) {
    return <div className="flex items-center justify-center h-64"><Loader2 className="h-8 w-8 animate-spin text-primary" /></div>;
  }

  const renderVersionsPanel = (action: Action) => (
    <div className="border rounded-md p-3 bg-muted/30 space-y-2">
      {versionsLoading ? (
        <div className="flex items-center gap-2 text-xs text-muted-foreground">
          <Loader2 className="h-3.5 w-3.5 animate-spin" /> Loading version history…
        </div>
      ) : versions.length === 0 ? (
        <p className="text-xs text-muted-foreground">No version history yet — this action hasn't been edited since it was created.</p>
      ) : (
        <ul className="space-y-1.5">
          {versions.map((v) => {
            const isCurrent = v.version === action.version;
            return (
              <li key={v.version} className="text-xs border rounded-md p-2 bg-background">
                <div className="flex items-center justify-between gap-2">
                  <div className="flex items-center gap-1.5">
                    <span className="font-medium">Version {v.version}</span>
                    {isCurrent && <Badge variant="success" className="text-[10px]">Current</Badge>}
                    <span className="text-muted-foreground">{v.created_at}{v.changed_by ? ` · User #${v.changed_by}` : ""}</span>
                  </div>
                  <div className="flex items-center gap-1">
                    <Button variant="ghost" size="sm" className="h-6 px-2" onClick={() => setViewingVersion(viewingVersion === v.version ? null : v.version)}>
                      {viewingVersion === v.version ? "Hide" : "View"}
                    </Button>
                    {!isCurrent && isAdmin && (
                      <Button variant="outline" size="sm" className="h-6 px-2 gap-1" onClick={() => handleRollback(action.id, v.version)}>
                        <RotateCcw className="h-3 w-3" /> Roll back to this
                      </Button>
                    )}
                  </div>
                </div>
                {viewingVersion === v.version && (
                  <pre className="text-xs bg-gray-950 text-success p-2 rounded-md overflow-x-auto max-h-48 whitespace-pre-wrap mt-2">{v.script}</pre>
                )}
              </li>
            );
          })}
        </ul>
      )}
    </div>
  );

  return (
    <div className="space-y-6">
      <PageHeader
        title={
          <span className="flex items-center gap-2">
            Actions
            {actionList.length > 0 && <Badge variant="outline">{actionList.length}</Badge>}
          </span>
        }
        description="Reusable post-deploy automation — install packages, configure services, run scripts."
        actions={
          <>
            {actionList.length > 0 && (
              <div className="relative w-full sm:w-64">
                <Search className="absolute left-3 top-1/2 -translate-y-1/2 h-4 w-4 text-muted-foreground" />
                <Input
                  placeholder="Search actions..."
                  value={search}
                  onChange={(e) => setSearch(e.target.value)}
                  className="pl-9"
                />
                {search && (
                  <button className="absolute right-3 top-1/2 -translate-y-1/2" onClick={() => setSearch("")}>
                    <X className="h-4 w-4 text-muted-foreground hover:text-foreground" />
                  </button>
                )}
              </div>
            )}
            <ViewToggle />
            {actionList.length > 0 && (
              <Button variant="outline" onClick={handleExportAll} title="Download the actions currently shown as a JSON file">
                <Download className="h-4 w-4 mr-2" /> Export
              </Button>
            )}
            {isAdmin && (
              <>
                <input
                  ref={importInputRef}
                  type="file"
                  accept="application/json"
                  className="hidden"
                  onChange={handleImportFile}
                />
                <Button
                  variant="outline"
                  onClick={() => importInputRef.current?.click()}
                  disabled={importing}
                  title="Import actions from a JSON file exported from this page"
                >
                  {importing ? <Loader2 className="h-4 w-4 mr-2 animate-spin" /> : <Upload className="h-4 w-4 mr-2" />}
                  Import
                </Button>
                <Button onClick={() => { setShowForm(!showForm); setEditingId(null); setEditingDraft(null); setReview(null); setForm({ name: "", description: "", category: "custom" as Action["category"], script: "", parameters: [], tags: [] }); setConfigError(""); }}>
                  <Plus className="h-4 w-4 mr-2" /> Create Action
                </Button>
              </>
            )}
          </>
        }
      />

      {/* Category Filter */}
      {actionList.length > 0 && (
        <div className="flex flex-wrap gap-3">
          <div className="flex gap-1 flex-wrap">
            <Button
              size="sm"
              variant={categoryFilter === "all" ? "default" : "outline"}
              onClick={() => setCategoryFilter("all")}
            >
              All
            </Button>
            {categories.map((cat) => {
              const Icon = categoryIcons[cat] || Puzzle;
              return (
                <Button
                  key={cat}
                  size="sm"
                  variant={categoryFilter === cat ? "default" : "outline"}
                  onClick={() => setCategoryFilter(cat)}
                  className="gap-1"
                >
                  <Icon className="h-3 w-3" />
                  {categoryLabels[cat] || cat}
                  <Badge variant="secondary" className="ml-1 h-5 text-xs">{actionList.filter((a) => a.category === cat).length}</Badge>
                </Button>
              );
            })}
            {draftCount > 0 && (
              <Button size="sm" variant={statusFilter === "drafts" ? "default" : "outline"} onClick={() => setStatusFilter(statusFilter === "drafts" ? "all" : "drafts")} className="gap-1" title="Saved but not published — not runnable">
                <FileEdit className="h-3 w-3" /> Drafts
                <Badge variant="warning" className="ml-1 h-5 text-xs">{draftCount}</Badge>
              </Button>
            )}
          </div>
        </div>
      )}

      {/* Create/Edit Form */}
      {showForm && (
        <Card className="border-primary/30">
          <CardHeader>
            <div className="flex items-center justify-between gap-3 flex-wrap">
              <CardTitle className="flex items-center gap-2">
                {editingDraft ? "Review draft" : editingId ? "Edit Action" : "Create Action"}
                {editingDraft && <Badge variant="warning">{editingDraft.source === "ai" ? "AI draft" : "draft"} · not runnable</Badge>}
              </CardTitle>
              {aiOn && !showDraft && (
                <Button type="button" size="sm" variant="outline" className="gap-1.5" onClick={() => setShowDraft(true)} title={editingDraft ? "Regenerate this draft from a new description" : `Draft an action from a description with ${aiStatus?.model || "the configured model"}`}>
                  <Sparkles className="h-3.5 w-3.5" /> {editingDraft ? "Regenerate with AI" : "Draft with AI"}
                </Button>
              )}
            </div>
            {editingDraft?.draft_meta && (editingDraft.draft_meta.notes?.length || editingDraft.draft_meta.warnings?.length) ? (
              <div className="mt-2 space-y-1 text-xs">
                {(editingDraft.draft_meta.warnings || []).map((w, i) => <p key={`w${i}`} className="text-warning flex items-start gap-1.5"><AlertTriangle className="h-3.5 w-3.5 shrink-0 mt-px" /> {w}</p>)}
                {(editingDraft.draft_meta.notes || []).map((n, i) => <p key={`n${i}`} className="text-muted-foreground flex items-start gap-1.5"><Sparkles className="h-3.5 w-3.5 shrink-0 mt-px" /> {n}</p>)}
                {editingDraft.draft_meta.model && <p className="text-muted-foreground/80">Drafted by {editingDraft.draft_meta.model}{editingDraft.draft_meta.prompt ? ` from: "${editingDraft.draft_meta.prompt}"` : ""}</p>}
              </div>
            ) : null}
          </CardHeader>
          <CardContent className="space-y-4">
            {aiOn && showDraft && (
              <ActionDraftPanel modelName={aiStatus?.model} existingScript={editingDraft ? "" : form.script} existingParameters={editingDraft ? [] : form.parameters} draftActionId={editingDraft?.id} initialPrompt={draftPrompt} onSaved={onDraftSaved} onClose={() => setShowDraft(false)} />
            )}
            <div className="grid gap-4 sm:grid-cols-2">
              <div className="space-y-2 sm:col-span-2">
                <Label>Name *</Label>
                <Input value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} placeholder="Install Nginx" />
              </div>
              <div className="space-y-2 sm:col-span-2">
                <Label>Description</Label>
                <Input value={form.description} onChange={(e) => setForm({ ...form, description: e.target.value })} placeholder="What this action does..." />
              </div>
              <div className="space-y-2">
                <Label>Category</Label>
                <Select
                  value={form.category}
                  onChange={(e) => setForm({ ...form, category: e.target.value as Action["category"] })}
                >
                  <option value="packages">Packages</option>
                  <option value="scripts">Scripts</option>
                  <option value="security">Security</option>
                  <option value="monitoring">Monitoring</option>
                  <option value="custom">Custom</option>
                </Select>
              </div>
              <div className="space-y-2">
                <Label>Tags (comma separated)</Label>
                <Input
                  value={form.tags.join(", ")}
                  onChange={(e) => setForm({ ...form, tags: e.target.value.split(",").map((s) => s.trim().toLowerCase()).filter(Boolean) })}
                  placeholder="docker, containers, packages"
                />
                <p className="text-xs text-muted-foreground">Searchable keywords, e.g. "docker", "database", "security".</p>
              </div>
              <div className="space-y-2 sm:col-span-2">
                <Label>Script *</Label>
                <textarea
                  ref={scriptRef}
                  value={form.script}
                  onChange={(e) => { setForm({ ...form, script: e.target.value }); if (e.target.value) validateScript(e.target.value); if (review) setScriptHashHint("edited"); }}
                  placeholder={"#!/bin/bash\nset -euo pipefail\n\napt-get update -y\napt-get install -y nginx\nsystemctl enable --now nginx"}
                  rows={10}
                  className="w-full rounded-md border border-input bg-gray-950 text-success px-3 py-2 text-sm shadow-xs placeholder:text-gray-600 focus-visible:outline-hidden focus-visible:ring-1 focus-visible:ring-ring font-mono resize-y"
                />
                {configError && <p className="text-xs text-destructive">{configError}</p>}
                <div className="flex items-center justify-between gap-3 flex-wrap">
                  <p className="text-xs text-muted-foreground">Bash script that runs with sudo privileges on the target VM. Max 64KB.</p>
                  <Button type="button" size="sm" variant="outline" className="gap-1.5" onClick={runCheck} disabled={checking || !form.script.trim()} title={aiOn ? `Automatic checks plus a review by ${aiStatus?.model || "the configured model"}` : "Automatic checks: destructive commands, missing set -e, interactive package installs, secrets in the script, undeclared parameters, distro assumptions. Turn on AI assistance in Settings → AI for a model review too."}>
                    {checking ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : aiOn ? <Sparkles className="h-3.5 w-3.5" /> : <ShieldCheck className="h-3.5 w-3.5" />}
                    {checking ? `Checking…${reviewJob.running && reviewJob.elapsed ? ` ${reviewJob.elapsed}s` : ""}` : aiOn ? "Check with AI" : "Check script"}
                  </Button>
                </div>
                {review && review.script_hash && review.script_hash !== scriptHashHint && editingId && (
                  <p className="text-xs text-warning flex items-center gap-1.5"><AlertTriangle className="h-3.5 w-3.5" /> This check was made before the last edit — run Check again for the current script.</p>
                )}
                {review && <ActionReviewPanel review={review} onJumpToLine={jumpToLine} onAddParameters={addSuggestedParameters} onClose={() => setReview(null)} />}
              </div>

              {/* Parameters Section */}
              <div className="space-y-3 sm:col-span-2">
                <div className="flex items-center justify-between">
                  <Label className="flex items-center gap-1.5"><Settings2 className="h-3.5 w-3.5" /> Parameters</Label>
                  <Button
                    type="button"
                    variant="outline"
                    size="sm"
                    onClick={() => setForm({ ...form, parameters: [...form.parameters, { name: "", label: "", type: "string", required: false, default: "", placeholder: "", options: null, description: "" }] })}
                  >
                    <Plus className="h-3 w-3 mr-1" /> Add Parameter
                  </Button>
                </div>
                {form.parameters.length > 0 && (
                  <p className="text-xs text-muted-foreground">Parameters are exposed as PARAM_NAME environment variables in the script.</p>
                )}
                {form.parameters.map((param, idx) => (
                  <div key={idx} className="border rounded-md p-3 space-y-2 bg-muted/30">
                    <div className="flex items-center justify-between">
                      <span className="text-xs font-medium text-muted-foreground">Parameter {idx + 1}</span>
                      <div className="flex items-center gap-1">
                        {idx > 0 && (
                          <Button type="button" variant="ghost" size="sm" className="h-6 w-6 p-0" onClick={() => {
                            const p = [...form.parameters];
                            [p[idx - 1], p[idx]] = [p[idx], p[idx - 1]];
                            setForm({ ...form, parameters: p });
                          }}><ArrowUp className="h-3 w-3" /></Button>
                        )}
                        {idx < form.parameters.length - 1 && (
                          <Button type="button" variant="ghost" size="sm" className="h-6 w-6 p-0" onClick={() => {
                            const p = [...form.parameters];
                            [p[idx], p[idx + 1]] = [p[idx + 1], p[idx]];
                            setForm({ ...form, parameters: p });
                          }}><ArrowDown className="h-3 w-3" /></Button>
                        )}
                        <Button type="button" variant="ghost" size="sm" className="h-6 w-6 p-0 text-destructive" onClick={() => {
                          setForm({ ...form, parameters: form.parameters.filter((_, i) => i !== idx) });
                        }}><Trash2 className="h-3 w-3" /></Button>
                      </div>
                    </div>
                    <div className="grid gap-2 sm:grid-cols-3">
                      <div>
                        <Label className="text-xs">Name *</Label>
                        <Input
                          value={param.name}
                          onChange={(e) => {
                            const p = [...form.parameters];
                            p[idx] = { ...p[idx], name: e.target.value.toUpperCase().replace(/[^A-Z0-9_]/g, "") };
                            setForm({ ...form, parameters: p });
                          }}
                          placeholder="MY_PARAM"
                          className="h-8 text-xs font-mono"
                        />
                      </div>
                      <div>
                        <Label className="text-xs">Label *</Label>
                        <Input
                          value={param.label}
                          onChange={(e) => {
                            const p = [...form.parameters];
                            p[idx] = { ...p[idx], label: e.target.value };
                            setForm({ ...form, parameters: p });
                          }}
                          placeholder="My Parameter"
                          className="h-8 text-xs"
                        />
                      </div>
                      <div>
                        <Label className="text-xs">Type</Label>
                        <Select
                          value={param.type}
                          onChange={(e) => {
                            const p = [...form.parameters];
                            p[idx] = { ...p[idx], type: e.target.value as ActionParameter["type"] };
                            setForm({ ...form, parameters: p });
                          }}
                          className="h-8 text-xs"
                        >
                          <option value="string">String</option>
                          <option value="number">Number</option>
                          <option value="select">Select</option>
                          <option value="boolean">Boolean</option>
                          <option value="password">Password</option>
                        </Select>
                      </div>
                    </div>
                    <div className="grid gap-2 sm:grid-cols-3">
                      <div>
                        <Label className="text-xs">Default</Label>
                        <Input
                          value={param.default}
                          onChange={(e) => {
                            const p = [...form.parameters];
                            p[idx] = { ...p[idx], default: e.target.value };
                            setForm({ ...form, parameters: p });
                          }}
                          placeholder="default value"
                          className="h-8 text-xs"
                        />
                      </div>
                      <div>
                        <Label className="text-xs">Placeholder</Label>
                        <Input
                          value={param.placeholder}
                          onChange={(e) => {
                            const p = [...form.parameters];
                            p[idx] = { ...p[idx], placeholder: e.target.value };
                            setForm({ ...form, parameters: p });
                          }}
                          placeholder="placeholder text"
                          className="h-8 text-xs"
                        />
                      </div>
                      <div className="flex items-end gap-2">
                        <label className="flex items-center gap-1.5 text-xs cursor-pointer">
                          <input
                            type="checkbox"
                            checked={param.required}
                            onChange={(e) => {
                              const p = [...form.parameters];
                              p[idx] = { ...p[idx], required: e.target.checked };
                              setForm({ ...form, parameters: p });
                            }}
                            className="rounded"
                          />
                          Required
                        </label>
                      </div>
                    </div>
                    {param.type === "select" && (
                      <div>
                        <Label className="text-xs">Options (comma-separated)</Label>
                        <Input
                          value={(param.options || []).join(", ")}
                          onChange={(e) => {
                            const p = [...form.parameters];
                            p[idx] = { ...p[idx], options: e.target.value.split(",").map((s) => s.trim()).filter(Boolean) };
                            setForm({ ...form, parameters: p });
                          }}
                          placeholder="option1, option2, option3"
                          className="h-8 text-xs"
                        />
                      </div>
                    )}
                    <div>
                      <Label className="text-xs">Description</Label>
                      <Input
                        value={param.description}
                        onChange={(e) => {
                          const p = [...form.parameters];
                          p[idx] = { ...p[idx], description: e.target.value };
                          setForm({ ...form, parameters: p });
                        }}
                        placeholder="Help text for this parameter"
                        className="h-8 text-xs"
                      />
                    </div>
                  </div>
                ))}
              </div>

              <div className="sm:col-span-2 flex gap-2 flex-wrap items-center">
                {editingDraft ? (
                  <>
                    <Button onClick={handlePublish} disabled={!form.name || !form.script || !!configError} title="Make this action runnable"><Rocket className="h-3.5 w-3.5 mr-1" /> Publish</Button>
                    <Button variant="outline" onClick={handleSave} disabled={!form.name || !form.script || !!configError}>Save draft</Button>
                    <Button variant="destructive" onClick={handleDiscardDraft}>Discard</Button>
                    <span className="text-xs text-muted-foreground">Drafts are never run — not by you, deployments, blueprints or the MCP — until published.</span>
                  </>
                ) : (
                  <Button onClick={handleSave} disabled={!form.name || !form.script || !!configError}>
                    {editingId ? "Update" : "Create"}
                  </Button>
                )}
                <Button variant="outline" onClick={() => { setShowForm(false); setEditingId(null); setEditingDraft(null); setReview(null); setShowDraft(false); }}>Cancel</Button>
              </div>
            </div>
          </CardContent>
        </Card>
      )}

      {/* Actions List */}
      {actionList.length === 0 ? (
        <Card className="border-dashed">
          <CardContent className="flex flex-col items-center justify-center py-16 text-center">
            <Code2 className="h-12 w-12 text-muted-foreground/40 mb-4" />
            <h3 className="text-lg font-medium mb-1">No actions yet</h3>
            <p className="text-sm text-muted-foreground mb-4 max-w-md">
              Create reusable automation snippets to run on your VMs. Install packages, configure services, or run custom scripts.
            </p>
            {isAdmin && (
              <Button onClick={() => { setShowForm(true); setEditingId(null); setForm({ name: "", description: "", category: "custom" as Action["category"], script: "", parameters: [], tags: [] }); }}>
                <Plus className="h-4 w-4 mr-2" /> Create Your First Action
              </Button>
            )}
          </CardContent>
        </Card>
      ) : filtered.length === 0 ? (
        <div className="text-center py-12">
          <p className="text-muted-foreground">No actions match your search</p>
          <Button variant="link" onClick={() => { setSearch(""); setCategoryFilter("all"); }}>Clear filters</Button>
        </div>
      ) : viewMode === "table" ? (
        <div className="rounded-md border">
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b bg-muted/50">
                <SortableTh label="Name" field="name" currentField={actSortField} currentDir={actSortDir} onSort={actToggleSort} />
                <SortableTh label="Category" field="category" currentField={actSortField} currentDir={actSortDir} onSort={actToggleSort} className="hidden sm:table-cell" />
                <th className="text-left px-4 py-2 font-medium hidden md:table-cell">Description</th>
                <th className="text-left px-4 py-2 font-medium hidden lg:table-cell">Type</th>
                <th className="text-right px-4 py-2 font-medium">Actions</th>
              </tr>
            </thead>
            <tbody>
              {paginated.map((action) => (
                <React.Fragment key={action.id}>
                <tr className="border-b last:border-0 hover:bg-muted/30 transition-colors">
                  <td className="px-4 py-2.5 font-medium">
                    {action.name}
                    {action.status === "draft" && (
                      <Badge variant="warning" className="ml-1.5 text-[10px]" title="Saved but not published — not runnable">{action.source === "ai" ? "AI draft" : "draft"}</Badge>
                    )}
                    {!action.builtin && action.version && action.version > 1 && (
                      <Badge variant="outline" className="ml-1.5 text-[10px]">v{action.version}</Badge>
                    )}
                  </td>
                  <td className="px-4 py-2.5 hidden sm:table-cell">
                    <span className={`text-xs px-1.5 py-0.5 rounded ${categoryColors[action.category] || categoryColors.custom}`}>
                      {action.category}
                    </span>
                  </td>
                  <td className="px-4 py-2.5 text-muted-foreground text-xs hidden md:table-cell max-w-xs truncate">{action.description || "—"}</td>
                  <td className="px-4 py-2.5 hidden lg:table-cell">
                    {action.builtin ? <Badge variant="outline" className="text-xs">Built-in</Badge> : <Badge variant="secondary" className="text-xs">Custom</Badge>}
                  </td>
                  <td className="px-4 py-2.5 text-right">
                    <div className="flex items-center justify-end gap-1">
                      <Button variant="ghost" size="sm" onClick={() => setExpandedId(expandedId === action.id ? null : action.id)} title="View script">
                        <Code2 className="h-3.5 w-3.5" />
                      </Button>
                      <Button variant="ghost" size="sm" onClick={() => handleExportOne(action)} title="Download as JSON">
                        <Download className="h-3.5 w-3.5" />
                      </Button>
                      {!action.builtin && (
                        <Button variant="ghost" size="sm" onClick={() => toggleVersions(action.id)} title="Version history">
                          <History className="h-3.5 w-3.5" />
                        </Button>
                      )}
                      {isAdmin && !action.builtin && (
                        <>
                          <Button variant="ghost" size="sm" onClick={() => handleEdit(action)} title="Edit">
                            <Edit2 className="h-3.5 w-3.5" />
                          </Button>
                          <Button variant="ghost" size="sm" onClick={() => handleDelete(action.id)} title="Delete" className="text-destructive">
                            <Trash2 className="h-3.5 w-3.5" />
                          </Button>
                        </>
                      )}
                    </div>
                  </td>
                </tr>
                {expandedId === action.id && (
                  <tr className="border-b last:border-0">
                    <td colSpan={5} className="px-4 py-3">
                      <pre className="text-xs bg-gray-950 text-success p-3 rounded-md overflow-x-auto max-h-64 whitespace-pre-wrap">{action.script}</pre>
                    </td>
                  </tr>
                )}
                {versionsOpenId === action.id && (
                  <tr className="border-b last:border-0">
                    <td colSpan={5} className="px-4 py-3">
                      {renderVersionsPanel(action)}
                    </td>
                  </tr>
                )}
                </React.Fragment>
              ))}
            </tbody>
          </table>
        </div>
      ) : (
        Object.entries(grouped).map(([category, categoryActions]) => {
          const Icon = categoryIcons[category] || Puzzle;
          return (
            <div key={category} className="space-y-3">
              <div className="flex items-center gap-2">
                <Icon className="h-4 w-4 text-muted-foreground" />
                <h2 className="text-lg font-semibold">{categoryLabels[category] || category}</h2>
                <Badge variant="secondary">{categoryActions.length}</Badge>
              </div>
              <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
                {categoryActions.map((action) => (
                  <Card key={action.id} className="group hover:border-primary/50 transition-colors">
                    <CardHeader className="pb-2">
                      <div className="flex items-start justify-between">
                        <div className="flex items-center gap-2">
                          <CardTitle className="text-base">{action.name}</CardTitle>
                          {action.status === "draft" && (
                            <Badge variant="warning" className="text-[10px]" title="Saved but not published — not runnable">{action.source === "ai" ? "AI draft" : "draft"}</Badge>
                          )}
                          {!action.builtin && action.version && action.version > 1 && (
                            <Badge variant="outline" className="text-[10px]">v{action.version}</Badge>
                          )}
                        </div>
                        <div className="flex items-center gap-1">
                          {action.builtin && <Badge variant="outline" className="text-xs">Built-in</Badge>}
                          {action.parameters && action.parameters.length > 0 && (
                            <Badge variant="outline" className="text-xs"><Settings2 className="h-2.5 w-2.5 mr-0.5" />{action.parameters.length}</Badge>
                          )}
                          <span className={`text-xs px-1.5 py-0.5 rounded ${categoryColors[action.category] || categoryColors.custom}`}>
                            {action.category}
                          </span>
                        </div>
                      </div>
                    </CardHeader>
                    <CardContent>
                      <p className="text-sm text-muted-foreground mb-3">{action.description || "No description"}</p>

                      {action.tags && action.tags.length > 0 && (
                        <div className="flex flex-wrap gap-1 mb-3">
                          {action.tags.map((tag) => (
                            <button
                              key={tag}
                              className="text-[10px] px-1.5 py-0.5 rounded bg-muted text-muted-foreground hover:bg-muted/70 transition-colors"
                              onClick={() => setSearch(tag)}
                              title={`Search for "${tag}"`}
                            >
                              #{tag}
                            </button>
                          ))}
                        </div>
                      )}

                      {/* Collapsible config preview */}
                      <div className="flex items-center justify-between mb-1">
                        <button
                          className="flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground"
                          onClick={() => setExpandedId(expandedId === action.id ? null : action.id)}
                        >
                          <Code2 className="h-3 w-3" />
                          <span>Script</span>
                          {expandedId === action.id ? <ChevronUp className="h-3 w-3" /> : <ChevronDown className="h-3 w-3" />}
                        </button>
                        <button
                          className="flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground"
                          onClick={() => handleExportOne(action)}
                          title="Download as JSON"
                        >
                          <Download className="h-3 w-3" />
                        </button>
                      </div>
                      {expandedId === action.id && (
                        <div className="relative mb-3">
                          <pre className="text-xs bg-gray-950 text-success p-3 pr-10 rounded-md overflow-x-auto max-h-64 whitespace-pre-wrap">{action.script}</pre>
                          <button
                            className="absolute top-2 right-2 p-1.5 rounded-md bg-gray-800 hover:bg-gray-700 text-gray-400 hover:text-gray-200 transition-colors"
                            onClick={() => {
                              navigator.clipboard.writeText(action.script);
                              setCopiedId(action.id);
                              setTimeout(() => setCopiedId(null), 2000);
                            }}
                            title="Copy script"
                          >
                            {copiedId === action.id ? <Check className="h-3.5 w-3.5 text-success" /> : <Copy className="h-3.5 w-3.5" />}
                          </button>
                        </div>
                      )}

                      {!action.builtin && action.status !== "draft" && (
                        <div className="flex gap-2 mt-2 pt-2 border-t">
                          <button
                            className="flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground"
                            onClick={() => toggleVersions(action.id)}
                          >
                            <History className="h-3 w-3" />
                            <span>Version History</span>
                            {versionsOpenId === action.id ? <ChevronUp className="h-3 w-3" /> : <ChevronDown className="h-3 w-3" />}
                          </button>
                        </div>
                      )}
                      {versionsOpenId === action.id && (
                        <div className="mt-2">{renderVersionsPanel(action)}</div>
                      )}

                      {isAdmin && !action.builtin && (
                        <div className="flex gap-2 mt-2 pt-2 border-t opacity-0 group-hover:opacity-100 transition-opacity">
                          <Button variant="outline" size="sm" onClick={() => handleEdit(action)}>
                            <Edit2 className="h-3 w-3 mr-1" /> Edit
                          </Button>
                          <Button variant="outline" size="sm" onClick={() => handleDelete(action.id)} className="text-destructive hover:text-destructive">
                            <Trash2 className="h-3 w-3 mr-1" /> Delete
                          </Button>
                        </div>
                      )}
                    </CardContent>
                  </Card>
                ))}
              </div>
            </div>
          );
        })
      )}

      {/* Pagination */}
      <Pagination
        page={page}
        pageSize={pageSize}
        totalItems={filtered.length}
        onPageChange={setPage}
        onPageSizeChange={setPageSize}
        itemLabel="actions"
      />
    </div>
  );
}
