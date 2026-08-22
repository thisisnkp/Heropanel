import { defineConfig, devices } from "@playwright/test";
import path from "node:path";
import { fileURLToPath } from "node:url";

// The config is ESM, so there is no __dirname. The storage-state path has to be
// absolute: Playwright resolves it relative to the process cwd, not this file.
const here = path.dirname(fileURLToPath(import.meta.url));

// Browser-level end-to-end tests, driven against a **real npd** serving the real
// built SPA — not a mock and not the dev server.
//
// Scope, deliberately: these cover what only a browser can prove — routing,
// forms, keyboard handling, error states, and that the bundle npd embeds
// actually boots. They do *not* cover privileged operations (file ops as the
// site's Linux user, a PTY, session recording), because those need the root
// broker and a real Linux account; `deploy/docker/e2e/*.sh` drives those against
// actual software in a container, which is the right place for them. Splitting
// it this way keeps each suite honest about what it is evidence of.
//
// npd is started by webServer below with a throwaway SQLite datastore, so a run
// needs no manual setup. It runs a *prebuilt* binary with the bundle embedded in
// it, so `npm run test:e2e` rebuilds both first: running `playwright test`
// against a stale bin/npd silently tests the previous bundle, which can turn a
// broken change green — the failure mode a browser suite exists to prevent.

const PORT = Number(process.env.NP_E2E_PORT ?? 18780);

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

  projects: [
    // Bootstraps the admin and finishes the first-run wizard once, then hands
    // the session to every other spec. Without it each spec would arrive at a
    // blank panel and be redirected to /welcome — and only the first would be
    // able to bootstrap, since creating the first administrator happens once.
    { name: "setup", testMatch: /auth\.setup\.ts/ },
    {
      name: "chromium",
      use: {
        ...devices["Desktop Chrome"],
        storageState: path.join(here, "e2e", ".auth", "state.json"),
      },
      dependencies: ["setup"],
    },
  ],

  webServer: {
    // A fresh database per run, seeded with sites, then npd.
    //
    // Fresh because these tests bootstrap the first admin, which is a one-time
    // act - a leftover database from a previous run would make the suite pass
    // or fail depending on what ran before it.
    //
    // Seeded because npd here has no broker, and creating a site means creating
    // a Linux user, a directory tree, an FPM pool and a vhost through it. npd
    // correctly refuses, so without seeding every screen under /sites/{uid} has
    // nothing to render. tools/e2eseed writes the rows through the real
    // repository; the host-side effects stay the container suite's job.
    command:
      process.platform === "win32"
        ? `powershell -NoProfile -Command "Remove-Item -Force -ErrorAction SilentlyContinue $env:TEMP\\np-e2e.db; go run ../tools/e2eseed/main.go \\"$env:TEMP\\np-e2e.db\\"; $env:NP_SERVER_PORT='${PORT}'; $env:NP_SECURITY_RATE_LIMIT_ENABLED='false'; $env:NP_DATABASE_DRIVER='sqlite'; $env:NP_DATABASE_DSN=\\"$env:TEMP\\np-e2e.db\\"; ../bin/npd.exe"`
        : `sh -c "rm -f /tmp/np-e2e.db && go run ../tools/e2eseed/main.go /tmp/np-e2e.db && NP_SERVER_PORT=${PORT} NP_SECURITY_RATE_LIMIT_ENABLED=false NP_DATABASE_DRIVER=sqlite NP_DATABASE_DSN=/tmp/np-e2e.db ../bin/npd"`,
    url: `http://127.0.0.1:${PORT}/healthz`,
    reuseExistingServer: false,
    timeout: 60_000,
    stdout: "pipe",
    stderr: "pipe",
  },
});
