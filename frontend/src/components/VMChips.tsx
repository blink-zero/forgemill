import { BookOpen, Import, Layers, Link2 } from "lucide-react";
import { OSBadge } from "@/components/OSBadge";
import { Badge } from "@/components/ui/badge";
import { cn } from "@/lib/utils";
import type { ManagedVM } from "@/types";

/*
  The chip row under a VM's name on the VMs page (cards and table rows): what
  it runs, where it came from, and anything about its host worth knowing.
  Every chip is built the same way as the status Badge — one height, one
  border weight, one tint — so a row of them reads as one line of facts
  rather than a pile of differently styled pills.

  Order is fixed: OS · template · origin · host caveat. A deployed VM on a
  normal host shows just the OS and its template.
*/
interface VMChipsProps {
  vm: Pick<ManagedVM, "os_type" | "platform" | "origin" | "template_name">;
  /** Capability note of the VM's target when that target is inventory-only. */
  inventoryOnlyNote?: string;
  className?: string;
}

export function VMChips({ vm, inventoryOnlyNote, className }: VMChipsProps) {
  return (
    <div className={cn("flex items-center gap-1 flex-wrap", className)}>
      <OSBadge osType={vm.os_type} platform={vm.platform} />
      {vm.template_name && (
        <Badge variant="outline" className="font-mono max-w-[14rem]" title={`Deployed from template ${vm.template_name}`}>
          <Layers className="h-2.5 w-2.5 shrink-0" />
          <span className="truncate">{vm.template_name}</span>
        </Badge>
      )}
      {vm.origin === "adopted" && (
        <Badge variant="outline" title="Discovered on the target and adopted into Forgemill">
          <Import className="h-2.5 w-2.5" />
          adopted
        </Badge>
      )}
      {vm.origin === "registered" && (
        <Badge variant="outline" title="Registered by its hypervisor reference">
          <Link2 className="h-2.5 w-2.5" />
          registered
        </Badge>
      )}
      {inventoryOnlyNote && (
        <Badge variant="warning" title={inventoryOnlyNote}>
          <BookOpen className="h-2.5 w-2.5" />
          inventory-only
        </Badge>
      )}
    </div>
  );
}
