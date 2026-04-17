import { ReactNode } from "react";
import { cn } from "@/lib/utils";

interface PageHeaderProps {
  title: ReactNode;
  description?: ReactNode;
  icon?: ReactNode;
  actions?: ReactNode;
  className?: string;
  children?: ReactNode;
}

export function PageHeader({ title, description, icon, actions, className, children }: PageHeaderProps) {
  return (
    <div className={cn("flex flex-col gap-3 pb-5 mb-6 border-b border-border sm:flex-row sm:items-start sm:justify-between", className)}>
      <div className="flex items-start gap-3 min-w-0">
        {icon && <div className="shrink-0 mt-0.5 text-muted-foreground">{icon}</div>}
        <div className="min-w-0">
          <h1 className="text-xl font-semibold tracking-tight text-foreground sm:text-2xl">{title}</h1>
          {description && (
            <p className="mt-1 text-sm text-muted-foreground">{description}</p>
          )}
          {children}
        </div>
      </div>
      {actions && (
        <div className="flex flex-wrap items-center gap-2 shrink-0">{actions}</div>
      )}
    </div>
  );
}
