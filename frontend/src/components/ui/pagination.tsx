import { Button } from "@/components/ui/button";
import { Select } from "@/components/ui/select";
import { ChevronLeft, ChevronRight, ChevronsLeft, ChevronsRight } from "lucide-react";

export const DEFAULT_PAGE_SIZE_OPTIONS = [10, 25, 50, 100];

interface PaginationProps {
  /** 1-indexed current page */
  page: number;
  /** Total items across all pages (preferred — enables the "X–Y of Z" readout) */
  totalItems?: number;
  /** Current page size. Required if onPageSizeChange is provided. */
  pageSize?: number;
  /** Total pages. Optional: if totalItems + pageSize are given, this is derived. */
  totalPages?: number;
  onPageChange: (page: number) => void;
  /** If provided, shows the per-page Select. */
  onPageSizeChange?: (size: number) => void;
  /** Page size options shown in the Select. Defaults to [10, 25, 50, 100]. */
  pageSizeOptions?: number[];
  /** Hide the page-size selector even if onPageSizeChange is provided. */
  hidePageSize?: boolean;
  /** Label for the items (e.g. "VMs", "actions"). Used in the "X–Y of Z X" readout. */
  itemLabel?: string;
  className?: string;
}

export function Pagination({
  page,
  totalItems,
  pageSize,
  totalPages,
  onPageChange,
  onPageSizeChange,
  pageSizeOptions = DEFAULT_PAGE_SIZE_OPTIONS,
  hidePageSize = false,
  itemLabel,
  className,
}: PaginationProps) {
  // Derive totalPages if we have the raw pieces.
  const derivedTotalPages = (() => {
    if (typeof totalPages === "number") return Math.max(1, totalPages);
    if (typeof totalItems === "number" && typeof pageSize === "number" && pageSize > 0) {
      return Math.max(1, Math.ceil(totalItems / pageSize));
    }
    return 1;
  })();

  const currentPage = Math.min(Math.max(1, page), derivedTotalPages);
  const showPageSizeSelector = !hidePageSize && typeof onPageSizeChange === "function" && typeof pageSize === "number";

  // Compute "X–Y of Z" range when we have the data.
  let rangeText = "";
  if (typeof totalItems === "number" && typeof pageSize === "number" && pageSize > 0) {
    if (totalItems === 0) {
      rangeText = `0 ${itemLabel ?? "items"}`;
    } else {
      const from = (currentPage - 1) * pageSize + 1;
      const to = Math.min(currentPage * pageSize, totalItems);
      rangeText = itemLabel
        ? `${from}–${to} of ${totalItems} ${itemLabel}`
        : `${from}–${to} of ${totalItems}`;
    }
  }

  // Nothing to show at all — no size selector, single page, no readout. Hide.
  const hasNav = derivedTotalPages > 1;
  if (!hasNav && !showPageSizeSelector && !rangeText) return null;

  return (
    <div
      className={
        "flex flex-col gap-2 pt-3 sm:flex-row sm:items-center sm:justify-between" +
        (className ? " " + className : "")
      }
    >
      {/* Left: item count + per-page selector */}
      <div className="flex items-center gap-3 text-sm text-muted-foreground">
        {rangeText && <span>{rangeText}</span>}
        {showPageSizeSelector && (
          <div className="flex items-center gap-2">
            <label className="text-xs">Show</label>
            <Select
              className="h-8 w-auto text-xs"
              value={pageSize}
              onChange={(e) => onPageSizeChange!(Number(e.target.value))}
              aria-label="Items per page"
            >
              {pageSizeOptions.map((n) => (
                <option key={n} value={n}>{n}</option>
              ))}
            </Select>
          </div>
        )}
      </div>

      {/* Right: prev / next + page info */}
      {hasNav && (
        <div className="flex items-center gap-1">
          <Button
            variant="outline"
            size="sm"
            className="h-8 w-8 p-0"
            onClick={() => onPageChange(1)}
            disabled={currentPage === 1}
            aria-label="First page"
            title="First page"
          >
            <ChevronsLeft className="h-4 w-4" />
          </Button>
          <Button
            variant="outline"
            size="sm"
            className="h-8 w-8 p-0"
            onClick={() => onPageChange(Math.max(1, currentPage - 1))}
            disabled={currentPage === 1}
            aria-label="Previous page"
            title="Previous page"
          >
            <ChevronLeft className="h-4 w-4" />
          </Button>
          <span className="text-sm text-muted-foreground px-2 min-w-[7ch] text-center">
            {currentPage} / {derivedTotalPages}
          </span>
          <Button
            variant="outline"
            size="sm"
            className="h-8 w-8 p-0"
            onClick={() => onPageChange(Math.min(derivedTotalPages, currentPage + 1))}
            disabled={currentPage === derivedTotalPages}
            aria-label="Next page"
            title="Next page"
          >
            <ChevronRight className="h-4 w-4" />
          </Button>
          <Button
            variant="outline"
            size="sm"
            className="h-8 w-8 p-0"
            onClick={() => onPageChange(derivedTotalPages)}
            disabled={currentPage === derivedTotalPages}
            aria-label="Last page"
            title="Last page"
          >
            <ChevronsRight className="h-4 w-4" />
          </Button>
        </div>
      )}
    </div>
  );
}
