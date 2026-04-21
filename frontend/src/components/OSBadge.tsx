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
 * "windows9_64Guest", etc.). Returns a clean short label and a colour class.
 */
function classify(osType: string, platform: string): { label: string; color: string } {
  const t = (osType || "").toLowerCase();

  // Windows first since some fields put "windows" in os_type
  if (t.includes("windows") || platform === "windows") {
    return { label: "Windows", color: "bg-blue-500/15 text-blue-400" };
  }
  if (t.includes("ubuntu")) {
    return { label: "Ubuntu", color: "bg-orange-500/15 text-orange-400" };
  }
  if (t.includes("debian")) {
    return { label: "Debian", color: "bg-red-500/15 text-red-400" };
  }
  if (t.includes("rocky")) {
    return { label: "Rocky", color: "bg-emerald-500/15 text-emerald-400" };
  }
  if (t.includes("rhel") || t.includes("redhat") || t.includes("red hat")) {
    return { label: "RHEL", color: "bg-red-600/15 text-red-500" };
  }
  if (t.includes("centos")) {
    return { label: "CentOS", color: "bg-purple-500/15 text-purple-400" };
  }
  if (t.includes("fedora")) {
    return { label: "Fedora", color: "bg-blue-600/15 text-blue-500" };
  }
  if (t.includes("suse") || t.includes("sles") || t.includes("opensuse")) {
    return { label: "SUSE", color: "bg-green-500/15 text-green-400" };
  }
  if (t.includes("alpine")) {
    return { label: "Alpine", color: "bg-sky-500/15 text-sky-400" };
  }
  if (t.includes("linux")) {
    return { label: "Linux", color: "bg-emerald-500/15 text-emerald-400" };
  }
  if (t.includes("bsd")) {
    return { label: "BSD", color: "bg-pink-500/15 text-pink-400" };
  }
  if (t) {
    // Unknown but present — show a short form of the os_type
    const short = t.replace(/\d+.*guest$/, "").replace(/_.*$/, "").slice(0, 12);
    return { label: short || "—", color: "bg-muted text-muted-foreground" };
  }
  return { label: "Unknown", color: "bg-muted text-muted-foreground" };
}

export function OSBadge({ osType, platform, className, size = "sm" }: OSBadgeProps) {
  const { label, color } = classify(osType || "", platform || "");
  const sizing = size === "xs"
    ? "text-[10px] px-1.5 py-0"
    : "text-[11px] px-2 py-0.5";
  return (
    <span
      title={osType || label}
      className={cn(
        "inline-flex items-center rounded font-medium shrink-0",
        sizing,
        color,
        className
      )}
    >
      {label}
    </span>
  );
}
