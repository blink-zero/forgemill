import { ReactNode } from "react";
import { cn } from "@/lib/utils";
import type { LucideIcon } from "lucide-react";

interface EmptyStateProps {
  icon?: LucideIcon;
  title: ReactNode;
  description?: ReactNode;
  action?: ReactNode;
  className?: string;
}

export function EmptyState({ icon: Icon, title, description, action, className }: EmptyStateProps) {
  return (
    <div className={cn("flex flex-col items-center justify-center text-center py-14 px-4 rounded-lg border border-dashed border-border bg-card/40", className)}>
      {Icon && (
        <div className="mb-3 flex h-10 w-10 items-center justify-center rounded-md border border-border bg-card text-muted-foreground shadow-xs">
          <Icon className="h-5 w-5" aria-hidden="true" />
        </div>
      )}
      <h3 className="text-sm font-semibold text-foreground">{title}</h3>
      {description && (
        <p className="mt-1 max-w-sm text-13 text-muted-foreground">{description}</p>
      )}
      {action && <div className="mt-4 flex items-center gap-2">{action}</div>}
    </div>
  );
}
