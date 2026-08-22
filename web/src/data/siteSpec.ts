import type { Site } from "@/stores/sites";
import { row, type Spec } from "@/data/spec";
import { dbBase } from "@/data/siteDetail";

function shellUser(domain: string) {
  return "site_" + domain.split(".")[0].replace(/-/g, "");
}

/** The language a stack runs, and the versions installed for it. */
export const LANG_FOR_STACK: Record<string, string> = {
  php: "PHP", wp: "PHP", node: "Node.js", python: "Python",
};

// The same ranges the Apps catalogue offers (web/src/data/apps.ts), newest
// first. Two lists of PHP versions that disagree is a panel telling the operator
// two different things about the same host.
export const LANG_VERSIONS: Record<string, readonly string[]> = {
  PHP: ["8.5", "8.4", "8.3", "8.2", "8.1", "8.0", "7.4"],
  "Node.js": ["24 LTS", "22 LTS", "20", "18", "16", "14"],
  Python: ["3.14", "3.13", "3.12", "3.11", "3.10", "3.9", "3.8"],
};

export type SpecKey =
  | "aitrouble" | "pagespeed" | "cdn" | "analytics" | "malware" | "ssl"
  | "subdomains" | "parked" | "redirects" | "wpinstall" | "wpmigrate" | "wpstaging"
  | "ftp" | "phpmyadmin" | "remotedb" | "lang" | "cron" | "sshsite" | "git"
  | "ipmanage" | "hotlink" | "cachemgr" | "activity";

/**
 * Builds the spec for one screen.
 *
 * Takes the site because most screens quote the domain, the branch or the
 * derived database name back at the reader — a generic screen that says
 * "your site" where the design said "novaretail.in" is a downgrade, not a
 * simplification.
 */
export function buildSiteSpec(key: SpecKey, s: Site): Spec {
  const lang = LANG_FOR_STACK[s.stackKey] ?? "Runtime";
  const versions = LANG_VERSIONS[lang] ?? ["1.0"];
  const db = dbBase(s.domain);

  switch (key) {
    case "lang":
      return {
        kicker: "Advanced",
        title: lang + " version",
        sub: "Switch versions per site. Your files and databases are untouched.",
        stats: [
          { label: "Active", value: lang + " " + versions[0], sub: "restarts in ~2 seconds" },
          { label: "Installed", value: String(versions.length), sub: "switch any time" },
          { label: "Patch level", value: "Current", sub: "no advisories open" },
          { label: "Affected", value: "1 site", sub: s.domain },
        ],
        choiceTitle: "Active version for this site",
        choiceDefault: versions[0],
        choices: versions.map((v, i) => ({ label: v, sub: i === 0 ? "Recommended" : "Compatibility" })),
        toggleTitle: "Version policy",
        toggles: [
          { label: "Auto-install patch releases", sub: "Minor security patches only, never a major jump", flag: "autoPhp" },
          { label: "Warn before a deprecated version", sub: "Email 30 days before end of support", flag: "notifyEmail" },
        ],
        sideTitle: lang + " limits",
        sideNote: "Applies to this site only. Raise limits if long imports time out.",
        fields:
          lang === "PHP"
            ? [{ label: "memory_limit", value: "512M" }, { label: "max_execution_time", value: "120" }, { label: "upload_max_filesize", value: "256M" }]
            : lang === "Node.js"
              ? [{ label: "Start command", value: "npm run start" }, { label: "Instances", value: "2 (cluster)" }, { label: "Max old space", value: "1024 MB" }]
              : [{ label: "Entry point", value: "gunicorn app:app" }, { label: "Workers", value: "3" }, { label: "Timeout", value: "60s" }],
        sideActions: [{ label: "Edit limits", primary: true }, { label: "Restart now" }],
        table1: {
          title: "Installed versions",
          action: "Install another",
          columns: ["Version", "Status", "Support until"],
          rows: versions.map((v, i) =>
            row(lang + " " + v, i === 0 ? "Active here" : "Available", i === 0 ? "Nov 2027" : i === 1 ? "Dec 2026" : "ended", i === 0 ? "Active" : "Use this"),
          ),
        },
      };

    case "cron":
      return {
        kicker: "Advanced",
        title: "Cron jobs",
        sub: "Scheduled commands for this site, with output kept for 7 days.",
        stats: [
          { label: "Jobs", value: "4", sub: "3 enabled" },
          { label: "Last run", value: "4 min ago", sub: "exit code 0" },
          { label: "Failures (7d)", value: "1", sub: "sitemap job timed out" },
          { label: "Next run", value: "02:00", sub: "nightly cleanup" },
        ],
        toggleTitle: "Behaviour",
        toggles: [
          { label: "Email me on failure", sub: "One mail per failing job, not per attempt", flag: "notifyEmail" },
          { label: "Skip if previous run is still going", sub: "Prevents overlapping jobs piling up", flag: "autoBlock" },
        ],
        sideTitle: "Add a job",
        sideNote: "Pick a preset schedule or write your own cron expression.",
        fields: [{ label: "Command", value: "php artisan schedule:run" }, { label: "Schedule", value: "*/5 * * * *" }],
        sideActions: [{ label: "Create cron job", primary: true }],
        table1: {
          title: "Cron jobs",
          action: "Add job",
          columns: ["Command", "Schedule", "Last run"],
          rows: [
            row("php artisan schedule:run", "every 5 min", "ok · 4 min ago", "Edit", "Delete"),
            row("node scripts/sitemap.js", "daily 01:30", "failed · timeout", "Edit", "Delete"),
            row("wp cron event run --due-now", "every 15 min", "ok · 9 min ago", "Edit", "Delete"),
            row("bash bin/cleanup.sh", "nightly 02:00", "disabled", "Enable", "Delete"),
          ],
        },
        logs: [
          { time: "12:05:00", text: "cron start: php artisan schedule:run" },
          { time: "12:05:02", text: "exit 0 · 1.8s", tone: "success" },
          { time: "01:30:00", text: "cron start: node scripts/sitemap.js" },
          { time: "01:31:00", text: "killed after 60s timeout", tone: "warning" },
        ],
        logName: "cron/site-jobs.log",
      };

    case "sshsite":
      return {
        kicker: "Advanced",
        title: "SSH access",
        sub: "Shell access scoped to this site's directory only.",
        stats: [
          { label: "Access", value: "Enabled", sub: "jailed to site root" },
          { label: "Keys", value: "2", sub: "per-site keys" },
          { label: "Last login", value: "2 hours ago", sub: "from 49.36.12.5" },
          { label: "SFTP", value: "Enabled", sub: "same credentials" },
        ],
        toggleTitle: "Access",
        toggles: [
          { label: "SSH for this site", sub: "Turn off to allow file manager access only", flag: "ssh" },
          { label: "Password login", sub: "Keys only is strongly recommended", flag: "passwordLogin", warn: true },
        ],
        sideTitle: "Connection details",
        sideNote: "Use these with any SSH or SFTP client.",
        fields: [
          { label: "Host", value: s.domain },
          { label: "User", value: shellUser(s.domain) },
          { label: "Port", value: "2202" },
          { label: "Home", value: "/home/nexp/" + s.domain },
        ],
        sideActions: [{ label: "Add SSH key", primary: true }, { label: "Open web terminal" }],
        table1: {
          title: "Authorised keys",
          action: "Add key",
          columns: ["Name", "Fingerprint", "Last used"],
          rows: [
            row("macbook-aarav", "SHA256:4f2a…c91", "2 hours ago", "Revoke"),
            row("deploy-runner", "SHA256:9b71…e40", "yesterday", "Revoke"),
          ],
        },
      };

    case "git":
      return {
        kicker: "Advanced",
        title: "Git",
        sub:
          s.deploy === "GitHub"
            ? "Repository, deploy key and branch settings for this site."
            : "Not connected yet — link a repository to deploy on push.",
        stats: [
          { label: "Repository", value: s.deploy === "GitHub" ? "Connected" : "None", sub: s.repo },
          { label: "Branch", value: s.branch, sub: "tracked" },
          { label: "Last commit", value: "4f2a1c9", sub: s.lastDeploy },
          { label: "Deploy key", value: "Active", sub: "read-only" },
        ],
        toggleTitle: "Deployment",
        toggles: [
          { label: "Auto-deploy on push", sub: "Every push to " + s.branch + " goes live", flag: "autoOs" },
          { label: "Run build command", sub: s.stackKey === "node" ? "npm ci && npm run build" : "composer install --no-dev", flag: "autoPhp" },
          { label: "Keep .env out of the repo", sub: "Environment variables stay in the panel", flag: "autoWp" },
        ],
        sideTitle: "Repository",
        sideNote: "Deploy keys are read-only. Rotate any time without touching the repo.",
        fields: [
          { label: "Repository", value: s.repo },
          { label: "Branch", value: s.branch },
          { label: "Webhook", value: "https://hook.nexpanel.app/g/4f2a" },
        ],
        sideActions: [
          { label: s.deploy === "GitHub" ? "Pull now" : "Connect repository", primary: true },
          { label: "Rotate deploy key" },
        ],
        table1: {
          title: "Recent commits",
          action: "Open on GitHub",
          columns: ["Commit", "Author", "When"],
          rows: [
            row("fix: cart totals with coupons", "aarav", "2h ago", "Deploy this"),
            row("chore: bump dependencies", "priya", "yesterday", "Deploy this"),
            row("feat: order webhooks", "aarav", "3 days ago", "Deploy this"),
          ],
        },
      };

    case "ipmanage":
      return {
        kicker: "Advanced",
        title: "IP manage",
        sub: "Block or allow visitors for this site, without touching the server firewall.",
        stats: [
          { label: "Blocked", value: "12", sub: "3 added this week" },
          { label: "Allowed", value: "2", sub: "bypass all rules" },
          { label: "Blocked hits (24h)", value: "318", sub: "requests refused" },
          { label: "Country rules", value: "1", sub: "admin area only" },
        ],
        toggleTitle: "Rules",
        toggles: [
          { label: "Block admin area outside India", sub: "Applies to /wp-admin and /login", flag: "ipBlock" },
          { label: "Auto-block abusive IPs", sub: "100 requests in 10s = 1 hour block", flag: "autoBlock" },
        ],
        sideTitle: "Add an IP",
        sideNote: "Ranges in CIDR notation are supported, e.g. 45.148.10.0/24.",
        fields: [{ label: "IP or range", value: "45.148.10.0/24" }, { label: "Action", value: "Block" }, { label: "Expires", value: "never" }],
        sideActions: [{ label: "Add rule", primary: true }],
        table1: {
          title: "Blocked IPs",
          action: "Block an IP",
          columns: ["IP / range", "Reason", "Added"],
          rows: [
            row("45.148.10.0/24", "login brute force", "18 min ago", "Unblock"),
            row("185.220.101.4", "SQL injection attempts", "2 hours ago", "Unblock"),
            row("103.76.44.19", "content scraping", "yesterday", "Unblock"),
          ],
        },
        table2: {
          title: "Allowlist",
          action: "Add IP",
          columns: ["IP / range", "Note", "Added"],
          rows: [
            row("49.36.12.5", "office", "3 weeks ago", "Remove"),
            row("106.51.88.240", "Aarav home", "3 weeks ago", "Remove"),
          ],
        },
      };

    case "hotlink":
      return {
        kicker: "Advanced",
        title: "Hotlink protection",
        sub: "Stop other sites embedding your images and video and burning your bandwidth.",
        stats: [
          { label: "Status", value: "Protecting", sub: "images, video, fonts" },
          { label: "Blocked (7d)", value: "2,104", sub: "requests refused" },
          { label: "Bandwidth saved", value: "3.8 GB", sub: "this month" },
          { label: "Allowed sites", value: "3", sub: "plus your own domains" },
        ],
        toggleTitle: "Protection",
        toggles: [
          { label: "Hotlink protection", sub: "Requests with a foreign referrer get a 403", flag: "autoQuarantine" },
          { label: "Allow empty referrer", sub: "Keeps direct links and some browsers working", flag: "autoOs" },
        ],
        sideTitle: "Protected file types",
        sideNote: "Only these extensions are checked. Everything else is served normally.",
        fields: [
          { label: "Extensions", value: "jpg, jpeg, png, webp, gif, mp4, woff2" },
          { label: "Response", value: "403 Forbidden" },
        ],
        sideActions: [{ label: "Edit file types", primary: true }],
        table1: {
          title: "Allowed referrers",
          action: "Add domain",
          columns: ["Domain", "Note", "Added"],
          rows: [
            row(s.domain, "this site", "automatic", "Locked"),
            row("cdn.novaretail.in", "own CDN", "2 weeks ago", "Remove"),
            row("partner-shop.in", "reseller catalogue", "5 days ago", "Remove"),
          ],
        },
        table2: {
          title: "Recently blocked referrers",
          action: "Export",
          columns: ["Referrer", "Requests", "Last seen"],
          rows: [
            row("cheap-deals.xyz", "1,402", "12 min ago", "Allow"),
            row("scraper-blog.net", "508", "2 hours ago", "Allow"),
            row("images.aggregator.io", "194", "yesterday", "Allow"),
          ],
        },
      };

    case "cachemgr":
      return {
        kicker: "Advanced",
        title: "Cache manager",
        sub: "Page, object and browser caching for this site.",
        stats: [
          { label: "Hit rate", value: "92%", sub: "last 24 hours" },
          { label: "Cached pages", value: "1,284", sub: "184 MB on disk" },
          { label: "Avg response", value: "38 ms", sub: "312 ms uncached" },
          { label: "Last purge", value: "2 hours ago", sub: "after deploy" },
        ],
        choiceTitle: "Page cache mode",
        choiceDefault: "Standard",
        choices: [
          { label: "Off", sub: "For dashboards and logged-in-only apps." },
          { label: "Standard", sub: "Cache guests for 1 hour, skip cookies." },
          { label: "Aggressive", sub: "Cache everything for 24 hours, ignore query strings." },
        ],
        toggleTitle: "Layers",
        toggles: [
          { label: "Object cache (Redis)", sub: "Database query and session caching", flag: "autoPhp" },
          { label: "Browser cache headers", sub: "Static assets cached for 30 days", flag: "autoWp" },
          { label: "Purge automatically after deploy", sub: "Keeps visitors off stale pages", flag: "autoOs" },
        ],
        sideTitle: "Purge",
        sideNote: "Purging is instant. Rebuilds happen on the next visit.",
        fields: [{ label: "Exclude paths", value: "/cart, /checkout, /wp-admin" }, { label: "TTL", value: "3600s" }],
        sideActions: [{ label: "Purge everything", primary: true }, { label: "Purge a single URL" }],
      };

    case "activity":
      return {
        kicker: "Advanced",
        title: "Activity logs",
        sub: "Every panel action taken on this site, and by whom.",
        stats: [
          { label: "Events (7d)", value: "146", sub: "3 team members" },
          { label: "Deploys", value: "12", sub: "1 failed" },
          { label: "File changes", value: "38", sub: "via file manager" },
          { label: "Retention", value: "1 year", sub: "exportable as CSV" },
        ],
        table1: {
          title: "Panel activity",
          action: "Export CSV",
          columns: ["Action", "By", "When"],
          rows: [
            row("Redeployed " + s.branch + "@4f2a1c9", "aarav", "2 hours ago", "Details"),
            row("Edited environment variable STRIPE_SECRET", "aarav", "yesterday", "Details"),
            row("Uploaded 14 files to /public/assets", "priya", "2 days ago", "Details"),
            row("Changed PHP version 8.2 → 8.3", "aarav", "5 days ago", "Details"),
            row("Restored backup 08 Aug 02:00", "priya", "last week", "Details"),
          ],
        },
        logs: [
          { time: "12:09:31", text: "aarav purged page cache" },
          { time: "11:20:14", text: "aarav deployed " + s.branch + "@4f2a1c9", tone: "success" },
          { time: "10:02:41", text: "priya opened web terminal" },
          { time: "09:41:55", text: "file changed /public/index.php", tone: "warning" },
        ],
        logName: "audit/" + s.domain + ".log",
      };

    case "aitrouble":
      return {
        kicker: "Performance",
        title: "AI troubleshooter",
        sub: "Describe what feels wrong, or let it read the logs and metrics itself.",
        stats: [
          { label: "Last scan", value: "9 min ago", sub: "3 findings" },
          { label: "Confidence", value: "High", sub: "reproduced twice" },
          { label: "Est. gain", value: "−410 ms", sub: "if all applied" },
          { label: "Auto-fixable", value: "2 of 3", sub: "one needs your call" },
        ],
        sideTitle: "Run a diagnosis",
        sideNote: "It reads slow queries, error logs, PHP timings and cache hit rate — nothing leaves your server.",
        fields: [{ label: "What are you seeing?", value: "Checkout is slow after 6pm" }],
        sideActions: [{ label: "Diagnose now", primary: true }, { label: "Explain last result" }],
        toggleTitle: "Behaviour",
        toggles: [
          { label: "Scan automatically after each deploy", sub: "Flags regressions before your visitors do", flag: "autoOs" },
          { label: "Apply safe fixes without asking", sub: "Cache and index changes only, always reversible", flag: "autoBlock" },
        ],
        table1: {
          title: "Findings",
          action: "Re-scan",
          columns: ["Finding", "Impact", "Confidence"],
          rows: [
            row("Missing index on orders.created_at", "~280 ms per request", "High", "Apply fix", "Dismiss"),
            row("Page cache bypassed by tracking query strings", "~90 ms, 38% of hits", "High", "Apply fix", "Dismiss"),
            row("Uncompressed hero image 2.4 MB", "~40 ms on mobile", "Medium", "Show file", "Dismiss"),
          ],
        },
        logs: [
          { time: "12:04:11", text: "analysing 4,812 requests over 24h…" },
          { time: "12:04:38", text: "slow query detected: SELECT * FROM orders WHERE created_at…", tone: "warning" },
          { time: "12:04:52", text: "suggestion ready · 3 findings", tone: "success" },
        ],
        logName: "ai/troubleshooter.log",
      };

    case "pagespeed":
      return {
        kicker: "Performance",
        title: "PageSpeed",
        sub: "Real Core Web Vitals from your visitors, plus a lab run on demand.",
        stats: [
          { label: "Performance", value: "92", sub: "mobile, lab run" },
          { label: "LCP", value: "1.4 s", sub: "good · field data" },
          { label: "CLS", value: "0.02", sub: "good" },
          { label: "INP", value: "96 ms", sub: "good" },
        ],
        sideTitle: "Run a test",
        sideNote: "Lab runs take about 30 seconds and do not affect live traffic.",
        fields: [{ label: "URL", value: "https://" + s.domain + "/" }, { label: "Device", value: "Mobile · 4G" }],
        sideActions: [{ label: "Run test", primary: true }, { label: "Compare with last week" }],
        toggleTitle: "Optimisations",
        toggles: [
          { label: "Convert images to WebP", sub: "Originals kept, served by browser support", flag: "autoWp" },
          { label: "Minify CSS and JavaScript", sub: "Skipped for files with a source map", flag: "autoPhp" },
          { label: "Lazy-load below-the-fold images", sub: 'Adds loading="lazy" on delivery', flag: "autoOs" },
        ],
        table1: {
          title: "Opportunities",
          action: "Re-run",
          columns: ["Opportunity", "Saving", "Status"],
          rows: [
            row("Serve hero image as WebP", "1.8 s → 1.4 s", "Available", "Apply"),
            row("Preload the display font", "120 ms", "Available", "Apply"),
            row("Defer third-party analytics", "210 ms", "Needs review", "Details"),
          ],
        },
      };

    case "cdn":
      return {
        kicker: "Performance",
        title: "Cloudflare CDN",
        sub: "Cache your site at the edge and hide your origin IP.",
        stats: [
          { label: "Status", value: "Proxied", sub: "286 edge locations" },
          { label: "Cached", value: "78%", sub: "of all bytes served" },
          { label: "Bandwidth saved", value: "112 GB", sub: "this month" },
          { label: "Origin load", value: "−64%", sub: "vs last month" },
        ],
        toggleTitle: "Edge settings",
        toggles: [
          { label: "Proxy traffic through Cloudflare", sub: "Hides your server IP and enables edge caching", flag: "waf" },
          { label: "Always Use HTTPS", sub: "Redirects http requests at the edge", flag: "autoOs" },
          { label: "Brotli compression", sub: "Smaller text responses for modern browsers", flag: "autoPhp" },
          { label: "Development mode", sub: "Bypasses cache for 3 hours while you work", flag: "schedScan" },
        ],
        sideTitle: "Connection",
        sideNote: "API token is stored encrypted and only used for cache purges and DNS.",
        fields: [{ label: "Zone", value: s.domain }, { label: "Plan", value: "Free" }, { label: "API token", value: "cf_••••8a21" }],
        sideActions: [{ label: "Purge edge cache", primary: true }, { label: "Reconnect zone" }],
        table1: {
          title: "Cache rules",
          action: "Add rule",
          columns: ["Pattern", "Behaviour", "Edge TTL"],
          rows: [
            row("/*", "Cache static assets", "30 days", "Edit"),
            row("/cart*, /checkout*", "Bypass cache", "—", "Edit"),
            row("/api/*", "Bypass cache", "—", "Edit"),
          ],
        },
      };

    case "analytics":
      return {
        kicker: "Site",
        title: "Analytics",
        sub: "Server-side traffic for " + s.domain + ". No cookies, no script to add.",
        stats: [
          { label: "Visits (24h)", value: "4,812", sub: "+12% vs yesterday" },
          { label: "Unique visitors", value: "3,104", sub: "by IP + agent" },
          { label: "Bandwidth", value: "46 GB", sub: "this month" },
          { label: "Error rate", value: "0.4%", sub: "18× 404, 2× 500" },
        ],
        table1: {
          title: "Top pages",
          action: "Last 7 days",
          columns: ["Path", "Views", "Avg time"],
          rows: [
            row("/", "1,842", "38 ms", "Details"),
            row("/products", "1,104", "64 ms", "Details"),
            row("/checkout", "612", "212 ms", "Details"),
            row("/blog/diwali-sale", "388", "44 ms", "Details"),
          ],
        },
        table2: {
          title: "Traffic sources",
          action: "Export",
          columns: ["Source", "Visits", "Share"],
          rows: [
            row("Direct", "1,904", "40%", "Details"),
            row("Google organic", "1,612", "33%", "Details"),
            row("Instagram", "804", "17%", "Details"),
            row("Referrals", "492", "10%", "Details"),
          ],
        },
      };

    case "malware":
      return {
        kicker: "Security",
        title: "Malware scanner",
        sub: "Signature and heuristic scan of this site's files.",
        stats: [
          { label: "Last scan", value: "6 days ago", sub: "18,204 files" },
          { label: "Threats", value: "1", sub: "pending review" },
          { label: "Quarantine", value: "1", sub: "file isolated" },
          { label: "Status", value: "Needs review", sub: "not clean" },
        ],
        sideTitle: "Scan now",
        sideNote: "A full scan takes about a minute and does not slow the site. Threats are isolated, never deleted silently.",
        sideActions: [{ label: "Run full scan", primary: true }, { label: "Scan uploads only" }],
        toggleTitle: "Automation",
        toggles: [
          { label: "Scheduled daily scan", sub: "Paid add-on · runs at 03:00", flag: "schedScan", paid: true },
          { label: "Real-time write protection", sub: "Paid add-on · blocks malicious writes", flag: "realtime", paid: true },
        ],
        table1: {
          title: "Detected threats",
          action: "Review all",
          columns: ["File", "Signature", "Found"],
          rows: [row("/public/wp-content/uploads/x.php", "PHP.Backdoor.Web-Shell", "6 days ago", "Quarantine", "Ignore")],
        },
        table2: {
          title: "Quarantine",
          action: "Empty",
          columns: ["File", "Isolated", "Size"],
          rows: [row("uploads/2026/07/theme.php", "6 days ago", "14 KB", "Restore", "Delete")],
        },
      };

    case "ssl":
      return {
        kicker: "Security",
        title: "SSL",
        sub: "Certificates and HTTPS behaviour for " + s.domain + ".",
        stats: [
          { label: "Certificate", value: "Active", sub: "Let's Encrypt" },
          { label: "Expires", value: "in 74 days", sub: "auto-renews at 30" },
          { label: "Grade", value: "A", sub: "TLS 1.2 and 1.3 only" },
          { label: "HSTS", value: "Enabled", sub: "max-age 1 year" },
        ],
        toggleTitle: "HTTPS",
        toggles: [
          { label: "Force HTTPS", sub: "Redirect every http request permanently", flag: "autoOs" },
          { label: "HSTS", sub: "Browsers refuse http for a year — set once you are sure", flag: "autoWp" },
          { label: "Auto-renew certificate", sub: "Renews 30 days before expiry", flag: "autoPhp" },
        ],
        sideTitle: "Certificate",
        sideNote: "You can also upload your own certificate and private key.",
        fields: [
          { label: "Issuer", value: "Let's Encrypt R11" },
          { label: "Covers", value: s.domain + ", www." + s.domain },
          { label: "Issued", value: "02 Aug 2026" },
        ],
        sideActions: [{ label: "Reissue now", primary: true }, { label: "Upload custom certificate" }],
      };

    case "subdomains":
      return {
        kicker: "Domains",
        title: "Subdomains",
        sub: "Each subdomain can point at its own folder or its own app.",
        stats: [
          { label: "Subdomains", value: "3", sub: "all with SSL" },
          { label: "Wildcard", value: "On", sub: "*." + s.domain },
          { label: "SSL", value: "Covered", sub: "single wildcard cert" },
          { label: "DNS", value: "Automatic", sub: "records created for you" },
        ],
        toggleTitle: "Options",
        toggles: [
          { label: "Wildcard subdomain", sub: "Anything." + s.domain + " resolves here", flag: "autoBlock" },
          { label: "Auto-issue SSL for new subdomains", sub: "Certificate is extended on creation", flag: "autoPhp" },
        ],
        sideTitle: "Create subdomain",
        sideNote: "DNS and SSL are handled automatically. Live in under a minute.",
        fields: [{ label: "Subdomain", value: "staging" }, { label: "Document root", value: "/staging/public" }],
        sideActions: [{ label: "Create subdomain", primary: true }],
        table1: {
          title: "Subdomains",
          action: "Create",
          columns: ["Subdomain", "Document root", "SSL"],
          rows: [
            row("staging." + s.domain, "/staging/public", "Active", "Manage", "Delete"),
            row("cdn." + s.domain, "/public/assets", "Active", "Manage", "Delete"),
            row("docs." + s.domain, "/docs/site", "Active", "Manage", "Delete"),
          ],
        },
      };

    case "parked":
      return {
        kicker: "Domains",
        title: "Parked domains",
        sub: "Extra domains that show the same site — useful for spellings and old brands.",
        stats: [
          { label: "Parked", value: "2", sub: "both with SSL" },
          { label: "SEO", value: "301 to primary", sub: "no duplicate content" },
          { label: "Primary", value: s.domain, sub: "canonical host" },
          { label: "Available", value: "18 slots", sub: "on your plan" },
        ],
        sideTitle: "Park a domain",
        sideNote: "Point the domain's nameservers at NexPanel first, then park it here.",
        fields: [{ label: "Domain", value: "novaretail.co.in" }, { label: "Behaviour", value: "301 redirect to primary" }],
        sideActions: [{ label: "Park domain", primary: true }],
        toggleTitle: "Behaviour",
        toggles: [
          { label: "Redirect parked domains to primary", sub: "Recommended — keeps search rankings on one host", flag: "autoOs" },
          { label: "Issue SSL for parked domains", sub: "Avoids browser warnings on the redirect", flag: "autoPhp" },
        ],
        table1: {
          title: "Parked domains",
          action: "Park domain",
          columns: ["Domain", "Behaviour", "SSL"],
          rows: [
            row("novaretail.co.in", "301 → " + s.domain, "Active", "Manage", "Remove"),
            row("nova-retail.in", "301 → " + s.domain, "Active", "Manage", "Remove"),
          ],
        },
      };

    case "redirects":
      return {
        kicker: "Domains",
        title: "Redirections",
        sub: "Send old URLs somewhere useful instead of a 404.",
        stats: [
          { label: "Rules", value: "4", sub: "3 permanent" },
          { label: "Hits (7d)", value: "1,204", sub: "redirects served" },
          { label: "Broken links", value: "18", sub: "404s worth a rule" },
          { label: "Loops", value: "0", sub: "checked on save" },
        ],
        sideTitle: "Add redirect",
        sideNote: "Wildcards and capture groups are supported, e.g. /blog/* → /articles/$1.",
        fields: [{ label: "From", value: "/old-shop/*" }, { label: "To", value: "/products/$1" }, { label: "Type", value: "301 permanent" }],
        sideActions: [{ label: "Add redirect", primary: true }, { label: "Import from CSV" }],
        table1: {
          title: "Redirect rules",
          action: "Add rule",
          columns: ["From", "To", "Type"],
          rows: [
            row("/old-shop/*", "/products/$1", "301", "Edit", "Delete"),
            row("/sale", "/products?tag=sale", "302", "Edit", "Delete"),
            row("/team.html", "/about#team", "301", "Edit", "Delete"),
            row("/feed.xml", "/rss", "301", "Edit", "Delete"),
          ],
        },
        table2: {
          title: "Frequent 404s",
          action: "Export",
          columns: ["Path", "Hits (7d)", "Last seen"],
          rows: [
            row("/wp-login.php", "412", "2 min ago", "Add rule"),
            row("/old-pricing", "96", "1 hour ago", "Add rule"),
            row("/img/logo-old.png", "44", "yesterday", "Add rule"),
          ],
        },
      };

    case "wpinstall":
      return {
        kicker: "WordPress manager",
        title: "Install",
        sub: "A fresh WordPress with SSL, cache and sensible security defaults.",
        stats: [
          { label: "Installed", value: "WordPress 6.7.1", sub: "on " + s.domain },
          { label: "Multisite", value: "No", sub: "single install" },
          { label: "Admin URL", value: "/wp-admin", sub: "IP-restricted" },
          { label: "Time to install", value: "~40 s", sub: "for a new install" },
        ],
        sideTitle: "New install",
        sideNote: "Installing into a folder that already has files will ask before overwriting.",
        fields: [
          { label: "Site title", value: "Nova Retail" },
          { label: "Admin email", value: "aarav@novaretail.in" },
          { label: "Install path", value: "/public" },
          { label: "Language", value: "English (India)" },
        ],
        sideActions: [{ label: "Install WordPress", primary: true }, { label: "Install into subfolder" }],
        toggleTitle: "Included by default",
        toggles: [
          { label: "Object cache with Redis", sub: "Drops in the plugin and wires it up", flag: "autoPhp" },
          { label: "Restrict wp-admin by IP", sub: "Only your trusted IPs can reach the login", flag: "ipBlock" },
          { label: "Disable file editing in admin", sub: "Blocks the most common takeover route", flag: "autoWp" },
        ],
      };

    case "wpmigrate":
      return {
        kicker: "WordPress manager",
        title: "Migrate website",
        sub: "Bring an existing WordPress here with no downtime.",
        stats: [
          { label: "Method", value: "Automatic", sub: "no plugin needed" },
          { label: "Typical time", value: "6 min", sub: "for 500 MB" },
          { label: "Downtime", value: "None", sub: "DNS switched last" },
          { label: "Last migration", value: "none yet", sub: "this site" },
        ],
        sideTitle: "Source site",
        sideNote:
          "We copy files and database over SFTP or SSH, then let you preview on a temporary URL before switching DNS.",
        fields: [
          { label: "Source URL", value: "https://old-host.example/novaretail" },
          { label: "SFTP host", value: "ftp.old-host.example" },
          { label: "Username", value: "novaretail" },
        ],
        sideActions: [{ label: "Start migration", primary: true }, { label: "Upload a backup instead" }],
        toggleTitle: "Options",
        toggles: [
          { label: "Search and replace old URLs", sub: "Rewrites the database to the new domain", flag: "autoWp" },
          { label: "Skip cache and log folders", sub: "Faster copy, nothing of value lost", flag: "autoOs" },
          { label: "Keep source site online until I switch", sub: "DNS stays put until you confirm", flag: "autoPhp" },
        ],
        table1: {
          title: "Migration steps",
          action: "Restart",
          columns: ["Step", "Detail", "Status"],
          rows: [
            row("Connect to source", "SFTP handshake", "Waiting", "Test"),
            row("Copy files", "~500 MB expected", "Queued", "Skip"),
            row("Copy database", "mysqldump over SSH", "Queued", "Skip"),
            row("Rewrite URLs", "old-host.example → " + s.domain, "Queued", "Skip"),
          ],
        },
      };

    case "wpstaging":
      return {
        kicker: "WordPress manager",
        title: "Create staging",
        sub: "A private copy of this site to test updates and changes on before they reach visitors.",
        stats: [
          { label: "Staging site", value: "None yet", sub: "create one in ~90 s" },
          { label: "URL", value: "Reserved", sub: "staging." + s.domain },
          { label: "Size estimate", value: "512 MB", sub: "files + database" },
          { label: "Push to live", value: "One click", sub: "with automatic backup" },
        ],
        sideTitle: "Create a staging copy",
        sideNote:
          "Files, database and plugins are cloned. Search engines are blocked and email sending is disabled on staging.",
        fields: [
          { label: "Staging URL", value: "staging." + s.domain },
          { label: "Copy", value: "Files + database" },
          { label: "Access", value: "Password-protected" },
        ],
        sideActions: [{ label: "Create staging site", primary: true }, { label: "Clone into a subfolder" }],
        toggleTitle: "Staging behaviour",
        toggles: [
          { label: "Block search engines", sub: "noindex header on every staging response", flag: "autoWp" },
          { label: "Disable outgoing email", sub: "Stops test orders mailing real customers", flag: "autoOs" },
          { label: "Keep staging in sync nightly", sub: "Pulls the live database into staging at 03:00", flag: "schedScan" },
        ],
        table1: {
          title: "Push to live",
          action: "Learn more",
          columns: ["What moves", "Default", "Note"],
          rows: [
            row("Theme and plugin files", "Included", "live backup taken first", "Change"),
            row("Uploads and media", "Included", "new files only", "Change"),
            row("Database tables", "Selected", "orders and users skipped", "Change"),
            row("wp-config.php", "Excluded", "live credentials preserved", "Change"),
          ],
        },
      };

    case "ftp":
      return {
        kicker: "Files",
        title: "FTP",
        sub: "SFTP accounts for designers and legacy tools. Plain FTP is off by default.",
        stats: [
          { label: "Accounts", value: "2", sub: "1 folder-scoped" },
          { label: "Protocol", value: "SFTP only", sub: "port 2202" },
          { label: "Last login", value: "yesterday", sub: "from 49.36.12.5" },
          { label: "Transfers (7d)", value: "184 MB", sub: "312 files" },
        ],
        toggleTitle: "Access",
        toggles: [
          { label: "SFTP enabled", sub: "Encrypted transfer over SSH", flag: "ssh" },
          { label: "Allow plain FTP", sub: "Unencrypted — only for very old tooling", flag: "passwordLogin", warn: true },
        ],
        sideTitle: "Connection details",
        sideNote: "Use these in FileZilla, Cyberduck or any SFTP client.",
        fields: [
          { label: "Host", value: s.domain },
          { label: "Port", value: "2202" },
          { label: "Protocol", value: "SFTP" },
          { label: "Root", value: "/home/nexp/" + s.domain },
        ],
        sideActions: [{ label: "Create FTP account", primary: true }],
        table1: {
          title: "FTP accounts",
          action: "Create account",
          columns: ["Username", "Scoped to", "Last login"],
          rows: [
            row("nova_deploy", "/ (site root)", "yesterday", "Password", "Delete"),
            row("nova_design", "/public/assets", "5 days ago", "Password", "Delete"),
          ],
        },
      };

    case "phpmyadmin":
      return {
        kicker: "Databases",
        title: "phpMyAdmin",
        sub: "Browse tables, run SQL and export dumps in the browser.",
        stats: [
          { label: "Session", value: "Ready", sub: "signs you in automatically" },
          { label: "Databases", value: "2", sub: "visible to this site" },
          { label: "Version", value: "5.2.1", sub: "latest" },
          { label: "Access", value: "Panel only", sub: "not publicly reachable" },
        ],
        sideTitle: "Open phpMyAdmin",
        sideNote:
          "Opens in a new tab with a one-time signed session. Nothing is exposed publicly and the link expires in 5 minutes.",
        sideActions: [{ label: "Open phpMyAdmin", primary: true }, { label: "Open read-only" }],
        toggleTitle: "Safety",
        toggles: [
          { label: "Confirm before DROP and TRUNCATE", sub: "Extra dialog on destructive SQL", flag: "autoQuarantine" },
          { label: "Snapshot before running SQL", sub: "Automatic database backup, kept 24 hours", flag: "autoOs" },
        ],
        table1: {
          title: "Databases",
          action: "Refresh",
          columns: ["Database", "Tables", "Size"],
          rows: [row(db, "38", "184 MB", "Open"), row(db + "_stg", "38", "22 MB", "Open")],
        },
      };

    case "remotedb":
      return {
        kicker: "Databases",
        title: "Remote DB",
        sub: "Let an external app or your laptop connect to this database directly.",
        stats: [
          { label: "Remote access", value: "Allowlist", sub: "2 IPs allowed" },
          { label: "Port", value: "3306", sub: "TLS required" },
          { label: "Connections", value: "3", sub: "of 60 max" },
          { label: "Last connect", value: "11 min ago", sub: "from 49.36.12.5" },
        ],
        toggleTitle: "Access",
        toggles: [
          { label: "Allow remote connections", sub: "Only from the IPs listed below", flag: "ipBlock" },
          { label: "Require TLS", sub: "Refuse unencrypted connections", flag: "autoPhp" },
        ],
        sideTitle: "Connection string",
        sideNote: "Create a dedicated user per app rather than sharing the admin credentials.",
        fields: [
          { label: "Host", value: s.domain },
          { label: "Port", value: "3306" },
          { label: "Database", value: db },
          { label: "User", value: "nexp_remote" },
        ],
        sideActions: [{ label: "Add allowed IP", primary: true }, { label: "Create DB user" }],
        table1: {
          title: "Allowed IPs",
          action: "Add IP",
          columns: ["IP / range", "Note", "Added"],
          rows: [
            row("49.36.12.5", "office", "3 weeks ago", "Remove"),
            row("106.51.88.240", "Aarav laptop", "2 weeks ago", "Remove"),
          ],
        },
        table2: {
          title: "Database users",
          action: "Create user",
          columns: ["User", "Privileges", "Host"],
          rows: [row("nexp_admin", "ALL", "localhost", "Manage"), row("nexp_remote", "SELECT, INSERT, UPDATE", "%", "Manage")],
        },
      };
  }
}
