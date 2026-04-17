import { NavLink, useNavigate } from "react-router-dom";
import { ShieldCheck, ChevronLeft, ChevronRight, LogOut } from "lucide-react";
import { cn } from "@/lib/utils";
import { navSections } from "@/config/navigation";
import { useState, useEffect } from "react";
import { Button } from "@/components/ui/button";
import { ForgemillLogo } from "@/components/ForgemillLogo";
import { useAuth } from "@/hooks/useAuth";

export function Sidebar() {
  const navigate = useNavigate();
  const { user, logout } = useAuth();
  const [collapsed, setCollapsed] = useState(() => {
    const stored = localStorage.getItem("forgemill_sidebar_collapsed");
    return stored === "true";
  });
  const [appVersion, setAppVersion] = useState("");

  useEffect(() => {
    localStorage.setItem("forgemill_sidebar_collapsed", String(collapsed));
  }, [collapsed]);

  useEffect(() => {
    fetch("/api/version")
      .then((r) => r.json())
      .then((data) => {
        if (data.version && data.version !== "dev") {
          setAppVersion(data.version.startsWith("v") ? data.version : `v${data.version}`);
        } else if (data.version === "dev") {
          setAppVersion("dev");
        }
      })
      .catch(() => { /* version fetch is non-critical */ });
  }, []);

  return (
    <aside
      className={cn(
        "hidden md:flex md:flex-col border-r border-sidebar-border bg-sidebar text-sidebar-foreground transition-[width] duration-200",
        collapsed ? "md:w-16" : "md:w-60"
      )}
    >
      {/* Brand */}
      <div
        className={cn(
          "flex h-14 items-center shrink-0",
          collapsed ? "px-3 justify-center" : "px-5"
        )}
      >
        <ForgemillLogo size={26} className="shrink-0" />
        {!collapsed && (
          <span className="text-[15px] font-semibold ml-2 tracking-tight">Forgemill</span>
        )}
      </div>

      {/* Nav sections */}
      <nav className={cn("flex-1 overflow-y-auto", collapsed ? "px-2 py-2" : "px-3 py-2")}>
        {navSections.map((section, idx) => (
          <div key={idx} className={cn(idx > 0 && (collapsed ? "mt-2 pt-2 border-t border-sidebar-border/60" : "mt-4"))}>
            {!collapsed && section.heading && (
              <div className="px-2 pb-1.5 text-[10px] font-semibold uppercase tracking-wider text-muted-foreground">
                {section.heading}
              </div>
            )}
            <div className="space-y-0.5">
              {section.items.map((item) => (
                <NavLink
                  key={item.to}
                  to={item.to}
                  end={item.to === "/"}
                  title={collapsed ? item.label : undefined}
                  className={({ isActive }) =>
                    cn(
                      "group relative flex items-center rounded-md text-sm font-medium transition-colors",
                      collapsed ? "justify-center p-2" : "gap-2.5 px-2 py-1.5",
                      isActive
                        ? "bg-primary/10 text-foreground"
                        : "text-muted-foreground hover:bg-accent hover:text-foreground"
                    )
                  }
                >
                  {({ isActive }) => (
                    <>
                      {isActive && !collapsed && (
                        <span className="absolute left-0 top-1.5 bottom-1.5 w-0.5 rounded-full bg-primary" aria-hidden="true" />
                      )}
                      <item.icon
                        className={cn(
                          "h-4 w-4 shrink-0",
                          isActive ? "text-primary" : "text-muted-foreground group-hover:text-foreground"
                        )}
                      />
                      {!collapsed && <span className="truncate">{item.label}</span>}
                    </>
                  )}
                </NavLink>
              ))}
            </div>
          </div>
        ))}
      </nav>

      {/* Footer: user + meta + collapse toggle */}
      <div className={cn("shrink-0 border-t border-sidebar-border", collapsed ? "p-2" : "p-3")}>
        {user && !collapsed && (
          <div className="mb-3 flex items-center gap-2 rounded-md border border-sidebar-border bg-card/50 px-2.5 py-2">
            <div className="flex h-7 w-7 shrink-0 items-center justify-center rounded-full bg-primary/15 text-[11px] font-semibold text-primary">
              {(user.display_name || user.username).slice(0, 2).toUpperCase()}
            </div>
            <div className="flex-1 min-w-0">
              <div className="truncate text-xs font-medium">{user.display_name || user.username}</div>
              <div className="truncate text-[10px] text-muted-foreground capitalize">{user.role}</div>
            </div>
            <button
              onClick={logout}
              className="text-muted-foreground hover:text-foreground transition-colors"
              aria-label="Log out"
              title="Log out"
            >
              <LogOut className="h-3.5 w-3.5" />
            </button>
          </div>
        )}

        {user && collapsed && (
          <button
            onClick={logout}
            className="mb-1 w-full flex items-center justify-center rounded-md p-2 text-muted-foreground hover:bg-accent hover:text-foreground transition-colors"
            aria-label="Log out"
            title="Log out"
          >
            <LogOut className="h-4 w-4" />
          </button>
        )}

        {!collapsed && (
          <div className="mb-2 space-y-1.5 px-1">
            <div className="flex items-center gap-2 text-[11px] text-muted-foreground">
              <ShieldCheck className="h-3.5 w-3.5 text-success shrink-0" />
              <span>AES-256 encrypted</span>
            </div>
            {appVersion && (
              <button
                onClick={() => navigate("/settings")}
                className="text-[11px] text-muted-foreground/70 hover:text-muted-foreground transition-colors font-mono ml-5"
                title="View version details in Settings"
              >
                {appVersion}
              </button>
            )}
          </div>
        )}

        <Button
          variant="ghost"
          size="sm"
          onClick={() => setCollapsed(!collapsed)}
          className={cn("w-full text-muted-foreground hover:text-foreground", collapsed ? "justify-center p-2" : "")}
          title={collapsed ? "Expand sidebar" : "Collapse sidebar"}
        >
          {collapsed ? <ChevronRight className="h-4 w-4" /> : <ChevronLeft className="h-4 w-4" />}
          {!collapsed && <span className="ml-2 text-xs">Collapse</span>}
        </Button>
      </div>
    </aside>
  );
}
