import React from "react";
import ReactDOM from "react-dom/client";
import { BrowserRouter } from "react-router-dom";
import { AuthProvider } from "@/hooks/useAuth";
import { TimezoneProvider } from "@/hooks/useTimezone";
import { ErrorBoundary } from "@/components/ErrorBoundary";
import App from "./App";
// Self-hosted variable fonts: the production CSP (default-src 'self') blocks
// fonts.googleapis.com, so the Google Fonts <link> never loaded in practice.
import "@fontsource-variable/inter";
import "@fontsource-variable/jetbrains-mono";
import "./index.css";

// Apply the persisted theme before first paint. index.html ships with the
// dark class so there's no flash for the default; a stored "light" choice
// previously only took effect after the user toggled again.
try {
  if (localStorage.getItem("forgemill_theme") === "light") {
    document.documentElement.classList.remove("dark");
  }
} catch {
  /* storage unavailable (private mode) — keep the default */
}

ReactDOM.createRoot(document.getElementById("root")!).render(
  <React.StrictMode>
    <ErrorBoundary>
      <BrowserRouter>
        <AuthProvider>
          <TimezoneProvider>
            <App />
          </TimezoneProvider>
        </AuthProvider>
      </BrowserRouter>
    </ErrorBoundary>
  </React.StrictMode>
);
