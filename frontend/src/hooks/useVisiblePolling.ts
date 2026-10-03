import { useEffect } from "react";

/**
 * Run `fn` every `intervalMs` while the document is visible.
 *
 * Same cadence as a bare setInterval while the tab is in the foreground;
 * nothing at all while it is hidden (so idle tabs stop spending the per-IP
 * rate budget), and one immediate call when it becomes visible again so the
 * page is current the moment the user looks at it.
 *
 * `fn` is not called on mount — callers keep their own initial load, exactly
 * as they did with setInterval.
 */
export function useVisiblePolling(fn: () => void, intervalMs: number) {
  useEffect(() => {
    let timer: ReturnType<typeof setInterval> | null = null;
    const start = () => { if (!timer) timer = setInterval(fn, intervalMs); };
    const stop = () => { if (timer) { clearInterval(timer); timer = null; } };
    const onVisibility = () => {
      if (document.hidden) stop();
      else { fn(); start(); }
    };
    if (!document.hidden) start();
    document.addEventListener("visibilitychange", onVisibility);
    return () => { stop(); document.removeEventListener("visibilitychange", onVisibility); };
  }, [fn, intervalMs]);
}
