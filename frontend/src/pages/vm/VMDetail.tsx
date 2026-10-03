import { useTimezone } from "@/hooks/useTimezone";
import { useEffect, useState, useRef, useCallback } from "react";
import { useParams, useNavigate, useSearchParams } from "react-router-dom";
import { vms as vmApi, targets as targetApi } from "@/api/client";
import type { DeletePreview } from "@/api/client";
import { useProviders } from "@/context/ProviderContext";
import { useConfirm } from "@/components/ui/confirm-dialog";
import { useToast } from "@/components/ui/toast";
import type { ManagedVM, VMSnapshot, ResourceItem, VMNIC, VMDisk } from "@/types";
import { Select } from "@/components/ui/select";
import { Card, CardHeader, CardTitle, CardDescription, CardContent } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { InfoTip } from "@/components/ui/tooltip";
import { DangerZone, DangerZoneItem } from "@/components/ui/danger-zone";
import { TimeWithTooltip } from "@/components/ui/time-with-tooltip";
import { useNowTick } from "@/hooks/useNowTick";
import { vmLifecycleLabel, totalLifetimeRuntimeMs, formatDuration } from "@/lib/vmLifecycle";
import { timeAgo } from "@/lib/utils";
import {
  Play, Square, RotateCcw, Pause, Trash2, Camera, Undo2, ExternalLink, Cpu, MemoryStick,
  HardDrive, RefreshCw, KeyRound, Copy, X, Loader2, AlertTriangle, Clock, History, Network,
} from "lucide-react";
import { getErrorMessage, copyText } from "@/lib/utils";
import { powerVariant, powerLabel } from "@/lib/status";

import { NetworkAdaptersCard } from "./NetworkAdaptersCard";
import { DisksCard } from "./DisksCard";
import { CredentialsCard } from "./CredentialsCard";
import { ActionsTab } from "./ActionsTab";

type Tab = "overview" | "snapshots" | "actions";

export default function VMDetail() {
  const { formatDateTime } = useTimezone();
  const now = useNowTick();
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
  const [deletePreview, setDeletePreview] = useState<DeletePreview | null>(null);
  const [deletePreviewLoading, setDeletePreviewLoading] = useState(false);
  const [showResize, setShowResize] = useState(false);
  const [resizeCPU, setResizeCPU] = useState(0);
  const [resizeMem, setResizeMem] = useState(0);
  const [showExpandDisk, setShowExpandDisk] = useState(false);
  // Live disk inventory (GET /vms/:id/disks) — shared by the Disks card,
  // the Expand Disk panel and the Add Disk flow. null = not loaded yet.
  const [diskList, setDiskList] = useState<VMDisk[] | null>(null);
  const [disksLoading, setDisksLoading] = useState(false);
  const [disksError, setDisksError] = useState<string | null>(null);
  const disks = diskList ?? [];
  const [expandDiskKey, setExpandDiskKey] = useState<number | null>(null);
  const [expandDiskSize, setExpandDiskSize] = useState(0);
  // Add Disk — offered when the target's provider advertises features.disk_attach.
  const [showAddDisk, setShowAddDisk] = useState(false);
  const [addDiskSize, setAddDiskSize] = useState(20);
  const [addDiskDatastore, setAddDiskDatastore] = useState("");
  const [addDiskProvisioning, setAddDiskProvisioning] = useState("");
  const [diskDatastores, setDiskDatastores] = useState<ResourceItem[]>([]);
  const [diskDatastoresLoading, setDiskDatastoresLoading] = useState(false);
  // Add Network Adapter — only offered when the VM's target type advertises
  // features.nic_attach (vSphere today). Networks come from the target's live
  // resource inventory, the same list the deploy form uses.
  const { getProvider } = useProviders();
  const [targetType, setTargetType] = useState("");
  const [showAddNIC, setShowAddNIC] = useState(false);
  const [nicNetworks, setNicNetworks] = useState<ResourceItem[]>([]);
  const [nicNetworksLoading, setNicNetworksLoading] = useState(false);
  const [nicNetwork, setNicNetwork] = useState("");
  const [nicAdapter, setNicAdapter] = useState("");
  const [nicConnected, setNicConnected] = useState(true);
  const [nicVlan, setNicVlan] = useState("");
  // Live adapter inventory (GET /vms/:id/nics) — not persisted, fetched on
  // load and again after an attach. null = not loaded yet.
  const [nics, setNics] = useState<VMNIC[] | null>(null);
  const [nicsLoading, setNicsLoading] = useState(false);
  const [nicsError, setNicsError] = useState<string | null>(null);
  const [syncing, setSyncing] = useState(false);
  // Initial tab from ?tab= (the VMs card menu deep-links to snapshots); falls
  // back to overview for anything unrecognised.
  const [searchParams] = useSearchParams();
  const initialTab = searchParams.get("tab");
  const [tab, setTab] = useState<Tab>(initialTab === "snapshots" || initialTab === "actions" ? initialTab : "overview");

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

  const loadDisks = useCallback(async (): Promise<VMDisk[]> => {
    if (isNaN(vmId)) return [];
    setDisksLoading(true);
    try {
      const res = await vmApi.listDisks(vmId);
      const data = res.data || [];
      setDiskList(data);
      setDisksError(null);
      return data;
    } catch (e) {
      setDiskList([]);
      setDisksError(getErrorMessage(e, "Could not read disks from the hypervisor"));
      return [];
    } finally {
      setDisksLoading(false);
    }
  }, [vmId]);

  useEffect(() => { loadDisks(); }, [loadDisks]);

  // Expand Disk opens on a fresh read and pre-selects the first disk.
  const openExpandDisk = async () => {
    const data = await loadDisks();
    if (data.length > 0) {
      setExpandDiskKey(data[0].key);
      setExpandDiskSize(data[0].size_gb + 10);
    }
  };

  const doExpandDisk = async () => {
    if (expandDiskKey === null || expandDiskSize <= 0) return;
    setActing(true);
    try {
      await vmApi.expandDisk(vmId, expandDiskKey, { new_size_gb: expandDiskSize });
      setShowExpandDisk(false);
      reload();
      loadDisks();
      toast("Disk expanded successfully");
    } catch (e) {
      toast(getErrorMessage(e, "Failed to expand disk"), "error");
    } finally {
      setActing(false);
    }
  };

  const loadNICs = useCallback(async () => {
    if (isNaN(vmId)) return;
    setNicsLoading(true);
    try {
      const res = await vmApi.listNICs(vmId);
      setNics(res.data || []);
      setNicsError(null);
    } catch (e) {
      setNics([]);
      setNicsError(getErrorMessage(e, "Could not read network adapters from the hypervisor"));
    } finally {
      setNicsLoading(false);
    }
  }, [vmId]);

  useEffect(() => { loadNICs(); }, [loadNICs]);

  // Resolve the target's provider type so the NIC control can be gated on
  // the provider's declared capability rather than hardcoded platform names.
  useEffect(() => {
    if (!vm?.target_id) return;
    let cancelled = false;
    targetApi.get(vm.target_id)
      .then((res) => { if (!cancelled) setTargetType(res.data.type); })
      .catch(() => { /* leave the control hidden if the target can't be read */ });
    return () => { cancelled = true; };
  }, [vm?.target_id]);

  const nicProviderMeta = getProvider(targetType);
  const nicAttachSupported = Boolean(nicProviderMeta?.features?.nic_attach);
  // Adapter choices come from the provider's published list (first = default)
  // so the UI never offers a model the backend would reject.
  const nicAdapterTypes = nicProviderMeta?.nic_adapter_types?.length ? nicProviderMeta.nic_adapter_types : ["vmxnet3"];
  const nicVlanSupported = Boolean(nicProviderMeta?.features?.vlan_tagging);
  const diskAttachSupported = Boolean(nicProviderMeta?.features?.disk_attach);
  // Provisioning is only offered where the provider publishes choices
  // (vSphere: thin/thick); Proxmox's storage decides, so no selector.
  const diskProvisioningTypes = nicProviderMeta?.disk_provisioning_types ?? [];

  const loadDiskDatastores = async () => {
    if (!vm) return;
    if (!addDiskProvisioning && diskProvisioningTypes.length > 0) setAddDiskProvisioning(diskProvisioningTypes[0]);
    setDiskDatastoresLoading(true);
    try {
      const res = await targetApi.resources(vm.target_id);
      setDiskDatastores(res.data?.datastores || []);
    } catch (e) {
      toast(getErrorMessage(e, "Failed to load target datastores"), "error");
    } finally {
      setDiskDatastoresLoading(false);
    }
  };

  const doAddDisk = async () => {
    if (addDiskSize <= 0) return;
    setActing(true);
    try {
      const res = await vmApi.addDisk(vmId, {
        size_gb: addDiskSize,
        ...(addDiskDatastore ? { datastore: addDiskDatastore } : {}),
        ...(addDiskProvisioning ? { provisioning: addDiskProvisioning } : {}),
      });
      const disk = res.data?.disk;
      const summary = disk?.label ? `${disk.label} (${disk.size_gb} GB${disk.datastore ? ` on ${disk.datastore}` : ""})` : `${addDiskSize} GB disk`;
      if (disk?.pending) {
        toast(`${summary} saved — it attaches at the next power cycle (disk hot-plug is disabled on this VM)`);
      } else {
        toast(`${summary} attached — partition and format it inside the guest`);
      }
      setShowAddDisk(false);
      reload();
      loadDisks();
    } catch (e) {
      toast(getErrorMessage(e, "Failed to add disk"), "error");
    } finally {
      setActing(false);
    }
  };

  const loadNICNetworks = async () => {
    if (!vm) return;
    if (!nicAdapter) setNicAdapter(nicAdapterTypes[0]);
    setNicNetworksLoading(true);
    try {
      const res = await targetApi.resources(vm.target_id);
      const nets = res.data?.networks || [];
      setNicNetworks(nets);
      // Networks are submitted by inventory path when the provider reports one
      // (nested vCenter portgroups don't resolve by bare name) — same rule as
      // the deploy form.
      if (nets.length > 0 && !nicNetwork) setNicNetwork(nets[0].path || nets[0].name);
    } catch (e) {
      toast(getErrorMessage(e, "Failed to load target networks"), "error");
    } finally {
      setNicNetworksLoading(false);
    }
  };

  const copyAndNotify = (text: string) =>
    copyText(text).then(() => toast("Copied"), () => toast("Copy failed", "error"));

  const doAddNIC = async () => {
    if (!nicNetwork) return;
    setActing(true);
    try {
      const vlan = nicVlanSupported && nicVlan ? Number(nicVlan) : undefined;
      const res = await vmApi.addNIC(vmId, { network: nicNetwork, adapter_type: nicAdapter, connected: nicConnected, ...(vlan ? { vlan_tag: vlan } : {}) });
      const nic = res.data?.nic;
      const summary = nic?.label ? `${nic.label} (${nic.adapter_type}${nic.mac_address ? `, ${nic.mac_address}` : ""}${nic.vlan_tag ? `, VLAN ${nic.vlan_tag}` : ""})` : "Network adapter";
      if (nic?.pending) {
        toast(`${summary} saved — it attaches at the next power cycle (network hot-plug is disabled on this VM)`);
      } else if (nic && !nic.connected && nic.start_connected) {
        toast(`${summary} attached — it connects when the VM powers on`);
      } else {
        toast(`${summary} attached`);
      }
      setShowAddNIC(false);
      reload();
      loadNICs();
    } catch (e) {
      toast(getErrorMessage(e, "Failed to add network adapter"), "error");
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

  // Preview what a delete would actually affect before the user confirms —
  // fetched fresh each time a panel opens, since dependent resources can
  // change between visits.
  useEffect(() => {
    if (deleteMode === null) {
      setDeletePreview(null);
      return;
    }
    setDeletePreviewLoading(true);
    setDeletePreview(null);
    vmApi.previewDelete(vmId, deleteMode === "untrack")
      .then((res) => setDeletePreview(res.data))
      .catch(() => setDeletePreview(null))
      .finally(() => setDeletePreviewLoading(false));
  }, [deleteMode, vmId]);

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
    } catch (e: unknown) {
      toast(getErrorMessage(e, "Could not open the console"), "error");
    }
  };

  if (loading) {
    return <div className="flex items-center justify-center h-64"><Loader2 className="h-8 w-8 animate-spin text-primary" /></div>;
  }

  if (!vm) return <div className="text-muted-foreground">VM not found</div>;

  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-center gap-x-3 gap-y-2">
        <nav className="flex items-center gap-1 text-13 text-muted-foreground">
          <Button variant="ghost" size="sm" onClick={() => navigate("/vms")} className="h-auto p-0 font-normal hover:text-foreground">
            VMs
          </Button>
          <span>/</span>
          <span className="font-medium text-foreground">{vm.vm_name}</span>
        </nav>
        <h1 className="text-xl font-semibold tracking-tight">{vm.vm_name}</h1>
        <Badge variant={powerVariant(vm.power_state)} dot pulse={vm.power_state === "poweredOn" || vm.power_state === "running"}>
          {powerLabel(vm.power_state)}
        </Badge>
        <span className="hidden md:inline text-13 text-muted-foreground truncate">
          {vm.target_name}{vm.template_name ? ` · from ${vm.template_name}` : ""}
        </span>
        {/* Primary actions live in the header: power, console, sync. */}
        <div className="ml-auto flex items-center gap-1.5 flex-wrap">
          <div className="inline-flex items-center rounded-md border border-border bg-card shadow-xs overflow-hidden">
            <Button size="sm" variant="ghost" className="rounded-none h-8 px-2.5 gap-1.5 text-success hover:text-success" onClick={() => doPower("start")} disabled={acting} title="Start">
              <Play className="h-3.5 w-3.5" /> Start
            </Button>
            <span className="h-5 w-px bg-border" aria-hidden="true" />
            <Button size="sm" variant="ghost" className="rounded-none h-8 px-2.5 gap-1.5" onClick={() => doPower("stop")} disabled={acting} title="Stop">
              <Square className="h-3.5 w-3.5" /> Stop
            </Button>
            <span className="h-5 w-px bg-border" aria-hidden="true" />
            <Button size="sm" variant="ghost" className="rounded-none h-8 px-2.5 gap-1.5" onClick={() => doPower("restart")} disabled={acting} title="Restart">
              <RotateCcw className="h-3.5 w-3.5" /> Restart
            </Button>
            <span className="h-5 w-px bg-border" aria-hidden="true" />
            <Button size="sm" variant="ghost" className="rounded-none h-8 px-2.5 gap-1.5" onClick={() => doPower("suspend")} disabled={acting} title="Suspend">
              <Pause className="h-3.5 w-3.5" /> Suspend
            </Button>
          </div>
          <Button variant="outline" size="sm" className="gap-1.5" onClick={doConsole}>
            <ExternalLink className="h-3.5 w-3.5" /> Console
          </Button>
          <Button variant="outline" size="sm" onClick={doSync} disabled={syncing} className="gap-1.5">
            <RefreshCw className={`h-3.5 w-3.5 ${syncing ? "animate-spin" : ""}`} />
            {syncing ? "Syncing..." : "Sync"}
          </Button>
        </div>
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
          {/* Stat strip: the numbers people come for, in one row instead of six tiles. */}
          <Card>
            <div className="grid grid-cols-2 sm:grid-cols-3 lg:grid-cols-6 divide-y sm:divide-y-0 lg:divide-x divide-border">
              {[
                { label: "vCPU", value: vm.cpu || "—", icon: Cpu, tone: "text-info" },
                { label: "Memory", value: vm.memory_mb ? (vm.memory_mb >= 1024 ? `${(vm.memory_mb / 1024).toFixed(vm.memory_mb % 1024 ? 1 : 0)} GB` : `${vm.memory_mb} MB`) : "—", icon: MemoryStick, tone: "text-primary" },
                { label: "Disk", value: vm.disk_gb ? `${vm.disk_gb} GB` : "—", icon: HardDrive, tone: "text-success" },
                { label: "Address", value: vm.ip_address || "—", icon: Network, tone: "text-muted-foreground", mono: true, copy: !!vm.ip_address },
                { label: "Current session", value: vmLifecycleLabel(vm, now).label || "—", icon: Clock, tone: "text-warning", iso: vm.state_changed_at },
                { label: "Lifetime runtime", value: formatDuration(totalLifetimeRuntimeMs(vm, now)), icon: History, tone: "text-success" },
              ].map((st) => (
                <div key={st.label} className="flex items-center gap-2.5 px-4 py-3 min-w-0">
                  <st.icon className={`h-4 w-4 shrink-0 ${st.tone}`} aria-hidden="true" />
                  <div className="min-w-0">
                    <div className="text-2xs uppercase tracking-[0.08em] text-muted-foreground leading-4">{st.label}</div>
                    <div className={`text-[15px] font-semibold tabular-nums leading-5 truncate ${st.mono ? "font-mono text-sm" : ""}`}>
                      {st.iso ? <TimeWithTooltip iso={st.iso}>{String(st.value)}</TimeWithTooltip> : String(st.value)}
                      {st.copy && (
                        <button onClick={() => copyAndNotify(vm.ip_address)} className="ml-1.5 align-middle text-muted-foreground hover:text-foreground" aria-label="Copy IP address">
                          <Copy className="h-3 w-3" />
                        </button>
                      )}
                    </div>
                  </div>
                </div>
              ))}
            </div>
          </Card>

          <div className="grid gap-4 lg:grid-cols-3">
            <div className="lg:col-span-2 space-y-4">
            {/* VM Details */}
            <Card>
              <CardHeader>
                <CardTitle>Details</CardTitle>
              </CardHeader>
              <CardContent className="space-y-1">
                {[
                  { label: "Target", value: vm.target_name },
                  { label: "Template", value: vm.template_name || "N/A" },
                  { label: "IP Address", value: vm.ip_address || "N/A", mono: true, copyable: !!vm.ip_address },
                  { label: "OS Type", value: vm.os_type || "N/A" },
                ].map((row) => (
                  <div key={row.label} className="kv-row">
                    <span className="kv-label">{row.label}</span>
                    <div className="flex items-center gap-1.5 min-w-0">
                      <span className={`kv-value ${row.mono ? "font-mono" : ""}`}>{row.value}</span>
                      {row.copyable && (
                        <button
                          onClick={() => copyText(row.value).then(() => toast("Copied to clipboard"), (e) => toast(getErrorMessage(e, "Failed to copy"), "error"))}
                          className="text-muted-foreground hover:text-foreground transition-colors"
                          title="Copy to clipboard"
                        >
                          <Copy className="h-3.5 w-3.5" />
                        </button>
                      )}
                    </div>
                  </div>
                ))}
                {/* Rarely-needed identifiers: one muted footer line instead of full rows. */}
                <div className="flex flex-wrap items-center gap-x-3 gap-y-1 pt-2.5 text-2xs text-muted-foreground">
                  <span>VM ID <span className="font-mono text-foreground/80">{vm.id}</span></span>
                  <span aria-hidden="true">·</span>
                  <span>Ref <span className="font-mono text-foreground/80">{vm.vm_ref}</span></span>
                  <span aria-hidden="true">·</span>
                  <span>Last synced {vm.last_synced_at ? formatDateTime(vm.last_synced_at) : "never"}</span>
                </div>
              </CardContent>
            </Card>

            {/* Network adapters — live from the hypervisor */}
            <NetworkAdaptersCard nics={nics} loading={nicsLoading} error={nicsError} onRefresh={loadNICs} onCopy={copyAndNotify} />

            {/* Disks — live from the hypervisor */}
            <DisksCard disks={diskList} loading={disksLoading} error={disksError} onRefresh={loadDisks} />

            </div>
            <div className="space-y-4">
            {/* Power & Management */}
            <Card>
              <CardHeader>
                <CardTitle>Operations</CardTitle>
                <CardDescription>Resize, storage, networking and access.</CardDescription>
              </CardHeader>
              <CardContent className="space-y-2">
                <div className="space-y-2">
                  <Button size="sm" variant="outline" className="w-full justify-start gap-2" onClick={() => {
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
                        <div className="rounded-md border border-warning/30 bg-warning/5 px-3 py-2 flex items-start gap-2">
                          <AlertTriangle className="h-4 w-4 text-warning shrink-0 mt-0.5" />
                          <p className="text-xs text-warning">VM must be powered off before resizing.</p>
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

                <div className="pt-2 space-y-2">
                  <div className="flex items-center gap-1.5">
                    <Button size="sm" variant="outline" className="flex-1 justify-start gap-2" onClick={async () => {
                      if (!showExpandDisk) await openExpandDisk();
                      setShowExpandDisk(!showExpandDisk);
                    }}>
                      <HardDrive className="h-3.5 w-3.5" /> Expand Disk
                    </Button>
                    <InfoTip text="Grows the virtual disk only — it does not resize the partition or filesystem inside the guest OS, and it can't shrink a disk. Run the built-in “Expand Root Filesystem” action afterward to actually make the extra space usable." />
                  </div>
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

                {diskAttachSupported && (
                  <div className="pt-2 space-y-2">
                    <div className="flex items-center gap-1.5">
                      <Button size="sm" variant="outline" className="flex-1 justify-start gap-2" onClick={async () => {
                        if (!showAddDisk) await loadDiskDatastores();
                        setShowAddDisk(!showAddDisk);
                      }}>
                        <HardDrive className="h-3.5 w-3.5" /> Add Disk
                      </Button>
                      <InfoTip text="Attaches a new, empty virtual disk to this VM without a power cycle — hot-added on vSphere, and on Proxmox when the VM's hotplug setting includes disk (otherwise it attaches at the next power cycle). The guest sees a raw block device: partition and format it inside the OS afterwards." />
                    </div>
                    {showAddDisk && (
                      <div className="space-y-2 border rounded-md p-3 bg-muted/30">
                        <div>
                          <Label className="text-xs">Size (GB)</Label>
                          <Input type="number" min={1} max={65536} value={addDiskSize || ""} onChange={(e) => setAddDiskSize(Number(e.target.value))} placeholder="20" />
                        </div>
                        <div>
                          <Label className="text-xs">Datastore</Label>
                          {diskDatastoresLoading ? (
                            <p className="text-xs text-muted-foreground flex items-center gap-1.5"><Loader2 className="h-3 w-3 animate-spin" /> Loading datastores…</p>
                          ) : (
                            <Select value={addDiskDatastore} onChange={(e) => setAddDiskDatastore(e.target.value)}>
                              <option value="">Same as the VM's first disk</option>
                              {diskDatastores.map((d) => (
                                <option key={d.id} value={d.name}>{d.name}</option>
                              ))}
                            </Select>
                          )}
                        </div>
                        {diskProvisioningTypes.length > 0 && (
                          <div>
                            <Label className="text-xs">Provisioning</Label>
                            <Select value={addDiskProvisioning} onChange={(e) => setAddDiskProvisioning(e.target.value)}>
                              {diskProvisioningTypes.map((t, i) => (
                                <option key={t} value={t}>{t}{i === 0 ? " (recommended)" : ""}</option>
                              ))}
                            </Select>
                          </div>
                        )}
                        <Button size="sm" onClick={doAddDisk} disabled={acting || addDiskSize <= 0} className="w-full">Attach Disk</Button>
                      </div>
                    )}
                  </div>
                )}

                {nicAttachSupported && (
                  <div className="pt-2 space-y-2">
                    <div className="flex items-center gap-1.5">
                      <Button size="sm" variant="outline" className="flex-1 justify-start gap-2" onClick={async () => {
                        if (!showAddNIC) await loadNICNetworks();
                        setShowAddNIC(!showAddNIC);
                      }}>
                        <Network className="h-3.5 w-3.5" /> Add Network Adapter
                      </Button>
                      <InfoTip text="Adds a second (or further) virtual NIC to this VM without a power cycle — hot-added on vSphere, and on Proxmox when the VM's hotplug setting includes network (the default; otherwise it's saved and attaches at the next power cycle). The adapter appears in the guest as a new, unconfigured interface — assign it an address inside the guest OS afterwards. Existing adapters are not touched." />
                    </div>
                    {showAddNIC && (
                      <div className="space-y-2 border rounded-md p-3 bg-muted/30">
                        {nicNetworksLoading ? (
                          <p className="text-xs text-muted-foreground flex items-center gap-1.5"><Loader2 className="h-3 w-3 animate-spin" /> Loading networks…</p>
                        ) : nicNetworks.length === 0 ? (
                          <p className="text-xs text-muted-foreground">No networks reported by this target.</p>
                        ) : (
                          <>
                            <div>
                              <Label className="text-xs">Network</Label>
                              <Select value={nicNetwork} onChange={(e) => setNicNetwork(e.target.value)}>
                                {nicNetworks.map((n) => (
                                  <option key={n.id} value={n.path || n.name}>{n.name}</option>
                                ))}
                              </Select>
                            </div>
                            <div>
                              <Label className="text-xs">Adapter Type</Label>
                              <Select value={nicAdapter} onChange={(e) => setNicAdapter(e.target.value)}>
                                {nicAdapterTypes.map((t, i) => (
                                  <option key={t} value={t}>{t}{i === 0 ? " (recommended)" : ""}</option>
                                ))}
                              </Select>
                            </div>
                            {nicVlanSupported && (
                              <div>
                                <Label className="text-xs">VLAN Tag</Label>
                                <Input type="number" min={1} max={4094} value={nicVlan} onChange={(e) => setNicVlan(e.target.value)} placeholder="Untagged if empty" />
                              </div>
                            )}
                            <label className="flex items-center gap-2 text-xs cursor-pointer">
                              <input type="checkbox" checked={nicConnected} onChange={(e) => setNicConnected(e.target.checked)} className="h-3.5 w-3.5" />
                              Connect now and at power-on
                            </label>
                            <Button size="sm" onClick={doAddNIC} disabled={acting || !nicNetwork} className="w-full">Attach Adapter</Button>
                          </>
                        )}
                      </div>
                    )}
                  </div>
                )}

                <div className="pt-2 space-y-2">
                  <Button size="sm" variant="outline" className="w-full justify-start gap-2" onClick={async () => {
                    const ok = await showConfirm({ title: "Reset SSH Host Key", message: "This clears the stored SSH host key fingerprint. The next SSH connection will trust the new key automatically (TOFU). Use this after rebuilding a VM.", confirmLabel: "Reset" });
                    if (!ok) return;
                    try { await vmApi.resetHostKey(vm.id); toast("SSH host key reset — next connection will re-establish trust"); } catch (e) { toast(getErrorMessage(e, "Failed to reset host key"), "error"); }
                  }} disabled={acting}>
                    <KeyRound className="h-3.5 w-3.5" /> Reset SSH Host Key
                  </Button>
                </div>

              </CardContent>
            </Card>
            <Card>
              <CardHeader>
                <div className="flex items-center gap-1.5">
                  <CardTitle>Lifecycle</CardTitle>
                  <InfoTip text="Created is when Forgemill first tracked the VM. Current session is how long it has been in its present power state. Lifetime runtime only accrues while powered on — it freezes while suspended or off and is never reset." />
                </div>
              </CardHeader>
              <CardContent className="space-y-0">
                <div className="kv-row"><span className="kv-label">Created</span><span className="kv-value"><TimeWithTooltip iso={vm.created_at}>{timeAgo(vm.created_at)}</TimeWithTooltip></span></div>
                <div className="kv-row"><span className="kv-label">Current session</span><span className="kv-value"><TimeWithTooltip iso={vm.state_changed_at}>{vmLifecycleLabel(vm, now).label || "—"}</TimeWithTooltip></span></div>
                <div className="kv-row"><span className="kv-label">Last powered on</span><span className="kv-value"><TimeWithTooltip iso={vm.last_powered_on_at}>{vm.last_powered_on_at ? timeAgo(vm.last_powered_on_at) : "—"}</TimeWithTooltip></span></div>
                <div className="kv-row"><span className="kv-label">Last powered off</span><span className="kv-value"><TimeWithTooltip iso={vm.last_powered_off_at}>{vm.last_powered_off_at ? timeAgo(vm.last_powered_off_at) : "—"}</TimeWithTooltip></span></div>
                <div className="kv-row"><span className="kv-label">Lifetime runtime</span><span className="kv-value tabular-nums">{formatDuration(totalLifetimeRuntimeMs(vm, now))}</span></div>
              </CardContent>
            </Card>
              <CredentialsCard vmId={vmId} vmIp={vm.ip_address} />
            </div>
          </div>

          <div className="pt-3">
            <DangerZone description="Both remove this VM from Forgemill. Neither can be undone.">
              <DangerZoneItem
                title="Untrack VM"
                description="Forget it here; it keeps running on the hypervisor."
                expanded={deleteMode === "untrack" && (
                  <div className="border border-warning/30 rounded-md p-3 space-y-2 bg-warning/6">
                    <p className="text-2xs text-warning">Remove this VM from Forgemill only. The VM will continue running on the hypervisor — it just won't be tracked here anymore.</p>
                    <p className="text-2xs text-warning/80">⚠ This cannot be reversed. Untracked VMs cannot currently be re-imported into Forgemill.</p>
                    {deletePreviewLoading && (
                      <p className="text-2xs text-warning/80">Checking what else this affects…</p>
                    )}
                    {deletePreview && (deletePreview.dependent_snapshots > 0 || deletePreview.dependent_executions > 0) && (
                      <p className="text-2xs text-warning/80">
                        Forgemill also has {deletePreview.dependent_snapshots > 0 && `${deletePreview.dependent_snapshots} snapshot${deletePreview.dependent_snapshots === 1 ? "" : "s"}`}
                        {deletePreview.dependent_snapshots > 0 && deletePreview.dependent_executions > 0 && " and "}
                        {deletePreview.dependent_executions > 0 && `${deletePreview.dependent_executions} execution record${deletePreview.dependent_executions === 1 ? "" : "s"}`} on file for this VM.
                      </p>
                    )}
                    <Button size="sm" variant="secondary" onClick={() => doDelete(true)} disabled={acting} className="w-full">
                      Confirm Untrack
                    </Button>
                  </div>
                )}
              >
                <Button size="sm" variant="outline" className="gap-1.5" onClick={() => setDeleteMode(deleteMode === "untrack" ? null : "untrack")} disabled={acting} aria-expanded={deleteMode === "untrack"}>
                  <X className="h-3.5 w-3.5" /> Untrack VM
                </Button>
              </DangerZoneItem>
              <DangerZoneItem
                title="Destroy VM"
                description="Power off and delete it from the hypervisor, then remove it here."
                expanded={deleteMode === "destroy" && (
                  <div className="border border-destructive/30 rounded-md p-3 space-y-3 bg-destructive/6">
                    <p className="text-2xs text-destructive">This will permanently destroy this VM on the hypervisor and remove it from Forgemill. This cannot be undone.</p>
                    {deletePreviewLoading && (
                      <p className="text-2xs text-destructive/80">Checking what else this affects…</p>
                    )}
                    {deletePreview && (deletePreview.dependent_snapshots > 0 || deletePreview.dependent_executions > 0) && (
                      <p className="text-2xs text-destructive/80">
                        Forgemill also has {deletePreview.dependent_snapshots > 0 && `${deletePreview.dependent_snapshots} snapshot${deletePreview.dependent_snapshots === 1 ? "" : "s"}`}
                        {deletePreview.dependent_snapshots > 0 && deletePreview.dependent_executions > 0 && " and "}
                        {deletePreview.dependent_executions > 0 && `${deletePreview.dependent_executions} execution record${deletePreview.dependent_executions === 1 ? "" : "s"}`} on file for this VM.
                      </p>
                    )}
                    <div className="space-y-1.5">
                      <Label className="text-2xs text-destructive">Type <span className="font-mono font-bold">{vm?.vm_name}</span> to confirm:</Label>
                      <Input
                        value={destroyConfirmText}
                        onChange={(e) => setDestroyConfirmText(e.target.value)}
                        placeholder={vm?.vm_name}
                        autoComplete="off"
                        spellCheck={false}
                        className="font-mono text-13 border-destructive/50 focus-visible:border-destructive"
                      />
                    </div>
                    <Button size="sm" variant={destroyConfirmText === vm?.vm_name ? "danger" : "destructive"} onClick={() => doDelete(false)} disabled={acting || destroyConfirmText !== vm?.vm_name} className="w-full">
                      {destroyConfirmText === vm?.vm_name ? "Confirm Destroy" : "Type VM name to confirm"}
                    </Button>
                  </div>
                )}
              >
                <Button size="sm" variant="destructive" className="gap-1.5" onClick={() => { setDeleteMode(deleteMode === "destroy" ? null : "destroy"); setDestroyConfirmText(""); }} disabled={acting} aria-expanded={deleteMode === "destroy"}>
                  <Trash2 className="h-3.5 w-3.5" /> Destroy VM
                </Button>
              </DangerZoneItem>
            </DangerZone>
          </div>
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
