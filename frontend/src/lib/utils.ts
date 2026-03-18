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
