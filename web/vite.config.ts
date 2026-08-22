import { defineConfig } from "vitest/config";
import vue from "@vitejs/plugin-vue";
import Icons from "unplugin-icons/vite";
import IconsResolver from "unplugin-icons/resolver";
import Components from "unplugin-vue-components/vite";
import { fileURLToPath, URL } from "node:url";

// The panel serves the built SPA embedded in npd (same origin). During `vite
// dev`, API/health calls are proxied to the local npd instance so cookies stay
// same-origin.

const API_TARGET = process.env.NP_DEV_API ?? "http://127.0.0.1:8443";

type ProxyEmitter = {
  on: (ev: string, fn: (err: Error, req: unknown, res: unknown) => void) => void;
  removeAllListeners: (ev: string) => void;
};

/**
 * Answers proxy failures in the shape the panel's API client expects.
 *
 * Without this, npd being down produces a Node stack trace in the dev-server
 * terminal and an HTML error page in the browser — so the client reports "the
 * server returned a non-JSON response", which points at the response body
 * instead of at the one fact that matters: nothing is listening on 8443.
 *
 * The envelope matches docs/04, so the SPA takes its ordinary unreachable path
 * — the same screen a real outage produces, which is the point of matching it.
 */
function apiProxyErrors(proxy: ProxyEmitter) {
  let warned = false;

  const onError = (err: Error, _req: unknown, res: unknown) => {
    const down = (err as NodeJS.ErrnoException).code === "ECONNREFUSED";
    if (down && !warned) {
      // Once per dev-server run, and Vite's own handler is dropped after the
      // first: a reloading SPA retries on every navigation, and forty identical
      // ECONNREFUSED stack traces bury the one line that says what to do about
      // it. Ours is re-registered so later failures are still answered.
      warned = true;
      console.log(
        `\n  \x1b[33m➜\x1b[0m  no npd at ${API_TARGET} — start it with \x1b[1mnpm run dev:api\x1b[0m (see web/scripts/dev-api.mjs)\n`,
      );
      proxy.removeAllListeners("error");
      proxy.on("error", onError);
    }
    // A WebSocket upgrade (/api/v1/ws) fails against a socket, which has no
    // status line to write. Destroying it lets the client's own reconnect run.
    const out = res as { writeHead?: (s: number, h: object) => void; end?: (b?: string) => void; destroy?: () => void };
    if (typeof out?.writeHead !== "function") {
      out?.destroy?.();
      return;
    }
    out.writeHead(503, { "Content-Type": "application/json" });
    out.end?.(
      JSON.stringify({
        error: {
          code: down ? "api_unreachable" : "proxy_error",
          message: down
            ? `The NexPanel API is not running. Start npd with \`npm run dev:api\` — the dev server proxies /api to ${API_TARGET}.`
            : `The dev server could not reach ${API_TARGET}: ${err.message}`,
        },
      }),
    );
  };

  proxy.on("error", onError);
}

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
      "^/api/": { target: API_TARGET, changeOrigin: false, ws: true, configure: apiProxyErrors },
      "/healthz": { target: API_TARGET, changeOrigin: false, configure: apiProxyErrors },
      "/readyz": { target: API_TARGET, changeOrigin: false, configure: apiProxyErrors },
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
