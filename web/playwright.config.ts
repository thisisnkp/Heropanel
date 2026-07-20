import { defineConfig, devices } from "@playwright/test";

// Browser-level end-to-end tests, driven against a **real hpd** serving the real
// built SPA — not a mock and not the dev server.
//
// Scope, deliberately: these cover what only a browser can prove — routing,
// forms, keyboard handling, error states, and that the bundle hpd embeds
// actually boots. They do *not* cover privileged operations (file ops as the
// site's Linux user, a PTY, session recording), because those need the root
// broker and a real Linux account; `deploy/docker/e2e/*.sh` drives those against
// actual software in a container, which is the right place for them. Splitting
// it this way keeps each suite honest about what it is evidence of.
//
// hpd is started by webServer below with a throwaway SQLite datastore, so a run
// needs no manual setup. It runs a *prebuilt* binary with the bundle embedded in
// it, so `npm run test:e2e` rebuilds both first: running `playwright test`
// against a stale bin/hpd silently tests the previous bundle, which can turn a
// broken change green — the failure mode a browser suite exists to prevent.

const PORT = Number(process.env.HP_E2E_PORT ?? 18780);

export default defineConfig({
  testDir: "./e2e",
  // Each spec bootstraps or logs into the same panel, so they share one
  // datastore and must not race each other for it.
  fullyParallel: false,
  workers: 1,
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 1 : 0,
  reporter: process.env.CI ? [["list"], ["html", { open: "never" }]] : "list",
  timeout: 30_000,
  expect: { timeout: 10_000 },

  use: {
    baseURL: `http://127.0.0.1:${PORT}`,
    trace: "on-first-retry",
    screenshot: "only-on-failure",
  },

  projects: [{ name: "chromium", use: { ...devices["Desktop Chrome"] } }],

  webServer: {
    // A fresh database per run: these tests bootstrap the first admin, which is
    // a one-time act, and a leftover database from a previous run would make the
    // suite pass or fail depending on what ran before it.
    command:
      process.platform === "win32"
        ? `powershell -NoProfile -Command "Remove-Item -Force -ErrorAction SilentlyContinue $env:TEMP\\hp-e2e.db; $env:HP_SERVER_PORT='${PORT}'; $env:HP_SECURITY_RATE_LIMIT_ENABLED='false'; $env:HP_DATABASE_DRIVER='sqlite'; $env:HP_DATABASE_DSN=\\"$env:TEMP\\hp-e2e.db\\"; ../bin/hpd.exe"`
        : `sh -c "rm -f /tmp/hp-e2e.db && HP_SERVER_PORT=${PORT} HP_SECURITY_RATE_LIMIT_ENABLED=false HP_DATABASE_DRIVER=sqlite HP_DATABASE_DSN=/tmp/hp-e2e.db ../bin/hpd"`,
    url: `http://127.0.0.1:${PORT}/healthz`,
    reuseExistingServer: false,
    timeout: 60_000,
    stdout: "pipe",
    stderr: "pipe",
  },
});
