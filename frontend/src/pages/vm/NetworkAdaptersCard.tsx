import type { VMNIC } from "@/types";
import { Card, CardHeader, CardTitle, CardContent } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { InfoTip } from "@/components/ui/tooltip";
import { RefreshCw, Copy, Loader2, AlertTriangle } from "lucide-react";

interface NetworkAdaptersCardProps {
  /** null = not loaded yet; [] = loaded, none. */
  nics: VMNIC[] | null;
  loading: boolean;
  error: string | null;
  onRefresh: () => void;
  /** Copies and reports via toast — owned by the page so every copy on it reads the same. */
  onCopy: (text: string) => void;
}

/**
 * Live adapter inventory for one VM (GET /vms/:id/nics), as the hypervisor
 * reports it right now. Purely presentational: the page owns the data so the
 * "Add Network Adapter" flow in the Operations card can refresh it after an
 * attach.
 */
export function NetworkAdaptersCard({ nics, loading, error, onRefresh, onCopy }: NetworkAdaptersCardProps) {
  return (
    <Card>
      <CardHeader className="flex flex-row items-center justify-between space-y-0">
        <div className="flex items-center gap-1.5">
          <CardTitle>Network Adapters</CardTitle>
          <InfoTip text="Every virtual NIC on this VM as the hypervisor reports it right now. Addresses come from VMware Tools / the QEMU guest agent inside the guest — if those aren't running, the adapter still shows with its network and MAC but no addresses." />
        </div>
        <Button size="sm" variant="ghost" className="h-7 gap-1.5 text-xs" onClick={onRefresh} disabled={loading}>
          <RefreshCw className={`h-3 w-3 ${loading ? "animate-spin" : ""}`} /> Refresh
        </Button>
      </CardHeader>
      <CardContent>
        {nics === null || (loading && nics.length === 0) ? (
          <p className="text-sm text-muted-foreground flex items-center gap-1.5"><Loader2 className="h-3.5 w-3.5 animate-spin" /> Reading adapters…</p>
        ) : error ? (
          <div className="rounded-md border border-warning/30 bg-warning/5 px-3 py-2 flex items-start gap-2">
            <AlertTriangle className="h-4 w-4 text-warning shrink-0 mt-0.5" />
            <p className="text-xs text-warning">{error}</p>
          </div>
        ) : nics.length === 0 ? (
          <p className="text-sm text-muted-foreground">No network adapters on this VM.</p>
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead>
                <tr className="text-left text-xs text-muted-foreground border-b">
                  <th className="py-2 pr-3 font-medium">Adapter</th>
                  <th className="py-2 pr-3 font-medium">Network</th>
                  <th className="py-2 pr-3 font-medium">Type</th>
                  <th className="py-2 pr-3 font-medium">MAC</th>
                  <th className="py-2 pr-3 font-medium">Addresses</th>
                  <th className="py-2 font-medium">State</th>
                </tr>
              </thead>
              <tbody>
                {nics.map((n) => (
                  <tr key={n.key} className="border-b last:border-0">
                    <td className="py-2 pr-3 font-medium whitespace-nowrap">{n.label || `Adapter ${n.key}`}</td>
                    <td className="py-2 pr-3 whitespace-nowrap">
                      {n.network || <span className="text-muted-foreground">—</span>}
                      {n.vlan_tag ? <span className="ml-1.5 text-xs text-muted-foreground">VLAN {n.vlan_tag}</span> : null}
                    </td>
                    <td className="py-2 pr-3 text-muted-foreground whitespace-nowrap">{n.adapter_type || "—"}</td>
                    <td className="py-2 pr-3 whitespace-nowrap">
                      {n.mac_address ? (
                        <span className="inline-flex items-center gap-1">
                          <span className="font-mono text-xs">{n.mac_address}</span>
                          <button onClick={() => onCopy(n.mac_address)} className="text-muted-foreground hover:text-foreground" aria-label={`Copy MAC ${n.mac_address}`}>
                            <Copy className="h-3 w-3" />
                          </button>
                        </span>
                      ) : <span className="text-muted-foreground">—</span>}
                    </td>
                    <td className="py-2 pr-3">
                      {n.addresses && n.addresses.length > 0 ? (
                        <div className="flex flex-wrap gap-1">
                          {n.addresses.map((a) => (
                            <button key={a} onClick={() => onCopy(a)} className="font-mono text-xs rounded border px-1.5 py-0.5 bg-muted/40 hover:bg-muted" title="Copy">{a}</button>
                          ))}
                        </div>
                      ) : (
                        <span className="text-xs text-muted-foreground">{n.connected ? "not reported by guest" : "—"}</span>
                      )}
                    </td>
                    <td className="py-2 whitespace-nowrap">
                      {n.pending ? (
                        <Badge variant="warning" title="Saved; attaches at the next power cycle">Pending</Badge>
                      ) : n.connected ? (
                        <Badge variant="success">Connected</Badge>
                      ) : n.start_connected ? (
                        <Badge variant="info" title="Configured to connect when the VM powers on">Connects at power-on</Badge>
                      ) : (
                        <Badge variant="secondary" title="Link down — the adapter is attached but not connected">Disconnected</Badge>
                      )}
                    </td>
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
