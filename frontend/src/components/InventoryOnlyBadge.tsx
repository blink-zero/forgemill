import { Target } from "@/types";
import { Badge } from "@/components/ui/badge";
import { BookOpen, Hourglass } from "lucide-react";

/*
  Shown next to a target whose hypervisor refuses API writes (free-licensed
  standalone ESXi): Forgemill can read, sync, discover and adopt, but not
  deploy, power, reconfigure, snapshot or destroy. The full explanation is in
  the tooltip; the badge itself stays short so rows don't wrap.
*/
export function isInventoryOnly(t: Pick<Target, "deploy_supported"> | null | undefined): boolean {
  return t?.deploy_supported === false;
}

export function InventoryOnlyBadge({ target, className }: { target: Pick<Target, "deploy_supported" | "capability_note" | "license_edition">; className?: string }) {
  if (!isInventoryOnly(target)) return null;
  return (
    <Badge variant="warning" className={className} title={target.capability_note || "This host's license prohibits vSphere API writes; Forgemill can only read its inventory."}>
      <BookOpen className="h-2.5 w-2.5" />
      inventory-only{target.license_edition ? ` · ${target.license_edition}` : ""}
    </Badge>
  );
}

/** Whole days until an ISO instant, rounded up (1 hour left is "1 day"). Null when absent. */
export function evaluationDaysLeft(t: Pick<Target, "evaluation_expires_at"> | null | undefined, now: number = Date.now()): number | null {
  if (!t?.evaluation_expires_at) return null;
  const ms = new Date(t.evaluation_expires_at).getTime() - now;
  if (Number.isNaN(ms)) return null;
  return ms <= 0 ? 0 : Math.ceil(ms / 86_400_000);
}

/*
  Shown while a host runs on an evaluation license: how long until it flips
  to inventory-only. Quiet until two weeks out, then warning, then red.
*/
export function EvaluationBadge({ target, className }: { target: Pick<Target, "evaluation_expires_at" | "license_edition">; className?: string }) {
  const days = evaluationDaysLeft(target);
  if (days === null) return null;
  const when = target.evaluation_expires_at ? new Date(target.evaluation_expires_at).toLocaleDateString(undefined, { day: "numeric", month: "short", year: "numeric" }) : "";
  const variant = days <= 1 ? "destructive" : days <= 14 ? "warning" : "secondary";
  return (
    <Badge variant={variant} className={className} title={`Evaluation license ends ${when}. After that the host is inventory-only until a paid or VMUG key is assigned.`}>
      <Hourglass className="h-2.5 w-2.5" />
      {days === 0 ? "evaluation ends today" : `evaluation · ${days} day${days === 1 ? "" : "s"} left`}
    </Badge>
  );
}
