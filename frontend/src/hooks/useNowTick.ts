import { useEffect, useState } from "react";

/**
 * Returns the current time (ms since epoch), re-rendering every
 * intervalMs. Meant to be called once per page and threaded down as a
 * prop/value, not once per row — a page with 100 VM rows needs exactly one
 * of these, not 100 independent setIntervals.
 */
export function useNowTick(intervalMs = 60_000): number {
  const [now, setNow] = useState(() => Date.now());

  useEffect(() => {
    const id = setInterval(() => setNow(Date.now()), intervalMs);
    return () => clearInterval(id);
  }, [intervalMs]);

  return now;
}
