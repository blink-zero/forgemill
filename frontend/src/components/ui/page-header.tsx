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

/*
  One header per page: title left, actions right, hairline below. Title is
  20px semibold — large enough to anchor the page, small enough that the
  content starts within the first 100px of the viewport.
*/
export function PageHeader({ title, description, icon, actions, className, children }: PageHeaderProps) {
  return (
    <div className={cn("flex flex-col gap-3 pb-4 mb-5 border-b border-border sm:flex-row sm:items-end sm:justify-between", className)}>
      <div className="flex items-start gap-3 min-w-0">
        {icon && <div className="shrink-0 mt-1 text-muted-foreground [&>svg]:h-5 [&>svg]:w-5">{icon}</div>}
        <div className="min-w-0">
          <h1 className="text-xl font-semibold tracking-tight text-foreground leading-7">{title}</h1>
          {description && (
            <p className="mt-0.5 text-13 text-muted-foreground max-w-prose">{description}</p>
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
