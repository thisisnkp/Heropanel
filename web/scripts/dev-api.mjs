// Starts npd for `vite dev`.
//
// The dev server serves the SPA on :5173 and proxies /api, /healthz and /readyz
// to 127.0.0.1:8443 (vite.config.ts). Nothing starts npd on that port, so the
// proxy's ECONNREFUSED is not a bug — it is the backend not running. This is the
// one command that starts it with settings that work on a workstation.
//
// Two of those settings differ from production on purpose:
//
//   sqlite — a developer machine has no MariaDB, and the default driver is
//            mariadb with an empty DSN, which boots npd with no datastore at
//            all. The panel then reports `configured: false` and refuses every
//            login, which looks like a broken build rather than a missing DSN.
//   127.0.0.1 — production binds 0.0.0.0 behind a firewall. A dev box usually
//            has neither, and this panel bootstraps its first administrator
//            over plain HTTP with no session.
//
// No dependency, and no `concurrently`: run this in one terminal and
// `npm run dev` in another. A supervisor that owns both would hide whichever of
// the two failed, and the failure worth seeing here is almost always npd's.

import { spawn } from "node:child_process";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const repo = resolve(dirname(fileURLToPath(import.meta.url)), "..", "..");

// Defaults, each overridable: NP_DATABASE_DSN so a throwaway datastore can be
// pointed at without touching the checked-out one, the rest for the odd machine
// where 8443 is taken.
const env = {
  ...process.env,
  NP_SERVER_HOST: process.env.NP_SERVER_HOST ?? "127.0.0.1",
  NP_SERVER_PORT: process.env.NP_SERVER_PORT ?? "8443",
  NP_DATABASE_DRIVER: process.env.NP_DATABASE_DRIVER ?? "sqlite",
  NP_DATABASE_DSN: process.env.NP_DATABASE_DSN ?? join(repo, "np.db"),
};

console.log(
  `dev-api: npd on http://${env.NP_SERVER_HOST}:${env.NP_SERVER_PORT} ` +
    `(${env.NP_DATABASE_DRIVER} ${env.NP_DATABASE_DSN})`,
);
console.log("dev-api: the panel UI is `npm run dev` in another terminal, on :5173.\n");

// `go run` rather than bin/npd: in dev the binary's embedded bundle is not what
// is being served — Vite is — so a stale bin/npd would serve a stale *API*
// against fresh frontend code, which is the harder mismatch to notice.
const child = spawn("go", ["run", "./cmd/npd"], {
  cwd: repo,
  env,
  stdio: "inherit",
  shell: process.platform === "win32", // `go` is go.exe here; Windows needs the shell to resolve it
});

// Hand Ctrl-C to npd rather than orphaning it: a surviving npd keeps :8443 and
// the next run fails to bind, at which point the proxy error looks identical to
// having started nothing.
for (const sig of ["SIGINT", "SIGTERM"]) {
  process.on(sig, () => child.kill(sig));
}

child.on("error", (err) => {
  console.error(`dev-api: could not start go — ${err.message}`);
  console.error("dev-api: Go is required to run npd from source. https://go.dev/dl/");
  process.exit(1);
});
child.on("exit", (code, signal) => process.exit(signal ? 1 : (code ?? 0)));
