import { useAuth } from "@/hooks/useAuth";
import { Button } from "@/components/ui/button";
import { LogOut, Moon, Sun, Menu, Search } from "lucide-react";
import { useState, useEffect, useRef, useCallback } from "react";
import { NavLink } from "react-router-dom";
import { cn } from "@/lib/utils";
import { navItems } from "@/config/navigation";

export function Header() {
  const { user, logout } = useAuth();
  const [dark, setDark] = useState(() => {
    const stored = localStorage.getItem("forgemill_theme");
    if (stored) return stored === "dark";
    return document.documentElement.classList.contains("dark");
  });
  const [mobileOpen, setMobileOpen] = useState(false);
  const menuRef = useRef<HTMLDivElement>(null);
  const toggleRef = useRef<HTMLButtonElement>(null);

  // 7.11: Close mobile menu on Escape key or outside click
  useEffect(() => {
    if (!mobileOpen) return;
    const handleEscape = (e: KeyboardEvent) => {
      if (e.key === "Escape") setMobileOpen(false);
    };
    const handleClickOutside = (e: MouseEvent) => {
      const target = e.target as Node;
      // F-153: Exclude clicks on the hamburger toggle button to prevent
      // outside-click closing the menu while the button's onClick reopens it
      if (
        menuRef.current && !menuRef.current.contains(target) &&
        (!toggleRef.current || !toggleRef.current.contains(target))
      ) {
        setMobileOpen(false);
      }
    };
    document.addEventListener("keydown", handleEscape);
    document.addEventListener("mousedown", handleClickOutside);
    return () => {
      document.removeEventListener("keydown", handleEscape);
      document.removeEventListener("mousedown", handleClickOutside);
    };
  }, [mobileOpen]);

  const toggleTheme = () => {
    const next = !dark;
    document.documentElement.classList.toggle("dark", next);
    localStorage.setItem("forgemill_theme", next ? "dark" : "light");
    setDark(next);
  };

  return (
    <>
      <header className="flex h-14 items-center justify-between border-b border-border bg-card px-6">
        <div className="flex items-center gap-4">
          <Button
            variant="ghost"
            size="icon"
            className="md:hidden"
            onClick={() => setMobileOpen(!mobileOpen)}
            aria-label="Toggle navigation menu"
          >
            <Menu className="h-5 w-5" />
          </Button>
          <span className="text-sm text-muted-foreground md:hidden font-bold">Forgemill</span>
        </div>
        <div className="flex items-center gap-2">
          {/* Search pill - opens command palette */}
          <button
            onClick={() => document.dispatchEvent(new CustomEvent("openCommandPalette"))}
            className="hidden sm:flex items-center gap-2 px-3 py-1.5 text-sm text-muted-foreground bg-muted/50 hover:bg-muted rounded-lg border border-border transition-colors"
            aria-label="Open search"
          >
            <Search className="h-4 w-4" />
            <span>Search...</span>
            <kbd className="ml-2 pointer-events-none inline-flex h-5 select-none items-center gap-1 rounded border border-border bg-muted px-1.5 font-mono text-[10px] font-medium text-muted-foreground">
              {navigator.platform.includes("Mac") ? "⌘" : "Ctrl"}K
            </kbd>
          </button>
          <Button variant="ghost" size="icon" onClick={toggleTheme} aria-label={dark ? "Switch to light mode" : "Switch to dark mode"}>
            {dark ? <Sun className="h-4 w-4" /> : <Moon className="h-4 w-4" />}
          </Button>
          {user && (
            <div className="flex items-center gap-2">
              <span className="text-sm text-muted-foreground hidden sm:inline">
                {user.display_name || user.username}
              </span>
              <Button variant="ghost" size="icon" onClick={logout} aria-label="Log out">
                <LogOut className="h-4 w-4" />
              </Button>
            </div>
          )}
        </div>
      </header>

      {mobileOpen && (
        <div ref={menuRef} className="md:hidden border-b border-border bg-card p-4 space-y-1">
          {navItems.map((item) => (
            <NavLink
              key={item.to}
              to={item.to}
              end={item.to === "/"}
              onClick={() => setMobileOpen(false)}
              className={({ isActive }) =>
                cn(
                  "flex items-center gap-3 rounded-md px-3 py-2 text-sm font-medium transition-colors",
                  isActive
                    ? "bg-primary/10 text-primary"
                    : "text-muted-foreground hover:bg-accent hover:text-accent-foreground"
                )
              }
            >
              <item.icon className="h-4 w-4" />
              {item.label}
            </NavLink>
          ))}
        </div>
      )}
    </>
  );
}
