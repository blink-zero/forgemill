import { useState, useRef, useEffect } from "react";
import { createPortal } from "react-dom";
import { HelpCircle, Copy, Check, ExternalLink, X } from "lucide-react";
import { useToast } from "@/components/ui/toast";
import { cn } from "@/lib/utils";
import { permissionsForType, flattenPrivileges } from "@/config/permissions";

interface PermissionsHelpProps {
  providerType: string;
  /** Optional override label for the trigger's aria-label (e.g. "Permissions help") */
  ariaLabel?: string;
  className?: string;
}

/**
 * Small help-icon trigger that opens a popover listing the hypervisor
 * privileges required for the currently-selected target type. Designed to
 * sit inline next to a form field label without cluttering the form.
 */
export function PermissionsHelp({ providerType, ariaLabel, className }: PermissionsHelpProps) {
  const { toast } = useToast();
  const [open, setOpen] = useState(false);
  const [copied, setCopied] = useState(false);
  const [anchor, setAnchor] = useState<{ top: number; left: number } | null>(null);
  const triggerRef = useRef<HTMLButtonElement>(null);
  const panelRef = useRef<HTMLDivElement>(null);

  const perms = permissionsForType(providerType);

  // Position the portal'd panel relative to the trigger button each time it
  // opens and while the viewport scrolls / resizes. Using a portal ensures
  // the panel escapes any parent stacking context (sidebar, overflow-auto
  // main, backdrop-blur header) and renders above everything on screen.
  useEffect(() => {
    if (!open) return;
    const reposition = () => {
      const el = triggerRef.current;
      if (!el) return;
      const rect = el.getBoundingClientRect();
      const panelWidth = 420;
      const margin = 8;
      // Prefer opening right-ward from the trigger's left edge, but clamp
      // so the panel never leaves the viewport on either side.
      const maxLeft = window.innerWidth - panelWidth - margin;
      const clampedLeft = Math.max(margin, Math.min(rect.left, maxLeft));
      setAnchor({
        top: rect.bottom + 8,
        left: clampedLeft,
      });
    };
    reposition();
    window.addEventListener("scroll", reposition, true);
    window.addEventListener("resize", reposition);

    const onEsc = (e: KeyboardEvent) => { if (e.key === "Escape") setOpen(false); };
    const onClick = (e: MouseEvent) => {
      const t = e.target as Node;
      if (
        panelRef.current && !panelRef.current.contains(t) &&
        (!triggerRef.current || !triggerRef.current.contains(t))
      ) {
        setOpen(false);
      }
    };
    document.addEventListener("keydown", onEsc);
    document.addEventListener("mousedown", onClick);
    return () => {
      window.removeEventListener("scroll", reposition, true);
      window.removeEventListener("resize", reposition);
      document.removeEventListener("keydown", onEsc);
      document.removeEventListener("mousedown", onClick);
    };
  }, [open]);

  if (!perms) return null;

  const copyAll = async () => {
    const text = flattenPrivileges(perms);
    try {
      if (navigator.clipboard && window.isSecureContext) {
        await navigator.clipboard.writeText(text);
      } else {
        const ta = document.createElement("textarea");
        ta.value = text;
        ta.style.position = "fixed";
        ta.style.opacity = "0";
        document.body.appendChild(ta);
        ta.select();
        document.execCommand("copy");
        document.body.removeChild(ta);
      }
      setCopied(true);
      toast("Privileges copied to clipboard");
      setTimeout(() => setCopied(false), 2000);
    } catch {
      toast("Could not copy to clipboard", "error");
    }
  };

  const panel = open && anchor ? (
    <div
      ref={panelRef}
      role="dialog"
      aria-label="Required permissions"
      style={{ position: "fixed", top: anchor.top, left: anchor.left, zIndex: 1000 }}
      className="w-[420px] max-w-[calc(100vw-1rem)] rounded-md border border-border bg-popover text-popover-foreground shadow-lg"
    >
          {/* Header */}
          <div className="flex items-start justify-between gap-2 border-b border-border px-4 py-3">
            <div className="min-w-0">
              <h3 className="text-sm font-semibold">Required permissions</h3>
              <p className="text-xs text-muted-foreground mt-0.5">
                Privileges this account needs on the hypervisor.
              </p>
            </div>
            <button
              onClick={() => setOpen(false)}
              className="text-muted-foreground hover:text-foreground"
              aria-label="Close"
            >
              <X className="h-4 w-4" />
            </button>
          </div>

          {/* Content */}
          <div className="max-h-[60vh] overflow-y-auto px-4 py-3 space-y-3">
            <p className="text-xs text-muted-foreground">{perms.summary}</p>

            {perms.bundledRole && (
              <div className="rounded-md border border-border bg-muted/30 px-3 py-2">
                <p className="text-[11px] font-medium text-muted-foreground uppercase tracking-wider mb-1">Shortcut</p>
                <p className="text-xs">{perms.bundledRole}</p>
              </div>
            )}

            <div className="space-y-3">
              {perms.groups.map((g) => (
                <div key={g.category}>
                  <div className="flex items-center gap-1.5 mb-1">
                    <span className="text-xs font-semibold">{g.category}</span>
                    {g.optional && (
                      <span className="text-[10px] px-1.5 py-0.5 rounded bg-muted text-muted-foreground">Optional</span>
                    )}
                  </div>
                  <p className="text-[11px] text-muted-foreground mb-1.5">{g.description}</p>
                  <ul className="space-y-0.5">
                    {g.privileges.map((p) => (
                      <li key={p} className="font-mono text-[11px] text-foreground bg-muted/40 rounded px-2 py-0.5">
                        {p}
                      </li>
                    ))}
                  </ul>
                </div>
              ))}
            </div>
          </div>

          {/* Footer */}
          <div className="flex items-center justify-between gap-2 border-t border-border px-4 py-2.5">
            {perms.docsUrl && (
              <a
                href={perms.docsUrl}
                target="_blank"
                rel="noopener noreferrer"
                className="inline-flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground"
              >
                <ExternalLink className="h-3 w-3" />
                Docs
              </a>
            )}
            <button
              onClick={copyAll}
              className="inline-flex items-center gap-1.5 rounded-md border border-border px-2 py-1 text-xs hover:bg-accent hover:text-accent-foreground transition-colors"
              title={perms.copyAllLabel}
            >
              {copied ? <Check className="h-3 w-3" /> : <Copy className="h-3 w-3" />}
              {copied ? "Copied" : perms.copyAllLabel}
            </button>
          </div>
    </div>
  ) : null;

  return (
    <span className={cn("inline-flex", className)}>
      <button
        ref={triggerRef}
        type="button"
        onClick={() => setOpen((v) => !v)}
        className="inline-flex h-4 w-4 items-center justify-center text-muted-foreground hover:text-foreground transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring rounded"
        aria-label={ariaLabel ?? "Show required permissions"}
        aria-expanded={open}
        title="Required permissions"
      >
        <HelpCircle className="h-4 w-4" />
      </button>
      {panel && createPortal(panel, document.body)}
    </span>
  );
}
