import { Target } from "@/types";
import { Badge } from "@/components/ui/badge";
import { BookOpen } from "lucide-react";

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
