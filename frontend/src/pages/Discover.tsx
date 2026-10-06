import { useCallback, useEffect, useMemo, useState } from "react";
import { Link, useNavigate, useParams } from "react-router-dom";
import { targets as targetApi } from "@/api/client";
import type { DiscoverResult, DiscoveredVM, Target } from "@/types";
import { Card, CardContent } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { PageHeader } from "@/components/ui/page-header";
import { InfoTip } from "@/components/ui/tooltip";
import { useToast } from "@/components/ui/toast";
import { useConfirm } from "@/components/ui/confirm-dialog";
import { useAuth } from "@/hooks/useAuth";
import { useTimezone } from "@/hooks/useTimezone";
import { getErrorMessage } from "@/lib/utils";
import { powerVariant, powerLabel } from "@/lib/status";
import ProviderIcon from "@/components/ProviderIcon";
import { AlertTriangle, EyeOff, Eye, Loader2, RefreshCw, Search, Import } from "lucide-react";

/*
  Discover: everything the hypervisor has on this target that Forgemill does
  not manage. A live read (one listing call), never cached — the header says
  when it was taken. Adopt takes VMs under management; Ignore hides noise
  (appliances, other teams' VMs) per target until un-ignored.
*/
export default function Discover() {
  const { id } = useParams<{ id: string }>();
  const targetId = Number(id);
  const navigate = useNavigate();
  const { toast } = useToast();
  const { confirm } = useConfirm();
  const { user } = useAuth();
  const { formatDateTime } = useTimezone();

  const [target, setTarget] = useState<Target | null>(null);
  const [result, setResult] = useState<DiscoverResult | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [showIgnored, setShowIgnored] = useState(false);
  const [search, setSearch] = useState("");
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [working, setWorking] = useState<"adopt" | "ignore" | "unignore" | null>(null);

  // Viewers never see the write controls; users/admins see them and the
  // server decides (vm_adoption_role) — a 403 is explained in the toast.
  const canWrite = user?.role === "admin" || user?.role === "user";

  const load = useCallback(async () => {
    if (isNaN(targetId)) return;
    setLoading(true);
    try {
      const [t, r] = await Promise.all([targetApi.get(targetId), targetApi.discover(targetId, showIgnored)]);
      setTarget(t.data);
      setResult(r.data);
      setError(null);
      setSelected((prev) => {
        const next = new Set<string>();
        for (const vm of r.data.vms) if (prev.has(vm.ref)) next.add(vm.ref);
        return next;
      });
    } catch (e: unknown) {
      setError(getErrorMessage(e, "Could not read the target's inventory"));
    } finally {
      setLoading(false);
    }
  }, [targetId, showIgnored]);

  useEffect(() => { load(); }, [load]);

  const visible = useMemo(() => {
    const q = search.trim().toLowerCase();
    const vms = result?.vms ?? [];
    if (!q) return vms;
    return vms.filter((vm) => vm.name.toLowerCase().includes(q) || (vm.ip_address || "").includes(q) || (vm.guest_id || "").toLowerCase().includes(q) || (vm.host || "").toLowerCase().includes(q));
  }, [result, search]);

  const selectedVMs = visible.filter((vm) => selected.has(vm.ref));
  const allVisibleSelected = visible.length > 0 && visible.every((vm) => selected.has(vm.ref));
  const toggleAll = () => {
    setSelected((prev) => {
      const next = new Set(prev);
      if (allVisibleSelected) visible.forEach((vm) => next.delete(vm.ref));
      else visible.forEach((vm) => next.add(vm.ref));
      return next;
    });
  };
  const toggle = (ref: string) => setSelected((prev) => { const next = new Set(prev); if (next.has(ref)) next.delete(ref); else next.add(ref); return next; });

  const permissionHint = (e: unknown, fallback: string) => {
    const msg = getErrorMessage(e, fallback);
    return /insufficient permissions/i.test(msg) ? "Adopting VMs is limited to admins (change this under Settings → Preferences → VM adoption)" : msg;
  };

  const doAdopt = async () => {
    const toAdopt = selectedVMs.filter((vm) => !vm.ignored || showIgnored);
    if (toAdopt.length === 0) return;
    const ok = await confirm({
      title: `Adopt ${toAdopt.length === 1 ? toAdopt[0].name : `${toAdopt.length} VMs`}`,
      message: `Forgemill will start managing ${toAdopt.length === 1 ? "this VM" : "these VMs"} on ${target?.name ?? "the target"}. Nothing on the hypervisor changes.`,
      consequences: [
        "Each VM gets a Forgemill record and is synced right away (power, address, sizes, guest OS).",
        "Power, snapshots, disks, network adapters, sync and destroy work immediately.",
        "Running actions needs SSH credentials — set them on the VM page afterwards.",
      ],
      confirmLabel: `Adopt ${toAdopt.length === 1 ? "VM" : `${toAdopt.length} VMs`}`,
    });
    if (!ok) return;
    setWorking("adopt");
    try {
      const res = await targetApi.adopt(targetId, toAdopt.map((vm) => vm.ref));
      const n = res.data.adopted.length;
      const skipped = res.data.skipped.length;
      toast(`${n} VM${n === 1 ? "" : "s"} adopted${skipped ? ` · ${skipped} skipped (${res.data.skipped.map((s) => `${s.ref}: ${s.reason}`).join("; ")})` : ""}`);
      if (n > 0) navigate("/vms?origin=adopted");
      else load();
    } catch (e: unknown) {
      toast(permissionHint(e, "Failed to adopt VMs"), "error");
    } finally {
      setWorking(null);
    }
  };

  const doIgnore = async (vms: DiscoveredVM[]) => {
    if (vms.length === 0) return;
    setWorking("ignore");
    try {
      const names: Record<string, string> = {};
      vms.forEach((vm) => { names[vm.ref] = vm.name; });
      await targetApi.ignore(targetId, vms.map((vm) => vm.ref), names);
      toast(`${vms.length} VM${vms.length === 1 ? "" : "s"} ignored — hidden from Discover until un-ignored`);
      load();
    } catch (e: unknown) {
      toast(permissionHint(e, "Failed to ignore VMs"), "error");
    } finally {
      setWorking(null);
    }
  };

  const doUnignore = async (vms: DiscoveredVM[]) => {
    if (vms.length === 0) return;
    setWorking("unignore");
    try {
      await targetApi.unignore(targetId, vms.map((vm) => vm.ref));
      toast(`${vms.length} VM${vms.length === 1 ? "" : "s"} back in Discover`);
      load();
    } catch (e: unknown) {
      toast(permissionHint(e, "Failed to un-ignore VMs"), "error");
    } finally {
      setWorking(null);
    }
  };

  const fmtMem = (mb: number) => (mb >= 1024 ? `${(mb / 1024).toFixed(mb % 1024 ? 1 : 0)} GB` : `${mb} MB`);

  return (
    <div className="space-y-6">
      <PageHeader
        title={
          <span className="flex items-center gap-2">
            {target && <ProviderIcon type={target.type} size={20} />}
            Discover VMs{target ? ` on ${target.name}` : ""}
            <InfoTip text="Everything the hypervisor reports on this target that Forgemill doesn't manage, read live right now (templates excluded). Adopt takes VMs under management without changing anything on the hypervisor; Ignore hides VMs that will never be Forgemill's business." />
          </span>
        }
        description={
          result
            ? `${result.unmanaged} unmanaged · ${result.managed} managed · ${result.ignored} ignored · read ${formatDateTime(result.computed_at)}`
            : "Reading the target's inventory…"
        }
        actions={
          <>
            <Link to="/targets"><Button variant="ghost" size="sm">All targets</Button></Link>
            <Button variant="outline" size="sm" className="gap-1.5" onClick={load} disabled={loading}>
              <RefreshCw className={`h-3.5 w-3.5 ${loading ? "animate-spin" : ""}`} /> Refresh
            </Button>
          </>
        }
      />

      {error && (
        <div className="rounded-md border border-warning/30 bg-warning/5 px-3 py-2 flex items-start gap-2">
          <AlertTriangle className="h-4 w-4 text-warning shrink-0 mt-0.5" />
          <p className="text-sm text-warning">{error}</p>
        </div>
      )}

      <div className="flex flex-wrap items-center gap-3">
        <div className="relative w-full sm:w-72">
          <Search className="absolute left-3 top-1/2 -translate-y-1/2 h-4 w-4 text-muted-foreground" />
          <Input placeholder="Search name, address, OS, host…" value={search} onChange={(e) => setSearch(e.target.value)} className="pl-9" />
        </div>
        <label className="inline-flex items-center gap-2 text-13 text-muted-foreground cursor-pointer">
          <input type="checkbox" className="h-3.5 w-3.5" checked={showIgnored} onChange={(e) => setShowIgnored(e.target.checked)} />
          Show ignored{result ? ` (${result.ignored})` : ""}
        </label>
        {canWrite && selectedVMs.length > 0 && (
          <div className="ml-auto flex items-center gap-2">
            <span className="text-13 text-muted-foreground tabular-nums">{selectedVMs.length} selected</span>
            {selectedVMs.some((vm) => vm.ignored) ? (
              <Button variant="outline" size="sm" className="gap-1.5" disabled={working !== null} onClick={() => doUnignore(selectedVMs.filter((vm) => vm.ignored))}>
                <Eye className="h-3.5 w-3.5" /> Un-ignore
              </Button>
            ) : (
              <Button variant="outline" size="sm" className="gap-1.5" disabled={working !== null} onClick={() => doIgnore(selectedVMs)}>
                <EyeOff className="h-3.5 w-3.5" /> Ignore
              </Button>
            )}
            <Button size="sm" className="gap-1.5" disabled={working !== null} onClick={doAdopt}>
              {working === "adopt" ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Import className="h-3.5 w-3.5" />}
              Adopt {selectedVMs.length} VM{selectedVMs.length === 1 ? "" : "s"}
            </Button>
          </div>
        )}
      </div>

      <Card>
        <CardContent className="p-0">
          {loading && !result ? (
            <p className="p-6 text-sm text-muted-foreground flex items-center gap-2"><Loader2 className="h-4 w-4 animate-spin" /> Reading inventory from the hypervisor…</p>
          ) : visible.length === 0 ? (
            <div className="p-10 text-center space-y-2">
              <Import className="h-10 w-10 text-muted-foreground mx-auto" />
              <p className="font-medium">{search ? "No unmanaged VMs match your search." : result && result.ignored > 0 && !showIgnored ? `Everything else on ${result.target_name} is managed — ${result.ignored} ignored.` : `Everything on ${result?.target_name ?? "this target"} is already managed by Forgemill.`}</p>
              <p className="text-sm text-muted-foreground">Templates are never listed here. Run Sync All on the VMs page to refresh the counts shown on the Targets page.</p>
            </div>
          ) : (
            <div className="overflow-x-auto">
              <table className="w-full text-sm">
                <thead>
                  <tr className="border-b bg-muted/50 text-left">
                    <th className="px-3 py-2 w-8">
                      {canWrite && <input type="checkbox" className="h-3.5 w-3.5" checked={allVisibleSelected} onChange={toggleAll} aria-label="Select all" />}
                    </th>
                    <th className="px-3 py-2 font-medium">Name</th>
                    <th className="px-3 py-2 font-medium">Power</th>
                    <th className="px-3 py-2 font-medium hidden sm:table-cell">Address</th>
                    <th className="px-3 py-2 font-medium hidden md:table-cell">vCPU / RAM / Disk</th>
                    <th className="px-3 py-2 font-medium hidden lg:table-cell">Guest OS</th>
                    <th className="px-3 py-2 font-medium hidden lg:table-cell">Host</th>
                    <th className="px-3 py-2 font-medium text-right">Ref</th>
                  </tr>
                </thead>
                <tbody>
                  {visible.map((vm) => (
                    <tr key={vm.ref} className={`border-b last:border-0 hover:bg-muted/30 transition-colors ${vm.ignored ? "opacity-60" : ""}`} onClick={() => canWrite && toggle(vm.ref)}>
                      <td className="px-3 py-2" onClick={(e) => e.stopPropagation()}>
                        {canWrite && <input type="checkbox" className="h-3.5 w-3.5" checked={selected.has(vm.ref)} onChange={() => toggle(vm.ref)} aria-label={`Select ${vm.name}`} />}
                      </td>
                      <td className="px-3 py-2">
                        <span className="font-medium">{vm.name}</span>
                        {vm.ignored && <Badge variant="secondary" className="ml-2" title="Hidden from Discover">ignored</Badge>}
                      </td>
                      <td className="px-3 py-2"><Badge variant={powerVariant(vm.power_state)} dot>{powerLabel(vm.power_state)}</Badge></td>
                      <td className="px-3 py-2 font-mono text-xs text-muted-foreground hidden sm:table-cell">{vm.ip_address || "—"}</td>
                      <td className="px-3 py-2 text-muted-foreground hidden md:table-cell whitespace-nowrap tabular-nums">{vm.cpu || "?"} · {vm.memory_mb ? fmtMem(vm.memory_mb) : "?"} · {vm.disk_gb ? `${vm.disk_gb} GB` : "?"}</td>
                      <td className="px-3 py-2 text-muted-foreground hidden lg:table-cell">{vm.guest_id || "—"}</td>
                      <td className="px-3 py-2 text-muted-foreground hidden lg:table-cell">{vm.host || "—"}</td>
                      <td className="px-3 py-2 text-right font-mono text-xs text-muted-foreground">{vm.ref}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </CardContent>
      </Card>
    </div>
  );
}
