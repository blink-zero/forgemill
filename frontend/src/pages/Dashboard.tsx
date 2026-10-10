import { useCallback, useEffect, useMemo, useState } from "react";
import { Link } from "react-router-dom";
import { dashboard } from "@/api/client";
import type { DashboardData, Target } from "@/types";
import { Card, CardHeader, CardTitle, CardContent } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Server, Box, Rocket, Monitor, Zap, Plus, ArrowRight, Terminal, Import, BookOpen, Hourglass, AlertTriangle, ChevronRight, RefreshCw, Activity } from "lucide-react";
import ProviderIcon, { providerLabel } from "@/components/ProviderIcon";
import { InventoryOnlyBadge, EvaluationBadge, evaluationDaysLeft } from "@/components/InventoryOnlyBadge";
import { SkeletonCard, Skeleton } from "@/components/ui/skeleton";
import { PageHeader } from "@/components/ui/page-header";
import { deploymentStatusVariant, executionStatusVariant } from "@/lib/status";
import { useVisiblePolling } from "@/hooks/useVisiblePolling";
import { useNowTick } from "@/hooks/useNowTick";
import { timeAgo, getErrorMessage } from "@/lib/utils";

/*
  The dashboard answers three questions in order: what is the fleet doing
  (instrument tiles), what needs me (one attention list, worst first), and
  what happened lately (activity feed + targets). It refreshes itself every
  30 s while the tab is visible, so a running deployment moves without a
  reload.
*/
const REFRESH_MS = 30_000;

type Tone = "destructive" | "warning" | "info";
type Attention = { key: string; tone: Tone; icon: typeof AlertTriangle; title: string; detail: string; link: string };
const TONE_RANK: Record<Tone, number> = { destructive: 0, warning: 1, info: 2 };
const TONE_TILE: Record<Tone, string> = {
  destructive: "bg-destructive/10 border-destructive/20 text-destructive",
  warning: "bg-warning/10 border-warning/20 text-warning",
  info: "bg-info/10 border-info/20 text-info",
};

function plural(n: number, one: string, many = `${one}s`) {
  return `${n} ${n === 1 ? one : many}`;
}

function buildAttention(data: DashboardData, now: number): Attention[] {
  const out: Attention[] = [];
  const targets = data.targets || [];

  const errored = targets.filter((t) => t.status === "error");
  if (errored.length > 0) {
    out.push({
      key: "targets-error", tone: "destructive", icon: AlertTriangle, link: "/targets",
      title: `${plural(errored.length, "target")} can't be reached`,
      detail: `${errored.map((t) => t.name).join(", ")} — deployments and syncs there fail until the connection is fixed.`,
    });
  }

  const dayAgo = now - 24 * 3600 * 1000;
  const failed = (data.recent_deployments || []).filter((d) => d.status === "failed" && new Date(d.completed_at || d.created_at).getTime() >= dayAgo);
  if (failed.length > 0) {
    const first = failed[0];
    out.push({
      key: "deploys-failed", tone: "destructive", icon: Rocket, link: failed.length === 1 ? `/deploy/${first.id}` : "/history",
      title: failed.length === 1 ? `Deployment of ${first.vm_name || "a VM"} failed` : `${failed.length} deployments failed in the last 24 hours`,
      detail: failed.length === 1 ? (first.error_message || `${first.template_name} → ${first.target_name}`) : failed.map((d) => d.vm_name).join(", "),
    });
  }

  const soon = targets.map((t) => ({ t, days: evaluationDaysLeft(t, now) })).filter((x): x is { t: Target; days: number } => x.days !== null && x.days <= 14);
  if (soon.length > 0) {
    const worst = Math.min(...soon.map((x) => x.days));
    out.push({
      key: "evaluation", tone: worst <= 1 ? "destructive" : "warning", icon: Hourglass, link: "/targets",
      title: soon.length === 1 ? `Evaluation license on ${soon[0].t.name} ${soon[0].days === 0 ? "ends today" : `ends in ${plural(soon[0].days, "day")}`}` : `${soon.length} targets have evaluation licenses ending within two weeks`,
      detail: `${soon.map((x) => `${x.t.name}: ${x.days === 0 ? "today" : `${x.days}d`}`).join(" · ")} — assign a paid or VMUG key before then; afterwards the host is inventory-only.`,
    });
  }

  const unmanagedTargets = targets.filter((t) => (t.unmanaged_vms ?? 0) > 0);
  const unmanaged = unmanagedTargets.reduce((n, t) => n + (t.unmanaged_vms ?? 0), 0);
  if (unmanaged > 0) {
    out.push({
      key: "unmanaged", tone: "warning", icon: Import, link: `/targets/${unmanagedTargets[0].id}/discover`,
      title: `${plural(unmanaged, "VM")} on your targets ${unmanaged === 1 ? "isn't" : "aren't"} managed by Forgemill`,
      detail: `${unmanagedTargets.map((t) => `${t.name}: ${t.unmanaged_vms}`).join(" · ")} — discover and adopt them to bring them under management.`,
    });
  }

  const inventoryOnly = targets.filter((t) => t.deploy_supported === false);
  if (inventoryOnly.length > 0) {
    out.push({
      key: "inventory-only", tone: "info", icon: BookOpen, link: "/targets",
      title: `${plural(inventoryOnly.length, "target")} ${inventoryOnly.length === 1 ? "is" : "are"} inventory-only`,
      detail: `${inventoryOnly.map((t) => `${t.name}${t.license_edition ? ` (${t.license_edition})` : ""}`).join(" · ")} — the license prohibits hypervisor writes; inventory, discover, adopt and SSH actions still work.`,
    });
  }

  if (data.stats.running_deploys > 0) {
    const running = (data.recent_deployments || []).filter((d) => d.status === "running");
    out.push({
      key: "running", tone: "info", icon: Rocket, link: running.length === 1 ? `/deploy/${running[0].id}` : "/history",
      title: `${plural(data.stats.running_deploys, "deployment")} in progress`,
      detail: running.length > 0 ? running.map((d) => `${d.vm_name} → ${d.target_name}`).join(" · ") : "Open History to follow it.",
    });
  }

  return out.sort((a, b) => TONE_RANK[a.tone] - TONE_RANK[b.tone]);
}

export default function Dashboard() {
  const [data, setData] = useState<DashboardData | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const [updatedAt, setUpdatedAt] = useState<number | null>(null);
  const [refreshing, setRefreshing] = useState(false);
  const now = useNowTick(15_000);

  const load = useCallback(async (quiet = false) => {
    if (!quiet) setLoading(true); else setRefreshing(true);
    try {
      const res = await dashboard.get();
      setData(res.data);
      setError(null);
      setUpdatedAt(Date.now());
    } catch (e: unknown) {
      // Keep the last good view on a failed refresh; only a first load shows the error state.
      setError(getErrorMessage(e, "Could not load the dashboard"));
    } finally {
      setLoading(false);
      setRefreshing(false);
    }
  }, []);
  useEffect(() => { void load(); }, [load]);
  const refresh = useCallback(() => { void load(true); }, [load]);
  useVisiblePolling(refresh, REFRESH_MS);

  const attention = useMemo(() => (data ? buildAttention(data, now) : []), [data, now]);

  if (loading) {
    return (
      <div className="space-y-5">
        <div className="flex items-center justify-between pb-4 border-b border-border">
          <Skeleton className="h-7 w-32" />
          <Skeleton className="h-9 w-28" />
        </div>
        <div className="grid gap-3 grid-cols-2 sm:grid-cols-3 lg:grid-cols-5">
          {[1, 2, 3, 4, 5].map((i) => <SkeletonCard key={i} />)}
        </div>
        <div className="grid gap-5 lg:grid-cols-5">
          <div className="lg:col-span-3 rounded-lg border bg-card p-4 space-y-3">
            <Skeleton className="h-5 w-32" />
            {[1, 2, 3, 4].map((i) => (
              <div key={i} className="flex items-center gap-3 py-1.5">
                <Skeleton className="h-7 w-7 rounded-md" />
                <div className="flex-1 space-y-1.5"><Skeleton className="h-4 w-40" /><Skeleton className="h-3 w-24" /></div>
                <Skeleton className="h-4 w-16" />
              </div>
            ))}
          </div>
          <div className="lg:col-span-2 rounded-lg border bg-card p-4 space-y-3">
            <Skeleton className="h-5 w-24" />
            {[1, 2, 3].map((i) => (
              <div key={i} className="flex items-center gap-3 py-1.5">
                <Skeleton className="h-6 w-6 rounded" />
                <div className="flex-1 space-y-1.5"><Skeleton className="h-4 w-32" /><Skeleton className="h-3 w-20" /></div>
              </div>
            ))}
          </div>
        </div>
      </div>
    );
  }

  if (!data) {
    return (
      <div className="space-y-5">
        <PageHeader title="Dashboard" />
        <Card className="border-dashed">
          <CardContent className="py-8 text-center space-y-3">
            <AlertTriangle className="h-8 w-8 text-destructive mx-auto" />
            <p className="text-sm text-muted-foreground">{error || "Could not load the dashboard."}</p>
            <Button variant="outline" size="sm" onClick={() => void load()}><RefreshCw className="h-3.5 w-3.5" /> Try again</Button>
          </CardContent>
        </Card>
      </div>
    );
  }

  const { stats } = data;
  const targets = data.targets || [];
  const inventoryOnlyCount = targets.filter((t) => t.deploy_supported === false).length;
  const tiles: { label: string; value: number; sub: string; icon: typeof Server; tone: string; link: string }[] = [
    {
      label: "VMs", value: stats.total_vms, icon: Monitor, link: "/vms", tone: "bg-success/10 border-success/20 text-success",
      sub: stats.total_vms === 0 ? "none yet" : [`${stats.vms_running} running`, stats.vms_stopped > 0 ? `${stats.vms_stopped} stopped` : ""].filter(Boolean).join(" · "),
    },
    {
      label: "Deployments", value: stats.total_deployments, icon: Rocket, link: "/history", tone: "bg-primary/10 border-primary/20 text-primary",
      sub: stats.total_deployments === 0 ? "none yet" : [
        `${stats.deployments_today} today`,
        stats.running_deploys > 0 ? `${stats.running_deploys} running` : "",
        stats.failed_deployments_24h > 0 ? `${stats.failed_deployments_24h} failed (24h)` : "",
      ].filter(Boolean).join(" · "),
    },
    {
      label: "Targets", value: stats.total_targets, icon: Server, link: "/targets", tone: "bg-info/10 border-info/20 text-info",
      sub: stats.total_targets === 0 ? "none yet" : [
        `${stats.targets_connected} connected`,
        stats.targets_error > 0 ? `${stats.targets_error} error` : "",
        inventoryOnlyCount > 0 ? `${inventoryOnlyCount} inventory-only` : "",
      ].filter(Boolean).join(" · "),
    },
    {
      label: "Templates", value: stats.total_templates, icon: Box, link: "/templates", tone: "bg-primary/10 border-primary/20 text-primary",
      sub: stats.total_templates === 0 ? "none yet" : stats.managed_templates > 0 ? `${stats.managed_templates} built by the Factory` : "all imported",
    },
    {
      label: "Actions", value: stats.active_actions, icon: Zap, link: "/actions", tone: "bg-warning/10 border-warning/20 text-warning",
      sub: stats.draft_actions > 0 ? `${plural(stats.draft_actions, "draft")} to review` : "ready to run",
    },
  ];

  type Item = { id: string; type: "deploy" | "execution"; title: string; subtitle: string; status: string; date: string; link: string };
  const activity: Item[] = [
    ...(data.recent_deployments || []).map((d): Item => ({
      id: `deploy-${d.id}`, type: "deploy", title: d.vm_name || "Unnamed VM",
      subtitle: `${d.template_name || "template"} → ${d.target_name || "target"}`, status: d.status, date: d.created_at, link: `/deploy/${d.id}`,
    })),
    ...(data.recent_executions || []).map((e): Item => ({
      id: `exec-${e.id}`, type: "execution", title: e.action_name || "Ad-hoc script",
      subtitle: e.vm_name ? `on ${e.vm_name}` : `VM #${e.vm_id}`, status: e.status, date: e.created_at, link: `/vms/${e.vm_id}`,
    })),
  ].sort((a, b) => new Date(b.date).getTime() - new Date(a.date).getTime()).slice(0, 8);

  const isEmpty = stats.total_targets === 0;

  return (
    <div className="space-y-5">
      <PageHeader
        title="Dashboard"
        description={updatedAt ? <span className="inline-flex items-center gap-1.5">Updated {timeAgo(new Date(updatedAt).toISOString())}{refreshing && <RefreshCw className="h-3 w-3 animate-spin" aria-label="Refreshing" />}</span> : undefined}
        actions={
          <>
            <Button variant="outline" size="sm" onClick={refresh} disabled={refreshing} aria-label="Refresh"><RefreshCw className={`h-3.5 w-3.5 ${refreshing ? "animate-spin" : ""}`} /></Button>
            <Link to="/deploy"><Button size="sm"><Plus className="h-4 w-4" /> Deploy VM</Button></Link>
          </>
        }
      />

      {/* Instrument tiles: one number each, one line of what it is made of. */}
      <div className="grid gap-3 grid-cols-2 sm:grid-cols-3 lg:grid-cols-5">
        {tiles.map((t) => (
          <Link key={t.label} to={t.link} className="group rounded-lg focus-visible:outline-hidden focus-visible:ring-2 focus-visible:ring-ring">
            <Card className="h-full p-4 hover:border-primary/40 transition-colors">
              <div className="flex items-start justify-between gap-2">
                <p className="text-2xs font-semibold uppercase tracking-[0.08em] text-muted-foreground">{t.label}</p>
                <div className={`h-8 w-8 rounded-md border flex items-center justify-center shrink-0 ${t.tone}`}><t.icon className="h-4 w-4" /></div>
              </div>
              <p className="mt-2 text-3xl font-semibold tracking-tight tabular-nums leading-none">{t.value}</p>
              <p className="mt-2 text-2xs text-muted-foreground truncate" title={t.sub}>{t.sub}</p>
            </Card>
          </Link>
        ))}
      </div>

      {isEmpty && (
        <Card className="border-dashed">
          <CardContent className="py-8">
            <div className="text-center space-y-3">
              <div className="h-10 w-10 rounded-md border bg-muted/40 flex items-center justify-center mx-auto"><Rocket className="h-5 w-5 text-muted-foreground" /></div>
              <h3 className="text-base font-semibold">Get started</h3>
              <p className="text-13 text-muted-foreground max-w-md mx-auto">Add a target (ESXi, vCenter or Proxmox), build a template with the Template Factory, then deploy your first VM.</p>
              <div className="flex items-center justify-center gap-3 pt-1 flex-wrap">
                <Link to="/targets"><Button variant="outline" size="sm"><Server className="h-4 w-4" /> Add target</Button></Link>
                <ArrowRight className="h-4 w-4 text-muted-foreground" />
                <Link to="/factory"><Button variant="outline" size="sm"><Box className="h-4 w-4" /> Build template</Button></Link>
                <ArrowRight className="h-4 w-4 text-muted-foreground" />
                <Link to="/deploy"><Button variant="outline" size="sm"><Rocket className="h-4 w-4" /> Deploy VM</Button></Link>
              </div>
            </div>
          </CardContent>
        </Card>
      )}

      {/* One list for everything that wants a decision, worst first. */}
      {attention.length > 0 && (
        <Card data-testid="dashboard-attention">
          <CardHeader className="flex flex-row items-center justify-between pb-2">
            <CardTitle>Needs attention</CardTitle>
            <Badge variant={attention[0].tone === "destructive" ? "destructive" : attention[0].tone === "warning" ? "warning" : "info"}>{attention.length}</Badge>
          </CardHeader>
          <CardContent className="pt-0">
            <ul className="divide-y divide-border -mx-4">
              {attention.map((a) => (
                <li key={a.key}>
                  <Link to={a.link} className="flex items-center gap-3 px-4 py-2.5 hover:bg-muted/50 transition-colors">
                    <div className={`h-7 w-7 rounded-md border flex items-center justify-center shrink-0 ${TONE_TILE[a.tone]}`}><a.icon className="h-3.5 w-3.5" /></div>
                    <div className="min-w-0 flex-1">
                      <p className="text-13 font-medium leading-5 truncate">{a.title}</p>
                      <p className="text-xs text-muted-foreground leading-4 line-clamp-2">{a.detail}</p>
                    </div>
                    <ChevronRight className="h-4 w-4 text-muted-foreground/60 shrink-0" />
                  </Link>
                </li>
              ))}
            </ul>
          </CardContent>
        </Card>
      )}

      <div className="grid gap-5 lg:grid-cols-5">
        {/* Recent activity: deployments and action runs in one timeline. */}
        <Card className="lg:col-span-3">
          <CardHeader className="flex flex-row items-center justify-between pb-2">
            <CardTitle className="flex items-center gap-2"><Activity className="h-4 w-4 text-muted-foreground" /> Recent activity</CardTitle>
            <Link to="/history" className="text-xs text-muted-foreground hover:text-primary transition-colors">View all →</Link>
          </CardHeader>
          <CardContent className="pt-0">
            {activity.length === 0 ? (
              <p className="text-13 text-muted-foreground py-6 text-center">No deployments or action runs yet.</p>
            ) : (
              <ul className="divide-y divide-border -mx-4">
                {activity.map((item) => {
                  const live = item.status === "running" || item.status === "pending";
                  const variant = item.type === "deploy" ? deploymentStatusVariant(item.status) : executionStatusVariant(item.status);
                  return (
                    <li key={item.id}>
                      <Link to={item.link} className="flex items-center gap-3 px-4 py-2.5 hover:bg-muted/50 transition-colors">
                        <div className="h-7 w-7 rounded-md border bg-muted/40 flex items-center justify-center shrink-0 text-muted-foreground">
                          {item.type === "deploy" ? <Rocket className="h-3.5 w-3.5" /> : <Terminal className="h-3.5 w-3.5" />}
                        </div>
                        <div className="min-w-0 flex-1">
                          <p className="text-13 font-medium leading-5 truncate">{item.title}</p>
                          <p className="text-xs text-muted-foreground leading-4 truncate">{item.type === "deploy" ? "Deployment" : "Action"} · {item.subtitle}</p>
                        </div>
                        <span className="text-xs text-muted-foreground tabular-nums shrink-0 hidden sm:inline" title={new Date(item.date).toLocaleString()}>{timeAgo(item.date)}</span>
                        <Badge variant={variant} dot pulse={live} className="shrink-0">{item.status}</Badge>
                      </Link>
                    </li>
                  );
                })}
              </ul>
            )}
          </CardContent>
        </Card>

        {/* Targets: connection state and anything that limits what they can do. */}
        <Card className="lg:col-span-2">
          <CardHeader className="flex flex-row items-center justify-between pb-2">
            <CardTitle className="flex items-center gap-2"><Server className="h-4 w-4 text-muted-foreground" /> Targets</CardTitle>
            <Link to="/targets" className="text-xs text-muted-foreground hover:text-primary transition-colors">Manage →</Link>
          </CardHeader>
          <CardContent className="pt-0">
            {targets.length === 0 ? (
              <div className="text-center py-6 space-y-3">
                <p className="text-13 text-muted-foreground">No targets yet.</p>
                <Link to="/targets"><Button variant="outline" size="sm"><Plus className="h-4 w-4" /> Add target</Button></Link>
              </div>
            ) : (
              <ul className="divide-y divide-border -mx-4">
                {targets.map((t) => (
                  <li key={t.id}>
                    <Link to="/targets" className="flex items-center gap-3 px-4 py-2.5 hover:bg-muted/50 transition-colors">
                      <ProviderIcon type={t.type} size={22} />
                      <div className="min-w-0 flex-1">
                        <p className="text-13 font-medium leading-5 truncate">{t.name}</p>
                        <p className="text-xs text-muted-foreground leading-4 truncate font-mono">{t.hostname} <span className="font-sans">· {providerLabel(t.type)}</span></p>
                        {(t.deploy_supported === false || evaluationDaysLeft(t, now) !== null) && (
                          <div className="flex items-center gap-1 flex-wrap mt-1"><InventoryOnlyBadge target={t} /><EvaluationBadge target={t} /></div>
                        )}
                      </div>
                      <Badge variant={t.status === "connected" ? "success" : t.status === "error" ? "destructive" : "secondary"} dot className="shrink-0" title={t.last_connected_at ? `Last connected ${timeAgo(t.last_connected_at)}` : undefined}>{t.status}</Badge>
                    </Link>
                  </li>
                ))}
              </ul>
            )}
          </CardContent>
        </Card>
      </div>
    </div>
  );
}
