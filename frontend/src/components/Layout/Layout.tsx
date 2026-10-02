import { Outlet, Navigate } from "react-router-dom";
import { Sidebar } from "./Sidebar";
import { Header } from "./Header";
import { CommandPalette } from "@/components/ui/command-palette";
import { useAuth } from "@/hooks/useAuth";
import { Loader2 } from "lucide-react";

export function Layout() {
  const { user, loading } = useAuth();

  if (loading) {
    return (
      <div className="flex h-screen items-center justify-center" role="status" aria-live="polite">
        <Loader2 className="h-8 w-8 animate-spin text-primary" aria-label="Loading" />
        <span className="sr-only">Loading...</span>
      </div>
    );
  }

  if (!user) {
    return <Navigate to="/login" replace />;
  }

  return (
    <div className="flex h-screen overflow-hidden bg-background">
      {/* 7.16: Skip-to-content link for keyboard/screen reader users */}
      <a href="#main-content" className="sr-only focus:not-sr-only focus:absolute focus:z-50 focus:bg-background focus:px-4 focus:py-2 focus:text-primary">
        Skip to content
      </a>
      <Sidebar />
      <div className="flex flex-1 flex-col overflow-hidden min-w-0">
        <Header />
        {/* Content column: comfortable gutters, capped width so wide monitors
            don't stretch tables and forms into unreadable lines. */}
        <main id="main-content" className="flex-1 overflow-auto">
          <div className="mx-auto w-full max-w-[1600px] px-5 py-5 sm:px-6 lg:px-8">
            <Outlet />
          </div>
        </main>
      </div>
      <CommandPalette />
    </div>
  );
}
