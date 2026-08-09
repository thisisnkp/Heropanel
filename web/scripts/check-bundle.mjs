// Frontend performance budget. The panel is embedded in npd and served on first
// paint, so the eager entry bundle — the JavaScript every visitor downloads
// before anything renders — is the number that governs perceived load. Route
// pages, the code editor, and the terminal are code-split and excluded; they
// arrive only when their screen is opened.
//
// This asserts the gzipped entry chunk stays under budget. It fails the build
// when a change re-couples a heavy page into the entry (an eager import of a
// route, say), which is exactly the regression code-splitting is meant to
// prevent. Raise the budget deliberately, with a commit that says why — never to
// silence a breach.

import { readFileSync } from "node:fs";
import { gzipSync } from "node:zlib";
import { join, dirname, basename } from "node:path";
import { fileURLToPath } from "node:url";

const distDir = join(dirname(fileURLToPath(import.meta.url)), "..", "dist");

// Budget for the gzipped entry chunk. Headroom over today's size (~80 kB) leaves
// room for honest growth while still catching a whole-app re-bundle.
const ENTRY_GZIP_BUDGET = 130 * 1024;

let html;
try {
  html = readFileSync(join(distDir, "index.html"), "utf8");
} catch {
  console.error(`check-bundle: no build output at ${distDir} — run \`npm run build\` first.`);
  process.exit(1);
}

// The entry chunk is whatever index.html loads as its module script — not merely
// "some index-*.js", because Vite now also names the on-demand grammar chunks
// index-<hash>.js. Reading it from the HTML is the only reliable way to measure
// the bundle a first paint actually downloads.
const m = html.match(/<script[^>]*\btype="module"[^>]*\bsrc="([^"]+)"/);
if (!m) {
  console.error("check-bundle: could not find the module entry script in dist/index.html.");
  process.exit(1);
}
const entry = basename(m[1]);
const raw = readFileSync(join(distDir, "assets", entry));
const gzip = gzipSync(raw).length;
const kb = (n) => `${(n / 1024).toFixed(1)} kB`;

console.log(`entry chunk ${entry}: ${kb(raw.length)} raw, ${kb(gzip)} gzip (budget ${kb(ENTRY_GZIP_BUDGET)} gzip)`);

if (gzip > ENTRY_GZIP_BUDGET) {
  console.error(
    `check-bundle: entry chunk is ${kb(gzip)} gzip, over the ${kb(ENTRY_GZIP_BUDGET)} budget.\n` +
      "A heavy module was likely pulled into the eager bundle — code-split it (React.lazy) " +
      "or raise the budget deliberately in scripts/check-bundle.mjs.",
  );
  process.exit(1);
}
console.log("check-bundle: within budget.");
