import { lazy, Suspense } from "react";
import { Routes, Route } from "react-router-dom";
import { Layout } from "@/components/Layout/Layout";
import { ToastProvider } from "@/components/ui/toast";
import { ConfirmProvider } from "@/components/ui/confirm-dialog";
import { ProviderProvider } from "@/context/ProviderContext";
import { PreferencesProvider } from "@/context/PreferencesContext";
import { Loader2 } from "lucide-react";
import Login from "@/pages/Login";

// 7.13: Lazy load route components for code splitting
const Dashboard = lazy(() => import("@/pages/Dashboard"));
const Templates = lazy(() => import("@/pages/Templates"));
const Deploy = lazy(() => import("@/pages/Deploy"));
const DeployLive = lazy(() => import("@/pages/DeployLive"));
const Targets = lazy(() => import("@/pages/Targets"));
const HistoryPage = lazy(() => import("@/pages/History"));
const SettingsPage = lazy(() => import("@/pages/Settings"));
const VMs = lazy(() => import("@/pages/VMs"));
const VMDetail = lazy(() => import("@/pages/VMDetail"));
const ActionsPage = lazy(() => import("@/pages/Actions"));
const Factory = lazy(() => import("@/pages/Factory"));
const FactoryBuild = lazy(() => import("@/pages/FactoryBuild"));
const FactoryBuildProgress = lazy(() => import("@/pages/FactoryBuildProgress"));
const NotFound = lazy(() => import("@/pages/NotFound"));

function PageLoader() {
  return (
    <div className="flex items-center justify-center h-64" role="status" aria-live="polite">
      <Loader2 className="h-8 w-8 animate-spin text-primary" />
      <span className="sr-only">Loading...</span>
    </div>
  );
}

export default function App() {
  return (
    <ProviderProvider>
    <PreferencesProvider>
    <ToastProvider>
    <ConfirmProvider>
    <Suspense fallback={<PageLoader />}>
      <Routes>
        <Route path="/login" element={<Login />} />
        <Route element={<Layout />}>
          <Route path="/" element={<Dashboard />} />
          <Route path="/templates" element={<Templates />} />
          <Route path="/deploy" element={<Deploy />} />
          <Route path="/deploy/:id" element={<DeployLive />} />
          <Route path="/targets" element={<Targets />} />
          <Route path="/history" element={<HistoryPage />} />
          <Route path="/settings" element={<SettingsPage />} />
          <Route path="/vms" element={<VMs />} />
          <Route path="/vms/:id" element={<VMDetail />} />
          <Route path="/actions" element={<ActionsPage />} />
          <Route path="/factory" element={<Factory />} />
          <Route path="/factory/build" element={<FactoryBuild />} />
          <Route path="/factory/build/:id" element={<FactoryBuildProgress />} />
          {/* 7.8: Catch-all 404 route */}
          <Route path="*" element={<NotFound />} />
        </Route>
      </Routes>
    </Suspense>
    </ConfirmProvider>
    </ToastProvider>
    </PreferencesProvider>
    </ProviderProvider>
  );
}
