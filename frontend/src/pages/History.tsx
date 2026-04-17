import { useTimezone } from "@/hooks/useTimezone";
import { useEffect, useState } from "react";
import { Link } from "react-router-dom";
import { history as historyApi } from "@/api/client";
import type { Deployment, PaginatedResponse } from "@/types";
import { Card, CardHeader, CardTitle, CardContent } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Info, Loader2, Search, X, Rocket } from "lucide-react";
import { Select } from "@/components/ui/select";
import { Input } from "@/components/ui/input";
import { Pagination } from "@/components/ui/pagination";
import { PageHeader } from "@/components/ui/page-header";
import { EmptyState } from "@/components/ui/empty-state";

const statusVariant = (status: string) => {
  switch (status) {
    case "completed": return "success" as const;
    case "running": return "default" as const;
    case "failed": return "destructive" as const;
    case "cancelled": return "warning" as const;
    default: return "secondary" as const;
  }
};

export default function HistoryPage() {
  const { formatDateTime } = useTimezone();
  const [data, setData] = useState<PaginatedResponse<Deployment> | null>(null);
  const [loading, setLoading] = useState(true);
  const [page, setPage] = useState(1);
  const [statusFilter, setStatusFilter] = useState("");
  const [search, setSearch] = useState("");

  useEffect(() => {
    setLoading(true);
    historyApi
      .list({ page, per_page: 20, status: statusFilter || undefined, search: search || undefined })
      .then((res) => setData(res.data))
      .finally(() => setLoading(false));
  }, [page, statusFilter, search]);

  // Reset to page 1 when filters change
  useEffect(() => {
    setPage(1);
  }, [statusFilter, search]);

  const totalPages = data ? Math.ceil(data.total / data.per_page) : 0;

  return (
    <div className="space-y-6">
      <PageHeader
        title={
          <span className="flex items-center gap-2">
            Deployment History
            {data && data.total > 0 && <Badge variant="outline">{data.total}</Badge>}
          </span>
        }
        description="Track every VM deployment across your targets — status, logs, and details."
        actions={
          <>
            <div className="relative w-full sm:w-64">
              <Search className="absolute left-3 top-1/2 -translate-y-1/2 h-4 w-4 text-muted-foreground" />
              <Input
                placeholder="Search VM, template, target..."
                value={search}
                onChange={(e) => setSearch(e.target.value)}
                className="pl-9 pr-8"
              />
              {search && (
                <button
                  onClick={() => setSearch("")}
                  className="absolute right-2 top-1/2 -translate-y-1/2 text-muted-foreground hover:text-foreground"
                >
                  <X className="h-4 w-4" />
                </button>
              )}
            </div>
            <Select
              value={statusFilter}
              onChange={(e) => setStatusFilter(e.target.value)}
              className="w-auto"
            >
              <option value="">All statuses</option>
              <option value="completed">Completed</option>
              <option value="running">Running</option>
              <option value="failed">Failed</option>
              <option value="cancelled">Cancelled</option>
              <option value="pending">Pending</option>
            </Select>
          </>
        }
      />

      {loading ? (
        <div className="flex items-center justify-center h-64"><Loader2 className="h-8 w-8 animate-spin text-primary" /></div>
      ) : !data || !data.data || data.data.length === 0 ? (
        <EmptyState
          icon={Rocket}
          title="No deployments yet"
          description="Deploy your first VM to see its history here."
        />
      ) : (
        <Card>
          <CardContent className="p-0">
            <div className="overflow-x-auto">
              <table className="w-full text-sm">
                <thead>
                  <tr className="border-b">
                    <th className="text-left p-4 font-medium text-muted-foreground">ID</th>
                    <th className="text-left p-4 font-medium text-muted-foreground">VM Name</th>
                    <th className="text-left p-4 font-medium text-muted-foreground">Template</th>
                    <th className="text-left p-4 font-medium text-muted-foreground">Target</th>
                    <th className="text-left p-4 font-medium text-muted-foreground">Status</th>
                    <th className="text-left p-4 font-medium text-muted-foreground">Created</th>
                    <th className="text-left p-4 font-medium text-muted-foreground">Duration</th>
                    <th className="text-left p-4 font-medium text-muted-foreground"></th>
                  </tr>
                </thead>
                <tbody>
                  {data.data.map((d) => (
                    <tr key={d.id} className="border-b hover:bg-accent/50 transition-colors">
                      <td className="p-4">
                        <Link to={`/deploy/${d.id}`} className="text-primary hover:underline">#{d.id}</Link>
                      </td>
                      <td className="p-4 font-medium">{d.vm_name}</td>
                      <td className="p-4 text-muted-foreground">{d.template_name}</td>
                      <td className="p-4 text-muted-foreground">{d.target_name}</td>
                      <td className="p-4"><Badge variant={statusVariant(d.status)}>{d.status}</Badge></td>
                      <td className="p-4 text-muted-foreground">{formatDateTime(d.created_at)}</td>
                      <td className="p-4 text-muted-foreground">{formatDuration(d.started_at, d.completed_at)}</td>
                      <td className="p-4">
                        <Link to={`/deploy/${d.id}`} className="text-xs text-primary hover:underline">View</Link>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </CardContent>
        </Card>
      )}

      <Pagination page={page} totalPages={totalPages} onPageChange={setPage} />
    </div>
  );
}

function formatDuration(start: string | null, end: string | null): string {
  if (!start) return "-";
  // 8.11: Don't show a fake "live" duration that never updates
  if (!end) return "running...";
  const s = new Date(start).getTime();
  const e = new Date(end).getTime();
  const seconds = Math.floor((e - s) / 1000);
  if (seconds < 60) return `${seconds}s`;
  const minutes = Math.floor(seconds / 60);
  const remainingSeconds = seconds % 60;
  return `${minutes}m ${remainingSeconds}s`;
}
