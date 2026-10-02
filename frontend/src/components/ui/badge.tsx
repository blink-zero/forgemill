import * as React from "react";
import { cva, type VariantProps } from "class-variance-authority";
import { cn } from "@/lib/utils";

/*
  Status badges: a 10% tint of the semantic colour with a 25% border and the
  colour itself as text. The border is what keeps them legible on both the
  page and card surfaces; the optional dot gives a second, colour-independent
  cue for state (running / stopped / failed) at a glance.
*/
const badgeVariants = cva(
  "inline-flex items-center gap-1.5 whitespace-nowrap rounded border px-1.5 py-px text-2xs font-medium leading-4 transition-colors focus:outline-none focus:ring-2 focus:ring-ring focus:ring-offset-2",
  {
    variants: {
      variant: {
        default: "border-primary/25 bg-primary/10 text-primary",
        secondary: "border-border bg-muted text-muted-foreground",
        destructive: "border-destructive/30 bg-destructive/10 text-destructive",
        outline: "border-border bg-transparent text-muted-foreground",
        success: "border-success/30 bg-success/10 text-success",
        warning: "border-warning/35 bg-warning/10 text-warning",
        info: "border-info/30 bg-info/10 text-info",
      },
    },
    defaultVariants: {
      variant: "default",
    },
  }
);

export interface BadgeProps
  extends React.HTMLAttributes<HTMLDivElement>,
    VariantProps<typeof badgeVariants> {
  /** Prepend a small status dot in the badge's colour. */
  dot?: boolean;
  /** Pulse the dot — for in-progress states (running, building, syncing). */
  pulse?: boolean;
}

function Badge({ className, variant, dot, pulse, children, ...props }: BadgeProps) {
  return (
    <div className={cn(badgeVariants({ variant }), className)} {...props}>
      {dot && <span className={cn("status-dot", pulse && "status-dot-pulse")} aria-hidden="true" />}
      {children}
    </div>
  );
}

export { Badge, badgeVariants };
