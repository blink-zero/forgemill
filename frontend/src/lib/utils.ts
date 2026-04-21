import { type ClassValue, clsx } from "clsx";
import { twMerge } from "tailwind-merge";

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs));
}

export function getErrorMessage(error: unknown, fallback: string): string {
  if (error && typeof error === "object") {
    const axiosErr = error as { response?: { data?: { error?: string; message?: string } } };
    const apiMsg = axiosErr?.response?.data?.error || axiosErr?.response?.data?.message;
    if (apiMsg) return apiMsg;
    if (error instanceof Error) return error.message;
  }
  return fallback;
}

/**
 * Format an ISO timestamp as a compact relative time ("2m ago", "3h ago",
 * "5d ago", then a localised date for anything older than 30 days).
 * Returns "never" for null / undefined / unparseable input.
 */
export function timeAgo(iso?: string | null): string {
  if (!iso) return "never";
  const then = new Date(iso).getTime();
  if (Number.isNaN(then)) return "never";
  const diffS = Math.max(0, Math.floor((Date.now() - then) / 1000));
  if (diffS < 45) return "just now";
  if (diffS < 90) return "1m ago";
  const diffM = Math.floor(diffS / 60);
  if (diffM < 60) return `${diffM}m ago`;
  const diffH = Math.floor(diffM / 60);
  if (diffH < 24) return `${diffH}h ago`;
  const diffD = Math.floor(diffH / 24);
  if (diffD < 30) return `${diffD}d ago`;
  return new Date(iso).toLocaleDateString();
}

/**
 * Copy a string to the clipboard, falling back to the old execCommand
 * approach for non-secure-context pages. Returns a promise that resolves
 * once the copy is (best-effort) complete.
 */
export function copyText(text: string): Promise<void> {
  if (navigator.clipboard && window.isSecureContext) return navigator.clipboard.writeText(text);
  const ta = document.createElement("textarea");
  ta.value = text;
  ta.style.position = "fixed";
  ta.style.opacity = "0";
  document.body.appendChild(ta);
  ta.select();
  document.execCommand("copy");
  document.body.removeChild(ta);
  return Promise.resolve();
}
