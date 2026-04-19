import { useState, useEffect } from "react";

const DEFAULT_OPTIONS = [10, 25, 50, 100];
const KEY_PREFIX = "forgemill_pagesize_";

/**
 * usePageSize persists a per-page preference in localStorage keyed by page name,
 * so the user's choice survives reloads. Returns a tuple similar to useState.
 *
 * Use a distinct `key` for each page (e.g. "vms", "templates", "history") so
 * preferences don't collide across lists.
 */
export function usePageSize(
  key: string,
  defaultSize = 25,
  options: number[] = DEFAULT_OPTIONS
): [number, (next: number) => void] {
  const storageKey = KEY_PREFIX + key;

  const [size, setSizeState] = useState<number>(() => {
    try {
      const raw = localStorage.getItem(storageKey);
      if (raw) {
        const n = Number(raw);
        if (Number.isFinite(n) && options.includes(n)) {
          return n;
        }
      }
    } catch {
      // localStorage unavailable — fall through to default
    }
    return defaultSize;
  });

  useEffect(() => {
    try {
      localStorage.setItem(storageKey, String(size));
    } catch {
      // non-critical
    }
  }, [storageKey, size]);

  const setSize = (next: number) => {
    if (!options.includes(next)) return;
    setSizeState(next);
  };

  return [size, setSize];
}
