import { useEffect, useId, useRef, useState, type ReactNode, type ReactElement } from "react";
import { createPortal } from "react-dom";
import { HelpCircle } from "lucide-react";
import { cn } from "@/lib/utils";

const SHOW_DELAY_MS = 300;

interface TooltipProps {
  /** The help text (or richer content) shown on hover/focus. */
  content: ReactNode;
  /** The element the tooltip is attached to — must accept a ref-able wrapper. */
  children: ReactElement;
  className?: string;
}

/**
 * A small, themed hover/focus tooltip. Renders via a portal so it always
 * sits above the page regardless of the trigger's stacking context (sidebar,
 * overflow-auto panels, sticky headers), same approach as PermissionsHelp.
 *
 * Hover-only — dismisses on mouseleave/blur, no click-outside handling
 * needed. Flips below the trigger when there isn't enough room above.
 */
export function Tooltip({ content, children, className }: TooltipProps) {
  const [open, setOpen] = useState(false);
  const [pos, setPos] = useState<{ top: number; left: number; placement: "top" | "bottom" } | null>(null);
  const triggerRef = useRef<HTMLSpanElement>(null);
  const showTimer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const id = useId();

  const show = () => {
    if (showTimer.current) clearTimeout(showTimer.current);
    showTimer.current = setTimeout(() => {
      const el = triggerRef.current;
      if (!el) return;
      const rect = el.getBoundingClientRect();
      const tooltipWidth = 260;
      const margin = 8;
      const placement: "top" | "bottom" = rect.top > 60 ? "top" : "bottom";
      const left = Math.max(margin, Math.min(rect.left + rect.width / 2 - tooltipWidth / 2, window.innerWidth - tooltipWidth - margin));
      const top = placement === "top" ? rect.top - 8 : rect.bottom + 8;
      setPos({ top, left, placement });
      setOpen(true);
    }, SHOW_DELAY_MS);
  };

  const hide = () => {
    if (showTimer.current) {
      clearTimeout(showTimer.current);
      showTimer.current = null;
    }
    setOpen(false);
  };

  useEffect(() => {
    return () => {
      if (showTimer.current) clearTimeout(showTimer.current);
    };
  }, []);

  return (
    <span
      ref={triggerRef}
      className="inline-flex"
      onMouseEnter={show}
      onMouseLeave={hide}
      onFocus={show}
      onBlur={hide}
      aria-describedby={open ? id : undefined}
    >
      {children}
      {open && pos &&
        createPortal(
          <div
            role="tooltip"
            id={id}
            style={{
              position: "fixed",
              top: pos.top,
              left: pos.left,
              transform: pos.placement === "top" ? "translateY(-100%)" : undefined,
            }}
            className={cn(
              "z-[1000] max-w-[260px] rounded-md border border-border bg-popover text-popover-foreground text-xs leading-relaxed px-2.5 py-1.5 shadow-lg pointer-events-none",
              className
            )}
          >
            {content}
          </div>,
          document.body
        )}
    </span>
  );
}

interface InfoTipProps {
  /** The help text shown on hover/focus. */
  text: ReactNode;
  className?: string;
}

/**
 * Convenience wrapper: a small (?) icon whose sole purpose is to carry a
 * Tooltip. Drop it next to a Label when the field itself needs no visual
 * change but the concept behind it does — e.g. `<Label>Datastore</Label>
 * <InfoTip text="..." />`.
 */
export function InfoTip({ text, className }: InfoTipProps) {
  return (
    <Tooltip content={text}>
      <button
        type="button"
        // Not a real button — purely a hover/focus target, so it shouldn't
        // participate in form submission or steal Enter-key behavior.
        tabIndex={0}
        className={cn(
          "inline-flex h-3.5 w-3.5 items-center justify-center text-muted-foreground hover:text-foreground transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring rounded-full",
          className
        )}
        aria-label="More information"
        onClick={(e) => e.preventDefault()}
      >
        <HelpCircle className="h-3.5 w-3.5" />
      </button>
    </Tooltip>
  );
}
