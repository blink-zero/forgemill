import { classifyOS } from "@/components/OSBadge";
import { cn } from "@/lib/utils";
import type { ManagedVM } from "@/types";

/*
  How a VM introduces itself on the VMs page.

  The OS is the one attribute every VM has and the one you scan for, so it
  becomes the visual anchor: a small tinted tile with the distro's monogram
  (U, D, R, W…) in the distro's colour, sitting left of the name like an
  avatar. Everything else is quiet text in one meta line — OS name, then the
  template it came from — and only the exceptions get a tag: "adopted" /
  "registered" (soft, neutral) and "inventory-only" (soft, amber). No borders,
  no stacked pills; the power badge stays the only boxed element on a card.
*/

const monograms: Record<string, string> = { RHEL: "R", CentOS: "C", Unknown: "?" };

export function OSTile({ osType, platform, size = "md", className }: { osType?: string; platform?: string; size?: "sm" | "md"; className?: string }) {
  const { label, color } = classifyOS(osType || "", platform || "");
  const letter = monograms[label] ?? (label === "—" ? "?" : label.charAt(0).toUpperCase());
  return (
    <span
      title={osType ? `${label} · ${osType}` : label}
      aria-label={label}
      className={cn(
        "inline-flex shrink-0 items-center justify-center rounded-lg font-semibold select-none",
        size === "md" ? "h-9 w-9 text-sm" : "h-7 w-7 text-xs",
        color,
        className,
      )}
    >
      {letter}
    </span>
  );
}

export function SoftTag({ tone = "neutral", title, className, children }: { tone?: "neutral" | "warning"; title?: string; className?: string; children: React.ReactNode }) {
  return (
    <span
      title={title}
      className={cn(
        "inline-flex shrink-0 items-center rounded-full px-1.5 py-px text-[10px] font-medium leading-4 tracking-wide",
        tone === "warning" ? "bg-warning/15 text-warning" : "bg-muted text-muted-foreground",
        className,
      )}
    >
      {children}
    </span>
  );
}

/** "Ubuntu · ubuntu-24.04-cloudinit  adopted  inventory-only" — one quiet line of provenance, exceptions last. */
export function VMMetaLine({ vm, inventoryOnlyNote, className }: { vm: Pick<ManagedVM, "os_type" | "platform" | "origin" | "template_name">; inventoryOnlyNote?: string; className?: string }) {
  const { label } = classifyOS(vm.os_type || "", vm.platform || "");
  return (
    <div className={cn("flex items-center gap-1.5 min-w-0 text-xs text-muted-foreground", className)}>
      <span className="shrink-0" title={vm.os_type || undefined}>{label}</span>
      {vm.template_name && (
        <>
          <span className="opacity-40" aria-hidden="true">·</span>
          <span className="font-mono truncate" title={`Deployed from template ${vm.template_name}`}>{vm.template_name}</span>
        </>
      )}
      {vm.origin === "adopted" && <SoftTag title="Discovered on the target and adopted into Forgemill">adopted</SoftTag>}
      {vm.origin === "registered" && <SoftTag title="Registered by its hypervisor reference">registered</SoftTag>}
      {inventoryOnlyNote && <SoftTag tone="warning" title={inventoryOnlyNote}>inventory-only</SoftTag>}
    </div>
  );
}
