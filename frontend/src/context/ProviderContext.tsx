import { createContext, useContext, useEffect, useState, ReactNode } from "react";
import { targets as targetApi } from "@/api/client";

// Provider metadata from backend
export interface ProviderDefaults {
  port: number;
  username: string;
  name_placeholder: string;
  hostname_placeholder: string;
}

export interface ProviderFeatures {
  folders: boolean;
  clusters: boolean;
  disk_provisioning: boolean;
  linked_clones: boolean;
}

export interface DeployField {
  key: string;
  label: string;
  resource: string;
  placeholder?: string;
}

export interface ProviderMetadata {
  id: string;
  name: string;
  description: string;
  icon: string;
  defaults: ProviderDefaults;
  hints: Record<string, string>;
  features: ProviderFeatures;
  deploy_fields: DeployField[];
}

interface ProviderContextType {
  providers: ProviderMetadata[];
  loading: boolean;
  error: string | null;
  getProvider: (id: string) => ProviderMetadata | undefined;
}

const ProviderContext = createContext<ProviderContextType | undefined>(undefined);

export function ProviderProvider({ children }: { children: ReactNode }) {
  const [providers, setProviders] = useState<ProviderMetadata[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    // Only fetch when authenticated — the token must exist in localStorage.
    // Listen for storage events so we refetch after login (including cross-tab).
    const fetchTypes = () => {
      const token = localStorage.getItem("forgemill_token");
      if (!token) {
        setLoading(false);
        return;
      }
      targetApi.getTypes()
        .then((res) => {
          setProviders(res.data.types || []);
          setError(null);
        })
        .catch(() => {
          // Token may be expired — don't set error, will retry on next storage event
        })
        .finally(() => setLoading(false));
    };

    fetchTypes();

    // Re-fetch when token changes (login/logout from any tab)
    const onStorage = (e: StorageEvent) => {
      if (e.key === "forgemill_token") {
        setLoading(true);
        fetchTypes();
      }
    };
    window.addEventListener("storage", onStorage);

    // Also listen for same-tab token changes via a custom event
    const onTokenChange = () => {
      setLoading(true);
      fetchTypes();
    };
    window.addEventListener("forgemill:auth", onTokenChange);

    return () => {
      window.removeEventListener("storage", onStorage);
      window.removeEventListener("forgemill:auth", onTokenChange);
    };
  }, []);

  const getProvider = (id: string): ProviderMetadata | undefined => {
    return providers.find((p) => p.id === id);
  };

  return (
    <ProviderContext.Provider value={{ providers, loading, error, getProvider }}>
      {children}
    </ProviderContext.Provider>
  );
}

export function useProviders() {
  const context = useContext(ProviderContext);
  if (!context) {
    throw new Error("useProviders must be used within a ProviderProvider");
  }
  return context;
}

export function useProvider(id: string): ProviderMetadata | undefined {
  const { getProvider } = useProviders();
  return getProvider(id);
}
