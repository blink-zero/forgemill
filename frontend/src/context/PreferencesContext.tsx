import { createContext, useContext, useEffect, useState, useCallback, ReactNode } from "react";
import { preferences as preferencesApi } from "@/api/client";

interface PreferencesContextType {
  prefs: Record<string, string>;
  loading: boolean;
  getPreference: (key: string, defaultValue?: string) => string;
  setPreference: (key: string, value: string) => Promise<void>;
}

const PreferencesContext = createContext<PreferencesContextType | undefined>(undefined);

export function PreferencesProvider({ children }: { children: ReactNode }) {
  const [prefs, setPrefs] = useState<Record<string, string>>({});
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    const fetchPrefs = () => {
      const token = localStorage.getItem("forgemill_token");
      if (!token) {
        setLoading(false);
        return;
      }
      preferencesApi.get()
        .then((res) => setPrefs(res.data || {}))
        .catch(() => { /* non-critical */ })
        .finally(() => setLoading(false));
    };

    fetchPrefs();

    // Reload on auth changes
    const onAuth = () => {
      setLoading(true);
      fetchPrefs();
    };
    window.addEventListener("forgemill:auth", onAuth);
    const onStorage = (e: StorageEvent) => {
      if (e.key === "forgemill_token") onAuth();
    };
    window.addEventListener("storage", onStorage);

    return () => {
      window.removeEventListener("forgemill:auth", onAuth);
      window.removeEventListener("storage", onStorage);
    };
  }, []);

  const getPreference = useCallback(
    (key: string, defaultValue = "") => prefs[key] || defaultValue,
    [prefs]
  );

  const setPreference = useCallback(async (key: string, value: string) => {
    // Optimistic update
    setPrefs((prev) => ({ ...prev, [key]: value }));
    try {
      await preferencesApi.set(key, value);
    } catch {
      // Revert on failure
      setPrefs((prev) => {
        const reverted = { ...prev };
        delete reverted[key];
        return reverted;
      });
    }
  }, []);

  return (
    <PreferencesContext.Provider value={{ prefs, loading, getPreference, setPreference }}>
      {children}
    </PreferencesContext.Provider>
  );
}

export function usePreferences() {
  const context = useContext(PreferencesContext);
  if (!context) {
    throw new Error("usePreferences must be used within a PreferencesProvider");
  }
  return context;
}

export function usePreference(key: string, defaultValue = "") {
  const { getPreference } = usePreferences();
  return getPreference(key, defaultValue);
}
