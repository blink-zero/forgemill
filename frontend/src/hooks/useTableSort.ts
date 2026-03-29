import { useState, useMemo, useCallback } from "react";

export function useTableSort<T>(items: T[], defaultField: string, defaultDir: "asc" | "desc" = "asc") {
  const [sortField, setSortField] = useState(defaultField);
  const [sortDir, setSortDir] = useState<"asc" | "desc">(defaultDir);

  const toggleSort = useCallback((field: string) => {
    if (sortField === field) {
      setSortDir((d) => (d === "asc" ? "desc" : "asc"));
    } else {
      setSortField(field);
      setSortDir("asc");
    }
  }, [sortField]);

  const sorted = useMemo(() => {
    return [...items].sort((a, b) => {
      const aVal = String((a as Record<string, unknown>)[sortField] ?? "").toLowerCase();
      const bVal = String((b as Record<string, unknown>)[sortField] ?? "").toLowerCase();
      // Try numeric comparison first
      const aNum = Number(aVal);
      const bNum = Number(bVal);
      let cmp: number;
      if (!isNaN(aNum) && !isNaN(bNum)) {
        cmp = aNum - bNum;
      } else {
        cmp = aVal.localeCompare(bVal);
      }
      return sortDir === "asc" ? cmp : -cmp;
    });
  }, [items, sortField, sortDir]);

  return { sorted, sortField, sortDir, toggleSort };
}
