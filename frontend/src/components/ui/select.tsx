import { cn } from "@/lib/utils";
import { forwardRef } from "react";
import { ChevronDown } from "lucide-react";

export interface SelectProps extends React.SelectHTMLAttributes<HTMLSelectElement> {}

/*
  Native <select> with the UA arrow hidden and a lucide chevron drawn as an
  element. The chevron is an element rather than a background-image because
  the production CSP (default-src 'self') blocks data: URIs. The wrapper
  carries no width of its own, so `w-full` / `w-auto` on the select behave
  as they did on the bare element.
*/
const Select = forwardRef<HTMLSelectElement, SelectProps>(({ className, children, ...props }, ref) => {
  return (
    <div className="relative">
      <select
        ref={ref}
        className={cn(
          "flex h-9 w-full appearance-none rounded-md border border-input bg-card pl-3 pr-8 py-1 text-13 text-foreground shadow-xs transition-[border-color,box-shadow] hover:border-muted-foreground/40 focus-visible:outline-hidden focus-visible:border-ring focus-visible:ring-2 focus-visible:ring-ring/30 disabled:cursor-not-allowed disabled:opacity-50 disabled:bg-muted [&>option]:bg-popover [&>option]:text-popover-foreground",
          className
        )}
        {...props}
      >
        {children}
      </select>
      <ChevronDown className="pointer-events-none absolute right-2 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" aria-hidden="true" />
    </div>
  );
});
Select.displayName = "Select";

export { Select };
