import { useCallback, useEffect, useRef, useState } from "react";
import { ai as aiApi } from "@/api/client";
import type { AIJob } from "@/types";
import { getErrorMessage } from "@/lib/utils";

/*
  Runs a model-backed call as a background job and polls it. Every poll is a
  short request, so nothing between the browser and Forgemill can time it
  out. The job id is remembered in sessionStorage so a page refresh picks
  the job back up instead of losing a minute of model time.
*/
const POLL_MS = 2000;

export function useAIJob(storageKey: string) {
  const [job, setJob] = useState<AIJob | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [elapsed, setElapsed] = useState(0);
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const ticker = useRef<ReturnType<typeof setInterval> | null>(null);
  const running = job?.status === "running";

  const stop = useCallback(() => {
    if (timer.current) clearTimeout(timer.current);
    if (ticker.current) clearInterval(ticker.current);
    timer.current = null;
    ticker.current = null;
  }, []);

  const poll = useCallback((id: string, startedAt: number) => {
    const tick = async () => {
      try {
        const res = await aiApi.getJob(id);
        setJob(res.data);
        if (res.data.status === "running") {
          timer.current = setTimeout(tick, POLL_MS);
        } else {
          stop();
          try { sessionStorage.removeItem(storageKey); } catch { /* ignore */ }
          if (res.data.status === "failed") setError(res.data.error || "The request failed.");
        }
      } catch (e: unknown) {
        stop();
        try { sessionStorage.removeItem(storageKey); } catch { /* ignore */ }
        setJob(null);
        setError(getErrorMessage(e, "Lost track of the job — start it again."));
      }
    };
    ticker.current = setInterval(() => setElapsed(Math.floor((Date.now() - startedAt) / 1000)), 1000);
    setElapsed(Math.floor((Date.now() - startedAt) / 1000));
    tick();
  }, [stop, storageKey]);

  // Resume a job remembered from before a refresh.
  useEffect(() => {
    try {
      const raw = sessionStorage.getItem(storageKey);
      if (raw) {
        const saved = JSON.parse(raw) as { id: string; startedAt: number };
        if (saved?.id) {
          setJob({ id: saved.id, kind: "", status: "running", started_at: new Date(saved.startedAt).toISOString(), elapsed_ms: 0 });
          poll(saved.id, saved.startedAt);
        }
      }
    } catch { /* ignore */ }
    return stop;
  }, [poll, stop, storageKey]);

  /** Start a job; `starter` performs the POST that returns the 202 job. */
  const start = useCallback(async (starter: () => Promise<{ data: AIJob }>) => {
    stop();
    setError(null);
    setJob(null);
    try {
      const res = await starter();
      const startedAt = Date.now();
      try { sessionStorage.setItem(storageKey, JSON.stringify({ id: res.data.id, startedAt })); } catch { /* ignore */ }
      setJob(res.data);
      poll(res.data.id, startedAt);
    } catch (e: unknown) {
      setError(getErrorMessage(e, "Could not start the request."));
    }
  }, [poll, stop, storageKey]);

  const reset = useCallback(() => { stop(); setJob(null); setError(null); setElapsed(0); try { sessionStorage.removeItem(storageKey); } catch { /* ignore */ } }, [stop, storageKey]);

  return { job, running, stage: job?.stage, elapsed, error, start, reset };
}
