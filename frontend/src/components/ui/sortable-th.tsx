import { ChevronUp, ChevronDown, ChevronsUpDown } from "lucide-react";
import { cn } from "@/lib/utils";

interface SortableThProps {
  label: string;
  field: string;
  currentField: string;
  currentDir: "asc" | "desc";
  onSort: (field: string) => void;
  className?: string;
}

export function SortableTh({ label, field, currentField, currentDir, onSort, className }: SortableThProps) {
  const active = currentField === field;
  return (
    <th
      className={cn("text-left px-4 py-2 font-medium cursor-pointer select-none hover:text-foreground transition-colors", className)}
      onClick={() => onSort(field)}
    >
      <span className="inline-flex items-center gap-1">
        {label}
        {active ? (
          currentDir === "asc" ? <ChevronUp className="h-3.5 w-3.5" /> : <ChevronDown className="h-3.5 w-3.5" />
        ) : (
          <ChevronsUpDown className="h-3.5 w-3.5 opacity-30" />
        )}
      </span>
    </th>
  );
}
