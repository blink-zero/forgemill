import { ReactNode, useEffect, useRef, useState, cloneElement, isValidElement, Children } from "react";
import { cn } from "@/lib/utils";

interface DropdownMenuProps {
  trigger: ReactNode;
  children: ReactNode;
  align?: "start" | "end";
  className?: string;
}

// Lightweight dropdown menu — opens on trigger click, closes on outside click
// or Escape. Children are cloned with an onSelect handler that closes the
// menu after running the item's own onClick.
export function DropdownMenu({ trigger, children, align = "end", className }: DropdownMenuProps) {
  const [open, setOpen] = useState(false);
  const triggerRef = useRef<HTMLDivElement>(null);
  const panelRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!open) return;
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
      document.removeEventListener("keydown", onEsc);
      document.removeEventListener("mousedown", onClick);
    };
  }, [open]);

  const wrappedChildren = Children.map(children, (child) => {
    if (!isValidElement(child)) return child;
    const el = child as React.ReactElement<{ onClick?: (e: React.MouseEvent) => void }>;
    const existing = el.props.onClick;
    return cloneElement(el, {
      onClick: (e: React.MouseEvent) => {
        existing?.(e);
        setOpen(false);
      },
    });
  });

  return (
    <div className="relative inline-block">
      <div ref={triggerRef} onClick={() => setOpen((v) => !v)}>
        {trigger}
      </div>
      {open && (
        <div
          ref={panelRef}
          role="menu"
          className={cn(
            "absolute top-full mt-1 z-50 min-w-[180px] rounded-md border border-border bg-popover text-popover-foreground shadow-lg py-1",
            align === "end" ? "right-0" : "left-0",
            className
          )}
        >
          {wrappedChildren}
        </div>
      )}
    </div>
  );
}

interface DropdownMenuItemProps {
  children: ReactNode;
  icon?: ReactNode;
  onClick?: (e: React.MouseEvent) => void;
  disabled?: boolean;
  destructive?: boolean;
  className?: string;
}

export function DropdownMenuItem({
  children,
  icon,
  onClick,
  disabled,
  destructive,
  className,
}: DropdownMenuItemProps) {
  return (
    <button
      type="button"
      role="menuitem"
      onClick={onClick}
      disabled={disabled}
      className={cn(
        "flex items-center gap-2 w-full px-3 py-1.5 text-sm text-left transition-colors",
        "hover:bg-accent hover:text-accent-foreground",
        "focus-visible:outline-none focus-visible:bg-accent focus-visible:text-accent-foreground",
        disabled && "opacity-50 cursor-not-allowed hover:bg-transparent",
        destructive && "text-destructive hover:bg-destructive/10 hover:text-destructive",
        className
      )}
    >
      {icon && <span className="shrink-0 text-muted-foreground [&>svg]:h-3.5 [&>svg]:w-3.5">{icon}</span>}
      <span className="flex-1">{children}</span>
    </button>
  );
}

export function DropdownMenuSeparator() {
  return <div role="separator" className="my-1 h-px bg-border" />;
}
