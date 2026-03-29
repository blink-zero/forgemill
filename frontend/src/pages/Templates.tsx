import { useTimezone } from "@/hooks/useTimezone";
import { useEffect, useState, useCallback } from "react";
import { useNavigate } from "react-router-dom";
import { templates as templateApi, factoryApi, templateHistory, targets as targetsApi } from "@/api/client";
import type { Template, TemplateDetailInfo, Target, UpdateCheckResult, TemplateHistory as THistory, TemplateSchedule, TemplateFamily } from "@/types";
import { useToast } from "@/components/ui/toast";
import { Card, CardHeader, CardTitle, CardContent } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Select } from "@/components/ui/select";
import { CheckCircle, Cpu, HardDrive, Search, Monitor, RefreshCw, Clock, AlertTriangle, History, Rocket, ShieldCheck, Trash2, X, AlertCircle, XCircle, Info, Power, Loader2, MoreHorizontal, Hammer, RotateCcw } from "lucide-react";
import { Pagination } from "@/components/ui/pagination";
import ProviderIcon from "@/components/ProviderIcon";
import { getErrorMessage } from "@/lib/utils";
import { SkeletonTemplateCard, Skeleton } from "@/components/ui/skeleton";
import { ViewToggle } from "@/components/ui/view-toggle";
import { usePreference } from "@/context/PreferencesContext";
import { SortableTh } from "@/components/ui/sortable-th";
import { useTableSort } from "@/hooks/useTableSort";

const ITEMS_PER_PAGE = 12;

export default function Templates() {
  const { formatDate, formatDateTime } = useTimezone();
  const { toast } = useToast();
  const navigate = useNavigate();
  const [templates, setTemplates] = useState<Template[]>([]);
  const [loading, setLoading] = useState(true);
  const [search, setSearch] = useState("");
  const [updateChecks, setUpdateChecks] = useState<Record<number, UpdateCheckResult>>({});
  const [checking, setChecking] = useState(false);
  const [hasChecked, setHasChecked] = useState(false);
  const [showUpdateModal, setShowUpdateModal] = useState(false);
  const [updateProgress, setUpdateProgress] = useState<{ name: string; status: "pending" | "checking" | "up-to-date" | "update-available" | "error" }[]>([]);
  const [historyOpen, setHistoryOpen] = useState<number | null>(null);
  const [historyData, setHistoryData] = useState<THistory[]>([]);
  const [schedules, setSchedules] = useState<TemplateSchedule[]>([]);
  const [scheduleOpen, setScheduleOpen] = useState<number | null>(null);
  const [scheduleStrategy, setScheduleStrategy] = useState("on_update");
  const [scheduleIntervalDays, setScheduleIntervalDays] = useState(30);
  const [scheduleEnabled, setScheduleEnabled] = useState(true);
  const [rebuilding, setRebuilding] = useState<number | null>(null);
  const [targetsList, setTargetsList] = useState<Target[]>([]);
  const [families, setFamilies] = useState<TemplateFamily[]>([]);
  const [rebuildModal, setRebuildModal] = useState<Template | null>(null);
  const [deleteModal, setDeleteModal] = useState<{
    template: Template;
    preview: { deployments: number; vms: number; builds: number };
  } | null>(null);
  const [deleting, setDeleting] = useState<false | "untrack" | "destroy">(false);
  const [keepVMs, setKeepVMs] = useState(true);
  const [destroyConfirmText, setDestroyConfirmText] = useState("");
  const [page, setPage] = useState(1);
  const [detailModal, setDetailModal] = useState<TemplateDetailInfo | null>(null);
  const [detailLoading, setDetailLoading] = useState<number | null>(null);
  const [menuOpen, setMenuOpen] = useState<number | null>(null);

  // Close overflow menu when clicking outside
  useEffect(() => {
    if (menuOpen === null) return;
    const handler = () => setMenuOpen(null);
    document.addEventListener("click", handler);
    return () => document.removeEventListener("click", handler);
  }, [menuOpen]);

    const loadData = () => {
    Promise.all([
      templateApi.list(),
      targetsApi.list(),
      factoryApi.listTemplateFamilies(),
      factoryApi.listSchedules(),
    ]).then(([templatesRes, targetsRes, familiesRes, schedulesRes]) => {
      setTemplates(templatesRes.data || []);
      setTargetsList(targetsRes.data || []);
      setFamilies(familiesRes.data || []);
      setSchedules(schedulesRes.data || []);
    }).finally(() => setLoading(false));
  };

  useEffect(() => { loadData(); }, []);

  // Reset page when search changes
  useEffect(() => { setPage(1); }, [search]);

  // F-157: Guard against null/undefined fields to prevent TypeError crashes
  const filtered = templates.filter(
    (t) =>
      (t.name || "").toLowerCase().includes(search.toLowerCase()) ||
      (t.os_type || "").toLowerCase().includes(search.toLowerCase()) ||
      (t.target_name || "").toLowerCase().includes(search.toLowerCase())
  );

  const checkAllUpdates = async () => {
    const managed = templates.filter((t) => t.managed_by_forgemill && t.lifecycle_status === "active");
    if (managed.length === 0) return;

    // Initialize modal with all templates as "pending"
    setUpdateProgress(managed.map((t) => ({ name: t.name, status: "pending" })));
    setShowUpdateModal(true);
    setChecking(true);

    const updates: Record<number, UpdateCheckResult> = {};

    // Check each template sequentially so user can see progress
    for (let i = 0; i < managed.length; i++) {
      const t = managed[i];
      setUpdateProgress((prev) => prev.map((p, idx) => idx === i ? { ...p, status: "checking" } : p));

      try {
        const res = await factoryApi.checkTemplateUpdate(t.id);
        updates[t.id] = res.data;
        setUpdateProgress((prev) => prev.map((p, idx) =>
          idx === i ? { ...p, status: res.data.update_available ? "update-available" : "up-to-date" } : p
        ));
      } catch {
        setUpdateProgress((prev) => prev.map((p, idx) => idx === i ? { ...p, status: "error" } : p));
      }
    }

    setUpdateChecks((prev) => ({ ...prev, ...updates }));
    setHasChecked(true);
    setChecking(false);
  };

  const handleDeleteClick = async (template: Template) => {
    try {
      const res = await templateApi.deletePreview(template.id);
      setKeepVMs(true);
      setDestroyConfirmText("");
      setDeleteModal({ template, preview: res.data });
    } catch (e) {
      toast(getErrorMessage(e, "Failed to load delete preview"), "error");
    }
  };

  const handleDetailClick = async (template: Template) => {
    setDetailLoading(template.id);
    try {
      const res = await templateApi.getDetail(template.id);
      setDetailModal(res.data);
    } catch (e) {
      toast(getErrorMessage(e, "Failed to load template details"), "error");
    } finally {
      setDetailLoading(null);
    }
  };

  const handleDeleteConfirm = async (destroy: boolean) => {
    if (!deleteModal) return;
    setDeleting(destroy ? "destroy" : "untrack");
    try {
      await templateApi.delete(deleteModal.template.id, destroy, keepVMs);
      setDeleteModal(null);
      loadData();
    } catch (e) {
      toast(getErrorMessage(e, "Failed to delete template"), "error");
    } finally {
      setDeleting(false);
    }
  };

  const handleRebuildClick = (template: Template) => {
    setRebuildModal(template);
  };

  const handleRebuildConfirm = async () => {
    if (!rebuildModal) return;
    setRebuilding(rebuildModal.id);
    try {
      const res = await factoryApi.rebuildTemplate(rebuildModal.id);
      setRebuildModal(null);
      navigate(`/factory/build/${res.data.id}`);
    } catch {
      setRebuilding(null);
    }
  };

  const openHistory = async (templateId: number) => {
    if (historyOpen === templateId) {
      setHistoryOpen(null);
      return;
    }
    try {
      const res = await templateHistory.get(templateId);
      setHistoryData(res.data || []);
      setHistoryOpen(templateId);
    } catch {
      setHistoryData([]);
      setHistoryOpen(templateId);
    }
  };

  const getScheduleForTemplate = (templateId: number) =>
    schedules.find((s) => s.template_id === templateId);

  const openSchedulePanel = (templateId: number) => {
    if (scheduleOpen === templateId) {
      setScheduleOpen(null);
      return;
    }
    const existing = getScheduleForTemplate(templateId);
    if (existing) {
      setScheduleStrategy(existing.strategy);
      setScheduleIntervalDays(existing.interval_days);
      setScheduleEnabled(existing.enabled);
    } else {
      setScheduleStrategy("on_update");
      setScheduleIntervalDays(30);
      setScheduleEnabled(true);
    }
    setScheduleOpen(templateId);
  };

  const saveSchedule = async (templateId: number) => {
    const existing = getScheduleForTemplate(templateId);
    try {
      if (existing) {
        const res = await factoryApi.updateSchedule(existing.id, {
          strategy: scheduleStrategy as TemplateSchedule["strategy"],
          interval_days: scheduleIntervalDays,
          enabled: scheduleEnabled,
        });
        setSchedules((prev) => prev.map((s) => s.id === existing.id ? res.data : s));
      } else {
        const res = await factoryApi.createSchedule({
          template_id: templateId,
          strategy: scheduleStrategy as TemplateSchedule["strategy"],
          interval_days: scheduleIntervalDays,
          check_interval_hours: 24,
          enabled: scheduleEnabled,
          build_config_json: "{}",
        });
        setSchedules((prev) => [...prev, res.data]);
      }
      setScheduleOpen(null);
      toast("Schedule saved", "success");
    } catch (e) {
      toast(getErrorMessage(e, "Failed to save schedule"), "error");
    }
  };

  const deleteSchedule = async (scheduleId: number) => {
    try {
      await factoryApi.deleteSchedule(scheduleId);
      setSchedules((prev) => prev.filter((s) => s.id !== scheduleId));
      setScheduleOpen(null);
      toast("Schedule deleted", "success");
    } catch (e) {
      toast(getErrorMessage(e, "Failed to delete schedule"), "error");
    }
  };

  const toggleScheduleEnabled = async (schedule: TemplateSchedule) => {
    try {
      const res = await factoryApi.updateSchedule(schedule.id, { enabled: !schedule.enabled });
      setSchedules((prev) => prev.map((s) => s.id === schedule.id ? res.data : s));
    } catch (e) {
      toast(getErrorMessage(e, "Failed to update schedule"), "error");
    }
  };

  // Hooks must be before any early returns to satisfy React's rules of hooks
  const viewMode = usePreference("view_mode", "cards");
  const { sorted: tableSorted, sortField: tSortField, sortDir: tSortDir, toggleSort: tToggleSort } = useTableSort(filtered, "name");

  if (loading) {
    return (
      <div className="space-y-6">
        <div className="flex items-center justify-between">
          <Skeleton className="h-8 w-32" />
          <div className="flex items-center gap-3">
            <Skeleton className="h-10 w-28" />
            <Skeleton className="h-10 w-28" />
          </div>
        </div>
        <Skeleton className="h-16 w-full rounded-lg" />
        <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
          {[1, 2, 3, 4, 5, 6].map((i) => (
            <SkeletonTemplateCard key={i} />
          ))}
        </div>
      </div>
    );
  }

  const hasManagedTemplates = templates.some((t) => t.managed_by_forgemill);

  return (
    <div className="space-y-6">
      <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
        <div className="flex items-center gap-2">
          <h1 className="text-2xl font-bold">Templates</h1>
          {templates.length > 0 && <Badge variant="outline">{templates.length}</Badge>}
        </div>
        <div className="flex flex-wrap items-center gap-2 sm:gap-3">
          {hasManagedTemplates && (
            <Button variant="outline" size="sm" onClick={checkAllUpdates} disabled={checking}>
              <RefreshCw className={`h-4 w-4 mr-1 ${checking ? "animate-spin" : ""}`} />
              Check Updates
            </Button>
          )}
          {templates.length === 0 ? (
            <Button size="sm" onClick={() => navigate("/factory")}>
              <Hammer className="h-4 w-4 mr-1" />
              Build Template
            </Button>
          ) : (
            <Button size="sm" onClick={() => navigate("/deploy")}>
              <Rocket className="h-4 w-4 mr-1" />
              Deploy VM
            </Button>
          )}
          <ViewToggle />
          <div className="relative w-full sm:w-64">
            <Search className="absolute left-3 top-1/2 -translate-y-1/2 h-4 w-4 text-muted-foreground" />
            <Input
              placeholder="Search templates..."
              value={search}
              onChange={(e) => setSearch(e.target.value)}
              className="pl-9"
            />
          </div>
        </div>
      </div>

      <div className="rounded-lg border bg-blue-500/5 border-blue-500/20 px-4 py-3 flex items-start gap-3">
        <Info className="h-5 w-5 text-blue-500 shrink-0 mt-0.5" />
        <div>
          <p className="text-sm font-medium">Template library</p>
          <p className="text-xs text-muted-foreground">VM templates synced from your hypervisors. Forgemill-managed templates can be rebuilt and versioned automatically.</p>
        </div>
      </div>

      {filtered.length === 0 ? (
        templates.length === 0 ? (
          // Truly empty — no templates at all
          <div className="flex flex-col items-center justify-center py-16 text-center gap-4">
            <div className="h-16 w-16 rounded-full bg-muted flex items-center justify-center">
              <Monitor className="h-8 w-8 text-muted-foreground opacity-60" />
            </div>
            <div>
              <p className="font-medium text-lg">No templates yet</p>
              <p className="text-sm text-muted-foreground mt-1 max-w-sm">
                Build a template with Forgemill Factory, or sync one from an existing hypervisor target.
              </p>
            </div>
            <div className="flex items-center gap-3 mt-2">
              <Button onClick={() => navigate("/factory")}>
                <Hammer className="h-4 w-4 mr-1" />
                Build Template
              </Button>
              <Button variant="outline" onClick={() => navigate("/targets")}>
                <RotateCcw className="h-4 w-4 mr-1" />
                Sync from Target
              </Button>
            </div>
          </div>
        ) : (
          // Search filtered — templates exist but nothing matches
          <div className="text-center py-12 text-muted-foreground">
            <Search className="h-12 w-12 mx-auto mb-4 opacity-50" />
            <p>No templates match your search</p>
            <p className="text-sm mt-1">Try a different name, OS type, or target</p>
          </div>
        )
      ) : (
        <>
        {viewMode === "table" ? (
        <div className="rounded-md border">
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b bg-muted/50">
                <SortableTh label="Name" field="name" currentField={tSortField} currentDir={tSortDir} onSort={tToggleSort} />
                <SortableTh label="Target" field="target_name" currentField={tSortField} currentDir={tSortDir} onSort={tToggleSort} className="hidden sm:table-cell" />
                <SortableTh label="CPU" field="cpu" currentField={tSortField} currentDir={tSortDir} onSort={tToggleSort} className="hidden md:table-cell" />
                <th className="text-left px-4 py-2 font-medium hidden lg:table-cell">Status</th>
                <th className="text-right px-4 py-2 font-medium">Actions</th>
              </tr>
            </thead>
            <tbody>
              {tableSorted.slice((page - 1) * ITEMS_PER_PAGE, page * ITEMS_PER_PAGE).map((t) => {
                const targetObj = targetsList.find((tg) => tg.id === t.target_id);
                return (
                  <tr key={t.id} className={`border-b last:border-0 hover:bg-muted/30 transition-colors ${t.lifecycle_status === "superseded" ? "opacity-60" : ""}`}>
                    <td className="px-4 py-2.5">
                      <div className="flex items-center gap-2">
                        {targetObj && <ProviderIcon type={targetObj.type} size={18} />}
                        <div>
                          <span className="font-medium">{t.name}</span>
                          {t.managed_by_forgemill && t.version ? <span className="ml-1.5 text-xs text-muted-foreground">v{t.version}</span> : null}
                        </div>
                      </div>
                    </td>
                    <td className="px-4 py-2.5 text-muted-foreground hidden sm:table-cell">{t.target_name}</td>
                    <td className="px-4 py-2.5 text-muted-foreground hidden md:table-cell">
                      {t.cpu || "?"}C · {t.memory_mb ? (t.memory_mb >= 1024 ? `${(t.memory_mb / 1024).toFixed(0)}G` : `${t.memory_mb}M`) : "?"} · {t.disk_gb || "?"}G
                    </td>
                    <td className="px-4 py-2.5 hidden lg:table-cell">
                      {t.managed_by_forgemill ? (
                        <Badge variant={t.lifecycle_status === "superseded" ? "warning" : "success"} className="text-xs">
                          {t.lifecycle_status === "superseded" ? "superseded" : "managed"}
                        </Badge>
                      ) : (
                        <Badge variant="secondary" className="text-xs">synced</Badge>
                      )}
                    </td>
                    <td className="px-4 py-2.5 text-right">
                      <div className="flex items-center justify-end gap-1">
                        <Button size="sm" variant="ghost" onClick={() => navigate(`/deploy?template=${t.id}`)} title="Deploy">
                          <Rocket className="h-3.5 w-3.5" />
                        </Button>
                        {t.managed_by_forgemill && t.lifecycle_status === "active" && (
                          <Button size="sm" variant="ghost" onClick={() => handleRebuildClick(t)} disabled={rebuilding === t.id} title="Rebuild">
                            <RefreshCw className={`h-3.5 w-3.5 ${rebuilding === t.id ? "animate-spin" : ""}`} />
                          </Button>
                        )}
                        <Button size="sm" variant="ghost" onClick={() => handleDetailClick(t)} disabled={detailLoading === t.id} title="Details">
                          <Info className="h-3.5 w-3.5" />
                        </Button>
                      </div>
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
        ) : (
        <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
          {filtered.slice((page - 1) * ITEMS_PER_PAGE, page * ITEMS_PER_PAGE).map((t) => {
            const updateInfo = updateChecks[t.id];
            const hasUpdate = updateInfo?.update_available;
            const targetObj = targetsList.find((tg) => tg.id === t.target_id);
            const schedule = getScheduleForTemplate(t.id);

            return (
              <Card key={t.id} className={`p-4 transition-colors ${t.lifecycle_status === "superseded" ? "opacity-70 border-dashed" : "hover:border-primary/50"}`}>
                <div className="flex items-start justify-between">
                  <div className="flex items-start gap-3">
                    {targetObj && <ProviderIcon type={targetObj.type} size={28} />}
                    <div>
                      <div className="flex items-center gap-2 flex-wrap">
                        <h3 className="font-medium">{t.name}</h3>
                        {t.managed_by_forgemill && t.version && (
                          <Badge variant="outline" className="text-xs shrink-0">v{t.version}</Badge>
                        )}
                        {hasUpdate && (
                          <Badge variant="destructive" className="text-xs flex items-center gap-1 shrink-0">
                            <AlertTriangle className="h-3 w-3" /> Update
                          </Badge>
                        )}
                        {t.lifecycle_status === "superseded" && (
                          <Badge variant="warning" className="text-xs shrink-0">superseded</Badge>
                        )}
                      </div>
                      <p className="text-sm text-muted-foreground">
                        {t.target_name}
                        {t.managed_by_forgemill && (
                          <span className="inline-flex items-center gap-0.5 ml-2 text-green-500"><ShieldCheck className="h-3 w-3" /> Built by Forgemill</span>
                        )}
                      </p>
                    </div>
                  </div>
                </div>

                <div className="mt-3 grid grid-cols-3 gap-2 text-xs text-muted-foreground">
                  <div className="flex items-center gap-1">
                    <Cpu className="h-3 w-3" /> {t.cpu || "?"} vCPU
                  </div>
                  <div className="flex items-center gap-1">
                    <HardDrive className="h-3 w-3" /> {t.memory_mb ? (t.memory_mb >= 1024 ? `${(t.memory_mb / 1024).toFixed(t.memory_mb % 1024 ? 1 : 0)} GB` : `${t.memory_mb} MB`) : "?"} RAM
                  </div>
                  <div className="flex items-center gap-1">
                    <HardDrive className="h-3 w-3" /> {t.disk_gb || "?"} GB
                  </div>
                </div>

                {t.built_at && (
                  <p className="text-xs text-muted-foreground mt-2">
                    Built {formatDate(t.built_at)}
                    {t.iso_checksum && !hasUpdate && <span className="ml-1" title={`ISO: ${t.iso_checksum}`}>· ISO verified ✓</span>}
                  </p>
                )}
                {hasUpdate && updateInfo?.update && (
                  <div className="mt-2 rounded-md border border-yellow-500/30 bg-yellow-500/5 p-2 text-xs space-y-1">
                    <p className="font-medium text-yellow-500">ISO checksum changed upstream</p>
                    <div className="font-mono text-muted-foreground">
                      <p>Built with: <span className="text-red-400">{updateInfo.update.current_checksum?.slice(0, 20)}...</span></p>
                      <p>Upstream:&nbsp;&nbsp; <span className="text-green-400">{updateInfo.update.latest_checksum?.slice(0, 20)}...</span></p>
                    </div>
                    <p className="text-muted-foreground">Rebuild to get the latest ISO contents.</p>
                  </div>
                )}
                {!t.built_at && t.last_synced_at && (
                  <p className="text-xs text-muted-foreground mt-2">
                    Synced {formatDate(t.last_synced_at)}
                  </p>
                )}

                {schedule && t.managed_by_forgemill && (
                  <div className={`mt-2 flex items-center gap-1.5 text-xs ${schedule.enabled ? "text-blue-500" : "text-muted-foreground"}`}>
                    <Clock className="h-3 w-3" />
                    <span>
                      {schedule.strategy === "on_update" ? "on_update" : schedule.strategy === "interval" ? `every ${schedule.interval_days}d` : `on_update + every ${schedule.interval_days}d`}
                      {schedule.next_check_at && ` · Next: ${formatDate(schedule.next_check_at)}`}
                    </span>
                    {!schedule.enabled && <Badge variant="secondary" className="text-[10px] px-1 py-0">paused</Badge>}
                  </div>
                )}

                {t.managed_by_forgemill && t.lifecycle_status === "active" && (
                  <div className="mt-3 flex gap-2">
                    <Button
                      variant="outline"
                      size="sm"
                      className="flex-1"
                      onClick={(e) => { e.stopPropagation(); handleRebuildClick(t); }}
                      disabled={rebuilding === t.id}
                    >
                      <RefreshCw className={`h-3.5 w-3.5 mr-1 ${rebuilding === t.id ? "animate-spin" : ""}`} />
                      Rebuild
                    </Button>
                    <Button
                      size="sm"
                      className="flex-1"
                      onClick={() => navigate(`/deploy?template=${t.id}`)}
                    >
                      <Rocket className="h-3.5 w-3.5 mr-1" />
                      Deploy
                    </Button>
                    <div className="relative">
                      <Button
                        variant="outline"
                        size="sm"
                        onClick={(e) => { e.stopPropagation(); setMenuOpen(menuOpen === t.id ? null : t.id); }}
                      >
                        <MoreHorizontal className="h-3.5 w-3.5" />
                      </Button>
                      {menuOpen === t.id && (
                        <div className="absolute right-0 top-full mt-1 z-20 bg-popover border rounded-md shadow-lg py-1 min-w-[160px]">
                          <button className="w-full flex items-center gap-2 px-3 py-1.5 text-sm hover:bg-muted text-left" onClick={(e) => { e.stopPropagation(); setMenuOpen(null); openHistory(t.id); }}>
                            <History className="h-3.5 w-3.5" /> Build History
                          </button>
                          <button className="w-full flex items-center gap-2 px-3 py-1.5 text-sm hover:bg-muted text-left" onClick={(e) => { e.stopPropagation(); setMenuOpen(null); openSchedulePanel(t.id); }}>
                            <Clock className="h-3.5 w-3.5" /> {schedule ? "Manage Schedule" : "Schedule"}
                          </button>
                          <button className="w-full flex items-center gap-2 px-3 py-1.5 text-sm hover:bg-muted text-left" onClick={(e) => { e.stopPropagation(); setMenuOpen(null); handleDetailClick(t); }} disabled={detailLoading === t.id}>
                            <Info className="h-3.5 w-3.5" /> Details
                          </button>
                          <div className="border-t my-1" />
                          <button className="w-full flex items-center gap-2 px-3 py-1.5 text-sm hover:bg-muted text-left text-destructive" onClick={(e) => { e.stopPropagation(); setMenuOpen(null); handleDeleteClick(t); }}>
                            <Trash2 className="h-3.5 w-3.5" /> Delete
                          </button>
                        </div>
                      )}
                    </div>
                  </div>
                )}

                {t.managed_by_forgemill && t.lifecycle_status === "superseded" && (
                  <div className="mt-3 flex gap-2">
                    <Button
                      variant="outline"
                      size="sm"
                      className="flex-1"
                      onClick={() => navigate(`/deploy?template=${t.id}`)}
                    >
                      <Rocket className="h-3.5 w-3.5 mr-1" />
                      Deploy
                    </Button>
                    <div className="relative">
                      <Button
                        variant="outline"
                        size="sm"
                        onClick={(e) => { e.stopPropagation(); setMenuOpen(menuOpen === t.id ? null : t.id); }}
                      >
                        <MoreHorizontal className="h-3.5 w-3.5" />
                      </Button>
                      {menuOpen === t.id && (
                        <div className="absolute right-0 top-full mt-1 z-20 bg-popover border rounded-md shadow-lg py-1 min-w-[160px]">
                          <button className="w-full flex items-center gap-2 px-3 py-1.5 text-sm hover:bg-muted text-left" onClick={(e) => { e.stopPropagation(); setMenuOpen(null); handleDetailClick(t); }} disabled={detailLoading === t.id}>
                            <Info className="h-3.5 w-3.5" /> Details
                          </button>
                          <button className="w-full flex items-center gap-2 px-3 py-1.5 text-sm hover:bg-muted text-left" onClick={(e) => { e.stopPropagation(); setMenuOpen(null); openHistory(t.id); }}>
                            <History className="h-3.5 w-3.5" /> Build History
                          </button>
                          <div className="border-t my-1" />
                          <button className="w-full flex items-center gap-2 px-3 py-1.5 text-sm hover:bg-muted text-left text-destructive" onClick={(e) => { e.stopPropagation(); setMenuOpen(null); handleDeleteClick(t); }}>
                            <Trash2 className="h-3.5 w-3.5" /> Delete
                          </button>
                        </div>
                      )}
                    </div>
                  </div>
                )}

                {!t.managed_by_forgemill && (
                  <div className="mt-3 flex gap-2">
                    <Button
                      size="sm"
                      className="flex-1"
                      onClick={() => navigate(`/deploy?template=${t.id}`)}
                    >
                      <Rocket className="h-3.5 w-3.5 mr-1" />
                      Deploy
                    </Button>
                    <div className="relative">
                      <Button
                        variant="outline"
                        size="sm"
                        onClick={(e) => { e.stopPropagation(); setMenuOpen(menuOpen === t.id ? null : t.id); }}
                      >
                        <MoreHorizontal className="h-3.5 w-3.5" />
                      </Button>
                      {menuOpen === t.id && (
                        <div className="absolute right-0 top-full mt-1 z-20 bg-popover border rounded-md shadow-lg py-1 min-w-[160px]">
                          <button className="w-full flex items-center gap-2 px-3 py-1.5 text-sm hover:bg-muted text-left" onClick={(e) => { e.stopPropagation(); setMenuOpen(null); handleDetailClick(t); }} disabled={detailLoading === t.id}>
                            <Info className="h-3.5 w-3.5" /> Details
                          </button>
                          <div className="border-t my-1" />
                          <button className="w-full flex items-center gap-2 px-3 py-1.5 text-sm hover:bg-muted text-left text-destructive" onClick={(e) => { e.stopPropagation(); setMenuOpen(null); handleDeleteClick(t); }}>
                            <Trash2 className="h-3.5 w-3.5" /> Delete
                          </button>
                        </div>
                      )}
                    </div>
                  </div>
                )}

                  {/* Build History Panel */}
                  {historyOpen === t.id && (
                    <div className="mt-3 border rounded-md p-3 space-y-2">
                      <h4 className="text-sm font-medium">Build History</h4>
                      {historyData.length === 0 ? (
                        <p className="text-xs text-muted-foreground">No history available</p>
                      ) : (
                        historyData.map((h, idx) => (
                          <div key={`${h.template_id}-${h.version}-${idx}`} className="flex items-center justify-between text-xs">
                            <span className="font-medium">{h.template_name}</span>
                            <div className="flex items-center gap-2">
                              <Badge variant={h.status === "active" ? "default" : "secondary"} className="text-xs">
                                {h.status === "active" ? "current" : h.status}
                              </Badge>
                              {h.built_at && (
                                <span className="text-muted-foreground">
                                  {formatDate(h.built_at)}
                                </span>
                              )}
                            </div>
                          </div>
                        ))
                      )}
                    </div>
                  )}

                  {/* Schedule Config Panel */}
                  {scheduleOpen === t.id && (
                    <div className="mt-3 border rounded-md p-3 space-y-3">
                      <div className="flex items-center justify-between">
                        <h4 className="text-sm font-medium">{schedule ? "Manage Schedule" : "Schedule Auto-Rebuild"}</h4>
                        {schedule && (
                          <button
                            className="text-xs flex items-center gap-1 cursor-pointer hover:opacity-80"
                            onClick={() => toggleScheduleEnabled(schedule)}
                          >
                            <Power className="h-3 w-3" />
                            {schedule.enabled ? "Enabled" : "Disabled"}
                            <div className={`ml-1 w-7 h-4 rounded-full relative transition-colors ${schedule.enabled ? "bg-green-500" : "bg-muted-foreground/30"}`}>
                              <div className={`absolute top-0.5 h-3 w-3 rounded-full bg-white transition-transform ${schedule.enabled ? "translate-x-3.5" : "translate-x-0.5"}`} />
                            </div>
                          </button>
                        )}
                      </div>
                      {schedule && schedule.next_check_at && (
                        <p className="text-xs text-muted-foreground">
                          Next check: {formatDateTime(schedule.next_check_at)}
                          {schedule.last_rebuilt_at && ` · Last rebuilt: ${formatDate(schedule.last_rebuilt_at)}`}
                        </p>
                      )}
                      <div>
                        <label className="text-xs text-muted-foreground">Strategy</label>
                        <Select
                          className="mt-1"
                          value={scheduleStrategy}
                          onChange={(e) => setScheduleStrategy(e.target.value)}
                        >
                          <option value="on_update">On Update (rebuild when ISO changes)</option>
                          <option value="interval">Interval (rebuild every N days)</option>
                          <option value="both">Both (whichever comes first)</option>
                        </Select>
                      </div>
                      {(scheduleStrategy === "interval" || scheduleStrategy === "both") && (
                        <div>
                          <label className="text-xs text-muted-foreground">Interval (days)</label>
                          <Input
                            type="number"
                            value={scheduleIntervalDays}
                            onChange={(e) => setScheduleIntervalDays(parseInt(e.target.value) || 30)}
                            className="mt-1"
                          />
                        </div>
                      )}
                      <div className="flex gap-2">
                        <Button size="sm" className="flex-1" onClick={() => saveSchedule(t.id)}>
                          {schedule ? "Update Schedule" : "Create Schedule"}
                        </Button>
                        {schedule && (
                          <Button size="sm" variant="outline" className="text-destructive hover:text-destructive" onClick={() => deleteSchedule(schedule.id)}>
                            <Trash2 className="h-3.5 w-3.5" />
                          </Button>
                        )}
                      </div>
                    </div>
                  )}
              </Card>
            );
          })}
        </div>
        )}
        {/* Pagination */}
        <Pagination page={page} totalPages={Math.ceil(filtered.length / ITEMS_PER_PAGE)} onPageChange={setPage} />
        </>
      )}

      {/* Delete Template Modal */}
      {deleteModal && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50" onClick={() => !deleting && setDeleteModal(null)}>
          <div className="bg-card border rounded-lg shadow-xl max-w-md w-full mx-4 p-6 space-y-4" onClick={(e) => e.stopPropagation()}>
            <div className="flex items-start gap-3">
              <div className="h-10 w-10 rounded-full bg-destructive/10 flex items-center justify-center shrink-0">
                <AlertCircle className="h-5 w-5 text-destructive" />
              </div>
              <div>
                <h3 className="text-lg font-semibold">Delete Template</h3>
                <p className="text-sm text-muted-foreground mt-1">
                  <span className="font-medium text-foreground">{deleteModal.template.name}</span> on {deleteModal.template.target_name}
                </p>
              </div>
            </div>

            {(deleteModal.preview.deployments > 0 || deleteModal.preview.vms > 0 || deleteModal.preview.builds > 0) && (
              <div className="rounded-md border bg-muted/50 p-3 space-y-1 text-sm">
                <p className="font-medium text-sm">This will also remove:</p>
                {deleteModal.preview.deployments > 0 && (
                  <p className="text-muted-foreground">• {deleteModal.preview.deployments} deployment record{deleteModal.preview.deployments !== 1 ? "s" : ""}</p>
                )}
                {deleteModal.preview.vms > 0 && (
                  <p className="text-muted-foreground">• {deleteModal.preview.vms} tracked VM{deleteModal.preview.vms !== 1 ? "s" : ""}</p>
                )}
                {deleteModal.preview.builds > 0 && (
                  <p className="text-muted-foreground">• {deleteModal.preview.builds} build record{deleteModal.preview.builds !== 1 ? "s" : ""}</p>
                )}
              </div>
            )}

            {deleteModal.preview.vms > 0 && (
              <label className="flex items-center gap-2 text-sm cursor-pointer">
                <input
                  type="checkbox"
                  checked={keepVMs}
                  onChange={(e) => setKeepVMs(e.target.checked)}
                  className="rounded border-input"
                />
                Keep tracked VMs (unlink from template)
              </label>
            )}

            <div className="space-y-2">
              <button
                className="w-full flex items-start gap-3 rounded-md border px-4 py-3 text-left hover:bg-muted/50 transition-colors disabled:opacity-50"
                onClick={() => handleDeleteConfirm(false)}
                disabled={!!deleting}
              >
                {deleting === "untrack" ? <Loader2 className="h-5 w-5 shrink-0 mt-0.5 animate-spin" /> : <X className="h-5 w-5 shrink-0 mt-0.5" />}
                <div>
                  <p className="font-medium text-sm">{deleting === "untrack" ? "Removing..." : "Untrack"}</p>
                  <p className="text-xs text-muted-foreground">Remove from Forgemill only. Template stays on the hypervisor.</p>
                </div>
              </button>
              <div className="rounded-md border border-destructive/30 bg-destructive/5 p-4 space-y-3">
                <div className="flex items-start gap-3">
                  <Trash2 className="h-5 w-5 shrink-0 mt-0.5 text-destructive" />
                  <div>
                    <p className="font-medium text-sm text-destructive">Destroy</p>
                    <p className="text-xs text-muted-foreground">Delete from hypervisor AND remove from Forgemill. Cannot be undone.</p>
                  </div>
                </div>
                <div className="space-y-1.5">
                  <label className="text-xs text-destructive">Type <span className="font-mono font-bold">{deleteModal.template.name}</span> to confirm:</label>
                  <Input
                    value={destroyConfirmText}
                    onChange={(e) => setDestroyConfirmText(e.target.value)}
                    placeholder={deleteModal.template.name}
                    className="text-sm border-destructive/50 focus:border-destructive"
                    disabled={!!deleting}
                  />
                </div>
                <button
                  className="w-full rounded-md bg-destructive text-destructive-foreground px-4 py-2 text-sm font-medium hover:bg-destructive/90 transition-colors disabled:opacity-50"
                  onClick={() => handleDeleteConfirm(true)}
                  disabled={!!deleting || destroyConfirmText !== deleteModal.template.name}
                >
                  {deleting === "destroy" ? "Destroying..." : destroyConfirmText === deleteModal.template.name ? "Confirm Destroy" : "Type template name to confirm"}
                </button>
              </div>
            </div>

            <div className="flex justify-end">
              <Button variant="ghost" size="sm" onClick={() => setDeleteModal(null)} disabled={!!deleting}>Cancel</Button>
            </div>
          </div>
        </div>
      )}

      {/* Rebuild Confirmation Modal */}
      {rebuildModal && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50" onClick={() => setRebuildModal(null)}>
          <div className="bg-card border rounded-lg shadow-xl max-w-md w-full mx-4 p-6 space-y-4" onClick={(e) => e.stopPropagation()}>
            <div className="flex items-start gap-3">
              <div className="rounded-full p-2 bg-blue-500/10">
                <RefreshCw className="h-6 w-6 text-blue-500" />
              </div>
              <div className="flex-1 min-w-0">
                <h3 className="text-lg font-semibold">Rebuild Template</h3>
                <p className="text-sm text-muted-foreground mt-1">
                  <span className="font-medium text-foreground">{rebuildModal.name}</span> on {rebuildModal.target_name}
                </p>
              </div>
            </div>

            <div className="rounded-md border bg-muted/50 p-3 space-y-1 text-sm">
              <p className="font-medium">This will:</p>
              <p className="text-muted-foreground">• Create a new version: <span className="font-medium text-foreground">v{rebuildModal.version + 1}</span></p>
              <p className="text-muted-foreground">• Download the latest ISO from upstream</p>
              <p className="text-muted-foreground">• Build a fresh template with current packages</p>
              <p className="text-muted-foreground">• Mark the current version as superseded</p>
            </div>

            <p className="text-sm text-muted-foreground">
              The build will run in the background. You'll be taken to the build page to monitor progress.
            </p>

            <div className="flex justify-end gap-2">
              <Button variant="ghost" size="sm" onClick={() => setRebuildModal(null)} disabled={rebuilding === rebuildModal.id}>
                Cancel
              </Button>
              <Button
                size="sm"
                onClick={handleRebuildConfirm}
                disabled={rebuilding === rebuildModal.id}
              >
                {rebuilding === rebuildModal.id ? (
                  <>
                    <RefreshCw className="h-4 w-4 mr-1 animate-spin" />
                    Starting Build...
                  </>
                ) : (
                  <>
                    <Rocket className="h-4 w-4 mr-1" />
                    Start Rebuild
                  </>
                )}
              </Button>
            </div>
          </div>
        </div>
      )}

      {/* Template Detail Modal */}
      {detailModal && (() => {
        const isProxmox = detailModal.platform === "proxmox";
        const isVMware = detailModal.platform === "vmware";

        // Human-readable guest OS type
        const guestOSLabel = (() => {
          const id = detailModal.guest_id;
          if (!id) return "—";
          // Proxmox guest types
          if (id === "l26") return "Linux (2.6+ kernel)";
          if (id === "l24") return "Linux (2.4 kernel)";
          if (id === "other") return "Other";
          if (id.startsWith("win")) return id.replace("win", "Windows ");
          if (id === "solaris") return "Solaris";
          // VMware guest types — these are already descriptive
          if (id.includes("Guest")) return id.replace("Guest", "").replace(/64$/, " (64-bit)").replace(/([a-z])([A-Z])/g, "$1 $2");
          return id;
        })();

        return (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50" onClick={() => setDetailModal(null)}>
          <div className="bg-card border rounded-lg shadow-xl max-w-lg w-full mx-4 p-6 space-y-4 max-h-[85vh] overflow-y-auto" onClick={(e) => e.stopPropagation()}>
            <div className="flex items-start justify-between">
              <div>
                <h3 className="text-lg font-semibold">{detailModal.name}</h3>
                <div className="flex items-center gap-2 mt-1">
                  <Badge variant="outline" className="text-xs">{detailModal.os_type}</Badge>
                  <Badge variant="secondary" className="text-xs">{isProxmox ? "Proxmox" : isVMware ? "VMware" : detailModal.platform}</Badge>
                </div>
              </div>
              <Button variant="ghost" size="icon" onClick={() => setDetailModal(null)} aria-label="Close details">
                <X className="h-4 w-4" />
              </Button>
            </div>

            <div className="grid grid-cols-2 gap-3 text-sm">
              {/* Common fields */}
              <div>
                <p className="text-xs text-muted-foreground">CPU</p>
                <p className="font-medium">{detailModal.cpu} vCPU{isProxmox && detailModal.cpu_type ? ` (${detailModal.cpu_type})` : ""}</p>
              </div>
              <div>
                <p className="text-xs text-muted-foreground">Memory</p>
                <p className="font-medium">{detailModal.memory_mb >= 1024 ? `${(detailModal.memory_mb / 1024).toFixed(detailModal.memory_mb % 1024 ? 1 : 0)} GB` : `${detailModal.memory_mb} MB`}</p>
              </div>
              <div>
                <p className="text-xs text-muted-foreground">Disk</p>
                <p className="font-medium">
                  {detailModal.disk_gb ? `${detailModal.disk_gb} GB` : "—"}
                  {detailModal.disk_format ? ` (${detailModal.disk_format})` : ""}
                </p>
              </div>
              <div>
                <p className="text-xs text-muted-foreground">Guest OS</p>
                <p className="font-medium text-xs">{guestOSLabel}</p>
              </div>

              {/* Firmware */}
              {detailModal.firmware && (
                <div>
                  <p className="text-xs text-muted-foreground">Firmware</p>
                  <p className="font-medium uppercase">{detailModal.firmware === "seabios" ? "BIOS (SeaBIOS)" : detailModal.firmware === "ovmf" ? "UEFI (OVMF)" : detailModal.firmware}</p>
                </div>
              )}

              {/* VMware-specific fields */}
              {isVMware && detailModal.hardware_version && (
                <div>
                  <p className="text-xs text-muted-foreground">HW Version</p>
                  <p className="font-medium">{detailModal.hardware_version}</p>
                </div>
              )}
              {isVMware && detailModal.tools_status && (
                <div>
                  <p className="text-xs text-muted-foreground">VMware Tools</p>
                  <p className="font-medium">{detailModal.tools_status}</p>
                </div>
              )}
              {isVMware && detailModal.folder && (
                <div>
                  <p className="text-xs text-muted-foreground">VM Folder</p>
                  <p className="font-medium font-mono text-xs">{detailModal.folder}</p>
                </div>
              )}

              {/* Proxmox-specific fields */}
              {isProxmox && detailModal.node && (
                <div>
                  <p className="text-xs text-muted-foreground">Node</p>
                  <p className="font-medium">{detailModal.node}</p>
                </div>
              )}
              {isProxmox && detailModal.scsi_type && (
                <div>
                  <p className="text-xs text-muted-foreground">SCSI Controller</p>
                  <p className="font-medium">{detailModal.scsi_type}</p>
                </div>
              )}
              {isProxmox && detailModal.tools_status && (
                <div>
                  <p className="text-xs text-muted-foreground">QEMU Agent</p>
                  <p className="font-medium">{detailModal.tools_status}</p>
                </div>
              )}
              {isProxmox && (
                <div>
                  <p className="text-xs text-muted-foreground">Cloud-Init</p>
                  <p className="font-medium">{detailModal.cloud_init ? "Configured" : "Not configured"}</p>
                </div>
              )}

              {/* Storage — common but different label */}
              {detailModal.datastore && (
                <div className="col-span-2">
                  <p className="text-xs text-muted-foreground">{isProxmox ? "Storage" : "Datastore"}</p>
                  <p className="font-medium">{detailModal.datastore}</p>
                </div>
              )}

              {/* Networks */}
              {detailModal.networks && detailModal.networks.length > 0 && (
                <div className="col-span-2">
                  <p className="text-xs text-muted-foreground mb-1">{isProxmox ? "Network Bridges" : "Networks"}</p>
                  <div className="flex flex-wrap gap-1">
                    {detailModal.networks.map((n, i) => (
                      <Badge key={i} variant="outline" className="text-xs">{n}</Badge>
                    ))}
                  </div>
                </div>
              )}

              {/* Created date — VMware only (Proxmox doesn't provide it) */}
              {detailModal.created_at && (
                <div>
                  <p className="text-xs text-muted-foreground">Created</p>
                  <p className="font-medium">{formatDate(detailModal.created_at)}</p>
                </div>
              )}
            </div>

            {detailModal.annotation && (
              <div>
                <p className="text-xs text-muted-foreground mb-1">{isProxmox ? "Description" : "Annotation"}</p>
                <div className="rounded-md border bg-muted/50 p-3 text-sm whitespace-pre-wrap">{detailModal.annotation}</div>
              </div>
            )}

            <div className="pt-2 border-t">
              <p className="text-xs text-muted-foreground">{isProxmox ? "VMID" : "VM Reference (moref)"}</p>
              <p className="font-mono text-xs text-muted-foreground">{detailModal.moref}</p>
            </div>

            <div className="flex justify-end">
              <Button variant="outline" size="sm" onClick={() => setDetailModal(null)}>Close</Button>
            </div>
          </div>
        </div>
        );
      })()}

      {/* Update Check Modal */}
      {showUpdateModal && (
        <div className="fixed inset-0 bg-black/60 flex items-center justify-center z-50" onClick={() => !checking && setShowUpdateModal(false)}>
          <Card className="w-full max-w-lg mx-4" onClick={(e) => e.stopPropagation()}>
            <CardHeader>
              <CardTitle className="flex items-center gap-2">
                {checking ? <RefreshCw className="h-5 w-5 animate-spin" /> : <ShieldCheck className="h-5 w-5" />}
                ISO Update Check
              </CardTitle>
              <p className="text-sm text-muted-foreground">
                Comparing each template's ISO checksum against upstream release mirrors to detect newer images.
              </p>
            </CardHeader>
            <CardContent className="space-y-3">
              {updateProgress.map((item, idx) => (
                <div key={idx} className="flex items-center justify-between py-2 border-b last:border-0">
                  <span className="text-sm font-medium">{item.name}</span>
                  <div className="flex items-center gap-2">
                    {item.status === "pending" && (
                      <span className="text-xs text-muted-foreground">Waiting...</span>
                    )}
                    {item.status === "checking" && (
                      <span className="text-xs text-blue-500 flex items-center gap-1">
                        <RefreshCw className="h-3 w-3 animate-spin" /> Checking upstream ISO...
                      </span>
                    )}
                    {item.status === "up-to-date" && (
                      <span className="text-xs text-green-500 flex items-center gap-1">
                        <CheckCircle className="h-3 w-3" /> ISO unchanged
                      </span>
                    )}
                    {item.status === "update-available" && (
                      <span className="text-xs text-orange-500 flex items-center gap-1">
                        <AlertTriangle className="h-3 w-3" /> New ISO available
                      </span>
                    )}
                    {item.status === "error" && (
                      <span className="text-xs text-destructive flex items-center gap-1">
                        <XCircle className="h-3 w-3" /> Check failed
                      </span>
                    )}
                  </div>
                </div>
              ))}

              {!checking && updateProgress.length > 0 && (
                <div className="pt-3 border-t">
                  {updateProgress.every((p) => p.status === "up-to-date") ? (
                    <p className="text-sm text-green-500 flex items-center gap-2">
                      <CheckCircle className="h-4 w-4" />
                      All templates are up to date — ISO checksums match upstream mirrors.
                    </p>
                  ) : updateProgress.some((p) => p.status === "update-available") ? (
                    <p className="text-sm text-orange-500 flex items-center gap-2">
                      <AlertTriangle className="h-4 w-4" />
                      {updateProgress.filter((p) => p.status === "update-available").length} template{updateProgress.filter((p) => p.status === "update-available").length !== 1 ? "s have" : " has"} a newer ISO available. Rebuild from the template card to update.
                    </p>
                  ) : null}
                  <div className="flex justify-end mt-3">
                    <Button variant="outline" size="sm" onClick={() => setShowUpdateModal(false)}>Close</Button>
                  </div>
                </div>
              )}
            </CardContent>
          </Card>
        </div>
      )}
    </div>
  );
}
