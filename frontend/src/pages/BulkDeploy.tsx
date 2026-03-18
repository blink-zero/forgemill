import { useEffect, useState } from "react";
import { useParams, useNavigate, Link } from "react-router-dom";
import { bulkDeploy } from "@/api/client";
import type { BulkDeployment } from "@/types";
import { Card, CardHeader, CardTitle, CardContent } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { ArrowLeft, Info, Layers, ListChecks, PlayCircle, Loader2 } from "lucide-react";

const statusVariant = (status: string) => {
  switch (status) {
    case "completed": return "success" as const;
    case "running": return "default" as const;
    case "failed": return "destructive" as const;
    default: return "secondary" as const;
  }
};

export function BulkDeployListPage() {
  const [bulks, setBulks] = useState<BulkDeployment[]>([]);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    bulkDeploy.list().then((res) => setBulks(res.data)).finally(() => setLoading(false));
  }, []);

  if (loading) {
    return <div className="flex items-center justify-center h-64"><Loader2 className="h-8 w-8 animate-spin text-primary" /></div>;
  }

  return (
    <div className="space-y-6">
      <h1 className="text-2xl font-bold">Bulk Deployments</h1>

      {/* Documentation section explaining bulk deploy workflow */}
      <Card className="border-blue-200 dark:border-blue-900 bg-blue-50/50 dark:bg-blue-950/20">
        <CardContent className="p-4 space-y-4">
          <div className="flex items-center gap-2">
            <Info className="h-5 w-5 text-blue-500" />
            <p className="font-medium text-sm text-blue-900 dark:text-blue-100">How Bulk Deployments Work</p>
          </div>
          <div className="grid gap-4 sm:grid-cols-3 text-sm">
            <div className="flex gap-3">
              <ListChecks className="h-5 w-5 text-blue-500 mt-0.5 shrink-0" />
              <div>
                <p className="font-medium text-blue-900 dark:text-blue-100">1. Define VMs</p>
                <p className="text-blue-700 dark:text-blue-300">
                  Submit a bulk deployment via the API with a list of VM definitions &mdash; each specifying a name, template, target, and config.
                </p>
              </div>
            </div>
            <div className="flex gap-3">
              <PlayCircle className="h-5 w-5 text-blue-500 mt-0.5 shrink-0" />
              <div>
                <p className="font-medium text-blue-900 dark:text-blue-100">2. Deploy</p>
                <p className="text-blue-700 dark:text-blue-300">
                  Forgemill deploys all VMs either sequentially or in parallel. Progress is tracked in real time on the detail page.
                </p>
              </div>
            </div>
            <div className="flex gap-3">
              <Layers className="h-5 w-5 text-blue-500 mt-0.5 shrink-0" />
              <div>
                <p className="font-medium text-blue-900 dark:text-blue-100">3. Monitor</p>
                <p className="text-blue-700 dark:text-blue-300">
                  Track each VM's status below. Click any bulk deployment to see per-VM progress, success/failure counts, and logs.
                </p>
              </div>
            </div>
          </div>
        </CardContent>
      </Card>

      {bulks.length === 0 ? (
        <Card>
          <CardContent className="py-10 text-center text-muted-foreground">
            No bulk deployments yet. Use the API to create a bulk deployment.
          </CardContent>
        </Card>
      ) : (
        <div className="space-y-4">
          {bulks.map((b) => (
            <Link key={b.id} to={`/deploy/bulk/${b.id}`}>
              <Card className="hover:bg-accent transition-colors">
                <CardContent className="flex items-center justify-between p-4">
                  <div>
                    <p className="text-sm font-medium">{b.name}</p>
                    <p className="text-xs text-muted-foreground">
                      {b.completed_vms}/{b.total_vms} completed
                      {b.failed_vms > 0 && ` (${b.failed_vms} failed)`}
                      {b.parallel && " | parallel"}
                    </p>
                  </div>
                  <Badge variant={statusVariant(b.status)}>{b.status}</Badge>
                </CardContent>
              </Card>
            </Link>
          ))}
        </div>
      )}
    </div>
  );
}

export function BulkDeployDetailPage() {
  const { id } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const [bulk, setBulk] = useState<BulkDeployment | null>(null);
  const [loading, setLoading] = useState(true);

  const fetchBulk = () => {
    const bulkId = parseInt(id || "0");
    if (!bulkId) return;
    bulkDeploy.get(bulkId).then((res) => setBulk(res.data)).finally(() => setLoading(false));
  };

  // I-11: Stop polling when bulk deployment reaches a terminal state
  const isTerminal = bulk?.status === "completed" || bulk?.status === "failed" || bulk?.status === "cancelled" || bulk?.status === "partial_failure";

  useEffect(() => {
    fetchBulk();
    if (isTerminal) return;
    const interval = setInterval(fetchBulk, 5000);
    return () => clearInterval(interval);
  }, [id, isTerminal]);

  if (loading) {
    return <div className="flex items-center justify-center h-64"><Loader2 className="h-8 w-8 animate-spin text-primary" /></div>;
  }

  if (!bulk) return <div className="text-muted-foreground">Bulk deployment not found</div>;

  const progress = bulk.total_vms > 0 ? ((bulk.completed_vms + bulk.failed_vms) / bulk.total_vms) * 100 : 0;

  return (
    <div className="space-y-6">
      <div className="flex items-center gap-4">
        <Button variant="ghost" size="sm" onClick={() => navigate("/deploy/bulk")}><ArrowLeft className="h-4 w-4 mr-1" /> Back</Button>
        <h1 className="text-2xl font-bold">{bulk.name}</h1>
        <Badge variant={statusVariant(bulk.status)}>{bulk.status}</Badge>
      </div>

      <Card>
        <CardContent className="p-4 space-y-3">
          <div className="flex justify-between text-sm">
            <span>Progress: {bulk.completed_vms + bulk.failed_vms}/{bulk.total_vms}</span>
            <span>{Math.round(progress)}%</span>
          </div>
          <div className="w-full bg-secondary rounded-full h-2">
            <div className="bg-primary h-2 rounded-full transition-all" style={{ width: `${progress}%` }} />
          </div>
          <div className="flex gap-4 text-sm">
            <span className="text-green-500">Completed: {bulk.completed_vms}</span>
            {bulk.failed_vms > 0 && <span className="text-red-500">Failed: {bulk.failed_vms}</span>}
            {bulk.parallel && <span className="text-muted-foreground">Mode: Parallel</span>}
          </div>
        </CardContent>
      </Card>

      <Card>
        <CardHeader><CardTitle>Deployments</CardTitle></CardHeader>
        <CardContent>
          {(bulk.deployments || []).length === 0 ? (
            <p className="text-sm text-muted-foreground">No deployments</p>
          ) : (
            <div className="space-y-2">
              {(bulk.deployments || []).map((d) => (
                <div key={d.id} className="flex items-center justify-between rounded-md border p-3">
                  <div>
                    <p className="text-sm font-medium">{d.vm_name}</p>
                    <p className="text-xs text-muted-foreground">{d.template_name} &middot; {d.target_name}</p>
                  </div>
                  <Badge variant={statusVariant(d.status)}>{d.status}</Badge>
                </div>
              ))}
            </div>
          )}
        </CardContent>
      </Card>
    </div>
  );
}
