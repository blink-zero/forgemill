import { createContext, useContext, useState, useEffect, ReactNode } from "react";

const STORAGE_KEY = "forgemill-timezone";

function getDefaultTimezone(): string {
  return localStorage.getItem(STORAGE_KEY) || Intl.DateTimeFormat().resolvedOptions().timeZone;
}

interface TimezoneContextValue {
  timezone: string;
  setTimezone: (tz: string) => void;
  formatDate: (date: string | Date | null | undefined) => string;
  formatTime: (date: string | Date | null | undefined) => string;
  formatDateTime: (date: string | Date | null | undefined) => string;
}

const TimezoneContext = createContext<TimezoneContextValue | null>(null);

export function TimezoneProvider({ children }: { children: ReactNode }) {
  const [timezone, setTimezoneState] = useState(getDefaultTimezone);

  const setTimezone = (tz: string) => {
    localStorage.setItem(STORAGE_KEY, tz);
    setTimezoneState(tz);
  };

  // Sync if another tab changes it
  useEffect(() => {
    const handler = (e: StorageEvent) => {
      if (e.key === STORAGE_KEY && e.newValue) setTimezoneState(e.newValue);
    };
    window.addEventListener("storage", handler);
    return () => window.removeEventListener("storage", handler);
  }, []);

  const fmt = (date: string | Date | null | undefined, options: Intl.DateTimeFormatOptions): string => {
    if (!date) return "—";
    try {
      const d = typeof date === "string" ? new Date(date) : date;
      if (isNaN(d.getTime())) return "—";
      return d.toLocaleString(undefined, { timeZone: timezone, ...options });
    } catch {
      return String(date);
    }
  };

  const formatDate = (date: string | Date | null | undefined) =>
    fmt(date, { year: "numeric", month: "short", day: "numeric" });

  const formatTime = (date: string | Date | null | undefined) =>
    fmt(date, { hour: "2-digit", minute: "2-digit", second: "2-digit" });

  const formatDateTime = (date: string | Date | null | undefined) =>
    fmt(date, { year: "numeric", month: "short", day: "numeric", hour: "2-digit", minute: "2-digit", second: "2-digit" });

  return (
    <TimezoneContext.Provider value={{ timezone, setTimezone, formatDate, formatTime, formatDateTime }}>
      {children}
    </TimezoneContext.Provider>
  );
}

export function useTimezone() {
  const ctx = useContext(TimezoneContext);
  if (!ctx) throw new Error("useTimezone must be used within TimezoneProvider");
  return ctx;
}

/** Common timezones for the picker */
export const COMMON_TIMEZONES = [
  "UTC",
  "Pacific/Auckland",
  "Australia/Sydney",
  "Australia/Adelaide",
  "Australia/Perth",
  "Asia/Tokyo",
  "Asia/Shanghai",
  "Asia/Kolkata",
  "Asia/Dubai",
  "Europe/Moscow",
  "Europe/Istanbul",
  "Europe/Berlin",
  "Europe/Paris",
  "Europe/London",
  "America/Sao_Paulo",
  "America/New_York",
  "America/Chicago",
  "America/Denver",
  "America/Los_Angeles",
  "America/Anchorage",
  "Pacific/Honolulu",
];
