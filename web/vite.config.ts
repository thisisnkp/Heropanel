import { defineConfig } from "vitest/config";
import vue from "@vitejs/plugin-vue";
import Icons from "unplugin-icons/vite";
import IconsResolver from "unplugin-icons/resolver";
import Components from "unplugin-vue-components/vite";
import { fileURLToPath, URL } from "node:url";

// The panel serves the built SPA embedded in npd (same origin). During `vite
// dev`, API/health calls are proxied to the local npd instance so cookies stay
// same-origin.
export default defineConfig({
  plugins: [
    vue(),

    // Icons are resolved by name at build time and inlined as SVG, so a screen
    // that uses six glyphs ships six paths rather than a ~200KB icon font. The
    // design was drawn with Material Symbols Outlined; `simple-icons` covers the
    // third-party marks (Cloudflare, WordPress, Docker) that have no Material
    // equivalent.
    Components({
      dts: "src/components.d.ts",
      resolvers: [IconsResolver({ prefix: "Icon", enabledCollections: ["material-symbols", "simple-icons"] })],
      // Auto-import our own components too, so a view does not carry ten import
      // lines for the primitives every view uses.
      dirs: ["src/components"],
      extensions: ["vue"],
      deep: true,
    }),
    Icons({ compiler: "vue3", scale: 1, defaultClass: "nx-ico" }),
  ],
  resolve: {
    alias: {
      "@": fileURLToPath(new URL("./src", import.meta.url)),
    },
  },
  server: {
    port: 5173,
    proxy: {
      // Anchored regex, not the bare "/api" prefix: Vite matches a string key
      // as a prefix, which also swallowed UI routes such as /api-tokens and
      // served them from the backend instead of the SPA.
      // ws:true so the realtime WebSocket upgrade (/api/v1/ws) is proxied too.
      "^/api/": { target: "http://127.0.0.1:8443", changeOrigin: false, ws: true },
      "/healthz": { target: "http://127.0.0.1:8443", changeOrigin: false },
      "/readyz": { target: "http://127.0.0.1:8443", changeOrigin: false },
    },
  },
  build: {
    outDir: "dist",
    emptyOutDir: true,
    sourcemap: false,
  },
  test: {
    environment: "node",
    include: ["src/**/*.test.ts"],
  },
});
