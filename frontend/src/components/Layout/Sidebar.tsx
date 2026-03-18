import { NavLink } from "react-router-dom";
import { ShieldCheck, ChevronLeft, ChevronRight } from "lucide-react";
import { cn } from "@/lib/utils";
import { navItems } from "@/config/navigation";
import { useState, useEffect } from "react";
import { Button } from "@/components/ui/button";
import { ForgemillLogo } from "@/components/ForgemillLogo";

export function Sidebar() {
  // Load collapsed state from localStorage
  const [collapsed, setCollapsed] = useState(() => {
    const stored = localStorage.getItem("forgemill_sidebar_collapsed");
    return stored === "true";
  });

  // Persist collapsed state
  useEffect(() => {
    localStorage.setItem("forgemill_sidebar_collapsed", String(collapsed));
  }, [collapsed]);

  return (
    <aside className={cn(
      "hidden md:flex md:flex-col border-r border-border bg-card transition-all duration-200",
      collapsed ? "md:w-16" : "md:w-64"
    )}>
      <div className={cn(
        "flex h-14 items-center border-b border-border",
        collapsed ? "px-3 justify-center" : "px-6"
      )}>
        <ForgemillLogo size={28} className="shrink-0" />
        {!collapsed && <span className="text-lg font-bold ml-2">Forgemill</span>}
      </div>
      <nav className={cn("flex-1 space-y-1", collapsed ? "p-2" : "p-4")}>
        {navItems.map((item) => (
          <NavLink
            key={item.to}
            to={item.to}
            end={item.to === "/"}
            title={collapsed ? item.label : undefined}
            className={({ isActive }) =>
              cn(
                "flex items-center rounded-md text-sm font-medium transition-colors",
                collapsed ? "justify-center p-2" : "gap-3 px-3 py-2",
                isActive
                  ? "bg-primary/10 text-primary"
                  : "text-muted-foreground hover:bg-accent hover:text-accent-foreground"
              )
            }
          >
            <item.icon className="h-4 w-4 shrink-0" />
            {!collapsed && item.label}
          </NavLink>
        ))}
      </nav>
      <div className={cn("border-t border-border", collapsed ? "p-2" : "p-4")}>
        {!collapsed && (
          <div className="flex items-center gap-2 text-xs text-muted-foreground mb-2">
            <ShieldCheck className="h-3.5 w-3.5 text-green-500 shrink-0" />
            <span>AES-256 encrypted credentials</span>
          </div>
        )}
        <Button
          variant="ghost"
          size="sm"
          onClick={() => setCollapsed(!collapsed)}
          className={cn("w-full", collapsed ? "justify-center p-2" : "")}
          title={collapsed ? "Expand sidebar" : "Collapse sidebar"}
        >
          {collapsed ? <ChevronRight className="h-4 w-4" /> : <ChevronLeft className="h-4 w-4" />}
          {!collapsed && <span className="ml-2 text-xs">Collapse</span>}
        </Button>
      </div>
    </aside>
  );
}
