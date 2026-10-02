import { ReactNode } from "react";
import { AlertTriangle } from "lucide-react";
import { cn } from "@/lib/utils";

/*
  DangerZone visually quarantines irreversible actions. Everything inside it
  shares one red-tinted enclosure with an explicit label, sits at the end of
  its parent (never between routine controls), and its buttons use the
  outlined `destructive` variant — the solid `danger` fill is only shown by
  the confirmation step itself.

    <DangerZone description="These remove the VM. Neither can be undone.">
      <DangerZoneItem title="Untrack" description="Forget this VM in Forgemill; leave it on the hypervisor.">
        <Button variant="destructive" size="sm">Untrack</Button>
      </DangerZoneItem>
      <DangerZoneItem title="Destroy" description="Power off and delete from the hypervisor.">
        <Button variant="destructive" size="sm">Destroy…</Button>
      </DangerZoneItem>
    </DangerZone>
*/
interface DangerZoneProps {
  title?: ReactNode;
  description?: ReactNode;
  children: ReactNode;
  className?: string;
}

export function DangerZone({ title = "Danger zone", description, children, className }: DangerZoneProps) {
  return (
    <section
      aria-label={typeof title === "string" ? title : "Danger zone"}
      className={cn("rounded-lg border border-destructive/30 bg-destructive/3 overflow-hidden", className)}
    >
      <header className="flex items-start gap-2 px-3 py-2 border-b border-destructive/20 bg-destructive/5">
        <AlertTriangle className="h-3.5 w-3.5 text-destructive shrink-0 mt-0.5" aria-hidden="true" />
        <div className="min-w-0">
          <div className="text-2xs font-semibold uppercase tracking-[0.08em] text-destructive">{title}</div>
          {description && <p className="text-2xs text-destructive/80 mt-0.5">{description}</p>}
        </div>
      </header>
      <div className="divide-y divide-destructive/15">{children}</div>
    </section>
  );
}

interface DangerZoneItemProps {
  title: ReactNode;
  description?: ReactNode;
  /** The control(s) for this action — typically a `destructive` Button. */
  children: ReactNode;
  /** Expanded confirmation UI rendered beneath the row when the action is armed. */
  expanded?: ReactNode;
  className?: string;
}

export function DangerZoneItem({ title, description, children, expanded, className }: DangerZoneItemProps) {
  return (
    <div className={cn("px-3 py-2.5", className)}>
      <div className="flex items-start justify-between gap-3">
        <div className="min-w-0">
          <div className="text-13 font-medium text-foreground">{title}</div>
          {description && <p className="text-2xs text-muted-foreground mt-0.5">{description}</p>}
        </div>
        <div className="shrink-0 flex items-center gap-2">{children}</div>
      </div>
      {expanded && <div className="mt-2.5">{expanded}</div>}
    </div>
  );
}
