import { Button } from "@/components/ui/button";
import { Moon, Sun, Menu, Search } from "lucide-react";
import { useState, useEffect, useRef } from "react";
import { NavLink } from "react-router-dom";
import { cn } from "@/lib/utils";
import { navItems } from "@/config/navigation";
import { Breadcrumbs } from "@/components/ui/breadcrumbs";
import { NotificationBell } from "./NotificationBell";

export function Header() {
  const [dark, setDark] = useState(() => {
    const stored = localStorage.getItem("forgemill_theme");
    if (stored) return stored === "dark";
    return document.documentElement.classList.contains("dark");
  });
  const [mobileOpen, setMobileOpen] = useState(false);
  const menuRef = useRef<HTMLDivElement>(null);
  const toggleRef = useRef<HTMLButtonElement>(null);

  useEffect(() => {
    if (!mobileOpen) return;
    const handleEscape = (e: KeyboardEvent) => {
      if (e.key === "Escape") setMobileOpen(false);
    };
    const handleClickOutside = (e: MouseEvent) => {
      const target = e.target as Node;
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
      <header className="grid h-14 shrink-0 grid-cols-[1fr_minmax(0,28rem)_1fr] items-center gap-4 bg-background/80 backdrop-blur supports-[backdrop-filter]:bg-background/60 px-6">
        {/* Left: hamburger (mobile) + breadcrumbs (desktop) */}
        <div className="flex items-center gap-3 min-w-0">
          <Button
            ref={toggleRef}
            variant="ghost"
            size="icon"
            className="md:hidden"
            onClick={() => setMobileOpen(!mobileOpen)}
            aria-label="Toggle navigation menu"
          >
            <Menu className="h-5 w-5" />
          </Button>
          <span className="text-sm text-muted-foreground md:hidden font-semibold">Forgemill</span>
          <div className="hidden md:block min-w-0">
            <Breadcrumbs />
          </div>
        </div>

        {/* Center: search pill */}
        <div className="flex justify-center">
          <button
            onClick={() => document.dispatchEvent(new CustomEvent("openCommandPalette"))}
            className="hidden sm:flex items-center gap-2 w-full max-w-md px-3.5 py-2 text-sm text-muted-foreground bg-muted/60 hover:bg-muted rounded-md border border-border/60 hover:border-border transition-colors"
            aria-label="Open search"
          >
            <Search className="h-4 w-4 shrink-0" />
            <span className="flex-1 text-left">Search...</span>
            <kbd className="pointer-events-none inline-flex h-5 select-none items-center gap-1 rounded border border-border bg-background/80 px-1.5 font-mono text-[10px] font-medium text-muted-foreground">
              {navigator.platform.includes("Mac") ? "⌘" : "Ctrl"}K
            </kbd>
          </button>
        </div>

        {/* Right: notifications + theme toggle */}
        <div className="flex items-center gap-1 justify-end">
          <NotificationBell />
          <Button
            variant="ghost"
            size="icon"
            onClick={toggleTheme}
            aria-label={dark ? "Switch to light mode" : "Switch to dark mode"}
          >
            {dark ? <Sun className="h-4 w-4" /> : <Moon className="h-4 w-4" />}
          </Button>
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
