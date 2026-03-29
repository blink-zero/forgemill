import { createContext, useContext, useState, useEffect, useCallback, type ReactNode } from "react";
import { auth } from "@/api/client";
import type { User } from "@/types";

interface AuthContextType {
  user: User | null;
  loading: boolean;
  login: (username: string, password: string) => Promise<void>;
  logout: () => void;
}

const AuthContext = createContext<AuthContextType | null>(null);

export function AuthProvider({ children }: { children: ReactNode }) {
  const [user, setUser] = useState<User | null>(null);
  const [loading, setLoading] = useState(true);

  // 7.10: Abort stale auth check on unmount
  useEffect(() => {
    const controller = new AbortController();
    const token = localStorage.getItem("forgemill_token");
    if (!token) {
      setLoading(false);
      return;
    }
    auth
      .me()
      .then((res) => {
        if (!controller.signal.aborted) setUser(res.data);
      })
      .catch(() => {
        if (!controller.signal.aborted) localStorage.removeItem("forgemill_token");
      })
      .finally(() => {
        if (!controller.signal.aborted) setLoading(false);
      });
    return () => controller.abort();
  }, []);

  // 7.9: Synchronize auth state across browser tabs
  useEffect(() => {
    const handleStorageChange = (e: StorageEvent) => {
      if (e.key === "forgemill_token") {
        if (!e.newValue) {
          setUser(null);
        } else {
          auth.me().then((res) => setUser(res.data)).catch(() => setUser(null));
        }
      }
    };
    window.addEventListener("storage", handleStorageChange);
    return () => window.removeEventListener("storage", handleStorageChange);
  }, []);

  // V5-M6: localStorage token storage is an accepted risk — see comment in client.ts
  const login = useCallback(async (username: string, password: string) => {
    const res = await auth.login(username, password);
    localStorage.setItem("forgemill_token", res.data.token);
    setUser(res.data.user);
    // Notify ProviderContext to fetch metadata now that we're authenticated
    window.dispatchEvent(new Event("forgemill:auth"));
  }, []);

  const logout = useCallback(() => {
    // Server-side logout is best-effort; local state is always cleared
    auth.logout().catch(() => { /* intentionally silent */ });
    localStorage.removeItem("forgemill_token");
    setUser(null);
  }, []);

  return (
    <AuthContext.Provider value={{ user, loading, login, logout }}>
      {children}
    </AuthContext.Provider>
  );
}

export function useAuth() {
  const ctx = useContext(AuthContext);
  if (!ctx) throw new Error("useAuth must be used within AuthProvider");
  return ctx;
}
