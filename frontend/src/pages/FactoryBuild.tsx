import { useState, useEffect } from "react";
import { useNavigate, useSearchParams } from "react-router-dom";
import { factoryApi, targets as targetsApi } from "@/api/client";
import type { OSDefinition, Target, Resources } from "@/types";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { ArrowLeft, ArrowRight, Loader2, Play, Info } from "lucide-react";
import { Select } from "@/components/ui/select";

type WizardStep = "os" | "target" | "configure" | "review";

export default function FactoryBuild() {
  const navigate = useNavigate();
  const [searchParams] = useSearchParams();
  const preselectedOS = searchParams.get("os") || "";

  const [step, setStep] = useState<WizardStep>(preselectedOS ? "target" : "os");
  const [definitions, setDefinitions] = useState<OSDefinition[]>([]);
  const [targetsList, setTargetsList] = useState<Target[]>([]);
  const [resources, setResources] = useState<Resources | null>(null);
  const [loadingResources, setLoadingResources] = useState(false);
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState("");

  // Form state
  const [selectedOS, setSelectedOS] = useState(preselectedOS);
  const [selectedTarget, setSelectedTarget] = useState<number>(0);
  const [cpu, setCpu] = useState(2);
  const [memoryMB, setMemoryMB] = useState(2048);
  const [diskGB, setDiskGB] = useState(40);

  // VMware fields
  const [datacenter, setDatacenter] = useState("");
  const [cluster, setCluster] = useState("");
  const [datastore, setDatastore] = useState("");
  const [folder, setFolder] = useState("");
  const [network, setNetwork] = useState("");

  // Proxmox fields
  const [node, setNode] = useState("");
  const [storagePool, setStoragePool] = useState("");
  const [bridge, setBridge] = useState("vmbr0");
  const [isoStorage, setISOStorage] = useState("");

  useEffect(() => {
    loadInitialData();
  }, []);

  const loadInitialData = async () => {
    try {
      const [defsRes, targetsRes] = await Promise.all([
        factoryApi.listOSDefinitions(),
        targetsApi.list(),
      ]);
      setDefinitions(defsRes.data);
      setTargetsList(targetsRes.data);

      if (preselectedOS) {
        const def = defsRes.data.find((d) => d.id === preselectedOS);
        if (def) {
          setCpu(def.min_cpu);
          setMemoryMB(def.min_memory_mb);
          setDiskGB(def.min_disk_gb);
        }
      }
    } catch {
      // handle error silently
    }
  };

  const loadResources = async (targetId: number) => {
    setLoadingResources(true);
    try {
      const res = await targetsApi.resources(targetId);
      setResources(res.data);
    } catch {
      setResources(null);
    } finally {
      setLoadingResources(false);
    }
  };

  const selectOS = (id: string) => {
    setSelectedOS(id);
    const def = definitions.find((d) => d.id === id);
    if (def) {
      setCpu(def.min_cpu);
      setMemoryMB(def.min_memory_mb);
      setDiskGB(def.min_disk_gb);
    }
    setStep("target");
  };

  const selectTarget = (id: number) => {
    setSelectedTarget(id);
    loadResources(id);
    setStep("configure");
  };

  // Auto-select when resources have only one option
  useEffect(() => {
    if (!resources) return;
    const target = targetsList.find((t) => t.id === selectedTarget);
    const isVMw = target?.type === "vcenter" || target?.type === "esxi";
    const isPve = target?.type === "proxmox";

    if (isVMw) {
      if (resources.datacenters?.length === 1 && !datacenter) setDatacenter(resources.datacenters[0].name);
      if (resources.datastores?.length === 1 && !datastore) setDatastore(resources.datastores[0].name);
      if (resources.networks?.length === 1 && !network) setNetwork(resources.networks[0].name);
      if (resources.clusters?.length === 1 && !cluster) setCluster(resources.clusters[0].name);
    }
    if (isPve) {
      if (resources.datacenters?.length === 1 && !node) setNode(resources.datacenters[0].name);
      if (resources.networks?.length === 1 && !bridge) setBridge(resources.networks[0].name);
      if (resources.iso_storages?.length === 1 && !isoStorage) setISOStorage(resources.iso_storages[0].name);
    }
  }, [resources]);

  const selectedTargetObj = targetsList.find((t) => t.id === selectedTarget);
  const selectedOSObj = definitions.find((d) => d.id === selectedOS);
  const isVMware =
    selectedTargetObj?.type === "vcenter" ||
    selectedTargetObj?.type === "esxi";
  const isProxmox = selectedTargetObj?.type === "proxmox";

  const canSubmit = selectedOS && selectedTarget > 0;

  const handleSubmit = async () => {
    if (!canSubmit) {
      setError("Please fill in all required fields: OS and target.");
      return;
    }
    setError("");
    setSubmitting(true);

    try {
      const data: Record<string, unknown> = {
        os_definition_id: selectedOS,
        target_id: selectedTarget,
        cpu,
        memory_mb: memoryMB,
        disk_gb: diskGB,
      };

      if (isVMware) {
        data.datacenter = datacenter;
        data.cluster = cluster;
        data.datastore = datastore;
        data.folder = folder;
        data.network = network;
      }

      if (isProxmox) {
        data.node = node;
        data.storage_pool = storagePool;
        data.bridge = bridge;
        data.iso_storage = isoStorage;
      }

      const res = await factoryApi.startBuild(data);
      navigate(`/factory/build/${res.data.id}`);
    } catch (err: unknown) {
      const msg =
        err && typeof err === "object" && "response" in err
          ? (err as { response?: { data?: { error?: string } } }).response?.data
              ?.error || "Build failed to start"
          : "Build failed to start";
      setError(msg);
    } finally {
      setSubmitting(false);
    }
  };

  const steps: { key: WizardStep; label: string }[] = [
    { key: "os", label: "Select OS" },
    { key: "target", label: "Select Target" },
    { key: "configure", label: "Configure" },
    { key: "review", label: "Review & Build" },
  ];

  const stepIndex = steps.findIndex((s) => s.key === step);

  return (
    <div className="space-y-6">
      <div className="flex items-center gap-3">
        <Button variant="ghost" size="icon" onClick={() => navigate("/factory")} aria-label="Back to Factory">
          <ArrowLeft className="h-4 w-4" />
        </Button>
        <div>
          <h1 className="text-2xl font-bold">Build Template</h1>
          <p className="text-muted-foreground">
            Configure and build a VM template using Packer
          </p>
        </div>
      </div>

      <div className="rounded-lg border bg-blue-500/5 border-blue-500/20 px-4 py-3 flex items-start gap-3">
        <Info className="h-5 w-5 text-blue-500 shrink-0 mt-0.5" />
        <div>
          <p className="text-sm font-medium">Build from ISO</p>
          <p className="text-xs text-muted-foreground">Create new VM templates from OS installation media. Packer handles the unattended install — Forgemill manages the lifecycle.</p>
        </div>
      </div>

      {/* Step indicator */}
      <div className="flex items-center gap-2">
        {steps.map((s, i) => (
          <div key={s.key} className="flex items-center gap-2">
            <div
              className={`flex h-8 w-8 items-center justify-center rounded-full text-sm font-medium ${
                i <= stepIndex
                  ? "bg-primary text-primary-foreground"
                  : "bg-muted text-muted-foreground"
              }`}
            >
              {i + 1}
            </div>
            <span
              className={`text-sm hidden sm:inline ${
                i <= stepIndex ? "text-foreground" : "text-muted-foreground"
              }`}
            >
              {s.label}
            </span>
            {i < steps.length - 1 && (
              <ArrowRight className="h-4 w-4 text-muted-foreground mx-1" />
            )}
          </div>
        ))}
      </div>

      {/* Step 1: Select OS */}
      {step === "os" && (
        <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
          {definitions.map((def) => (
            <Card
              key={def.id}
              className={`p-4 cursor-pointer transition-colors hover:border-primary/50 ${
                selectedOS === def.id ? "border-primary" : ""
              }`}
              onClick={() => selectOS(def.id)}
            >
              <h3 className="font-medium">{def.name}</h3>
              <p className="text-sm text-muted-foreground mt-1">
                {def.arch} &middot; {def.install_method} &middot; Min{" "}
                {def.min_disk_gb}GB disk
              </p>
            </Card>
          ))}
        </div>
      )}

      {/* Step 2: Select Target */}
      {step === "target" && (
        <div className="space-y-4">
          {targetsList.length === 0 ? (
            <Card className="p-6 text-center text-muted-foreground">
              No targets configured. Add a target first.
            </Card>
          ) : (
            <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
              {targetsList.map((target) => (
                <Card
                  key={target.id}
                  className={`p-4 cursor-pointer transition-colors hover:border-primary/50 ${
                    selectedTarget === target.id ? "border-primary" : ""
                  }`}
                  onClick={() => selectTarget(target.id)}
                >
                  <h3 className="font-medium">{target.name}</h3>
                  <p className="text-sm text-muted-foreground mt-1">
                    {target.type} &middot; {target.hostname}
                  </p>
                </Card>
              ))}
            </div>
          )}
          <Button variant="ghost" onClick={() => setStep("os")}>
            <ArrowLeft className="h-4 w-4 mr-1" /> Back
          </Button>
        </div>
      )}

      {/* Step 3: Configure */}
      {step === "configure" && (
        <div className="space-y-4">
          <Card className="p-6 space-y-4">
            <h3 className="font-medium">General Settings</h3>
            <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
              <div>
                <Label>vCPUs</Label>
                <Input
                  type="number"
                  min={selectedOSObj?.min_cpu || 1}
                  value={cpu}
                  onChange={(e) => setCpu(parseInt(e.target.value) || 1)}
                />
              </div>
              <div>
                <Label>Memory (MB)</Label>
                <Input
                  type="number"
                  min={selectedOSObj?.min_memory_mb || 512}
                  step={512}
                  value={memoryMB}
                  onChange={(e) => setMemoryMB(parseInt(e.target.value) || 512)}
                />
              </div>
              <div>
                <Label>Disk (GB)</Label>
                <Input
                  type="number"
                  min={selectedOSObj?.min_disk_gb || 10}
                  value={diskGB}
                  onChange={(e) => setDiskGB(parseInt(e.target.value) || 10)}
                />
              </div>
            </div>
          </Card>

          {isVMware && (
            <Card className="p-6 space-y-4">
              <h3 className="font-medium">VMware Settings</h3>
              {loadingResources ? (
                <div className="flex items-center gap-2 text-muted-foreground">
                  <Loader2 className="h-4 w-4 animate-spin" /> Loading resources...
                </div>
              ) : (
                <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                  <div>
                    <Label>Datacenter</Label>
                    {resources?.datacenters && resources.datacenters.length > 0 ? (
                      <Select
                        value={datacenter}
                        onChange={(e) => setDatacenter(e.target.value)}
                      >
                        <option value="">Select datacenter</option>
                        {resources.datacenters.map((dc) => (
                          <option key={dc.id} value={dc.name}>
                            {dc.name}
                          </option>
                        ))}
                      </Select>
                    ) : (
                      <Input
                        value={datacenter}
                        onChange={(e) => setDatacenter(e.target.value)}
                        placeholder="Datacenter name"
                      />
                    )}
                  </div>
                  <div>
                    <Label>Cluster</Label>
                    {resources?.clusters && resources.clusters.length > 0 ? (
                      <Select
                        value={cluster}
                        onChange={(e) => setCluster(e.target.value)}
                      >
                        <option value="">Select cluster</option>
                        {resources.clusters.map((c) => (
                          <option key={c.id} value={c.name}>
                            {c.name}
                          </option>
                        ))}
                      </Select>
                    ) : (
                      <Input
                        value={cluster}
                        onChange={(e) => setCluster(e.target.value)}
                        placeholder="Cluster name"
                      />
                    )}
                  </div>
                  <div>
                    <Label>Datastore</Label>
                    {resources?.datastores && resources.datastores.length > 0 ? (
                      <Select
                        value={datastore}
                        onChange={(e) => setDatastore(e.target.value)}
                      >
                        <option value="">Select datastore</option>
                        {resources.datastores.map((ds) => (
                          <option key={ds.id} value={ds.name}>
                            {ds.name}
                          </option>
                        ))}
                      </Select>
                    ) : (
                      <Input
                        value={datastore}
                        onChange={(e) => setDatastore(e.target.value)}
                        placeholder="Datastore name"
                      />
                    )}
                  </div>
                  {selectedTargetObj?.type !== "esxi" && (
                  <div>
                    <Label>Folder</Label>
                    {resources?.folders && resources.folders.length > 0 ? (
                      <Select
                        value={folder}
                        onChange={(e) => setFolder(e.target.value)}
                      >
                        <option value="">Select folder</option>
                        {resources.folders.map((f) => (
                          <option key={f.id} value={f.name}>
                            {f.name}
                          </option>
                        ))}
                      </Select>
                    ) : (
                      <Input
                        value={folder}
                        onChange={(e) => setFolder(e.target.value)}
                        placeholder="VM folder"
                      />
                    )}
                  </div>
                  )}
                  <div>
                    <Label>Network</Label>
                    {resources?.networks && resources.networks.length > 0 ? (
                      <Select
                        value={network}
                        onChange={(e) => setNetwork(e.target.value)}
                      >
                        <option value="">Select network</option>
                        {resources.networks.map((n) => (
                          <option key={n.id} value={n.name}>
                            {n.name}
                          </option>
                        ))}
                      </Select>
                    ) : (
                      <Input
                        value={network}
                        onChange={(e) => setNetwork(e.target.value)}
                        placeholder="Network name"
                      />
                    )}
                  </div>
                </div>
              )}
            </Card>
          )}

          {isProxmox && (
            <Card className="p-6 space-y-4">
              <h3 className="font-medium">Proxmox Settings</h3>
              {loadingResources ? (
                <div className="flex items-center gap-2 text-muted-foreground">
                  <Loader2 className="h-4 w-4 animate-spin" /> Loading resources...
                </div>
              ) : (
                <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                  <div>
                    <Label>Node</Label>
                    {resources?.datacenters && resources.datacenters.length > 0 ? (
                      <Select
                        value={node}
                        onChange={(e) => setNode(e.target.value)}
                      >
                        <option value="">Select node</option>
                        {resources.datacenters.map((n) => (
                          <option key={n.id} value={n.name}>{n.name}</option>
                        ))}
                      </Select>
                    ) : (
                      <Input value={node} onChange={(e) => setNode(e.target.value)} placeholder="pve" />
                    )}
                  </div>
                  <div>
                    <Label>Storage Pool</Label>
                    {resources?.datastores && resources.datastores.length > 0 ? (
                      <Select
                        value={storagePool}
                        onChange={(e) => setStoragePool(e.target.value)}
                      >
                        <option value="">Select storage</option>
                        {resources.datastores.map((s) => (
                          <option key={s.id} value={s.name}>{s.name}</option>
                        ))}
                      </Select>
                    ) : (
                      <Input value={storagePool} onChange={(e) => setStoragePool(e.target.value)} placeholder="local-lvm" />
                    )}
                  </div>
                  <div>
                    <Label>Network Bridge</Label>
                    {resources?.networks && resources.networks.length > 0 ? (
                      <Select
                        value={bridge}
                        onChange={(e) => setBridge(e.target.value)}
                      >
                        <option value="">Select bridge</option>
                        {resources.networks.map((n) => (
                          <option key={n.id} value={n.name}>{n.name}</option>
                        ))}
                      </Select>
                    ) : (
                      <Input value={bridge} onChange={(e) => setBridge(e.target.value)} placeholder="vmbr0" />
                    )}
                  </div>
                  <div>
                    <Label>ISO Storage <span className="text-xs text-muted-foreground">(directory-type only)</span></Label>
                    {(resources?.iso_storages && resources.iso_storages.length > 0) ? (
                      <Select
                        value={isoStorage}
                        onChange={(e) => setISOStorage(e.target.value)}
                      >
                        <option value="">Select ISO storage</option>
                        {resources.iso_storages.map((s) => (
                          <option key={s.id} value={s.name}>{s.name}</option>
                        ))}
                      </Select>
                    ) : (
                      <Input value={isoStorage} onChange={(e) => setISOStorage(e.target.value)} placeholder="local" />
                    )}
                  </div>
                </div>
              )}
            </Card>
          )}

          <div className="flex gap-2">
            <Button variant="ghost" onClick={() => setStep("target")}>
              <ArrowLeft className="h-4 w-4 mr-1" /> Back
            </Button>
            <Button onClick={() => setStep("review")}>
              Review <ArrowRight className="h-4 w-4 ml-1" />
            </Button>
          </div>
        </div>
      )}

      {/* Step 4: Review & Build */}
      {step === "review" && (
        <div className="space-y-4">
          <Card className="p-6 space-y-3">
            <h3 className="font-medium">Build Summary</h3>
            <div className="grid grid-cols-2 gap-2 text-sm">
              <span className="text-muted-foreground">Operating System</span>
              <span>{selectedOSObj?.name}</span>
              <span className="text-muted-foreground">Target</span>
              <span>
                {selectedTargetObj?.name} ({selectedTargetObj?.type})
              </span>
              <span className="text-muted-foreground">Template Name</span>
              <span className="italic text-muted-foreground">Auto-generated</span>
              <span className="text-muted-foreground">CPU</span>
              <span>{cpu} vCPU</span>
              <span className="text-muted-foreground">Memory</span>
              <span>{memoryMB} MB</span>
              <span className="text-muted-foreground">Disk</span>
              <span>{diskGB} GB</span>

              {isVMware && (
                <>
                  <span className="text-muted-foreground">Datacenter</span>
                  <span>{datacenter || "-"}</span>
                  <span className="text-muted-foreground">Cluster</span>
                  <span>{cluster || "-"}</span>
                  <span className="text-muted-foreground">Datastore</span>
                  <span>{datastore || "-"}</span>
                  <span className="text-muted-foreground">Folder</span>
                  <span>{folder || "-"}</span>
                  <span className="text-muted-foreground">Network</span>
                  <span>{network || "-"}</span>
                </>
              )}

              {isProxmox && (
                <>
                  <span className="text-muted-foreground">Node</span>
                  <span>{node || "-"}</span>
                  <span className="text-muted-foreground">Storage Pool</span>
                  <span>{storagePool || "-"}</span>
                  <span className="text-muted-foreground">Bridge</span>
                  <span>{bridge || "-"}</span>
                  <span className="text-muted-foreground">ISO Storage</span>
                  <span>{isoStorage || "-"}</span>
                </>
              )}
            </div>
          </Card>

          {error && (
            <Card className="p-4 border-red-500/50 bg-red-500/5">
              <p className="text-sm text-red-500">{error}</p>
            </Card>
          )}

          <div className="flex gap-2">
            <Button variant="ghost" onClick={() => setStep("configure")}>
              <ArrowLeft className="h-4 w-4 mr-1" /> Back
            </Button>
            <Button onClick={handleSubmit} disabled={submitting || !canSubmit}>
              {submitting ? (
                <Loader2 className="h-4 w-4 mr-1 animate-spin" />
              ) : (
                <Play className="h-4 w-4 mr-1" />
              )}
              Start Build
            </Button>
          </div>
        </div>
      )}
    </div>
  );
}
