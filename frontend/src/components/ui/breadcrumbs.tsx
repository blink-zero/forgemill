import { Link, useLocation } from "react-router-dom";
import { ChevronRight, Home } from "lucide-react";
import { navItems } from "@/config/navigation";
import { cn } from "@/lib/utils";

interface Crumb {
  label: string;
  to?: string;
}

const staticLabels: Record<string, string> = {
  vms: "VMs",
  targets: "Targets",
  templates: "Templates",
  factory: "Template Factory",
  deploy: "Deploy",
  actions: "Actions",
  history: "History",
  settings: "Settings",
  build: "Build",
};

function buildCrumbs(pathname: string): Crumb[] {
  if (pathname === "/" || pathname === "") return [];

  const segments = pathname.split("/").filter(Boolean);
  const crumbs: Crumb[] = [];
  let acc = "";

  segments.forEach((seg, i) => {
    acc += `/${seg}`;
    const isLast = i === segments.length - 1;
    // Try to match top-level nav items for labels
    const navMatch = navItems.find((n) => n.to === acc);
    let label = navMatch?.label ?? staticLabels[seg];
    if (!label) {
      // Numeric IDs → shorten; other dynamic bits → keep capitalized
      if (/^\d+$/.test(seg)) {
        label = `#${seg}`;
      } else {
        label = seg.charAt(0).toUpperCase() + seg.slice(1);
      }
    }
    crumbs.push({ label, to: isLast ? undefined : acc });
  });

  return crumbs;
}

export function Breadcrumbs({ className }: { className?: string }) {
  const location = useLocation();
  const crumbs = buildCrumbs(location.pathname);

  if (crumbs.length === 0) return null;

  return (
    <nav aria-label="Breadcrumb" className={cn("flex items-center text-sm text-muted-foreground min-w-0", className)}>
      <Link
        to="/"
        className="flex items-center hover:text-foreground transition-colors shrink-0"
        aria-label="Home"
      >
        <Home className="h-3.5 w-3.5" />
      </Link>
      {crumbs.map((c, i) => (
        <div key={i} className="flex items-center min-w-0">
          <ChevronRight className="mx-1.5 h-3.5 w-3.5 text-muted-foreground/50 shrink-0" />
          {c.to ? (
            <Link to={c.to} className="hover:text-foreground transition-colors truncate">
              {c.label}
            </Link>
          ) : (
            <span className="text-foreground font-medium truncate">{c.label}</span>
          )}
        </div>
      ))}
    </nav>
  );
}
