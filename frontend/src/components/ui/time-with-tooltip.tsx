import type { ReactNode } from "react";
import { Tooltip } from "@/components/ui/tooltip";
import { useTimezone } from "@/hooks/useTimezone";

interface TimeWithTooltipProps {
  /** ISO-8601 timestamp. Renders children plainly (no tooltip) if missing. */
  iso?: string | null;
  children: ReactNode;
  className?: string;
}

/**
 * Wraps any relative-time text (children) with a tooltip showing the exact
 * timestamp — both in the user's configured display timezone and in UTC —
 * so "Up 3d 14h" is always one hover away from being unambiguous.
 */
export function TimeWithTooltip({ iso, children, className }: TimeWithTooltipProps) {
  const { formatDateTime } = useTimezone();
  if (!iso) return <span className={className}>{children}</span>;

  const content = (
    <div className="space-y-0.5 whitespace-nowrap">
      <div><span className="text-muted-foreground">Local:</span> {formatDateTime(iso)}</div>
      <div><span className="text-muted-foreground">UTC:</span> {iso}</div>
    </div>
  );

  return (
    <Tooltip content={content}>
      <span className={className}>{children}</span>
    </Tooltip>
  );
}
