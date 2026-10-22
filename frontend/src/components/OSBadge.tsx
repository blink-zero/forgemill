import { cn } from "@/lib/utils";

interface OSBadgeProps {
  osType?: string;
  platform?: "linux" | "windows" | string;
  className?: string;
  size?: "sm" | "xs";
}

/**
 * Guess the distro family from a raw os_type string (which can be anything
 * the hypervisor reports — "ubuntu64Guest", "debian12_64Guest",
 * "windows9_64Guest", etc.). Returns a clean short label
 * and the chip's border/tint/text classes — the same construction as the
 * status Badge, so OS chips sit flush with "adopted", "inventory-only" and
 * the power badge.
 */
function classify(osType: string, platform: string): { label: string; color: string } {
  const t = (osType || "").toLowerCase();

  // Windows first since some fields put "windows" in os_type
  if (t.includes("windows") || platform === "windows") {
    return { label: "Windows", color: "border-blue-500/25 bg-blue-500/10 text-blue-400" };
  }
  if (t.includes("ubuntu")) {
    return { label: "Ubuntu", color: "border-orange-500/25 bg-orange-500/10 text-orange-400" };
  }
  if (t.includes("debian")) {
    return { label: "Debian", color: "border-red-500/25 bg-red-500/10 text-red-400" };
  }
  if (t.includes("rocky")) {
    return { label: "Rocky", color: "border-emerald-500/25 bg-emerald-500/10 text-emerald-400" };
  }
  if (t.includes("rhel") || t.includes("redhat") || t.includes("red hat")) {
    return { label: "RHEL", color: "border-red-600/25 bg-red-600/10 text-red-500" };
  }
  if (t.includes("centos")) {
    return { label: "CentOS", color: "border-purple-500/25 bg-purple-500/10 text-purple-400" };
  }
  if (t.includes("fedora")) {
    return { label: "Fedora", color: "border-blue-600/25 bg-blue-600/10 text-blue-500" };
  }
  if (t.includes("suse") || t.includes("sles") || t.includes("opensuse")) {
    return { label: "SUSE", color: "border-green-500/25 bg-green-500/10 text-green-400" };
  }
  if (t.includes("alpine")) {
    return { label: "Alpine", color: "border-sky-500/25 bg-sky-500/10 text-sky-400" };
  }
  if (t.includes("oracle")) {
    return { label: "Oracle", color: "border-red-500/25 bg-red-500/10 text-red-400" };
  }
  if (t.includes("amazon")) {
    return { label: "Amazon", color: "border-amber-500/25 bg-amber-500/10 text-amber-400" };
  }
  if (t.includes("arch")) {
    return { label: "Arch", color: "border-cyan-500/25 bg-cyan-500/10 text-cyan-400" };
  }
  if (t.includes("bsd")) {
    return { label: "BSD", color: "border-pink-500/25 bg-pink-500/10 text-pink-400" };
  }
  // Family-only values: vSphere "otherLinux64Guest" / "other5xLinux64Guest",
  // Proxmox "l26" / "l24", or a plain "linux".
  if (t.includes("linux") || /^l2[46]$/.test(t)) {
    return { label: "Linux", color: "border-emerald-500/25 bg-emerald-500/10 text-emerald-400" };
  }
  if (t) {
    // Unknown but present — show a short form of the os_type
    const short = t.replace(/\d+.*guest$/, "").replace(/_.*$/, "").slice(0, 12);
    return { label: short || "—", color: "border-border bg-muted text-muted-foreground" };
  }
  return { label: "Unknown", color: "border-border bg-muted text-muted-foreground" };
}

export function OSBadge({ osType, platform, className, size = "sm" }: OSBadgeProps) {
  const { label, color } = classify(osType || "", platform || "");
  // Both sizes share the Badge's metrics (11px text, 16px line, 6px padding);
  // "xs" is kept so existing call sites keep compiling.
  void size;
  return (
    <span
      title={osType || label}
      className={cn(
        "inline-flex items-center gap-1.5 whitespace-nowrap rounded border px-1.5 py-px text-2xs font-medium leading-4 shrink-0",
        color,
        className
      )}
    >
      <span className="h-1.5 w-1.5 rounded-full bg-current opacity-80" aria-hidden="true" />
      {label}
    </span>
  );
}
