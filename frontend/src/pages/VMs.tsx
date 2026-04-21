import { useEffect, useMemo, useState, useCallback } from "react";
import { Link, useNavigate } from "react-router-dom";
import { vms as vmApi } from "@/api/client";
import type { ManagedVM } from "@/types";
import { Card, CardContent } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  Search, Monitor, Cpu, MemoryStick, HardDrive, Power, RefreshCw,
  Play, Square, Rocket, MoreHorizontal, ExternalLink, Terminal,
  Camera, RotateCw, X, Box,
} from "lucide-react";
import { cn, getErrorMessage } from "@/lib/utils";
import { Select } from "@/components/ui/select";
import { Pagination } from "@/components/ui/pagination";
import { SkeletonVMCard, Skeleton } from "@/components/ui/skeleton";
import { ViewToggle } from "@/components/ui/view-toggle";
import { PageHeader } from "@/components/ui/page-header";
import { usePreference } from "@/context/PreferencesContext";
import { SortableTh } from "@/components/ui/sortable-th";
import { useTableSort } from "@/hooks/useTableSort";
import { usePageSize } from "@/hooks/usePageSize";
import { OSBadge } from "@/components/OSBadge";
import { DropdownMenu, DropdownMenuItem, DropdownMenuSeparator } from "@/components/ui/dropdown-menu";
import { useToast } from "@/components/ui/toast";

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
  return state;
};

const isRunning = (state: string) => state === "poweredOn" || state === "running";
const isStopped = (state: string) => state === "poweredOff" || state === "stopped";

function timeAgo(iso?: string | null): string {
  if (!iso) return "never";
  const then = new Date(iso).getTime();
  if (Number.isNaN(then)) return "never";
  const diffS = Math.max(0, Math.floor((Date.now() - then) / 1000));
  if (diffS < 45) return "just now";
  if (diffS < 90) return "1m ago";
  const diffM = Math.floor(diffS / 60);
  if (diffM < 60) return `${diffM}m ago`;
  const diffH = Math.floor(diffM / 60);
  if (diffH < 24) return `${diffH}h ago`;
  const diffD = Math.floor(diffH / 24);
  if (diffD < 30) return `${diffD}d ago`;
  return new Date(iso).toLocaleDateString();
}

function copyToClipboard(text: string): Promise<void> {
  if (navigator.clipboard && window.isSecureContext) return navigator.clipboard.writeText(text);
  const ta = document.createElement("textarea");
  ta.value = text;
  ta.style.position = "fixed";
  ta.style.opacity = "0";
  document.body.appendChild(ta);
  ta.select();
  document.execCommand("copy");
  document.body.removeChild(ta);
  return Promise.resolve();
}

export default function VMs() {
  const navigate = useNavigate();
  const { toast } = useToast();
  const [vmList, setVmList] = useState<ManagedVM[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [search, setSearch] = useState("");
  const [statusFilter, setStatusFilter] = useState<string>("all");
  const [targetFilter, setTargetFilter] = useState<string>("all");
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = usePageSize("vms", 25);
  const [syncing, setSyncing] = useState(false);
  const [syncingOne, setSyncingOne] = useState<number | null>(null);
  const [actingOn, setActingOn] = useState<number | null>(null);

  const reload = useCallback(() => {
    vmApi.list()
      .then((res) => setVmList(res.data || []))
      .catch(() => setError("Failed to load virtual machines"));
  }, []);

  useEffect(() => {
    vmApi.list()
      .then((res) => setVmList(res.data || []))
      .catch(() => setError("Failed to load virtual machines"))
      .finally(() => setLoading(false));
    const timer = setInterval(reload, 30000);
    return () => clearInterval(timer);
  }, [reload]);

  const targets = useMemo(
    () => [...new Set(vmList.map((v) => v.target_name).filter(Boolean))].sort(),
    [vmList]
  );
  const powerStates = useMemo(
    () => [...new Set(vmList.map((v) => v.power_state).filter(Boolean))].sort(),
    [vmList]
  );

  // Freshness readout: most recent last_synced_at across the list
  const mostRecentSync = useMemo(() => {
    let latest: number | null = null;
    for (const v of vmList) {
      if (!v.last_synced_at) continue;
      const t = new Date(v.last_synced_at).getTime();
      if (!Number.isNaN(t) && (latest === null || t > latest)) latest = t;
    }
    return latest ? new Date(latest).toISOString() : null;
  }, [vmList]);

  // Filters + sort share a single sorted array used by both views.
  const filtered = useMemo(() => {
    const q = search.toLowerCase();
    return vmList.filter((v) => {
      const matchesSearch =
        !q ||
        (v.vm_name || "").toLowerCase().includes(q) ||
        (v.target_name || "").toLowerCase().includes(q) ||
        (v.ip_address || "").toLowerCase().includes(q) ||
        (v.os_type || "").toLowerCase().includes(q) ||
        (v.template_name || "").toLowerCase().includes(q);
      const matchesStatus = statusFilter === "all" || v.power_state === statusFilter;
      const matchesTarget = targetFilter === "all" || v.target_name === targetFilter;
      return matchesSearch && matchesStatus && matchesTarget;
    });
  }, [vmList, search, statusFilter, targetFilter]);

  const viewMode = usePreference("view_mode", "cards");
  const { sorted, sortField, sortDir, toggleSort } = useTableSort(filtered, "vm_name");

  const paginated = sorted.slice((page - 1) * pageSize, page * pageSize);

  useEffect(() => {
    setPage(1);
  }, [search, statusFilter, targetFilter, sortField, sortDir, pageSize]);

  const activeFilters =
    (search ? 1 : 0) + (statusFilter !== "all" ? 1 : 0) + (targetFilter !== "all" ? 1 : 0);
  const clearFilters = () => {
    setSearch("");
    setStatusFilter("all");
    setTargetFilter("all");
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

  const doSyncOne = async (vmId: number) => {
    setSyncingOne(vmId);
    try {
      const res = await vmApi.sync(vmId);
      setVmList((prev) => prev.map((v) => (v.id === vmId ? { ...v, ...res.data } : v)));
    } catch (e) {
      toast(getErrorMessage(e, "Failed to sync VM"), "error");
    } finally {
      setSyncingOne(null);
    }
  };

  const doQuickPower = async (e: React.MouseEvent | React.SyntheticEvent, vmId: number, action: string) => {
    e.preventDefault();
    e.stopPropagation();
    setActingOn(vmId);
    try {
      await vmApi.power(vmId, action);
      setVmList((prev) =>
        prev.map((v) =>
          v.id === vmId
            ? { ...v, power_state: action === "start" ? "poweredOn" : "poweredOff" }
            : v
        )
      );
    } catch (err) {
      toast(getErrorMessage(err, "Power action failed"), "error");
    } finally {
      setActingOn(null);
    }
  };

  const doCopySSH = async (vm: ManagedVM) => {
    if (!vm.ip_address) {
      toast("This VM has no IP address yet", "error");
      return;
    }
    // Try to fetch credentials for a full "ssh user@ip" command. Falls back
    // to "ssh <ip>" if this VM wasn't deployed through Forgemill or creds
    // aren't retrievable.
    let cmd = `ssh ${vm.ip_address}`;
    try {
      const res = await vmApi.credentials(vm.id);
      if (res.data?.username) cmd = `ssh ${res.data.username}@${vm.ip_address}`;
    } catch {
      // ignore — use fallback
    }
    try {
      await copyToClipboard(cmd);
      toast(`Copied: ${cmd}`);
    } catch {
      toast("Could not copy to clipboard", "error");
    }
  };

  const doCopyIP = async (vm: ManagedVM) => {
    if (!vm.ip_address) {
      toast("This VM has no IP address yet", "error");
      return;
    }
    try {
      await copyToClipboard(vm.ip_address);
      toast(`Copied ${vm.ip_address}`);
    } catch {
      toast("Could not copy to clipboard", "error");
    }
  };

  const doOpenConsole = async (vm: ManagedVM) => {
    try {
      const res = await vmApi.console(vm.id);
      if (res.data?.url) {
        window.open(res.data.url, "_blank", "noopener,noreferrer");
      } else {
        toast("No console URL returned", "error");
      }
    } catch (e) {
      toast(getErrorMessage(e, "Could not open console"), "error");
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

  // Row actions menu used in both views. Centralised so both views render
  // the same menu items.
  const RowActions = ({ vm }: { vm: ManagedVM }) => (
    <DropdownMenu
      trigger={
        <Button variant="ghost" size="sm" className="h-7 w-7 p-0" aria-label="VM actions">
          <MoreHorizontal className="h-4 w-4" />
        </Button>
      }
    >
      <DropdownMenuItem icon={<ExternalLink />} onClick={() => navigate(`/vms/${vm.id}`)}>
        Open detail
      </DropdownMenuItem>
      <DropdownMenuItem icon={<Monitor />} disabled={!vm.ip_address} onClick={() => doOpenConsole(vm)}>
        Open console
      </DropdownMenuItem>
      <DropdownMenuSeparator />
      <DropdownMenuItem icon={<Terminal />} disabled={!vm.ip_address} onClick={() => doCopySSH(vm)}>
        Copy SSH command
      </DropdownMenuItem>
      <DropdownMenuItem icon={<Box />} disabled={!vm.ip_address} onClick={() => doCopyIP(vm)}>
        Copy IP address
      </DropdownMenuItem>
      <DropdownMenuSeparator />
      <DropdownMenuItem icon={<Camera />} onClick={() => navigate(`/vms/${vm.id}?tab=snapshots`)}>
        Snapshots
      </DropdownMenuItem>
      <DropdownMenuItem
        icon={<RotateCw className={syncingOne === vm.id ? "animate-spin" : ""} />}
        disabled={syncingOne === vm.id}
        onClick={() => doSyncOne(vm.id)}
      >
        Sync now
      </DropdownMenuItem>
    </DropdownMenu>
  );

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
                placeholder="Search name, IP, target, OS..."
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
            <Button variant="outline" size="sm" onClick={doSyncAll} disabled={syncing} className="shrink-0" title={mostRecentSync ? `Last hypervisor sync: ${timeAgo(mostRecentSync)}` : undefined}>
              <RefreshCw className={cn("h-4 w-4 mr-1", syncing && "animate-spin")} />
              {syncing ? "Syncing..." : "Sync All"}
            </Button>
          </>
        }
      />

      {/* Filter + freshness bar */}
      <div className="flex flex-wrap items-center gap-3">
        <Select
          value={statusFilter}
          onChange={(e) => setStatusFilter(e.target.value)}
          className="w-auto"
          aria-label="Filter by power state"
        >
          <option value="all">All states</option>
          {powerStates.map((s) => (
            <option key={s} value={s}>{powerLabel(s)}</option>
          ))}
        </Select>
        <Select
          value={targetFilter}
          onChange={(e) => setTargetFilter(e.target.value)}
          className="w-auto"
          aria-label="Filter by target"
        >
          <option value="all">All targets</option>
          {targets.map((t) => (
            <option key={t} value={t}>{t}</option>
          ))}
        </Select>

        {activeFilters > 0 && (
          <button
            onClick={clearFilters}
            className="inline-flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground border border-border rounded-md px-2 py-1 transition-colors"
            title="Clear all filters"
          >
            <X className="h-3 w-3" />
            {activeFilters} filter{activeFilters > 1 ? "s" : ""} active · Clear
          </button>
        )}

        {mostRecentSync && (
          <span className="ml-auto text-xs text-muted-foreground">
            Synced <span title={new Date(mostRecentSync).toLocaleString()}>{timeAgo(mostRecentSync)}</span>
          </span>
        )}
      </div>

      {paginated.length === 0 ? (
        <div className="text-center py-12 text-muted-foreground">
          <Monitor className="h-12 w-12 mx-auto mb-4 opacity-50" />
          <p>{vmList.length === 0 ? "No VMs deployed yet" : "No VMs match current filters"}</p>
          {vmList.length === 0 ? (
            <p className="text-sm mt-1">Deploy a VM from the Deploy page and it will appear here automatically.</p>
          ) : (
            <button onClick={clearFilters} className="text-sm mt-2 text-primary hover:underline">
              Clear filters
            </button>
          )}
        </div>
      ) : (
        <>
          {viewMode === "table" ? (
            <div className="rounded-md border">
              <table className="w-full text-sm">
                <thead>
                  <tr className="border-b bg-muted/50">
                    <SortableTh label="ID" field="id" currentField={sortField} currentDir={sortDir} onSort={toggleSort} className="w-16" />
                    <SortableTh label="Name" field="vm_name" currentField={sortField} currentDir={sortDir} onSort={toggleSort} />
                    <SortableTh label="IP Address" field="ip_address" currentField={sortField} currentDir={sortDir} onSort={toggleSort} className="hidden sm:table-cell" />
                    <th className="text-left px-4 py-2 font-medium hidden md:table-cell">Specs</th>
                    <SortableTh label="Target" field="target_name" currentField={sortField} currentDir={sortDir} onSort={toggleSort} className="hidden lg:table-cell" />
                    <SortableTh label="Status" field="power_state" currentField={sortField} currentDir={sortDir} onSort={toggleSort} />
                    <th className="text-right px-4 py-2 font-medium w-24">Actions</th>
                  </tr>
                </thead>
                <tbody>
                  {paginated.map((vm) => (
                    <tr
                      key={vm.id}
                      onClick={() => navigate(`/vms/${vm.id}`)}
                      className="border-b last:border-0 hover:bg-muted/30 transition-colors cursor-pointer"
                    >
                      <td className="px-4 py-2.5 text-muted-foreground font-mono text-xs">#{vm.id}</td>
                      <td className="px-4 py-2.5">
                        <div className="flex items-center gap-2 flex-wrap">
                          <span className="font-medium">{vm.vm_name}</span>
                          <OSBadge osType={vm.os_type} platform={vm.platform} size="xs" />
                          {vm.template_name && (
                            <span className="text-[10px] text-muted-foreground/70 font-mono" title={`From template ${vm.template_name}`}>
                              {vm.template_name}
                            </span>
                          )}
                        </div>
                      </td>
                      <td className="px-4 py-2.5 font-mono text-xs text-muted-foreground hidden sm:table-cell">
                        {vm.ip_address || "—"}
                      </td>
                      <td className="px-4 py-2.5 text-muted-foreground hidden md:table-cell whitespace-nowrap">
                        {vm.cpu || "?"}C · {vm.memory_mb ? `${Math.round(vm.memory_mb / 1024)}G` : "?"} · {vm.disk_gb || "?"}G
                      </td>
                      <td className="px-4 py-2.5 text-muted-foreground hidden lg:table-cell">{vm.target_name}</td>
                      <td className="px-4 py-2.5">
                        <Badge variant={powerVariant(vm.power_state)}>
                          <Power className="h-3 w-3 mr-1" />
                          {powerLabel(vm.power_state)}
                        </Badge>
                      </td>
                      <td className="px-2 py-2.5 text-right" onClick={(e) => e.stopPropagation()}>
                        <div className="flex items-center justify-end gap-0.5">
                          {isRunning(vm.power_state) ? (
                            <Button variant="ghost" size="sm" className="h-7 w-7 p-0" onClick={(e) => doQuickPower(e, vm.id, "stop")} disabled={actingOn === vm.id} title="Stop">
                              <Square className="h-3.5 w-3.5" />
                            </Button>
                          ) : isStopped(vm.power_state) ? (
                            <Button variant="ghost" size="sm" className="h-7 w-7 p-0" onClick={(e) => doQuickPower(e, vm.id, "start")} disabled={actingOn === vm.id} title="Start">
                              <Play className="h-3.5 w-3.5" />
                            </Button>
                          ) : null}
                          <RowActions vm={vm} />
                        </div>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          ) : (
            <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
              {paginated.map((vm) => (
                <Card
                  key={vm.id}
                  onClick={() => navigate(`/vms/${vm.id}`)}
                  className="hover:border-primary/50 transition-colors cursor-pointer"
                >
                  <CardContent className="p-4 space-y-3">
                    {/* Header row */}
                    <div className="flex items-start justify-between gap-2">
                      <div className="min-w-0 flex-1">
                        <div className="flex items-center gap-2 flex-wrap">
                          <span className="font-semibold text-foreground truncate">{vm.vm_name}</span>
                          <OSBadge osType={vm.os_type} platform={vm.platform} size="xs" />
                        </div>
                        <div className="text-xs text-muted-foreground mt-0.5 flex items-center gap-1.5 flex-wrap">
                          <span>{vm.target_name}</span>
                          <span className="font-mono opacity-60">· #{vm.id}</span>
                        </div>
                        {vm.template_name && (
                          <div className="text-[11px] text-muted-foreground/80 mt-1 truncate">
                            from <span className="font-mono">{vm.template_name}</span>
                          </div>
                        )}
                      </div>
                      <div className="flex items-center gap-1 shrink-0" onClick={(e) => e.stopPropagation()}>
                        {isRunning(vm.power_state) ? (
                          <Button variant="ghost" size="sm" className="h-7 w-7 p-0" onClick={(e) => doQuickPower(e, vm.id, "stop")} disabled={actingOn === vm.id} title="Stop">
                            <Square className="h-3 w-3" />
                          </Button>
                        ) : isStopped(vm.power_state) ? (
                          <Button variant="ghost" size="sm" className="h-7 w-7 p-0" onClick={(e) => doQuickPower(e, vm.id, "start")} disabled={actingOn === vm.id} title="Start">
                            <Play className="h-3 w-3" />
                          </Button>
                        ) : null}
                        <RowActions vm={vm} />
                      </div>
                    </div>

                    {/* Status + IP row */}
                    <div className="flex items-center justify-between gap-2">
                      <Badge variant={powerVariant(vm.power_state)}>
                        <Power className="h-3 w-3 mr-1" />
                        {powerLabel(vm.power_state)}
                      </Badge>
                      {vm.ip_address && (
                        <span className="font-mono text-xs text-muted-foreground truncate" title={vm.ip_address}>
                          {vm.ip_address}
                        </span>
                      )}
                    </div>

                    {/* Resources */}
                    <div className="grid grid-cols-3 gap-2 pt-1">
                      <div className="flex flex-col items-start">
                        <div className="flex items-center gap-1 text-muted-foreground">
                          <Cpu className="h-3 w-3" />
                          <span className="text-[10px] uppercase tracking-wider">CPU</span>
                        </div>
                        <span className="text-sm font-mono">{vm.cpu || "?"}</span>
                      </div>
                      <div className="flex flex-col items-start">
                        <div className="flex items-center gap-1 text-muted-foreground">
                          <MemoryStick className="h-3 w-3" />
                          <span className="text-[10px] uppercase tracking-wider">RAM</span>
                        </div>
                        <span className="text-sm font-mono">
                          {vm.memory_mb ? `${Math.round(vm.memory_mb / 1024)}G` : "?"}
                        </span>
                      </div>
                      <div className="flex flex-col items-start">
                        <div className="flex items-center gap-1 text-muted-foreground">
                          <HardDrive className="h-3 w-3" />
                          <span className="text-[10px] uppercase tracking-wider">Disk</span>
                        </div>
                        <span className="text-sm font-mono">{vm.disk_gb ? `${vm.disk_gb}G` : "?"}</span>
                      </div>
                    </div>

                    {/* Footer */}
                    <div className="flex items-center justify-between text-[11px] text-muted-foreground pt-1 border-t border-border">
                      <span title={vm.last_synced_at ? new Date(vm.last_synced_at).toLocaleString() : undefined}>
                        Synced {timeAgo(vm.last_synced_at)}
                      </span>
                      <span className="inline-flex items-center gap-1 text-primary/80 group-hover:text-primary">
                        Details →
                      </span>
                    </div>
                  </CardContent>
                </Card>
              ))}
            </div>
          )}

          <Pagination
            page={page}
            pageSize={pageSize}
            totalItems={sorted.length}
            onPageChange={setPage}
            onPageSizeChange={setPageSize}
            itemLabel="VMs"
          />
        </>
      )}
    </div>
  );
}
