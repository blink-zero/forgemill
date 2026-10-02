import { NavLink, useNavigate } from "react-router-dom";
import { ChevronLeft, ChevronRight, LogOut } from "lucide-react";
import { cn } from "@/lib/utils";
import { navSections } from "@/config/navigation";
import { useState, useEffect } from "react";
import { ForgemillLogo } from "@/components/ForgemillLogo";
import { useAuth } from "@/hooks/useAuth";

/*
  Sidebar: a quiet tonal panel one step darker than the page. Nav items are
  32px rows; the active one is a filled pill with a 2px accent bar so it
  reads at a glance even when the sidebar is collapsed to icons.
*/
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
        collapsed ? "md:w-14" : "md:w-56"
      )}
    >
      {/* Brand */}
      <div
        className={cn(
          "flex h-12 items-center shrink-0 border-b border-sidebar-border/70",
          collapsed ? "px-2 justify-center" : "px-4"
        )}
      >
        <ForgemillLogo size={22} className="shrink-0" />
        {!collapsed && (
          <span className="text-sm font-semibold ml-2 tracking-tight">Forgemill</span>
        )}
      </div>

      {/* Nav sections */}
      <nav className={cn("flex-1 overflow-y-auto", collapsed ? "px-2 py-2" : "px-2.5 py-2")} aria-label="Primary">
        {navSections.map((section, idx) => (
          <div key={idx} className={cn(idx > 0 && (collapsed ? "mt-2 pt-2 border-t border-sidebar-border/60" : "mt-3"))}>
            {!collapsed && section.heading && (
              <div className="px-2 pt-1 pb-1 text-[10px] font-semibold uppercase tracking-[0.1em] text-muted-foreground/80">
                {section.heading}
              </div>
            )}
            <div className="space-y-px">
              {section.items.map((item) => (
                <NavLink
                  key={item.to}
                  to={item.to}
                  end={item.to === "/"}
                  title={collapsed ? item.label : undefined}
                  className={({ isActive }) =>
                    cn(
                      "group relative flex items-center rounded-md text-13 font-medium transition-colors h-8",
                      collapsed ? "justify-center w-9 mx-auto" : "gap-2.5 px-2.5",
                      isActive
                        ? "bg-primary/10 text-foreground"
                        : "text-muted-foreground hover:bg-accent/70 hover:text-foreground"
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
                          "h-[15px] w-[15px] shrink-0",
                          isActive ? "text-primary" : "text-muted-foreground/90 group-hover:text-foreground"
                        )}
                        strokeWidth={isActive ? 2.25 : 2}
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
      <div className={cn("shrink-0 border-t border-sidebar-border", collapsed ? "p-2" : "p-2.5")}>
        {user && !collapsed && (
          <div className="mb-2 flex items-center gap-2 rounded-md border border-sidebar-border bg-card/60 px-2 py-1.5">
            <div className="flex h-6 w-6 shrink-0 items-center justify-center rounded bg-primary/15 text-[10px] font-semibold text-primary">
              {(user.display_name || user.username).slice(0, 2).toUpperCase()}
            </div>
            <div className="flex-1 min-w-0 leading-tight">
              <div className="truncate text-2xs font-medium">{user.display_name || user.username}</div>
              <div className="truncate text-[10px] text-muted-foreground capitalize">{user.role}</div>
            </div>
            <button
              onClick={logout}
              className="text-muted-foreground hover:text-foreground transition-colors rounded p-0.5"
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

        <div className={cn("flex items-center", collapsed ? "justify-center" : "justify-between px-1")}>
          {!collapsed && appVersion && (
            <button
              onClick={() => navigate("/settings")}
              className="text-[10px] text-muted-foreground/70 hover:text-muted-foreground transition-colors font-mono"
              title="View version details in Settings"
            >
              {appVersion}
            </button>
          )}
          <button
            onClick={() => setCollapsed(!collapsed)}
            className="flex items-center gap-1 rounded-md px-1.5 py-1 text-[10px] text-muted-foreground hover:bg-accent hover:text-foreground transition-colors"
            title={collapsed ? "Expand sidebar" : "Collapse sidebar"}
            aria-label={collapsed ? "Expand sidebar" : "Collapse sidebar"}
          >
            {collapsed ? <ChevronRight className="h-3.5 w-3.5" /> : <ChevronLeft className="h-3.5 w-3.5" />}
            {!collapsed && <span>Collapse</span>}
          </button>
        </div>
      </div>
    </aside>
  );
}
