import type { VMDisk } from "@/types";
import { Card, CardHeader, CardTitle, CardContent } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { InfoTip } from "@/components/ui/tooltip";
import { RefreshCw, Loader2, AlertTriangle } from "lucide-react";

interface DisksCardProps {
  /** null = not loaded yet; [] = loaded, none. */
  disks: VMDisk[] | null;
  loading: boolean;
  error: string | null;
  onRefresh: () => void;
}

/**
 * Live disk inventory for one VM (GET /vms/:id/disks), as the hypervisor
 * reports it right now. Presentational, like NetworkAdaptersCard: the page
 * owns the data so the Expand Disk and Add Disk flows can refresh it.
 */
export function DisksCard({ disks, loading, error, onRefresh }: DisksCardProps) {
  const total = (disks ?? []).reduce((sum, d) => sum + (d.size_gb || 0), 0);
  return (
    <Card>
      <CardHeader className="flex flex-row items-center justify-between space-y-0">
        <div className="flex items-center gap-1.5">
          <CardTitle>Disks</CardTitle>
          <InfoTip text="Every virtual disk on this VM as the hypervisor reports it right now, with the datastore (vSphere) or storage (Proxmox) it lives on. Grow one with Expand Disk, or attach another with Add Disk, in Operations." />
        </div>
        <div className="flex items-center gap-2">
          {disks && disks.length > 1 && total > 0 && (
            <span className="text-2xs text-muted-foreground tabular-nums">{total} GB total</span>
          )}
          <Button size="sm" variant="ghost" className="h-7 gap-1.5 text-xs" onClick={onRefresh} disabled={loading}>
            <RefreshCw className={`h-3 w-3 ${loading ? "animate-spin" : ""}`} /> Refresh
          </Button>
        </div>
      </CardHeader>
      <CardContent>
        {disks === null || (loading && disks.length === 0) ? (
          <p className="text-sm text-muted-foreground flex items-center gap-1.5"><Loader2 className="h-3.5 w-3.5 animate-spin" /> Reading disks…</p>
        ) : error ? (
          <div className="rounded-md border border-warning/30 bg-warning/5 px-3 py-2 flex items-start gap-2">
            <AlertTriangle className="h-4 w-4 text-warning shrink-0 mt-0.5" />
            <p className="text-xs text-warning">{error}</p>
          </div>
        ) : disks.length === 0 ? (
          <p className="text-sm text-muted-foreground">No disks on this VM.</p>
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead>
                <tr className="text-left text-xs text-muted-foreground border-b">
                  <th className="py-2 pr-3 font-medium">Disk</th>
                  <th className="py-2 pr-3 font-medium">Size</th>
                  <th className="py-2 pr-3 font-medium">Datastore</th>
                  <th className="py-2 pr-3 font-medium">Provisioning</th>
                  <th className="py-2 font-medium">Backing</th>
                </tr>
              </thead>
              <tbody>
                {disks.map((d) => (
                  <tr key={d.key} className="border-b last:border-0">
                    <td className="py-2 pr-3 font-medium whitespace-nowrap">
                      {d.label || `Disk ${d.key}`}
                      {d.pending && <Badge variant="warning" className="ml-1.5" title="Saved; attaches at the next power cycle">Pending</Badge>}
                    </td>
                    <td className="py-2 pr-3 whitespace-nowrap tabular-nums">{d.size_gb ? `${d.size_gb} GB` : <span className="text-muted-foreground">—</span>}</td>
                    <td className="py-2 pr-3 whitespace-nowrap">{d.datastore || <span className="text-muted-foreground">—</span>}</td>
                    <td className="py-2 pr-3 text-muted-foreground whitespace-nowrap">{d.provisioning || "—"}</td>
                    <td className="py-2 font-mono text-xs text-muted-foreground truncate max-w-[18rem]" title={d.backing}>{d.backing || "—"}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </CardContent>
    </Card>
  );
}
