import { useTimezone } from "@/hooks/useTimezone";
import { useState, useEffect, useMemo } from "react";
import { useNavigate } from "react-router-dom";
import { factoryApi, templates as templateApi, targets as targetsApi } from "@/api/client";
import type { OSDefinition, TemplateBuild, PrereqStatus, Template, Target, UpdateAvailable } from "@/types";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import {
  AlertTriangle,
  CheckCircle,
  Cpu,
  HardDrive,
  Info,
  RefreshCw,
  Cog,
  Search,
  ShieldCheck,
  Square,
  Trash2,
  XCircle,
  Loader2,
} from "lucide-react";
import { Select } from "@/components/ui/select";
import { Pagination } from "@/components/ui/pagination";
import { Input } from "@/components/ui/input";
import ProviderIcon from "@/components/ProviderIcon";
import { useToast } from "@/components/ui/toast";
import { PageHeader } from "@/components/ui/page-header";
import { usePageSize } from "@/hooks/usePageSize";

const statusColors: Record<string, string> = {
  pending: "bg-yellow-500/10 text-yellow-500",
  downloading: "bg-blue-500/10 text-blue-500",
  building: "bg-blue-500/10 text-blue-500",
  converting: "bg-blue-500/10 text-blue-500",
  completed: "bg-green-500/10 text-green-500",
  failed: "bg-red-500/10 text-red-500",
  cancelled: "bg-gray-500/10 text-gray-500",
};

export default function Factory() {
  const { formatDate, formatDateTime } = useTimezone();
  const { toast } = useToast();
  const navigate = useNavigate();
  const [definitions, setDefinitions] = useState<OSDefinition[]>([]);
  const [builds, setBuilds] = useState<TemplateBuild[]>([]);
  const [prereqs, setPrereqs] = useState<PrereqStatus | null>(null);
  const [loading, setLoading] = useState(true);
  const [managedTemplates, setManagedTemplates] = useState<Template[]>([]);
  const [updates, setUpdates] = useState<UpdateAvailable[]>([]);
  const [targetsList, setTargetsList] = useState<Target[]>([]);
  const [checkingUpdates, setCheckingUpdates] = useState(false);
  const [hasCheckedUpdates, setHasCheckedUpdates] = useState(false);
  const [buildSearch, setBuildSearch] = useState("");
  const [buildStatusFilter, setBuildStatusFilter] = useState("");
  const [buildTargetFilter, setBuildTargetFilter] = useState("");
  const [buildPage, setBuildPage] = useState(1);
  const [buildPageSize, setBuildPageSize] = usePageSize("factory_builds", 25);
  const [osSearch, setOsSearch] = useState("");

  useEffect(() => {
    loadData();
  }, []);

  // Reset build page on filter change
  useEffect(() => { setBuildPage(1); }, [buildSearch, buildStatusFilter, buildTargetFilter, buildPageSize]);

  const loadData = async () => {
    try {
      const [defsRes, buildsRes, prereqRes, templatesRes, targetsRes] = await Promise.all([
        factoryApi.listOSDefinitions(),
        factoryApi.listBuilds(),
        factoryApi.prerequisites(),
        templateApi.list(),
        targetsApi.list(),
      ]);
      setDefinitions(defsRes.data);
      setBuilds(buildsRes.data);
      setTargetsList(targetsRes.data || []);
      setPrereqs(prereqRes.data);
      setManagedTemplates((templatesRes.data || []).filter((t: Template) => t.managed_by_forgemill && t.lifecycle_status === "active"));
    } catch {
      toast("Failed to load factory data", "error");
    } finally {
      setLoading(false);
    }
  };

  const checkForUpdates = async () => {
    setCheckingUpdates(true);
    try {
      const res = await factoryApi.checkAllUpdates();
      setUpdates(res.data || []);
      setHasCheckedUpdates(true);
    } catch {
      toast("Failed to check for updates", "error");
    } finally {
      setCheckingUpdates(false);
    }
  };

  const deleteBuild = async (id: number) => {
    try {
      await factoryApi.deleteBuild(id);
      setBuilds((prev) => prev.filter((b) => b.id !== id));
    } catch {
      toast("Failed to delete build", "error");
    }
  };

  const cancelBuild = async (id: number) => {
    try {
      await factoryApi.cancelBuild(id);
      loadData();
    } catch {
      toast("Failed to cancel build", "error");
    }
  };

  const filteredBuilds = useMemo(() => builds.filter((b) => {
    if (buildStatusFilter && b.status !== buildStatusFilter) return false;
    if (buildTargetFilter && b.target_name !== buildTargetFilter) return false;
    if (buildSearch) {
      const q = buildSearch.toLowerCase();
      return (
        b.template_name.toLowerCase().includes(q) ||
        b.os_definition_id.toLowerCase().includes(q) ||
        (b.target_name || "").toLowerCase().includes(q) ||
        String(b.id).includes(q)
      );
    }
    return true;
  }), [builds, buildStatusFilter, buildTargetFilter, buildSearch]);

  const paginatedBuilds = useMemo(() => {
    return filteredBuilds.slice((buildPage - 1) * buildPageSize, buildPage * buildPageSize);
  }, [filteredBuilds, buildPage, buildPageSize]);

  if (loading) {
    return (
      <div className="flex items-center justify-center h-64">
        <Loader2 className="h-8 w-8 animate-spin text-primary" />
      </div>
    );
  }

  return (
    <div className="space-y-6">
      <PageHeader
        title="Template Factory"
        description="Build VM templates from ISO using Packer."
      />

      {/* Prerequisites Check */}
      {prereqs && !prereqs.packer_installed && (
        <Card className="p-4 border-yellow-500/50 bg-yellow-500/5">
          <div className="flex items-center gap-3">
            <AlertTriangle className="h-5 w-5 text-yellow-500" />
            <div>
              <p className="font-medium">Packer Not Installed</p>
              <p className="text-sm text-muted-foreground">
                Packer is required to build templates. Install it from{" "}
                <a
                  href="https://developer.hashicorp.com/packer/install"
                  target="_blank"
                  rel="noopener noreferrer"
                  className="text-primary underline"
                >
                  developer.hashicorp.com/packer/install
                </a>
              </p>
            </div>
          </div>
        </Card>
      )}

      {/* Update check results */}
      {updates.length > 0 ? (
        <div>
          <h2 className="text-lg font-semibold mb-3 flex items-center gap-2">
            <AlertTriangle className="h-5 w-5 text-yellow-500" />
            Updates Available
          </h2>
          <div className="space-y-2">
            {updates.map((u) => (
              <Card key={u.template_id} className="p-4 border-yellow-500/30">
                <div className="flex items-center justify-between">
                  <div>
                    <p className="font-medium">{u.template_name}</p>
                    <p className="text-sm text-muted-foreground">
                      {u.os_definition_id} &middot; v{u.current_version} &middot; ISO checksum changed
                    </p>
                    <div className="mt-1 text-xs font-mono text-muted-foreground space-y-0.5">
                      <p>Current: <span className="text-red-400">{u.current_checksum?.slice(0, 16)}...</span></p>
                      <p>Latest:&nbsp; <span className="text-green-400">{u.latest_checksum?.slice(0, 16)}...</span></p>
                    </div>
                  </div>
                  <Button
                    size="sm"
                    onClick={() => factoryApi.rebuildTemplate(u.template_id).then((res) => navigate(`/factory/build/${res.data.id}`))}
                  >
                    <RefreshCw className="h-4 w-4 mr-1" />
                    Rebuild
                  </Button>
                </div>
              </Card>
            ))}
          </div>
        </div>
      ) : hasCheckedUpdates && updates.length === 0 && (
        <Card className="p-4 border-green-500/30 bg-green-500/5">
          <div className="flex items-center gap-3">
            <CheckCircle className="h-5 w-5 text-green-500 shrink-0" />
            <div>
              <p className="font-medium text-sm">All templates are up to date</p>
              <p className="text-xs text-muted-foreground">
                ISO checksums match the upstream release mirrors. Click "Check for Updates" to re-verify.
              </p>
            </div>
          </div>
        </Card>
      )}

      {/* OS Definitions Grid */}
      <div>
        <div className="flex items-center justify-between mb-3">
          <h2 className="text-lg font-semibold">Available Operating Systems</h2>
          {definitions.length > 6 && (
            <div className="relative w-64">
              <Search className="absolute left-3 top-1/2 -translate-y-1/2 h-4 w-4 text-muted-foreground" />
              <Input
                placeholder="Search OS..."
                value={osSearch}
                onChange={(e) => setOsSearch(e.target.value)}
                className="pl-9"
              />
            </div>
          )}
        </div>
        <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
          {definitions
            .filter((def) => {
              if (!osSearch) return true;
              const q = osSearch.toLowerCase();
              return def.name.toLowerCase().includes(q) || def.arch.toLowerCase().includes(q) || def.install_method.toLowerCase().includes(q);
            })
            .map((def) => (
            <Card key={def.id} className="p-4">
              <div className="flex items-start justify-between">
                <div className="flex items-center gap-3">
                  <div className="h-10 w-10 rounded-lg bg-primary/10 flex items-center justify-center">
                    <Cpu className="h-5 w-5 text-primary" />
                  </div>
                  <div>
                    <h3 className="font-medium">{def.name}</h3>
                    <p className="text-sm text-muted-foreground">
                      {def.arch} &middot; {def.install_method}
                    </p>
                  </div>
                </div>
              </div>
              <div className="mt-3 flex items-center gap-4 text-sm text-muted-foreground">
                <span className="flex items-center gap-1">
                  <Cpu className="h-3 w-3" /> Min {def.min_cpu} vCPU
                </span>
                <span className="flex items-center gap-1">
                  <HardDrive className="h-3 w-3" /> Min {def.min_disk_gb}GB
                </span>
              </div>
              <Button
                className="mt-3 w-full"
                size="sm"
                disabled={!prereqs?.packer_installed}
                onClick={() =>
                  navigate(`/factory/build?os=${def.id}`)
                }
              >
                <Cog className="h-4 w-4 mr-1" />
                Build Template
              </Button>
            </Card>
          ))}
        </div>
      </div>

      {/* Recent Builds */}
      <div>
        <div className="flex flex-col sm:flex-row items-start sm:items-center justify-between gap-3 mb-3">
          <h2 className="text-lg font-semibold">Recent Builds</h2>
          {builds.length > 0 && (
            <div className="flex flex-wrap items-center gap-2 w-full sm:w-auto">
              <div className="relative flex-1 sm:flex-initial">
                <Search className="absolute left-2.5 top-1/2 -translate-y-1/2 h-3.5 w-3.5 text-muted-foreground" />
                <Input
                  value={buildSearch}
                  onChange={(e) => setBuildSearch(e.target.value)}
                  placeholder="Search builds..."
                  className="pl-8 h-8 text-sm w-full sm:w-48"
                />
              </div>
              <Select
                value={buildStatusFilter}
                onChange={(e) => setBuildStatusFilter(e.target.value)}
                className="h-8 w-auto px-2"
              >
                <option value="">All statuses</option>
                <option value="completed">Completed</option>
                <option value="building">Building</option>
                <option value="failed">Failed</option>
                <option value="cancelled">Cancelled</option>
                <option value="pending">Pending</option>
              </Select>
              {(() => {
                const targets = [...new Set(builds.map((b) => b.target_name).filter(Boolean))];
                return targets.length > 1 ? (
                  <Select
                    value={buildTargetFilter}
                    onChange={(e) => setBuildTargetFilter(e.target.value)}
                    className="h-8 w-auto px-2"
                  >
                    <option value="">All targets</option>
                    {targets.map((t) => (
                      <option key={t} value={t}>{t}</option>
                    ))}
                  </Select>
                ) : null;
              })()}
            </div>
          )}
        </div>
        {builds.length === 0 ? (
          <div className="text-center py-12 text-muted-foreground">
            <Cpu className="h-12 w-12 mx-auto mb-4 opacity-50" />
            <p>No template builds yet</p>
            <p className="text-sm mt-1">Select an OS above to start building</p>
          </div>
        ) : filteredBuilds.length === 0 ? (
            <div className="text-center py-12 text-muted-foreground">
              <Search className="h-12 w-12 mx-auto mb-4 opacity-50" />
              <p>No builds match your filters</p>
              <p className="text-sm mt-1">Try adjusting your search or status filter</p>
            </div>
          ) : (
          <div className="space-y-2">
            {paginatedBuilds.map((build) => (
              <Card key={build.id} className="p-4">
                <div className="flex items-center justify-between">
                  <div className="flex items-center gap-3">
                    <div>
                      <div className="flex items-center gap-2">
                        <span className="font-medium">
                          {build.template_name}
                        </span>
                        <Badge
                          variant="secondary"
                          className={statusColors[build.status] || ""}
                        >
                          {build.status}
                        </Badge>
                        {build.auto_triggered && (
                          <Badge variant="outline" className="text-xs">auto</Badge>
                        )}
                        {build.previous_build_id && (
                          <Badge variant="secondary" className="text-xs">rebuild</Badge>
                        )}
                      </div>
                      <p className="text-sm text-muted-foreground">
                        {build.os_definition_id} &middot; {build.target_name} &middot;{" "}
                        {formatDateTime(build.created_at)}
                      </p>
                    </div>
                  </div>
                  <div className="flex items-center gap-2">
                    {(build.status === "building" ||
                      build.status === "downloading" ||
                      build.status === "pending") && (
                      <>
                        <Button
                          variant="ghost"
                          size="sm"
                          onClick={() => navigate(`/factory/build/${build.id}`)}
                        >
                          View Progress
                        </Button>
                        <Button
                          variant="ghost"
                          size="sm"
                          onClick={() => cancelBuild(build.id)}
                        >
                          <Square className="h-4 w-4 mr-1" />
                          Cancel
                        </Button>
                      </>
                    )}
                    {build.status === "completed" && (
                      <Button
                        variant="ghost"
                        size="sm"
                        onClick={() => navigate(`/factory/build/${build.id}`)}
                      >
                        View Details
                      </Button>
                    )}
                    {build.status === "failed" && (
                      <Button
                        variant="ghost"
                        size="icon"
                        onClick={() => navigate(`/factory/build/${build.id}`)}
                      >
                        <XCircle className="h-4 w-4 text-red-500" />
                      </Button>
                    )}
                    {(build.status === "completed" ||
                      build.status === "failed" ||
                      build.status === "cancelled") && (
                      <Button
                        variant="ghost"
                        size="icon"
                        onClick={() => deleteBuild(build.id)}
                        className="text-destructive hover:text-destructive"
                      >
                        <Trash2 className="h-4 w-4" />
                      </Button>
                    )}
                  </div>
                </div>
              </Card>
            ))}
            {/* Build Pagination */}
            <Pagination
              page={buildPage}
              pageSize={buildPageSize}
              totalItems={filteredBuilds.length}
              onPageChange={setBuildPage}
              onPageSizeChange={setBuildPageSize}
              itemLabel="builds"
            />
          </div>
          )}
      </div>
    </div>
  );
}
