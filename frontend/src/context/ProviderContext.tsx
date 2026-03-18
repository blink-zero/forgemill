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
    targetApi.getTypes()
      .then((res) => {
        setProviders(res.data.types || []);
        setLoading(false);
      })
      .catch((err) => {
        console.error("[Forgemill] Failed to load provider metadata:", err);
        setError("Failed to load provider configuration");
        setLoading(false);
      });
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
