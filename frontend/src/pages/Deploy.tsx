import { useEffect, useState } from "react";
import { useNavigate, useSearchParams } from "react-router-dom";
import { templates as templateApi, targets as targetApi, deploy as deployApi } from "@/api/client";
import type { Template, Resources } from "@/types";
import { Card, CardHeader, CardTitle, CardContent } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { ChevronRight, ChevronLeft, ChevronDown, ChevronUp, Rocket, Info, Settings2, Box, Loader2, Hammer, RotateCcw, Search } from "lucide-react";
import { Select } from "@/components/ui/select";
import { useToast } from "@/components/ui/toast";
import { getErrorMessage } from "@/lib/utils";
import { useProvider } from "@/context/ProviderContext";

type Step = "template" | "configure" | "review";

const DISK_PROVISIONING_OPTIONS = [
  { value: "", label: "Inherit from Template" },
  { value: "thin", label: "Thin Provisioned" },
  { value: "thick", label: "Thick Lazy Zero" },
  { value: "thick_eager_zero", label: "Thick Eager Zero" },
];

// Deploy field definitions are now loaded from provider metadata via ProviderContext

export default function Deploy() {
  const { toast } = useToast();
  const navigate = useNavigate();
  const [searchParams] = useSearchParams();
  const [step, setStep] = useState<Step>("template");
  const [templates, setTemplates] = useState<Template[]>([]);
  const [resources, setResources] = useState<Resources | null>(null);
  const [loading, setLoading] = useState(true);
  const [deploying, setDeploying] = useState(false);
  const [showAdvanced, setShowAdvanced] = useState(false);
  const [vmNameError, setVmNameError] = useState("");

  // Validate VM name (hostname-safe characters)
  const validateVmName = (name: string) => {
    if (!name) {
      setVmNameError("");
      return;
    }
    // RFC 1123: lowercase alphanumeric, hyphens, max 63 chars, can't start/end with hyphen
    if (name.length > 63) {
      setVmNameError("Name must be 63 characters or less");
    } else if (!/^[a-z0-9]/.test(name)) {
      setVmNameError("Must start with a lowercase letter or number");
    } else if (/[^a-z0-9-]/.test(name)) {
      setVmNameError("Only lowercase letters, numbers, and hyphens allowed");
    } else if (name.endsWith("-")) {
      setVmNameError("Cannot end with a hyphen");
    } else {
      setVmNameError("");
    }
  };

  const [selectedTemplate, setSelectedTemplate] = useState<Template | null>(null);
  const [templateSearch, setTemplateSearch] = useState("");
  const [config, setConfig] = useState({
    vm_name: "", datacenter: "", cluster: "", datastore: "", folder: "", network: "",
    cpu: 0, memory_mb: 0, disk_gb: 0, disk_provisioning: "",
    ip_address: "", netmask: "", gateway: "", dns: "", hostname: "", domain_name: "",
    ssh_public_key: "",
  });

  const platform = selectedTemplate?.target_type || "vcenter";
  const providerMeta = useProvider(platform);
  const platformFields = providerMeta?.deploy_fields || [];
  const showDiskProvisioning = Boolean(providerMeta?.features?.disk_provisioning);

  const preselect = searchParams.get("template");
  useEffect(() => {
    templateApi.list()
      .then((tplRes) => {
        setTemplates(tplRes.data || []);
        if (preselect) {
          const tpl = (tplRes.data || []).find((t: Template) => t.id === Number(preselect));
          if (tpl) {
            setSelectedTemplate(tpl);
            setConfig((c) => ({ ...c, cpu: tpl.cpu, memory_mb: tpl.memory_mb, disk_gb: tpl.disk_gb }));
            setStep("configure");
          }
        }
      })
      .finally(() => setLoading(false));
  }, [preselect]);

  useEffect(() => {
    if (selectedTemplate) {
      targetApi.resources(selectedTemplate.target_id).then((res) => {
        const r = res.data;
        setResources(r);
        // Apply smart defaults from backend to any empty config fields
        if (r?.defaults) {
          setConfig((c) => {
            const updates: Record<string, string> = {};
            for (const [key, val] of Object.entries(r.defaults!)) {
              if (key in c && !(c as Record<string, unknown>)[key]) {
                updates[key] = val;
              }
            }
            return Object.keys(updates).length > 0 ? { ...c, ...updates } : c;
          });
        }
      }).catch((e) => {
        toast(getErrorMessage(e, "Failed to load resource defaults"), "error");
      });
    }
  }, [selectedTemplate]);

  const selectTemplate = (tpl: Template) => {
    setSelectedTemplate(tpl);
    setConfig((c) => ({ ...c, cpu: tpl.cpu || 2, memory_mb: tpl.memory_mb || 2048, disk_gb: tpl.disk_gb || 40 }));
    setStep("configure");
  };

  const handleDeploy = async () => {
    if (!selectedTemplate) return;
    setDeploying(true);
    try {
      const res = await deployApi.start({
        template_id: selectedTemplate.id,
        target_id: selectedTemplate.target_id,
        vm_name: config.vm_name,
        datacenter: config.datacenter,
        cluster: config.cluster,
        datastore: config.datastore,
        folder: config.folder,
        network: config.network,
        cpu: config.cpu,
        memory_mb: config.memory_mb,
        disk_gb: config.disk_gb,
        disk_provisioning: config.disk_provisioning || undefined,
        ip_address: config.ip_address,
        netmask: config.netmask,
        gateway: config.gateway,
        dns: config.dns ? config.dns.split(",").map((s: string) => s.trim()) : [],
        hostname: config.hostname || config.vm_name,
        domain_name: config.domain_name,
        ssh_public_key: config.ssh_public_key,
      });
      navigate(`/deploy/${res.data.id}`, {
        state: {
          credentials: {
            username: res.data.initial_username,
            password: res.data.initial_password,
            ssh_key_injected: res.data.ssh_key_injected,
          },
        },
      });
    } catch (e) {
      toast(getErrorMessage(e, "Failed to start deployment"), "error");
    } finally {
      setDeploying(false);
    }
  };

  // Build the hint text for collapsed advanced options
  const advancedHintParts = platformFields.map((f) => f.label);
  if (showDiskProvisioning) advancedHintParts.push("Provisioning");
  const advancedHint = advancedHintParts.join(", ");

  if (loading) {
    return <div className="flex items-center justify-center h-64"><Loader2 className="h-8 w-8 animate-spin text-primary" /></div>;
  }

  if (templates.length === 0) {
    return (
      <div className="space-y-6">
        <h1 className="text-2xl font-bold">Deploy VM</h1>

        <div className="rounded-lg border bg-blue-500/5 border-blue-500/20 px-4 py-3 flex items-start gap-3">
          <Info className="h-5 w-5 text-blue-500 shrink-0 mt-0.5" />
          <div>
            <p className="text-sm font-medium">Deploy a VM</p>
            <p className="text-xs text-muted-foreground">Launch a virtual machine from your template library with cloud-init customisation, networking, and post-deploy actions.</p>
          </div>
        </div>

        <div className="flex flex-col items-center justify-center py-16 text-center gap-4">
          <div className="h-16 w-16 rounded-full bg-muted flex items-center justify-center">
            <Box className="h-8 w-8 text-muted-foreground opacity-60" />
          </div>
          <div>
            <p className="font-medium text-lg">No templates available</p>
            <p className="text-sm text-muted-foreground mt-1 max-w-sm">
              You need at least one template to deploy a VM. Build one with Forgemill Factory, or sync from an existing hypervisor target.
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
      </div>
    );
  }

  return (
    <div className="space-y-6">
      <h1 className="text-2xl font-bold">Deploy VM</h1>

      <div className="rounded-lg border bg-blue-500/5 border-blue-500/20 px-4 py-3 flex items-start gap-3">
        <Info className="h-5 w-5 text-blue-500 shrink-0 mt-0.5" />
        <div>
          <p className="text-sm font-medium">Deploy a VM</p>
          <p className="text-xs text-muted-foreground">Launch a virtual machine from your template library with cloud-init customisation, networking, and post-deploy actions.</p>
        </div>
      </div>

      <div className="flex items-center gap-2 text-sm">
        {(["template", "configure", "review"] as Step[]).map((s, i) => (
          <div key={s} className="flex items-center gap-2">
            {i > 0 && <ChevronRight className="h-4 w-4 text-muted-foreground" />}
            <span className={step === s ? "text-primary font-medium" : "text-muted-foreground"}>
              {i + 1}. {s.charAt(0).toUpperCase() + s.slice(1)}
            </span>
          </div>
        ))}
      </div>

      {step === "template" && (() => {
        const filteredTemplates = templates.filter((t) =>
          (t.name || "").toLowerCase().includes(templateSearch.toLowerCase()) ||
          (t.target_name || "").toLowerCase().includes(templateSearch.toLowerCase()) ||
          (t.os_type || "").toLowerCase().includes(templateSearch.toLowerCase())
        );
        return (
        <>
          {templates.length > 6 && (
            <div className="relative w-full max-w-sm">
              <Search className="absolute left-3 top-1/2 -translate-y-1/2 h-4 w-4 text-muted-foreground" />
              <Input
                placeholder="Search templates..."
                value={templateSearch}
                onChange={(e) => setTemplateSearch(e.target.value)}
                className="pl-9"
              />
            </div>
          )}
          <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
          {filteredTemplates.length === 0 ? (
            templates.length === 0 ? (
              <div className="text-center py-12 text-muted-foreground col-span-full">
                <Box className="h-12 w-12 mx-auto mb-4 opacity-50" />
                <p>No templates available</p>
                <p className="text-sm mt-1">Sync templates from a target to get started</p>
              </div>
            ) : (
              <div className="text-center py-12 text-muted-foreground col-span-full">
                <Search className="h-12 w-12 mx-auto mb-4 opacity-50" />
                <p>No templates match your search</p>
                <p className="text-sm mt-1">Try a different name, target, or OS type</p>
              </div>
            )
          ) : (
            filteredTemplates.map((tpl) => (
              <Card
                key={tpl.id}
                className={`cursor-pointer transition-colors hover:border-primary/50 ${selectedTemplate?.id === tpl.id ? "border-primary" : ""}`}
                onClick={() => selectTemplate(tpl)}
              >
                <CardHeader className="pb-2">
                  <CardTitle className="text-base">{tpl.name}</CardTitle>
                  <p className="text-xs text-muted-foreground">{tpl.target_name}</p>
                </CardHeader>
                <CardContent>
                  <div className="flex gap-3 text-xs text-muted-foreground">
                    {tpl.cpu > 0 && <span>{tpl.cpu} vCPU</span>}
                    {tpl.memory_mb > 0 && <span>{Math.round(tpl.memory_mb / 1024)}GB RAM</span>}
                    {tpl.disk_gb > 0 && <span>{tpl.disk_gb}GB disk</span>}
                    {!tpl.cpu && !tpl.memory_mb && !tpl.disk_gb && <span className="text-muted-foreground italic">No specs available</span>}
                  </div>
                </CardContent>
              </Card>
            ))
          )}
          </div>
        </>
        );
      })()}

      {step === "configure" && selectedTemplate && (
        <Card>
          <CardHeader>
            <CardTitle>Configure VM &mdash; {selectedTemplate.name}</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="grid gap-4 sm:grid-cols-2">
              {/* Essential fields - always visible */}
              <div className="space-y-2 sm:col-span-2">
                <Label>VM Name *</Label>
                <Input
                  value={config.vm_name}
                  onChange={(e) => {
                    const value = e.target.value.toLowerCase();
                    setConfig({ ...config, vm_name: value });
                    validateVmName(value);
                  }}
                  placeholder="web-server-01"
                  className={vmNameError ? "border-destructive" : ""}
                />
                {vmNameError && <p className="text-xs text-destructive mt-1">{vmNameError}</p>}
              </div>

              <div className="space-y-2">
                <Label>CPUs</Label>
                <Input type="number" min={1} value={config.cpu} onChange={(e) => setConfig({ ...config, cpu: Number(e.target.value) })} />
              </div>
              <div className="space-y-2">
                <Label>Memory (MB)</Label>
                <Input type="number" min={512} step={512} value={config.memory_mb} onChange={(e) => setConfig({ ...config, memory_mb: Number(e.target.value) })} />
              </div>
              <div className="space-y-2">
                <Label>Disk (GB)</Label>
                <Input type="number" min={1} value={config.disk_gb} onChange={(e) => setConfig({ ...config, disk_gb: Number(e.target.value) })} />
              </div>

              {/* Advanced Options toggle */}
              <div className="sm:col-span-2 border-t pt-4 mt-2">
                <button
                  type="button"
                  onClick={() => setShowAdvanced(!showAdvanced)}
                  className="flex items-center gap-2 text-sm text-muted-foreground hover:text-foreground transition-colors"
                >
                  <Settings2 className="h-4 w-4" />
                  <span className="font-medium">Advanced Options</span>
                  {showAdvanced ? <ChevronUp className="h-4 w-4" /> : <ChevronDown className="h-4 w-4" />}
                </button>
                {!showAdvanced && advancedHint && (
                  <p className="text-xs text-muted-foreground mt-1 ml-6">({advancedHint})</p>
                )}
              </div>

              {showAdvanced && (
                <>
                  {platformFields.map((field) => {
                    const items = resources?.[field.resource as keyof Resources];
                    return (
                      <div key={field.key} className="space-y-2">
                        <Label>{field.label}</Label>
                        <Select
                          value={(config as Record<string, unknown>)[field.key] as string || ""}
                          onChange={(e) => setConfig({ ...config, [field.key]: e.target.value })}
                        >
                          <option value="">{field.placeholder || `Select ${field.label.toLowerCase()}...`}</option>
                          {Array.isArray(items) && items.map((item) => (
                            <option key={item.id} value={item.name}>{item.name}</option>
                          ))}
                        </Select>
                      </div>
                    );
                  })}
                  {showDiskProvisioning && (
                    <div className="space-y-2">
                      <Label>Disk Provisioning</Label>
                      <Select value={config.disk_provisioning} onChange={(e) => setConfig({ ...config, disk_provisioning: e.target.value })}>
                        {DISK_PROVISIONING_OPTIONS.map((opt) => <option key={opt.value} value={opt.value}>{opt.label}</option>)}
                      </Select>
                    </div>
                  )}
                </>
              )}

              <div className="sm:col-span-2 border-t pt-4 mt-2">
                <h3 className="text-sm font-medium mb-3">Network Configuration (optional)</h3>
              </div>
              <div className="space-y-2">
                <Label>IP Address</Label>
                <Input value={config.ip_address} onChange={(e) => setConfig({ ...config, ip_address: e.target.value })} placeholder="DHCP if empty" />
              </div>
              <div className="space-y-2">
                <Label>Netmask</Label>
                <Input value={config.netmask} onChange={(e) => setConfig({ ...config, netmask: e.target.value })} placeholder="255.255.255.0" />
              </div>
              <div className="space-y-2">
                <Label>Gateway</Label>
                <Input value={config.gateway} onChange={(e) => setConfig({ ...config, gateway: e.target.value })} />
              </div>
              <div className="space-y-2">
                <Label>DNS (comma separated)</Label>
                <Input value={config.dns} onChange={(e) => setConfig({ ...config, dns: e.target.value })} placeholder="8.8.8.8, 8.8.4.4" />
              </div>
              <div className="space-y-2">
                <Label>Hostname</Label>
                <Input value={config.hostname} onChange={(e) => setConfig({ ...config, hostname: e.target.value })} placeholder="Same as VM name if empty" />
              </div>
              <div className="space-y-2">
                <Label>Domain</Label>
                <Input value={config.domain_name} onChange={(e) => setConfig({ ...config, domain_name: e.target.value })} placeholder="example.com" />
              </div>

              <div className="sm:col-span-2 border-t pt-4 mt-2">
                <h3 className="text-sm font-medium mb-3">Access (optional)</h3>
              </div>
              <div className="space-y-2 sm:col-span-2">
                <Label>SSH Public Key</Label>
                <textarea
                  value={config.ssh_public_key}
                  onChange={(e) => setConfig({ ...config, ssh_public_key: e.target.value })}
                  placeholder="ssh-rsa AAAA... (optional)"
                  rows={3}
                  className="flex w-full rounded-md border border-input bg-transparent px-3 py-2 text-sm shadow-sm placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring font-mono"
                />
                <p className="text-xs text-muted-foreground">A temporary password will always be generated. SSH key is optional for key-based access.</p>
              </div>

              <div className="sm:col-span-2 flex gap-2 mt-4">
                <Button variant="outline" onClick={() => setStep("template")}><ChevronLeft className="h-4 w-4 mr-1" /> Back</Button>
                <Button onClick={() => setStep("review")} disabled={!config.vm_name || !!vmNameError}>Next <ChevronRight className="h-4 w-4 ml-1" /></Button>
              </div>
            </div>
          </CardContent>
        </Card>
      )}

      {step === "review" && selectedTemplate && (
        <Card>
          <CardHeader>
            <CardTitle>Review Deployment</CardTitle>
          </CardHeader>
          <CardContent className="space-y-4">
            <div className="grid gap-3 sm:grid-cols-2 text-sm">
              <div><span className="text-muted-foreground">Template:</span> <span className="font-medium">{selectedTemplate.name}</span></div>
              <div><span className="text-muted-foreground">VM Name:</span> <span className="font-medium">{config.vm_name}</span></div>
              <div><span className="text-muted-foreground">CPUs:</span> <span className="font-medium">{config.cpu}</span></div>
              <div><span className="text-muted-foreground">Memory:</span> <span className="font-medium">{config.memory_mb}MB</span></div>
              {config.disk_gb > 0 && <div><span className="text-muted-foreground">Disk:</span> <span className="font-medium">{config.disk_gb}GB</span></div>}
              {showDiskProvisioning && config.disk_provisioning && <div><span className="text-muted-foreground">Disk Provisioning:</span> <span className="font-medium">{DISK_PROVISIONING_OPTIONS.find((o) => o.value === config.disk_provisioning)?.label}</span></div>}
              {platformFields.map((field) => {
                const val = (config as Record<string, unknown>)[field.key] as string;
                return val ? <div key={field.key}><span className="text-muted-foreground">{field.label}:</span> <span className="font-medium">{val}</span></div> : null;
              })}
              {config.ip_address && <div><span className="text-muted-foreground">IP:</span> <span className="font-medium">{config.ip_address}</span></div>}
              {config.ssh_public_key && <div className="sm:col-span-2"><span className="text-muted-foreground">SSH Key:</span> <span className="font-medium font-mono text-xs">{config.ssh_public_key.substring(0, 40)}...</span></div>}
            </div>
            <p className="text-xs text-muted-foreground">A temporary password will be generated and shown after deployment starts.</p>
            <div className="flex gap-2 pt-4">
              <Button variant="outline" onClick={() => setStep("configure")}><ChevronLeft className="h-4 w-4 mr-1" /> Back</Button>
              <Button onClick={handleDeploy} disabled={deploying}>
                <Rocket className="h-4 w-4 mr-2" />
                {deploying ? "Deploying..." : "Deploy"}
              </Button>
            </div>
          </CardContent>
        </Card>
      )}
    </div>
  );
}
