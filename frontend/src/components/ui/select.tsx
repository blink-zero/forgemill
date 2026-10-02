import { cn } from "@/lib/utils";
import { forwardRef } from "react";

export interface SelectProps extends React.SelectHTMLAttributes<HTMLSelectElement> {}

const Select = forwardRef<HTMLSelectElement, SelectProps>(({ className, children, ...props }, ref) => {
  return (
    <select
      ref={ref}
      className={cn(
        "flex h-9 w-full appearance-none rounded-md border border-input bg-card pl-3 pr-8 py-1 text-13 text-foreground shadow-xs transition-[border-color,box-shadow] hover:border-muted-foreground/40 focus-visible:outline-none focus-visible:border-ring focus-visible:ring-2 focus-visible:ring-ring/30 disabled:cursor-not-allowed disabled:opacity-50 disabled:bg-muted [&>option]:bg-popover [&>option]:text-popover-foreground",
        className
      )}
      // Chevron via inline background props rather than arbitrary bg-[…]
      // classes: tailwind-merge can't classify those and would drop bg-card.
      style={{
        backgroundImage:
          "url(\"data:image/svg+xml;utf8,<svg xmlns='http://www.w3.org/2000/svg' width='16' height='16' viewBox='0 0 24 24' fill='none' stroke='%23888' stroke-width='2' stroke-linecap='round' stroke-linejoin='round'><path d='m6 9 6 6 6-6'/></svg>\")",
        backgroundRepeat: "no-repeat",
        backgroundPosition: "right 0.5rem center",
        backgroundSize: "16px 16px",
        ...props.style,
      }}
      {...props}
    >
      {children}
    </select>
  );
});
Select.displayName = "Select";

export { Select };
