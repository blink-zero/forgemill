import { useTimezone } from "@/hooks/useTimezone";
import { useEffect, useState, useRef, useCallback } from "react";
import { useParams, useNavigate } from "react-router-dom";
import { vms as vmApi, executions as execApi, actions as actionsApi } from "@/api/client";
import { usePageSize } from "@/hooks/usePageSize";
import { useConfirm } from "@/components/ui/confirm-dialog";
import { useToast } from "@/components/ui/toast";
import type { ManagedVM, VMSnapshot, Action, ActionExecution, ActionParameter } from "@/types";
import { Select } from "@/components/ui/select";
import { Card, CardHeader, CardTitle, CardContent } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Power, Play, Square, RotateCcw, Pause, Trash2,
  Camera, Undo2, ExternalLink, Cpu, MemoryStick, HardDrive, ArrowLeft,
  RefreshCw, KeyRound, Eye, EyeOff, Copy, Terminal, X,
  CheckCircle, XCircle, Loader2, AlertTriangle, Settings2,
} from "lucide-react";
import { Pagination } from "@/components/ui/pagination";
import { getErrorMessage } from "@/lib/utils";

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

const statusVariant = (status: string) => {
  if (status === "completed") return "success" as const;
  if (status === "failed") return "destructive" as const;
  if (status === "running" || status === "pending") return "warning" as const;
  if (status === "cancelled") return "secondary" as const;
  return "secondary" as const;
};

type Tab = "overview" | "snapshots" | "actions";

export default function VMDetail() {
  const { formatDateTime } = useTimezone();
  const { confirm: showConfirm } = useConfirm();
  const { toast } = useToast();
  const { id } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const [vm, setVM] = useState<ManagedVM | null>(null);
  const [snapshots, setSnapshots] = useState<VMSnapshot[]>([]);
  const [loading, setLoading] = useState(true);
  const [acting, setActing] = useState(false);
  const [snapName, setSnapName] = useState("");
  const [snapDesc, setSnapDesc] = useState("");
  // showDelete removed — replaced by deleteMode ("untrack" | "destroy")
  const [deleteMode, setDeleteMode] = useState<"untrack" | "destroy" | null>(null);
  const [destroyConfirmText, setDestroyConfirmText] = useState("");
  const [showResize, setShowResize] = useState(false);
  const [resizeCPU, setResizeCPU] = useState(0);
  const [resizeMem, setResizeMem] = useState(0);
  const [showExpandDisk, setShowExpandDisk] = useState(false);
  const [disks, setDisks] = useState<{ key: number; label: string; size_gb: number }[]>([]);
  const [expandDiskKey, setExpandDiskKey] = useState<number | null>(null);
  const [expandDiskSize, setExpandDiskSize] = useState(0);
  const [syncing, setSyncing] = useState(false);
  const [tab, setTab] = useState<Tab>("overview");

  const vmId = Number(id);
  // 8.14: Track timeouts for cleanup on unmount
  const pendingTimers = useRef<ReturnType<typeof setTimeout>[]>([]);

  const reload = useCallback(() => {
    if (isNaN(vmId)) return;
    vmApi.get(vmId).then((res) => setVM(res.data));
    vmApi.listSnapshots(vmId).then((res) => setSnapshots(res.data || []));
  }, [vmId]);

  useEffect(() => {
    // 8.7: Guard against NaN deployment ID
    if (isNaN(vmId)) {
      setLoading(false);
      return;
    }
    Promise.all([
      vmApi.get(vmId).then((res) => setVM(res.data)),
      vmApi.listSnapshots(vmId).then((res) => setSnapshots(res.data || [])),
    ]).finally(() => setLoading(false));
    // 8.14: Cleanup pending timers on unmount
    return () => {
      pendingTimers.current.forEach(clearTimeout);
    };
  }, [vmId]);

  const doPower = async (action: string) => {
    setActing(true);
    try {
      await vmApi.power(vmId, action);
      // 8.14: Track timer for cleanup
      const timer = setTimeout(reload, 1000);
      pendingTimers.current.push(timer);
    } finally {
      setActing(false);
    }
  };

  const doSync = async () => {
    setSyncing(true);
    try {
      const res = await vmApi.sync(vmId);
      setVM(res.data);
    } finally {
      setSyncing(false);
    }
  };

  const doCreateSnapshot = async () => {
    if (!snapName) return;
    setActing(true);
    try {
      await vmApi.createSnapshot(vmId, { name: snapName, description: snapDesc, memory: false });
      setSnapName("");
      setSnapDesc("");
      reload();
    } finally {
      setActing(false);
    }
  };

  // 8.22: Add confirmation for destructive snapshot operations
  const doRevertSnapshot = async (snapId: number) => {
    const ok = await showConfirm({ title: "Revert Snapshot", message: "Revert to this snapshot? The VM's current state will be lost.", confirmLabel: "Revert", variant: "destructive" });
    if (!ok) return;
    setActing(true);
    try {
      await vmApi.revertSnapshot(vmId, snapId);
      // 8.14: Track timer for cleanup
      const timer = setTimeout(reload, 1000);
      pendingTimers.current.push(timer);
    } finally {
      setActing(false);
    }
  };

  // 8.22: Add confirmation for destructive snapshot operations
  const doDeleteSnapshot = async (snapId: number) => {
    const ok2 = await showConfirm({ title: "Delete Snapshot", message: "Delete this snapshot? This cannot be undone.", confirmLabel: "Delete", variant: "destructive" });
    if (!ok2) return;
    setActing(true);
    try {
      await vmApi.deleteSnapshot(vmId, snapId);
      reload();
    } finally {
      setActing(false);
    }
  };

  const doResize = async () => {
    if (resizeCPU <= 0 && resizeMem <= 0) return;
    setActing(true);
    try {
      await vmApi.resize(vmId, { cpu: resizeCPU, memory_mb: resizeMem });
      setShowResize(false);
      reload();
    } finally {
      setActing(false);
    }
  };

  const loadDisks = async () => {
    try {
      const res = await vmApi.listDisks(vmId);
      const data = res.data || [];
      setDisks(data);
      if (data.length > 0) {
        setExpandDiskKey(data[0].key);
        setExpandDiskSize(data[0].size_gb + 10);
      }
    } catch (e) {
      toast(getErrorMessage(e, "Failed to load disks"), "error");
    }
  };

  const doExpandDisk = async () => {
    if (expandDiskKey === null || expandDiskSize <= 0) return;
    setActing(true);
    try {
      await vmApi.expandDisk(vmId, expandDiskKey, { new_size_gb: expandDiskSize });
      setShowExpandDisk(false);
      reload();
      toast("Disk expanded successfully");
    } catch (e) {
      toast(getErrorMessage(e, "Failed to expand disk"), "error");
    } finally {
      setActing(false);
    }
  };

  const doDelete = async (forceLocal: boolean) => {
    setActing(true);
    try {
      await vmApi.delete(vmId, forceLocal);
      navigate("/vms");
    } finally {
      setActing(false);
    }
  };

  const doConsole = async () => {
    try {
      const res = await vmApi.console(vmId);
      // HIGH-02/MED-29: Validate protocol before opening console URL
      const url = new URL(res.data.url);
      if (!["https:", "http:", "vmrc:"].includes(url.protocol)) {
        throw new Error("Invalid console URL protocol");
      }
      // LOW-31: Prevent tabnabbing via noopener,noreferrer
      window.open(url.toString(), "_blank", "noopener,noreferrer");
    } catch {
      // silent
    }
  };

  if (loading) {
    return <div className="flex items-center justify-center h-64"><Loader2 className="h-8 w-8 animate-spin text-primary" /></div>;
  }

  if (!vm) return <div className="text-muted-foreground">VM not found</div>;

  return (
    <div className="space-y-6">
      <div className="flex items-center gap-4">
        <nav className="flex items-center gap-1 text-sm text-muted-foreground">
          <Button variant="ghost" size="sm" onClick={() => navigate("/vms")} className="h-auto p-0 font-normal hover:text-foreground">
            VMs
          </Button>
          <span>/</span>
          <span className="font-medium text-foreground">{vm.vm_name}</span>
        </nav>
        <h1 className="text-2xl font-bold">{vm.vm_name}</h1>
        <Badge variant={powerVariant(vm.power_state)}>
          <Power className="h-3 w-3 mr-1" />{powerLabel(vm.power_state)}
        </Badge>
        <Button variant="outline" size="sm" onClick={doSync} disabled={syncing} className="ml-auto">
          <RefreshCw className={`h-3 w-3 mr-1 ${syncing ? "animate-spin" : ""}`} />
          {syncing ? "Syncing..." : "Sync"}
        </Button>
      </div>

      {/* Tabs */}
      <div className="flex border-b">
        {(["overview", "snapshots", "actions"] as Tab[]).map((t) => (
          <button
            key={t}
            className={`px-4 py-2 text-sm font-medium border-b-2 transition-colors ${
              tab === t
                ? "border-primary text-primary"
                : "border-transparent text-muted-foreground hover:text-foreground"
            }`}
            onClick={() => setTab(t)}
          >
            {t === "overview" ? "Overview" : t === "snapshots" ? "Snapshots" : "Actions"}
          </button>
        ))}
      </div>

      {tab === "overview" && (
        <>
          {/* Resource Stats */}
          <div className="grid gap-4 sm:grid-cols-3">
            <Card className="p-4">
              <div className="flex items-center gap-3">
                <div className="h-10 w-10 rounded-lg bg-blue-500/10 flex items-center justify-center">
                  <Cpu className="h-5 w-5 text-blue-500" />
                </div>
                <div>
                  <p className="text-2xl font-bold">{vm.cpu || "—"}</p>
                  <p className="text-xs text-muted-foreground">vCPU Cores</p>
                </div>
              </div>
            </Card>
            <Card className="p-4">
              <div className="flex items-center gap-3">
                <div className="h-10 w-10 rounded-lg bg-purple-500/10 flex items-center justify-center">
                  <MemoryStick className="h-5 w-5 text-purple-500" />
                </div>
                <div>
                  <p className="text-2xl font-bold">{vm.memory_mb ? (vm.memory_mb >= 1024 ? `${(vm.memory_mb / 1024).toFixed(vm.memory_mb % 1024 ? 1 : 0)} GB` : `${vm.memory_mb} MB`) : "—"}</p>
                  <p className="text-xs text-muted-foreground">Memory</p>
                </div>
              </div>
            </Card>
            <Card className="p-4">
              <div className="flex items-center gap-3">
                <div className="h-10 w-10 rounded-lg bg-green-500/10 flex items-center justify-center">
                  <HardDrive className="h-5 w-5 text-green-500" />
                </div>
                <div>
                  <p className="text-2xl font-bold">{vm.disk_gb ? `${vm.disk_gb} GB` : "—"}</p>
                  <p className="text-xs text-muted-foreground">Disk</p>
                </div>
              </div>
            </Card>
          </div>

          <div className="grid gap-6 lg:grid-cols-3">
            {/* VM Details */}
            <Card className="lg:col-span-2">
              <CardHeader>
                <CardTitle>Details</CardTitle>
              </CardHeader>
              <CardContent className="space-y-1">
                {[
                  { label: "Target", value: vm.target_name },
                  { label: "Template", value: vm.template_name || "N/A" },
                  { label: "IP Address", value: vm.ip_address || "N/A", mono: true, copyable: !!vm.ip_address },
                  { label: "OS Type", value: vm.os_type || "N/A" },
                  { label: "VM ID", value: String(vm.id), mono: true },
                  { label: "Last Synced", value: vm.last_synced_at ? formatDateTime(vm.last_synced_at) : "Never" },
                ].map((row) => (
                  <div key={row.label} className="flex items-center justify-between py-2 border-b last:border-0">
                    <span className="text-sm text-muted-foreground">{row.label}</span>
                    <div className="flex items-center gap-1.5">
                      <span className={`text-sm font-medium ${row.mono ? "font-mono" : ""}`}>{row.value}</span>
                      {row.copyable && (
                        <button
                          onClick={() => {
                            try {
                              if (navigator.clipboard && window.isSecureContext) {
                                navigator.clipboard.writeText(row.value);
                              } else {
                                const ta = document.createElement("textarea");
                                ta.value = row.value;
                                ta.style.position = "fixed";
                                ta.style.opacity = "0";
                                document.body.appendChild(ta);
                                ta.select();
                                document.execCommand("copy");
                                document.body.removeChild(ta);
                              }
                              toast("Copied to clipboard");
                            } catch (e) { toast(getErrorMessage(e, "Failed to copy"), "error"); }
                          }}
                          className="text-muted-foreground hover:text-foreground transition-colors"
                          title="Copy to clipboard"
                        >
                          <Copy className="h-3.5 w-3.5" />
                        </button>
                      )}
                    </div>
                  </div>
                ))}
              </CardContent>
            </Card>

            {/* Power & Management */}
            <Card>
              <CardHeader>
                <CardTitle>Management</CardTitle>
              </CardHeader>
              <CardContent className="space-y-3">
                <div className="grid grid-cols-2 gap-2">
                  <Button size="sm" onClick={() => doPower("start")} disabled={acting} className="gap-1.5">
                    <Play className="h-3.5 w-3.5" /> Start
                  </Button>
                  <Button size="sm" variant="secondary" onClick={() => doPower("stop")} disabled={acting} className="gap-1.5">
                    <Square className="h-3.5 w-3.5" /> Stop
                  </Button>
                  <Button size="sm" variant="secondary" onClick={() => doPower("restart")} disabled={acting} className="gap-1.5">
                    <RotateCcw className="h-3.5 w-3.5" /> Restart
                  </Button>
                  <Button size="sm" variant="secondary" onClick={() => doPower("suspend")} disabled={acting} className="gap-1.5">
                    <Pause className="h-3.5 w-3.5" /> Suspend
                  </Button>
                </div>

                <div className="border-t pt-3 space-y-2">
                  <Button size="sm" variant="outline" className="w-full gap-1.5" onClick={doConsole}>
                    <ExternalLink className="h-3.5 w-3.5" /> Open Console
                  </Button>
                  <Button size="sm" variant="outline" className="w-full gap-1.5" onClick={() => {
                    if (!showResize && vm) {
                      setResizeCPU(vm.cpu || 0);
                      setResizeMem(vm.memory_mb || 0);
                    }
                    setShowResize(!showResize);
                  }}>
                    <Cpu className="h-3.5 w-3.5" /> Resize
                  </Button>
                  {showResize && (
                    <div className="space-y-2 border rounded-md p-3 bg-muted/30">
                      {vm.power_state !== "poweredOff" && vm.power_state !== "stopped" && (
                        <div className="rounded-md border border-yellow-500/30 bg-yellow-500/5 px-3 py-2 flex items-start gap-2">
                          <AlertTriangle className="h-4 w-4 text-yellow-500 shrink-0 mt-0.5" />
                          <p className="text-xs text-yellow-600 dark:text-yellow-400">VM must be powered off before resizing.</p>
                        </div>
                      )}
                      <div>
                        <Label className="text-xs">CPU Cores</Label>
                        <Input type="number" min={1} value={resizeCPU || ""} onChange={(e) => setResizeCPU(Number(e.target.value))} placeholder="vCPUs" />
                      </div>
                      <div>
                        <Label className="text-xs">Memory (MB)</Label>
                        <Input type="number" min={512} step={512} value={resizeMem || ""} onChange={(e) => setResizeMem(Number(e.target.value))} placeholder="Memory MB" />
                      </div>
                      <Button size="sm" onClick={doResize} disabled={acting || (vm.power_state !== "poweredOff" && vm.power_state !== "stopped")} className="w-full">Apply Resize</Button>
                    </div>
                  )}
                </div>

                <div className="border-t pt-3 space-y-2">
                  <Button size="sm" variant="outline" className="w-full gap-1.5" onClick={async () => {
                    if (!showExpandDisk) await loadDisks();
                    setShowExpandDisk(!showExpandDisk);
                  }}>
                    <HardDrive className="h-3.5 w-3.5" /> Expand Disk
                  </Button>
                  {showExpandDisk && (
                    <div className="space-y-2 border rounded-md p-3 bg-muted/30">
                      {disks.length === 0 ? (
                        <p className="text-xs text-muted-foreground">No disks found.</p>
                      ) : (
                        <>
                          {disks.length > 1 && (
                            <div>
                              <Label className="text-xs">Disk</Label>
                              <select
                                className="w-full text-sm border rounded-md px-2 py-1.5 bg-background text-foreground [&>option]:bg-background [&>option]:text-foreground"
                                value={expandDiskKey ?? ""}
                                onChange={(e) => {
                                  const key = Number(e.target.value);
                                  setExpandDiskKey(key);
                                  const disk = disks.find(d => d.key === key);
                                  if (disk) setExpandDiskSize(disk.size_gb + 10);
                                }}
                              >
                                {disks.map(d => (
                                  <option key={d.key} value={d.key}>{d.label || `Disk ${d.key}`} — {d.size_gb} GB</option>
                                ))}
                              </select>
                            </div>
                          )}
                          {disks.length === 1 && (
                            <p className="text-xs text-muted-foreground">{disks[0].label || "Disk"} — currently {disks[0].size_gb} GB</p>
                          )}
                          <div>
                            <Label className="text-xs">New Size (GB)</Label>
                            <Input type="number" min={(disks.find(d => d.key === expandDiskKey)?.size_gb ?? 0) + 1} value={expandDiskSize || ""} onChange={(e) => setExpandDiskSize(Number(e.target.value))} placeholder="GB" />
                          </div>
                          <Button size="sm" onClick={doExpandDisk} disabled={acting || expandDiskSize <= (disks.find(d => d.key === expandDiskKey)?.size_gb ?? 0)} className="w-full">Apply Expand</Button>
                        </>
                      )}
                    </div>
                  )}
                </div>

                <div className="border-t pt-3 space-y-2">
                  <Button size="sm" variant="outline" className="w-full gap-1.5" onClick={async () => {
                    const ok = await showConfirm({ title: "Reset SSH Host Key", message: "This clears the stored SSH host key fingerprint. The next SSH connection will trust the new key automatically (TOFU). Use this after rebuilding a VM.", confirmLabel: "Reset" });
                    if (!ok) return;
                    try { await vmApi.resetHostKey(vm.id); toast("SSH host key reset — next connection will re-establish trust"); } catch (e) { toast(getErrorMessage(e, "Failed to reset host key"), "error"); }
                  }} disabled={acting}>
                    <KeyRound className="h-3.5 w-3.5" /> Reset SSH Host Key
                  </Button>
                </div>

                <div className="border-t pt-3 space-y-2">
                  <Button size="sm" variant="outline" className="w-full gap-1.5" onClick={() => setDeleteMode(deleteMode === "untrack" ? null : "untrack")} disabled={acting}>
                    <X className="h-3.5 w-3.5" /> Untrack VM
                  </Button>
                  {deleteMode === "untrack" && (
                    <div className="border border-yellow-500/30 rounded-md p-3 space-y-2 bg-yellow-500/5">
                      <p className="text-xs text-yellow-600 dark:text-yellow-400">Remove this VM from Forgemill only. The VM will continue running on the hypervisor — it just won't be tracked here anymore.</p>
                      <p className="text-xs text-yellow-600/70 dark:text-yellow-400/70">⚠ This cannot be reversed. Untracked VMs cannot currently be re-imported into Forgemill.</p>
                      <Button size="sm" variant="secondary" onClick={() => doDelete(true)} disabled={acting} className="w-full">
                        Confirm Untrack
                      </Button>
                    </div>
                  )}
                  <Button size="sm" variant="destructive" className="w-full gap-1.5" onClick={() => { setDeleteMode(deleteMode === "destroy" ? null : "destroy"); setDestroyConfirmText(""); }} disabled={acting}>
                    <Trash2 className="h-3.5 w-3.5" /> Destroy VM
                  </Button>
                  {deleteMode === "destroy" && (
                    <div className="border border-destructive/30 rounded-md p-3 space-y-3 bg-destructive/5">
                      <p className="text-xs text-destructive">This will permanently destroy this VM on the hypervisor and remove it from Forgemill. This cannot be undone.</p>
                      <div className="space-y-1.5">
                        <Label className="text-xs text-destructive">Type <span className="font-mono font-bold">{vm?.vm_name}</span> to confirm:</Label>
                        <Input
                          value={destroyConfirmText}
                          onChange={(e) => setDestroyConfirmText(e.target.value)}
                          placeholder={vm?.vm_name}
                          className="text-sm border-destructive/50 focus:border-destructive"
                        />
                      </div>
                      <Button size="sm" variant="destructive" onClick={() => doDelete(false)} disabled={acting || destroyConfirmText !== vm?.vm_name} className="w-full">
                        {destroyConfirmText === vm?.vm_name ? "Confirm Destroy" : "Type VM name to confirm"}
                      </Button>
                    </div>
                  )}
                </div>
              </CardContent>
            </Card>
          </div>

          {/* SSH Credentials */}
          <CredentialsCard vmId={vmId} vmIp={vm.ip_address} />
        </>
      )}

      {tab === "snapshots" && (
        <Card>
          <CardHeader>
            <CardTitle>Snapshots</CardTitle>
          </CardHeader>
          <CardContent className="space-y-4">
            <div className="flex gap-3 items-end">
              <div className="flex-1">
                <Label className="text-xs">Snapshot Name</Label>
                <Input value={snapName} onChange={(e) => setSnapName(e.target.value)} placeholder="e.g. before-update" />
              </div>
              <div className="flex-1">
                <Label className="text-xs">Description</Label>
                <Input value={snapDesc} onChange={(e) => setSnapDesc(e.target.value)} placeholder="Optional" />
              </div>
              <Button onClick={doCreateSnapshot} disabled={acting || !snapName}>
                <Camera className="h-3 w-3 mr-1" /> Create
              </Button>
            </div>

            {snapshots.length === 0 ? (
              <p className="text-sm text-muted-foreground">No snapshots</p>
            ) : (
              <div className="space-y-2">
                {snapshots.map((snap) => (
                  <div key={snap.id} className="flex items-center justify-between border rounded-md p-3">
                    <div>
                      <p className="text-sm font-medium">{snap.name}</p>
                      <p className="text-xs text-muted-foreground">{snap.description || "No description"} &middot; {formatDateTime(snap.created_at)}</p>
                    </div>
                    <div className="flex gap-2">
                      <Button size="sm" variant="outline" onClick={() => doRevertSnapshot(snap.id)} disabled={acting}>
                        <Undo2 className="h-3 w-3 mr-1" /> Revert
                      </Button>
                      <Button size="sm" variant="ghost" onClick={() => doDeleteSnapshot(snap.id)} disabled={acting} className="text-destructive hover:text-destructive">
                        <Trash2 className="h-3 w-3" />
                      </Button>
                    </div>
                  </div>
                ))}
              </div>
            )}
          </CardContent>
        </Card>
      )}

      {tab === "actions" && <ActionsTab vmId={vmId} vmPowerState={vm.power_state} />}
    </div>
  );
}

function CredentialsCard({ vmId, vmIp }: { vmId: number; vmIp?: string }) {
  const { toast } = useToast();
  const [creds, setCreds] = useState<{ username: string; password: string } | null>(null);
  const [showPwd, setShowPwd] = useState(false);
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(false);

  const reveal = async () => {
    setLoading(true);
    setError("");
    try {
      const res = await vmApi.credentials(vmId);
      setCreds(res.data);
    } catch {
      setError("No credentials available for this VM");
    } finally {
      setLoading(false);
    }
  };

  const copyToClipboard = (text: string) => {
    try {
      if (navigator.clipboard && window.isSecureContext) {
        navigator.clipboard.writeText(text);
      } else {
        const ta = document.createElement("textarea");
        ta.value = text;
        ta.style.position = "fixed";
        ta.style.opacity = "0";
        document.body.appendChild(ta);
        ta.select();
        document.execCommand("copy");
        document.body.removeChild(ta);
      }
      toast("Copied to clipboard");
    } catch (e) {
      toast(getErrorMessage(e, "Failed to copy"), "error");
    }
  };

  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          <KeyRound className="h-4 w-4" /> SSH Credentials
        </CardTitle>
      </CardHeader>
      <CardContent>
        {!creds && !error && (
          <Button size="sm" variant="outline" onClick={reveal} disabled={loading}>
            <Eye className="h-3 w-3 mr-1" /> {loading ? "Loading..." : "Reveal Credentials"}
          </Button>
        )}
        {error && <p className="text-sm text-muted-foreground">{error}</p>}
        {creds && (
          <div className="space-y-3">
            <div className="flex items-center gap-2">
              <Label className="w-20 text-xs text-muted-foreground">Username</Label>
              <code className="flex-1 bg-muted px-2 py-1 rounded text-sm">{creds.username}</code>
              <Button size="icon" variant="ghost" className="h-7 w-7" onClick={() => copyToClipboard(creds.username)} aria-label="Copy username">
                <Copy className="h-3 w-3" />
              </Button>
            </div>
            <div className="flex items-center gap-2">
              <Label className="w-20 text-xs text-muted-foreground">Password</Label>
              <code className="flex-1 bg-muted px-2 py-1 rounded text-sm font-mono">
                {showPwd
                  ? creds.password.split("").map((ch, i) => (
                      <span
                        key={i}
                        className={
                          /[0-9]/.test(ch)
                            ? "text-blue-500"
                            : /[a-zA-Z]/.test(ch)
                              ? "text-green-500"
                              : "text-orange-500"
                        }
                      >
                        {ch}
                      </span>
                    ))
                  : "\u2022\u2022\u2022\u2022\u2022\u2022\u2022\u2022\u2022\u2022\u2022\u2022"}
              </code>
              <Button size="icon" variant="ghost" className="h-7 w-7" onClick={() => setShowPwd(!showPwd)} aria-label={showPwd ? "Hide password" : "Show password"}>
                {showPwd ? <EyeOff className="h-3 w-3" /> : <Eye className="h-3 w-3" />}
              </Button>
              <Button size="icon" variant="ghost" className="h-7 w-7" onClick={() => copyToClipboard(creds.password)} aria-label="Copy password">
                <Copy className="h-3 w-3" />
              </Button>
            </div>
            {vmIp && (
              <Button
                size="sm"
                variant="outline"
                className="w-full mt-1"
                onClick={() => copyToClipboard(`ssh ${creds.username}@${vmIp}`)}
              >
                <Terminal className="h-3 w-3 mr-1" /> Copy SSH Command
              </Button>
            )}
            <p className="text-xs text-muted-foreground mt-2">
              Password is stored AES-256 encrypted. Only decrypted on reveal.
            </p>
          </div>
        )}
      </CardContent>
    </Card>
  );
}

function ActionsTab({ vmId, vmPowerState }: { vmId: number; vmPowerState: string }) {
  const { formatDateTime } = useTimezone();
  const { confirm: showConfirm } = useConfirm();
  const { toast } = useToast();
  const [availableActions, setAvailableActions] = useState<Action[]>([]);
  const [executionHistory, setExecutionHistory] = useState<ActionExecution[]>([]);
  const [adHocScript, setAdHocScript] = useState("");
  const [executing, setExecuting] = useState(false);
  const [error, setError] = useState("");
  const [activeExecution, setActiveExecution] = useState<ActionExecution | null>(null);
  const [outputLines, setOutputLines] = useState<string[]>([]);
  const [execStatus, setExecStatus] = useState<string>("");
  const [execExitCode, setExecExitCode] = useState<number | null>(null);
  const wsRef = useRef<WebSocket | null>(null);
  const outputRef = useRef<HTMLDivElement>(null);
  const [expandedExecId, setExpandedExecId] = useState<number | null>(null);
  const [actionSearch, setActionSearch] = useState("");
  const [actionCategoryFilter, setActionCategoryFilter] = useState<string>("all");
  const [historyPage, setHistoryPage] = useState(1);
  const [historyPerPage, setHistoryPerPage] = usePageSize("vmdetail_executions", 10);
  const [paramAction, setParamAction] = useState<Action | null>(null);
  const [paramValues, setParamValues] = useState<Record<string, string>>({});

  const isPoweredOn = vmPowerState === "poweredOn" || vmPowerState === "running";

  const loadData = useCallback(() => {
    actionsApi.list().then((res) => setAvailableActions(res.data || []));
    execApi.list(vmId).then((res) => setExecutionHistory(res.data || []));
  }, [vmId]);

  useEffect(() => {
    loadData();
  }, [loadData]);

  // Auto-scroll output to bottom
  useEffect(() => {
    if (outputRef.current) {
      outputRef.current.scrollTop = outputRef.current.scrollHeight;
    }
  }, [outputLines]);

  const connectWS = (executionId: number) => {
    const token = localStorage.getItem("forgemill_token") || "";
    if (!token) return;

    const protocol = window.location.protocol === "https:" ? "wss:" : "ws:";
    const ws = new WebSocket(
      `${protocol}//${window.location.host}/api/ws/execution/${executionId}`,
      [`token.${token}`]
    );

    ws.onmessage = (event) => {
      try {
        const msg = JSON.parse(event.data);
        if (msg.type === "output" && msg.data?.line !== undefined) {
          setOutputLines((prev) => {
            const next = [...prev, msg.data.line];
            if (next.length > 5000) return next.slice(-5000);
            return next;
          });
        } else if (msg.type === "status" && msg.data?.status) {
          setExecStatus(msg.data.status);
          if (msg.data.exit_code !== undefined) {
            setExecExitCode(msg.data.exit_code);
          }
          if (["completed", "failed", "cancelled"].includes(msg.data.status)) {
            loadData();
          }
        } else if (msg.type === "error" && msg.data?.message) {
          setOutputLines((prev) => [...prev, `[ERROR] ${msg.data.message}`]);
          setExecStatus("failed");
          loadData();
        }
      } catch {
        // ignore malformed
      }
    };

    ws.onerror = (e) => {
      console.error("WS error:", e);
    };

    ws.onclose = (e) => {
      console.log("WS closed:", e.code, e.reason, "wasClean:", e.wasClean);
      // If closed before any output, try polling the execution once
      if (e.code !== 1000 && e.code !== 1005) {
        // Abnormal close — fetch execution output from API as fallback
        execApi.get(executionId).then((res) => {
          if (res.data?.output) {
            setOutputLines(res.data.output.split("\n"));
          }
          if (res.data?.status) {
            setExecStatus(res.data.status);
            if (res.data.exit_code !== undefined) {
              setExecExitCode(res.data.exit_code);
            }
          }
          loadData();
        }).catch((e) => {
          toast(getErrorMessage(e, "Failed to fetch execution output"), "error");
        });
      }
    };

    wsRef.current = ws;
  };

  const doExecuteAction = async (actionId: number) => {
    const action = availableActions.find((a) => a.id === actionId);
    if (action?.parameters && action.parameters.length > 0) {
      // Has parameters — show parameter modal instead of executing directly
      const defaults: Record<string, string> = {};
      for (const p of action.parameters) {
        if (p.default) defaults[p.name] = p.default;
        else if (p.type === "boolean") defaults[p.name] = "false";
        else defaults[p.name] = "";
      }
      setParamValues(defaults);
      setParamAction(action);
      return;
    }
    const runOk = await showConfirm({ title: "Run Action", message: "Run this action on the VM?", confirmLabel: "Run" });
    if (!runOk) return;
    setExecuting(true);
    setError("");
    try {
      const res = await execApi.execute(vmId, { action_id: actionId });
      setActiveExecution(res.data);
      setOutputLines([]);
      setExecStatus("pending");
      setExecExitCode(null);
      connectWS(res.data.id);
    } catch (e: unknown) {
      const msg = e instanceof Error ? e.message : "Execution failed";
      const axiosErr = e as { response?: { data?: { error?: string } } };
      setError(axiosErr?.response?.data?.error || msg);
    } finally {
      setExecuting(false);
    }
  };

  const doExecuteWithParams = async () => {
    if (!paramAction) return;
    // Validate required params
    const missing = (paramAction.parameters || []).filter(
      (p) => p.required && !paramValues[p.name]?.trim()
    );
    if (missing.length > 0) {
      toast("Missing required parameters: " + missing.map((p) => p.label).join(", "), "error");
      return;
    }
    const actionId = paramAction.id;
    setParamAction(null);
    setExecuting(true);
    setError("");
    try {
      const res = await execApi.execute(vmId, { action_id: actionId, parameter_values: paramValues });
      setActiveExecution(res.data);
      setOutputLines([]);
      setExecStatus("pending");
      setExecExitCode(null);
      connectWS(res.data.id);
    } catch (e: unknown) {
      const msg = e instanceof Error ? e.message : "Execution failed";
      const axiosErr = e as { response?: { data?: { error?: string } } };
      setError(axiosErr?.response?.data?.error || msg);
    } finally {
      setExecuting(false);
    }
  };

  const doExecuteAdHoc = async () => {
    if (!adHocScript.trim()) return;
    const adHocOk = await showConfirm({ title: "Run Ad-Hoc Script", message: "Run this script on the VM? Scripts run with sudo privileges.", confirmLabel: "Run Script", variant: "destructive" });
    if (!adHocOk) return;
    setExecuting(true);
    setError("");
    try {
      const res = await execApi.execute(vmId, { script: adHocScript });
      setActiveExecution(res.data);
      setOutputLines([]);
      setExecStatus("pending");
      setExecExitCode(null);
      connectWS(res.data.id);
    } catch (e: unknown) {
      const msg = e instanceof Error ? e.message : "Execution failed";
      const axiosErr = e as { response?: { data?: { error?: string } } };
      setError(axiosErr?.response?.data?.error || msg);
    } finally {
      setExecuting(false);
    }
  };

  const doCancel = async () => {
    if (!activeExecution) return;
    try {
      await execApi.cancel(activeExecution.id);
    } catch {
      // silent
    }
  };

  const closeModal = () => {
    if (wsRef.current) {
      wsRef.current.close();
      wsRef.current = null;
    }
    setActiveExecution(null);
    setOutputLines([]);
    setExecStatus("");
    setExecExitCode(null);
    loadData();
  };

  const formatDuration = (start: string | null, end: string | null) => {
    if (!start) return "-";
    const s = new Date(start).getTime();
    const e = end ? new Date(end).getTime() : Date.now();
    const secs = Math.round((e - s) / 1000);
    if (secs < 60) return `${secs}s`;
    return `${Math.floor(secs / 60)}m ${secs % 60}s`;
  };

  const categoryColors: Record<string, string> = {
    packages: "bg-blue-100 text-blue-700 dark:bg-blue-900 dark:text-blue-300",
    scripts: "bg-purple-100 text-purple-700 dark:bg-purple-900 dark:text-purple-300",
    security: "bg-red-100 text-red-700 dark:bg-red-900 dark:text-red-300",
    monitoring: "bg-green-100 text-green-700 dark:bg-green-900 dark:text-green-300",
    custom: "bg-gray-100 text-gray-700 dark:bg-gray-800 dark:text-gray-300",
  };

  const isRunning = execStatus === "running" || execStatus === "pending";

  return (
    <div className="space-y-6">
      {/* Execution Modal */}
      {activeExecution && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50" onClick={(e) => { if (e.target === e.currentTarget && !isRunning) closeModal(); }}>
          <div className="bg-background rounded-lg shadow-xl w-full max-w-5xl mx-4 flex flex-col max-h-[85vh]">
            <div className="flex items-center justify-between p-4 border-b">
              <div className="flex items-center gap-3">
                <Terminal className="h-5 w-5" />
                <span className="font-medium">{activeExecution.action_name}</span>
                {isRunning && <Loader2 className="h-4 w-4 animate-spin text-yellow-500" />}
                {execStatus === "completed" && <CheckCircle className="h-4 w-4 text-green-500" />}
                {execStatus === "failed" && <XCircle className="h-4 w-4 text-red-500" />}
                {execStatus === "cancelled" && <X className="h-4 w-4 text-gray-500" />}
              </div>
              <div className="flex items-center gap-2">
                {isRunning && (
                  <Button size="sm" variant="destructive" onClick={doCancel}>
                    <X className="h-3 w-3 mr-1" /> Cancel
                  </Button>
                )}
                {execExitCode !== null && (
                  <span className={`inline-flex items-center gap-1.5 text-sm ${execExitCode === 0 ? "text-green-500" : "text-red-500"}`}>
                    {execExitCode === 0 ? <CheckCircle className="h-4 w-4" /> : <XCircle className="h-4 w-4" />}
                    {execExitCode === 0 ? "Completed successfully" : `Failed (exit code ${execExitCode})`}
                  </span>
                )}
                {outputLines.length > 0 && (
                  <Button size="sm" variant="outline" onClick={() => {
                    try {
                      const text = outputLines.join("\n");
                      if (navigator.clipboard && window.isSecureContext) {
                        navigator.clipboard.writeText(text);
                      } else {
                        const ta = document.createElement("textarea");
                        ta.value = text; ta.style.position = "fixed"; ta.style.opacity = "0";
                        document.body.appendChild(ta); ta.select(); document.execCommand("copy"); document.body.removeChild(ta);
                      }
                      toast("Output copied to clipboard");
                    } catch (e) { toast("Failed to copy", "error"); }
                  }}>
                    <Copy className="h-3 w-3 mr-1" /> Copy Output
                  </Button>
                )}
                {!isRunning && (
                  <Button size="sm" variant="outline" onClick={closeModal}>
                    Close
                  </Button>
                )}
                <Button size="icon" variant="ghost" onClick={closeModal}>
                  <X className="h-4 w-4" />
                </Button>
              </div>
            </div>
            <div
              ref={outputRef}
              className="flex-1 overflow-auto p-4 bg-gray-950 font-mono text-sm text-green-400 min-h-[400px]"
            >
              {outputLines.length === 0 && isRunning && (
                <p className="text-gray-500">Waiting for output...</p>
              )}
              {outputLines.map((line, i) => (
                <div key={i} className="whitespace-pre-wrap break-all">{line || "\u00A0"}</div>
              ))}
            </div>
          </div>
        </div>
      )}

      {/* Parameter Modal */}
      {paramAction && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50" onClick={(e) => { if (e.target === e.currentTarget) setParamAction(null); }}>
          <div className="bg-background rounded-lg shadow-xl w-full max-w-lg mx-4 flex flex-col max-h-[85vh]">
            <div className="flex items-center justify-between p-4 border-b">
              <div className="flex items-center gap-2">
                <Settings2 className="h-5 w-5" />
                <span className="font-medium">{paramAction.name}</span>
              </div>
              <Button size="icon" variant="ghost" onClick={() => setParamAction(null)}>
                <X className="h-4 w-4" />
              </Button>
            </div>
            <div className="flex-1 overflow-auto p-4 space-y-4">
              {paramAction.description && (
                <p className="text-sm text-muted-foreground">{paramAction.description}</p>
              )}
              {(paramAction.parameters || []).map((param) => (
                <div key={param.name} className="space-y-1.5">
                  <Label className="text-sm font-medium">
                    {param.label}
                    {param.required && <span className="text-red-500 ml-0.5">*</span>}
                  </Label>
                  {param.description && (
                    <p className="text-xs text-muted-foreground">{param.description}</p>
                  )}
                  {param.type === "select" && param.options ? (
                    <Select
                      value={paramValues[param.name] || ""}
                      onChange={(e) => setParamValues((prev) => ({ ...prev, [param.name]: e.target.value }))}
                    >
                      <option value="">-- Select --</option>
                      {param.options.map((opt) => (
                        <option key={opt} value={opt}>{opt}</option>
                      ))}
                    </Select>
                  ) : param.type === "boolean" ? (
                    <div className="flex items-center gap-2">
                      <input
                        type="checkbox"
                        checked={paramValues[param.name] === "true"}
                        onChange={(e) => setParamValues((prev) => ({ ...prev, [param.name]: e.target.checked ? "true" : "false" }))}
                        className="h-4 w-4 rounded border-gray-300"
                      />
                      <span className="text-sm text-muted-foreground">{paramValues[param.name] === "true" ? "Yes" : "No"}</span>
                    </div>
                  ) : (
                    <Input
                      type={param.type === "password" ? "password" : param.type === "number" ? "number" : "text"}
                      placeholder={param.placeholder || ""}
                      value={paramValues[param.name] || ""}
                      onChange={(e) => setParamValues((prev) => ({ ...prev, [param.name]: e.target.value }))}
                    />
                  )}
                </div>
              ))}
            </div>
            <div className="flex items-center justify-end gap-2 p-4 border-t">
              <Button variant="outline" onClick={() => setParamAction(null)}>Cancel</Button>
              <Button onClick={doExecuteWithParams} disabled={executing}>
                <Play className="h-3 w-3 mr-1" /> Run Action
              </Button>
            </div>
          </div>
        </div>
      )}

      {error && (
        <div className="bg-destructive/10 border border-destructive/30 rounded-md p-3 text-sm text-destructive flex items-center gap-2">
          <AlertTriangle className="h-4 w-4" />
          {error}
          <Button size="sm" variant="ghost" className="ml-auto h-6" onClick={() => setError("")}>
            <X className="h-3 w-3" />
          </Button>
        </div>
      )}

      {!isPoweredOn && (
        <div className="bg-yellow-50 dark:bg-yellow-900/20 border border-yellow-200 dark:border-yellow-800 rounded-md p-3 text-sm text-yellow-700 dark:text-yellow-300 flex items-center gap-2">
          <AlertTriangle className="h-4 w-4" />
          VM must be powered on to execute actions.
        </div>
      )}

      {/* Quick Actions */}
      <Card>
        <CardHeader>
          <div className="flex items-center justify-between">
            <CardTitle>Quick Actions</CardTitle>
            <Badge variant="secondary">{availableActions.length}</Badge>
          </div>
          {availableActions.length > 0 && (
            <div className="flex flex-col sm:flex-row gap-2 mt-2">
              <Input
                placeholder="Search actions..."
                value={actionSearch}
                onChange={(e) => setActionSearch(e.target.value)}
                className="sm:max-w-xs h-8 text-sm"
              />
              <div className="flex gap-1 flex-wrap">
                {["all", ...Array.from(new Set(availableActions.map((a) => a.category)))].map((cat) => (
                  <Button
                    key={cat}
                    size="sm"
                    variant={actionCategoryFilter === cat ? "default" : "outline"}
                    className="h-7 text-xs"
                    onClick={() => setActionCategoryFilter(cat)}
                  >
                    {cat === "all" ? "All" : cat}
                  </Button>
                ))}
              </div>
            </div>
          )}
        </CardHeader>
        <CardContent>
          {availableActions.length === 0 ? (
            <p className="text-sm text-muted-foreground">No actions available</p>
          ) : (() => {
            const filtered = availableActions.filter((a) => {
              const matchSearch = !actionSearch || a.name.toLowerCase().includes(actionSearch.toLowerCase()) || (a.description || "").toLowerCase().includes(actionSearch.toLowerCase());
              const matchCategory = actionCategoryFilter === "all" || a.category === actionCategoryFilter;
              return matchSearch && matchCategory;
            });
            return filtered.length === 0 ? (
              <p className="text-sm text-muted-foreground">No actions match your search</p>
            ) : (
              <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
                {filtered.map((action) => (
                  <button
                    key={action.id}
                    className="flex flex-col items-start gap-1 border rounded-lg p-3 hover:bg-muted/50 transition-colors text-left disabled:opacity-50 disabled:cursor-not-allowed"
                    disabled={!isPoweredOn || executing}
                    onClick={() => doExecuteAction(action.id)}
                  >
                    <div className="flex items-center gap-2 w-full">
                      <span className="font-medium text-sm">{action.name}</span>
                      <span className={`text-xs px-1.5 py-0.5 rounded ${categoryColors[action.category] || categoryColors.custom}`}>
                        {action.category}
                      </span>
                      {action.parameters && action.parameters.length > 0 && (
                        <Badge variant="secondary" className="text-xs">
                          <Settings2 className="h-3 w-3 mr-0.5" />{action.parameters.length} param{action.parameters.length !== 1 ? "s" : ""}
                        </Badge>
                      )}
                      {action.builtin && (
                        <Badge variant="outline" className="text-xs ml-auto">builtin</Badge>
                      )}
                    </div>
                    <p className="text-xs text-muted-foreground line-clamp-2">{action.description}</p>
                  </button>
                ))}
              </div>
            );
          })()}
        </CardContent>
      </Card>

      {/* Ad-Hoc Script */}
      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2">
            <Terminal className="h-4 w-4" /> Ad-Hoc Script
          </CardTitle>
        </CardHeader>
        <CardContent className="space-y-3">
          <p className="text-xs text-yellow-600 dark:text-yellow-400 flex items-center gap-1">
            <AlertTriangle className="h-3 w-3" />
            Scripts run with sudo privileges on the target VM.
          </p>
          <textarea
            className="w-full h-32 bg-gray-950 text-green-400 font-mono text-sm p-3 rounded-md border resize-y"
            placeholder="#!/bin/bash&#10;echo 'Hello from Forgemill'"
            value={adHocScript}
            onChange={(e) => setAdHocScript(e.target.value)}
            disabled={!isPoweredOn}
          />
          <Button
            size="sm"
            onClick={doExecuteAdHoc}
            disabled={!isPoweredOn || executing || !adHocScript.trim()}
          >
            <Play className="h-3 w-3 mr-1" /> Run Script
          </Button>
        </CardContent>
      </Card>

      {/* Execution History */}
      <Card>
        <CardHeader>
          <div className="flex items-center justify-between">
            <CardTitle>Execution History</CardTitle>
            <Button size="sm" variant="ghost" onClick={loadData}>
              <RefreshCw className="h-3 w-3 mr-1" /> Refresh
            </Button>
          </div>
        </CardHeader>
        <CardContent>
          {executionHistory.length === 0 ? (
            <p className="text-sm text-muted-foreground">No executions yet</p>
          ) : (() => {
            const paged = executionHistory.slice((historyPage - 1) * historyPerPage, historyPage * historyPerPage);
            return (
              <div className="space-y-2">
                {paged.map((exec) => (
                  <div key={exec.id} className="border rounded-md">
                    <button
                      className="w-full flex items-center justify-between p-3 text-left hover:bg-muted/30 transition-colors"
                      onClick={() => setExpandedExecId(expandedExecId === exec.id ? null : exec.id)}
                    >
                      <div className="flex items-center gap-3">
                        <span className="text-sm font-medium">{exec.action_name}</span>
                        <Badge variant={statusVariant(exec.status)}>{exec.status}</Badge>
                        {exec.exit_code !== null && exec.exit_code !== 0 && (
                          <span className="text-xs text-red-500 flex items-center gap-1">
                            <XCircle className="h-3.5 w-3.5" /> Exit code {exec.exit_code}
                          </span>
                        )}
                      </div>
                      <div className="flex items-center gap-3 text-xs text-muted-foreground">
                        <span>{formatDuration(exec.started_at, exec.completed_at)}</span>
                        <span>{formatDateTime(exec.created_at)}</span>
                      </div>
                    </button>
                    {expandedExecId === exec.id && exec.parameter_values && Object.keys(exec.parameter_values).length > 0 && (
                      <div className="border-t bg-muted/30 px-3 py-2">
                        <details className="text-xs">
                          <summary className="cursor-pointer text-muted-foreground font-medium flex items-center gap-1">
                            <Settings2 className="h-3 w-3" /> Parameters
                          </summary>
                          <div className="mt-1 grid grid-cols-[auto_1fr] gap-x-3 gap-y-0.5 pl-4">
                            {Object.entries(exec.parameter_values).map(([k, v]) => (
                              <><span key={`${k}-label`} className="text-muted-foreground">{k}:</span><span key={`${k}-value`} className="font-mono">{v}</span></>
                            ))}
                          </div>
                        </details>
                      </div>
                    )}
                    {expandedExecId === exec.id && exec.output && (
                      <div className="border-t bg-gray-950 p-3 font-mono text-xs text-green-400 max-h-60 overflow-auto whitespace-pre-wrap">
                        {exec.output}
                      </div>
                    )}
                  </div>
                ))}
                <Pagination
                  page={historyPage}
                  pageSize={historyPerPage}
                  totalItems={executionHistory.length}
                  onPageChange={setHistoryPage}
                  onPageSizeChange={(n) => { setHistoryPerPage(n); setHistoryPage(1); }}
                  itemLabel="executions"
                />
              </div>
            );
          })()}
        </CardContent>
      </Card>
    </div>
  );
}
