import { useEffect, useMemo, useState, useCallback } from "react";
import { Link } from "react-router-dom";
import { vms as vmApi } from "@/api/client";
import type { ManagedVM } from "@/types";
import { Card, CardHeader, CardTitle, CardContent } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Search, Monitor, Cpu, MemoryStick, HardDrive, Power, RefreshCw, Play, Square, Info, Loader2, Rocket } from "lucide-react";
import { cn } from "@/lib/utils";
import { getErrorMessage } from "@/lib/utils";
import { Select } from "@/components/ui/select";
import { Pagination } from "@/components/ui/pagination";
import { SkeletonVMCard, Skeleton } from "@/components/ui/skeleton";
import { ViewToggle } from "@/components/ui/view-toggle";
import { PageHeader } from "@/components/ui/page-header";
import { usePreference } from "@/context/PreferencesContext";
import { SortableTh } from "@/components/ui/sortable-th";
import { useTableSort } from "@/hooks/useTableSort";

const ITEMS_PER_PAGE = 12;

type SortField = "vm_name" | "power_state" | "target_name";
type SortDir = "asc" | "desc";

const powerVariant = (state: string) => {
  if (state === "poweredOn" || state === "running") return "success" as const;
  if (state === "poweredOff" || state === "stopped") return "secondary" as const;
  if (state === "suspended") return "warning" as const;
  return "secondary" as const;
};

const powerLabel = (state: string) => {
  if (state === "poweredOn" || state === "running") return "Running";
  if (state === "poweredOff" || state === "stopped") return "Stopped";
  if (state === "suspended") return "Suspended";
  return state; // Fallback to raw state for unknown values
};

export default function VMs() {
  const [vmList, setVmList] = useState<ManagedVM[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [search, setSearch] = useState("");
  const [statusFilter, setStatusFilter] = useState<string>("all");
  const [targetFilter, setTargetFilter] = useState<string>("all");
  const [sortField, setSortField] = useState<SortField>("vm_name");
  const [sortDir, setSortDir] = useState<SortDir>("asc");
  const [page, setPage] = useState(1);
  const [syncing, setSyncing] = useState(false);
  const [actingOn, setActingOn] = useState<number | null>(null);

  const [lastRefreshed, setLastRefreshed] = useState<Date | null>(null);

  const reload = useCallback(() => {
    vmApi.list()
      .then((res) => { setVmList(res.data || []); setLastRefreshed(new Date()); })
      .catch(() => setError("Failed to load virtual machines"));
  }, []);

  useEffect(() => {
    vmApi.list()
      .then((res) => { setVmList(res.data || []); setLastRefreshed(new Date()); })
      .catch(() => setError("Failed to load virtual machines"))
      .finally(() => setLoading(false));
    // Auto-refresh every 30 seconds
    const timer = setInterval(reload, 30000);
    return () => clearInterval(timer);
  }, [reload]);

  // Derive unique targets and power states for filter dropdowns
  const targets = useMemo(() => [...new Set(vmList.map((v) => v.target_name).filter(Boolean))].sort(), [vmList]);
  const powerStates = useMemo(() => [...new Set(vmList.map((v) => v.power_state).filter(Boolean))].sort(), [vmList]);

  // Filter
  const filtered = useMemo(() => {
    return vmList.filter((v) => {
      const matchesSearch =
        (v.vm_name || "").toLowerCase().includes(search.toLowerCase()) ||
        (v.target_name || "").toLowerCase().includes(search.toLowerCase()) ||
        (v.ip_address || "").toLowerCase().includes(search.toLowerCase());
      const matchesStatus = statusFilter === "all" || v.power_state === statusFilter;
      const matchesTarget = targetFilter === "all" || v.target_name === targetFilter;
      return matchesSearch && matchesStatus && matchesTarget;
    });
  }, [vmList, search, statusFilter, targetFilter]);

  // Sort
  const viewMode = usePreference("view_mode", "cards");
  const { sorted: vmTableSorted, sortField: vmSortField, sortDir: vmSortDir, toggleSort: vmToggleSort } = useTableSort(filtered, "vm_name");

  const sorted = useMemo(() => {
    return [...filtered].sort((a, b) => {
      const aVal = (a[sortField] || "").toLowerCase();
      const bVal = (b[sortField] || "").toLowerCase();
      const cmp = aVal.localeCompare(bVal);
      return sortDir === "asc" ? cmp : -cmp;
    });
  }, [filtered, sortField, sortDir]);

  // Paginate
  const totalPages = Math.max(1, Math.ceil(sorted.length / ITEMS_PER_PAGE));
  const paginated = sorted.slice((page - 1) * ITEMS_PER_PAGE, page * ITEMS_PER_PAGE);

  // Reset to page 1 when filters change
  useEffect(() => { setPage(1); }, [search, statusFilter, targetFilter, sortField, sortDir]);

  const toggleSort = (field: SortField) => {
    if (sortField === field) {
      setSortDir((d) => (d === "asc" ? "desc" : "asc"));
    } else {
      setSortField(field);
      setSortDir("asc");
    }
  };

  const doSyncAll = async () => {
    setSyncing(true);
    try {
      await vmApi.syncAll();
      reload();
    } finally {
      setSyncing(false);
    }
  };

  const doQuickPower = async (e: React.MouseEvent, vmId: number, action: string) => {
    e.preventDefault(); // prevent Link navigation
    e.stopPropagation();
    setActingOn(vmId);
    try {
      await vmApi.power(vmId, action);
      // Update local state optimistically
      setVmList((prev) =>
        prev.map((v) =>
          v.id === vmId
            ? { ...v, power_state: action === "start" ? "poweredOn" : "poweredOff" }
            : v
        )
      );
    } catch {
      // silent - user can see state didn't change
    } finally {
      setActingOn(null);
    }
  };

  if (loading) {
    return (
      <div className="space-y-6">
        <div className="flex items-center justify-between">
          <Skeleton className="h-8 w-40" />
          <div className="flex items-center gap-3">
            <Skeleton className="h-10 w-64" />
            <Skeleton className="h-10 w-24" />
          </div>
        </div>
        <Skeleton className="h-16 w-full rounded-lg" />
        <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
          {[1, 2, 3, 4, 5, 6].map((i) => (
            <SkeletonVMCard key={i} />
          ))}
        </div>
      </div>
    );
  }

  if (error) {
    return <div className="text-center py-12 text-destructive">{error}</div>;
  }

  return (
    <div className="space-y-6">
      <PageHeader
        title={
          <span className="flex items-center gap-2">
            Virtual Machines
            {vmList.length > 0 && <Badge variant="outline">{vmList.length}</Badge>}
          </span>
        }
        description="Tracked virtual machines across your targets."
        actions={
          <>
            <div className="relative w-full sm:w-64">
              <Search className="absolute left-3 top-1/2 -translate-y-1/2 h-4 w-4 text-muted-foreground" />
              <Input
                placeholder="Search by name, target, IP..."
                value={search}
                onChange={(e) => setSearch(e.target.value)}
                className="pl-9"
              />
            </div>
            <ViewToggle />
            <Link to="/deploy">
              <Button size="sm" className="shrink-0">
                <Rocket className="h-4 w-4 mr-1" />
                Deploy VM
              </Button>
            </Link>
            <Button variant="outline" size="sm" onClick={doSyncAll} disabled={syncing} className="shrink-0">
              <RefreshCw className={`h-4 w-4 mr-1 ${syncing ? "animate-spin" : ""}`} />
              {syncing ? "Syncing..." : "Sync All"}
            </Button>
          </>
        }
      />

      {/* Filter Bar */}
      <div className="flex flex-wrap items-center gap-3">
        <Select
          value={statusFilter}
          onChange={(e) => setStatusFilter(e.target.value)}
          className="w-auto"
          aria-label="Filter by power state"
        >
          <option value="all">All States</option>
          {powerStates.map((s) => (
            <option key={s} value={s}>{s}</option>
          ))}
        </Select>
        <Select
          value={targetFilter}
          onChange={(e) => setTargetFilter(e.target.value)}
          className="w-auto"
          aria-label="Filter by target"
        >
          <option value="all">All Targets</option>
          {targets.map((t) => (
            <option key={t} value={t}>{t}</option>
          ))}
        </Select>

      </div>

      {paginated.length === 0 ? (
        <div className="text-center py-12 text-muted-foreground">
          <Monitor className="h-12 w-12 mx-auto mb-4 opacity-50" />
          <p>{vmList.length === 0 ? "No VMs deployed yet" : "No VMs match current filters"}</p>
          {vmList.length === 0 && (
            <p className="text-sm mt-1">Deploy a VM from the Deploy page and it will appear here automatically.</p>
          )}
        </div>
      ) : (
        <>
          {viewMode === "table" ? (
          <div className="rounded-md border">
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b bg-muted/50">
                  <SortableTh label="ID" field="id" currentField={vmSortField} currentDir={vmSortDir} onSort={vmToggleSort} className="w-16" />
                  <SortableTh label="Name" field="vm_name" currentField={vmSortField} currentDir={vmSortDir} onSort={vmToggleSort} />
                  <SortableTh label="IP Address" field="ip_address" currentField={vmSortField} currentDir={vmSortDir} onSort={vmToggleSort} className="hidden sm:table-cell" />
                  <th className="text-left px-4 py-2 font-medium hidden md:table-cell">Specs</th>
                  <SortableTh label="Target" field="target_name" currentField={vmSortField} currentDir={vmSortDir} onSort={vmToggleSort} className="hidden lg:table-cell" />
                  <SortableTh label="Status" field="power_state" currentField={vmSortField} currentDir={vmSortDir} onSort={vmToggleSort} />
                  <th className="text-right px-4 py-2 font-medium">Power</th>
                </tr>
              </thead>
              <tbody>
                {vmTableSorted.slice((page - 1) * ITEMS_PER_PAGE, page * ITEMS_PER_PAGE).map((vm) => (
                  <tr key={vm.id} className="border-b last:border-0 hover:bg-muted/30 transition-colors">
                    <td className="px-4 py-2.5 text-muted-foreground font-mono text-xs">#{vm.id}</td>
                    <td className="px-4 py-2.5">
                      <Link to={`/vms/${vm.id}`} className="font-medium hover:text-primary transition-colors">
                        {vm.vm_name}
                      </Link>
                    </td>
                    <td className="px-4 py-2.5 font-mono text-xs text-muted-foreground hidden sm:table-cell">
                      {vm.ip_address || "—"}
                    </td>
                    <td className="px-4 py-2.5 text-muted-foreground hidden md:table-cell">
                      {vm.cpu || "?"}C · {vm.memory_mb ? `${Math.round(vm.memory_mb / 1024)}G` : "?"} · {vm.disk_gb || "?"}G
                    </td>
                    <td className="px-4 py-2.5 text-muted-foreground hidden lg:table-cell">{vm.target_name}</td>
                    <td className="px-4 py-2.5">
                      <Badge variant={powerVariant(vm.power_state)}>
                        <Power className="h-3 w-3 mr-1" />
                        {powerLabel(vm.power_state)}
                      </Badge>
                    </td>
                    <td className="px-4 py-2.5 text-right">
                      {(vm.power_state === "poweredOn" || vm.power_state === "running") ? (
                        <Button variant="ghost" size="sm" className="h-7 w-7 p-0" onClick={(e) => doQuickPower(e, vm.id, "stop")} disabled={actingOn === vm.id} title="Stop">
                          <Square className="h-3.5 w-3.5" />
                        </Button>
                      ) : (vm.power_state === "poweredOff" || vm.power_state === "stopped") ? (
                        <Button variant="ghost" size="sm" className="h-7 w-7 p-0" onClick={(e) => doQuickPower(e, vm.id, "start")} disabled={actingOn === vm.id} title="Start">
                          <Play className="h-3.5 w-3.5" />
                        </Button>
                      ) : null}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
          ) : (
          <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
            {paginated.map((vm) => (
              <Link key={vm.id} to={`/vms/${vm.id}`}>
                <Card className="hover:border-primary/50 transition-colors cursor-pointer">
                  <CardHeader className="pb-3">
                    <div className="flex items-center justify-between">
                      <CardTitle className="text-base">{vm.vm_name}</CardTitle>
                      <div className="flex items-center gap-1">
                        {/* Quick power toggle */}
                        {(vm.power_state === "poweredOn" || vm.power_state === "running") ? (
                          <Button
                            variant="ghost"
                            size="sm"
                            className="h-6 w-6 p-0"
                            onClick={(e) => doQuickPower(e, vm.id, "stop")}
                            disabled={actingOn === vm.id}
                            title="Stop VM"
                          >
                            <Square className="h-3 w-3" />
                          </Button>
                        ) : (vm.power_state === "poweredOff" || vm.power_state === "stopped") ? (
                          <Button
                            variant="ghost"
                            size="sm"
                            className="h-6 w-6 p-0"
                            onClick={(e) => doQuickPower(e, vm.id, "start")}
                            disabled={actingOn === vm.id}
                            title="Start VM"
                          >
                            <Play className="h-3 w-3" />
                          </Button>
                        ) : null}
                        <Badge variant={powerVariant(vm.power_state)}>
                          <Power className="h-3 w-3 mr-1" />
                          {powerLabel(vm.power_state)}
                        </Badge>
                      </div>
                    </div>
                    <p className="text-xs text-muted-foreground">
                      {vm.target_name}
                      <span className="font-mono ml-1 opacity-60">· #{vm.id}</span>
                    </p>
                  </CardHeader>
                  <CardContent>
                    <div className="flex gap-4 text-sm text-muted-foreground">
                      {vm.ip_address && (
                        <span className="font-mono text-xs">{vm.ip_address}</span>
                      )}
                      <div className="flex items-center gap-1">
                        <Cpu className="h-3.5 w-3.5" />
                        <span>{vm.cpu || "?"} vCPU</span>
                      </div>
                      <div className="flex items-center gap-1">
                        <MemoryStick className="h-3.5 w-3.5" />
                        <span>{vm.memory_mb ? `${Math.round(vm.memory_mb / 1024)} GB` : "?"}</span>
                      </div>
                      <div className="flex items-center gap-1">
                        <HardDrive className="h-3.5 w-3.5" />
                        <span>{vm.disk_gb ? `${vm.disk_gb} GB` : "?"}</span>
                      </div>
                    </div>
                  </CardContent>
                </Card>
              </Link>
            ))}
          </div>
          )}

          {/* Pagination */}
          <Pagination page={page} totalPages={totalPages} onPageChange={setPage} />
        </>
      )}
    </div>
  );
}
