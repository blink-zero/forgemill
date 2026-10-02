import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";
import path from "path";

export default defineConfig({
  plugins: [react(), tailwindcss()],
  build: {
    // Never inline assets as data: URIs — the production CSP is
    // default-src 'self' with no data: allowance, so an inlined small font
    // or image would be blocked. Emit everything as files instead.
    assetsInlineLimit: 0,
  },
  resolve: {
    alias: {
      "@": path.resolve(import.meta.dirname, "./src"),
    },
  },
  server: {
    port: 3000,
    proxy: {
      "/api": {
        target: "http://localhost:8080",
        changeOrigin: true,
        ws: true,
      },
    },
  },
});
