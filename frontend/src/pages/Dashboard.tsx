import { useEffect, useState } from "react";
import { Link } from "react-router-dom";
import { dashboard } from "@/api/client";
import type { DashboardData } from "@/types";
import { Card, CardHeader, CardTitle, CardContent } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Server, Box, Rocket, Monitor, Zap, Plus, ArrowRight, Terminal, Clock, Loader2 } from "lucide-react";
import ProviderIcon, { providerLabel } from "@/components/ProviderIcon";
import { SkeletonCard, Skeleton } from "@/components/ui/skeleton";

const statusVariant = (status: string) => {
  switch (status) {
    case "completed": return "success" as const;
    case "running": return "default" as const;
    case "failed": return "destructive" as const;
    case "cancelled": return "warning" as const;
    default: return "secondary" as const;
  }
};

function timeAgo(dateStr: string): string {
  const now = new Date();
  const then = new Date(dateStr);
  const diffMs = now.getTime() - then.getTime();
  const diffMin = Math.floor(diffMs / 60000);
  if (diffMin < 1) return "just now";
  if (diffMin < 60) return `${diffMin}m ago`;
  const diffHr = Math.floor(diffMin / 60);
  if (diffHr < 24) return `${diffHr}h ago`;
  // For items older than 24 hours, show formatted date
  const isThisYear = then.getFullYear() === now.getFullYear();
  const month = then.toLocaleDateString("en-US", { month: "short" });
  const day = then.getDate();
  if (isThisYear) {
    return `${month} ${day}`;
  }
  return `${month} ${day}, ${then.getFullYear()}`;
}

export default function Dashboard() {
  const [data, setData] = useState<DashboardData | null>(null);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    dashboard.get().then((res) => setData(res.data)).finally(() => setLoading(false));
  }, []);

  if (loading) {
    return (
      <div className="space-y-6">
        <div className="flex items-center justify-between">
          <Skeleton className="h-8 w-32" />
          <Skeleton className="h-10 w-28" />
        </div>
        <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
          {[1, 2, 3, 4].map((i) => (
            <SkeletonCard key={i} />
          ))}
        </div>
        <div className="grid gap-6 lg:grid-cols-2">
          <div className="rounded-lg border bg-card p-6 space-y-4">
            <Skeleton className="h-5 w-32" />
            {[1, 2, 3].map((i) => (
              <div key={i} className="flex items-center gap-3 py-2">
                <Skeleton className="h-8 w-8 rounded-full" />
                <div className="flex-1 space-y-2">
                  <Skeleton className="h-4 w-40" />
                  <Skeleton className="h-3 w-24" />
                </div>
              </div>
            ))}
          </div>
          <div className="rounded-lg border bg-card p-6 space-y-4">
            <Skeleton className="h-5 w-32" />
            {[1, 2, 3].map((i) => (
              <div key={i} className="flex items-center gap-3 py-2">
                <Skeleton className="h-8 w-8 rounded-full" />
                <div className="flex-1 space-y-2">
                  <Skeleton className="h-4 w-32" />
                  <Skeleton className="h-3 w-20" />
                </div>
              </div>
            ))}
          </div>
        </div>
      </div>
    );
  }

  if (!data) return <div className="text-muted-foreground">Failed to load dashboard</div>;

  const stats = [
    { label: "Targets", value: data.stats.total_targets, icon: Server, color: "text-blue-500", bg: "bg-blue-500/10", link: "/targets" },
    { label: "Templates", value: data.stats.total_templates, icon: Box, color: "text-purple-500", bg: "bg-purple-500/10", link: "/templates" },
    { label: "VMs", value: data.stats.total_vms, icon: Monitor, color: "text-green-500", bg: "bg-green-500/10", link: "/vms" },
    { label: "Actions", value: data.stats.total_actions, icon: Zap, color: "text-amber-500", bg: "bg-amber-500/10", link: "/actions" },
  ];

  // Merge deployments and executions into a single activity feed, sorted by date
  const activityItems: Array<{
    id: string;
    type: "deploy" | "execution";
    title: string;
    subtitle: string;
    status: string;
    date: string;
    link: string;
  }> = [];

  for (const d of data.recent_deployments || []) {
    activityItems.push({
      id: `deploy-${d.id}`,
      type: "deploy",
      title: d.vm_name || "Unnamed VM",
      subtitle: `${d.template_name} → ${d.target_name}`,
      status: d.status,
      date: d.created_at,
      link: d.status === "running" ? `/deploy/${d.id}` : "/history",
    });
  }

  for (const e of data.recent_executions || []) {
    activityItems.push({
      id: `exec-${e.id}`,
      type: "execution",
      title: e.action_name || "Ad-hoc Script",
      subtitle: `VM #${e.vm_id}`,
      status: e.status,
      date: e.created_at,
      link: `/vms/${e.vm_id}`,
    });
  }

  activityItems.sort((a, b) => new Date(b.date).getTime() - new Date(a.date).getTime());

  const isEmpty = data.stats.total_targets === 0;

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-bold">Dashboard</h1>
        <Link to="/deploy">
          <Button size="sm" className="gap-2">
            <Plus className="h-4 w-4" />
            Deploy VM
          </Button>
        </Link>
      </div>

      {/* Stats */}
      <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
        {stats.map((s) => (
          <Link key={s.label} to={s.link}>
            <Card className="hover:border-primary/50 cursor-pointer transition-colors">
              <CardHeader className="flex flex-row items-center justify-between pb-2">
                <CardTitle className="text-sm font-medium text-muted-foreground">{s.label}</CardTitle>
                <div className={`h-10 w-10 rounded-lg ${s.bg} flex items-center justify-center`}>
                  <s.icon className={`h-5 w-5 ${s.color}`} />
                </div>
              </CardHeader>
              <CardContent>
                <div className="text-4xl font-bold">{s.value}</div>
              </CardContent>
            </Card>
          </Link>
        ))}
      </div>

      {/* Getting Started - only show when no targets */}
      {isEmpty && (
        <Card className="border-dashed">
          <CardContent className="py-8">
            <div className="text-center space-y-3">
              <Rocket className="h-10 w-10 text-muted-foreground mx-auto" />
              <h3 className="text-lg font-semibold">Get Started</h3>
              <p className="text-sm text-muted-foreground max-w-md mx-auto">
                Add a target (ESXi or Proxmox), build a template with the Template Factory, then deploy your first VM.
              </p>
              <div className="flex items-center justify-center gap-3 pt-2">
                <Link to="/targets">
                  <Button variant="outline" size="sm" className="gap-2">
                    <Server className="h-4 w-4" />
                    Add Target
                  </Button>
                </Link>
                <ArrowRight className="h-4 w-4 text-muted-foreground" />
                <Link to="/factory">
                  <Button variant="outline" size="sm" className="gap-2">
                    <Box className="h-4 w-4" />
                    Build Template
                  </Button>
                </Link>
                <ArrowRight className="h-4 w-4 text-muted-foreground" />
                <Link to="/deploy">
                  <Button variant="outline" size="sm" className="gap-2">
                    <Rocket className="h-4 w-4" />
                    Deploy VM
                  </Button>
                </Link>
              </div>
            </div>
          </CardContent>
        </Card>
      )}

      <div className="grid gap-6 lg:grid-cols-2">
        {/* Recent Activity */}
        <Card>
          <CardHeader className="flex flex-row items-center justify-between">
            <CardTitle>Recent Activity</CardTitle>
            <Link to="/history" className="text-xs text-muted-foreground hover:text-primary transition-colors">
              View all →
            </Link>
          </CardHeader>
          <CardContent>
            {activityItems.length === 0 ? (
              <p className="text-sm text-muted-foreground py-4 text-center">No activity yet</p>
            ) : (
              <div className="space-y-3">
                {activityItems.slice(0, 8).map((item) => (
                  <Link
                    key={item.id}
                    to={item.link}
                    className="flex items-center justify-between rounded-md border p-3 hover:bg-accent transition-colors"
                  >
                    <div className="flex items-center gap-3">
                      {item.type === "deploy" ? (
                        <Rocket className="h-4 w-4 text-muted-foreground shrink-0" />
                      ) : (
                        <Terminal className="h-4 w-4 text-muted-foreground shrink-0" />
                      )}
                      <div className="min-w-0">
                        <p className="text-sm font-medium truncate">{item.title}</p>
                        <p className="text-xs text-muted-foreground truncate">{item.subtitle}</p>
                      </div>
                    </div>
                    <div className="flex items-center gap-2 shrink-0">
                      <span className="text-xs text-muted-foreground hidden sm:inline">
                        <Clock className="h-3 w-3 inline mr-1" />
                        {timeAgo(item.date)}
                      </span>
                      <Badge variant={statusVariant(item.status)}>{item.status}</Badge>
                    </div>
                  </Link>
                ))}
              </div>
            )}
          </CardContent>
        </Card>

        {/* Target Health */}
        <Card>
          <CardHeader className="flex flex-row items-center justify-between">
            <CardTitle>Target Health</CardTitle>
            <Link to="/targets" className="text-xs text-muted-foreground hover:text-primary transition-colors">
              Manage →
            </Link>
          </CardHeader>
          <CardContent>
            {data.targets?.length === 0 ? (
              <div className="text-center py-4">
                <p className="text-sm text-muted-foreground mb-3">No targets configured</p>
                <Link to="/targets">
                  <Button variant="outline" size="sm" className="gap-2">
                    <Plus className="h-4 w-4" />
                    Add Target
                  </Button>
                </Link>
              </div>
            ) : (
              <div className="space-y-3">
                {data.targets?.map((t) => (
                  <div key={t.id} className="flex items-center justify-between rounded-md border p-3">
                    <div className="flex items-center gap-3">
                      <ProviderIcon type={t.type} size={24} />
                      <div>
                        <p className="text-sm font-medium">{t.name}</p>
                        <p className="text-xs text-muted-foreground">{t.hostname} · {providerLabel(t.type)}</p>
                      </div>
                    </div>
                    <Badge variant={t.status === "connected" ? "success" : t.status === "error" ? "destructive" : "secondary"}>
                      {t.status}
                    </Badge>
                  </div>
                ))}
              </div>
            )}
          </CardContent>
        </Card>
      </div>
    </div>
  );
}
