import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import { fileURLToPath, URL } from "node:url";

// The panel serves the built SPA embedded in hpd (same origin). During `vite
// dev`, API/health calls are proxied to the local hpd instance so cookies stay
// same-origin.
export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: {
      "@": fileURLToPath(new URL("./src", import.meta.url)),
    },
  },
  server: {
    port: 5173,
    proxy: {
      // ws:true so the realtime WebSocket upgrade (/api/v1/ws) is proxied too.
      "/api": { target: "http://127.0.0.1:8443", changeOrigin: false, ws: true },
      "/healthz": { target: "http://127.0.0.1:8443", changeOrigin: false },
      "/readyz": { target: "http://127.0.0.1:8443", changeOrigin: false },
    },
  },
  build: {
    outDir: "dist",
    emptyOutDir: true,
    sourcemap: false,
  },
});
