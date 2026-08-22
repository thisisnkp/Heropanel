/**
 * Fixtures for the site screens that have their own layout — overview, git
 * deployments, runtime, environment, WordPress, databases, logs, backups.
 *
 * Everything here is derived from the site rather than hard-coded, because most
 * of it is per-stack: a Node site shows a start command and PM2 instances where
 * a PHP site shows memory_limit and extensions, and a screen that shows the
 * wrong one is worse than a screen that shows nothing.
 */
import type { Site } from "@/stores/sites";

export interface KeyValue {
  readonly label: string;
  readonly value: string;
}

export interface QuickAction {
  readonly label: string;
  readonly sub: string;
  readonly icon: string;
  readonly to: string;
  readonly tone: "brand" | "success" | "warning" | "danger";
  /** Opens in its own window — see SiteNavLeaf.newTab. */
  readonly newTab?: boolean;
}

export interface SecurityCheck {
  readonly label: string;
  readonly sub: string;
  readonly ok: boolean;
  readonly to: string;
}

export interface DeployRow {
  readonly message: string;
  readonly sha: string;
  readonly when: string;
  readonly state: "Deployed" | "Failed";
}

export interface FileRow {
  readonly name: string;
  readonly size: string;
  readonly when: string;
  readonly perm: string;
  readonly kind: "code" | "config" | "folder" | "secret" | "doc";
}

/** The runtime facts the design shows per stack. */
export function runtimeFields(s: Site): readonly KeyValue[] {
  if (s.stackKey === "node") {
    return [
      { label: "Node version", value: "20.11.1 LTS" },
      { label: "Start command", value: "npm run start" },
      { label: "Port", value: "3000 (proxied)" },
      { label: "Process manager", value: "PM2 · cluster, 2 instances" },
    ];
  }
  if (s.stackKey === "python") {
    return [
      { label: "Python version", value: "3.12.4" },
      { label: "Entry point", value: "gunicorn app:app" },
      { label: "Workers", value: "3" },
      { label: "Virtualenv", value: ".venv (managed)" },
    ];
  }
  return [
    { label: "PHP version", value: "8.4" },
    { label: "memory_limit", value: "512M" },
    { label: "max_execution_time", value: "120" },
    { label: "Extensions", value: "opcache, redis, imagick" },
  ];
}

export function runtimeTitle(s: Site) {
  if (s.stackKey === "node") return "Node.js configuration";
  if (s.stackKey === "python") return "Python configuration";
  return "PHP configuration";
}

export function processNote(s: Site) {
  return s.stackKey === "node"
    ? "Restart reloads your app with zero downtime across both PM2 instances."
    : "Restarting reloads PHP-FPM and clears the opcache for this site.";
}

export function logName(s: Site) {
  if (s.stackKey === "node") return "pm2/app-out.log";
  if (s.stackKey === "python") return "gunicorn/access.log";
  return "nginx/error.log";
}

export function documentRoot(s: Site) {
  return "/home/nexp/" + s.domain + "/public";
}

/**
 * The identifier the panel derives from a domain, and the prefix every database
 * and database user on this site is named with.
 *
 * One derivation, exported, because three places need it and two of them used to
 * compute it themselves: the table on the databases screen, the phpMyAdmin spec,
 * and now the create form — whose whole job is to show you the name you are
 * about to get. Two copies of this is how a form promises `nexp_shop` and the
 * list underneath shows `nexp_novaretail_shop`.
 */
export function dbBase(domain: string) {
  return "nexp_" + domain.split(".")[0].replace(/-/g, "_");
}

/** What a new database or user on this site is prefixed with. */
export function dbPrefix(domain: string) {
  return dbBase(domain) + "_";
}

export function databases(s: Site) {
  const base = dbBase(s.domain);
  return [
    { name: base, user: "nexp_admin", size: "184 MB" },
    { name: base + "_stg", user: "nexp_admin", size: "22 MB" },
  ];
}

/**
 * The shortcuts on the overview — at most six.
 *
 * Which ones depends on the stack and on whether a repo is connected: a static
 * site has no runtime to restart, a manual-deploy site has no "Redeploy from
 * Git". Every entry must also exist in that site's drawer, or the overview is
 * offering a screen the menu says the site does not have.
 *
 * Fewer than six is fine and happens — a manual Node site gets five, because the
 * upload shortcut and the file manager are the same destination and the dedupe
 * keeps one. Padding the list to six would mean inventing an entry.
 */
export function quickActions(s: Site): readonly QuickAction[] {
  const dbs = databases(s);

  const all: QuickAction[] = [
    { label: "File manager", sub: "public/", icon: "folder-open", to: "site-files", tone: "brand", newTab: true },
    // The sub-line counts the site's own databases rather than saying
    // "MySQL": the shortcut is worth taking when you know what is behind it.
    {
      label: "Databases",
      sub: dbs.length === 1 ? "1 database" : dbs.length + " databases",
      icon: "database",
      to: "site-db",
      tone: "brand",
    },
  ];

  if (s.stackKey === "wp") {
    all.push({ label: "Plugins & updates", sub: "WordPress 6.7.1", icon: "widgets", to: "site-wp-plugins", tone: "success" });
  } else if (s.deploy === "GitHub") {
    all.push({ label: "Redeploy from Git", sub: s.branch, icon: "cloud-sync", to: "site-git-deployments", tone: "success" });
  } else {
    all.push({ label: "Upload files", sub: "zip or folder", icon: "upload", to: "site-files", tone: "success", newTab: true });
  }

  // Keyed on the stack rather than falling through to a PHP default: the old
  // `else` handed a Python site "PHP settings", pointing at a screen that is not
  // even in that site's menu, and a static site a runtime it does not have.
  if (s.stackKey === "node") {
    all.push({ label: "Restart app", sub: "Node 20 · PM2", icon: "restart-alt", to: "site-runtime", tone: "brand" });
  } else if (s.stackKey === "python") {
    all.push({ label: "Restart app", sub: "Gunicorn", icon: "restart-alt", to: "site-runtime", tone: "brand" });
  } else if (s.stackKey === "wp") {
    all.push({ label: "Create staging", sub: "test safely", icon: "content-copy", to: "site-wp-staging", tone: "brand" });
  } else if (s.stackKey === "php") {
    all.push({ label: "PHP settings", sub: "PHP 8.4", icon: "tune", to: "site-php", tone: "brand" });
  }

  all.push({ label: "Domain & SSL", sub: "certificate active", icon: "lock", to: "site-ssl", tone: "success" });

  // Analytics only where there is no runtime to look at instead. Logs used to
  // sit here for every other stack and no longer do — they are one click away
  // under Advanced, and the space is better spent on the databases above.
  if (s.stackKey === "static") {
    all.push({ label: "Analytics", sub: "traffic today", icon: "bar-chart", to: "site-analytics", tone: "warning" });
  }

  all.push({ label: "Backups", sub: "14 days kept", icon: "backup", to: "site-backups", tone: "brand" });

  // Two branches can land on the same destination (file manager, for a manual
  // static site). Keep the first mention and drop the duplicate.
  return all.filter((q, i) => all.findIndex((y) => y.to === q.to) === i).slice(0, 6);
}

export const SITE_SECURITY = {
  score: 86,
  verdict: "Good",
  checks: [
    { label: "SSL certificate", sub: "Renews in 74 days", ok: true, to: "site-ssl" },
    { label: "Malware scan", sub: "1 threat pending", ok: false, to: "site-malware" },
    { label: "WAF", sub: "312 blocked this week", ok: true, to: "security-waf" },
    { label: "Backups", sub: "Last night 02:00", ok: true, to: "site-backups" },
  ] as readonly SecurityCheck[],
};

export function siteFiles(s: Site): readonly FileRow[] {
  return [
    { name: s.stackKey === "node" ? "server.js" : "index.php", size: "4.1 KB", when: "2 hours ago", perm: "0644", kind: "code" },
    { name: "package.json", size: "1.2 KB", when: "2 hours ago", perm: "0644", kind: "config" },
    { name: "public/", size: "—", when: "yesterday", perm: "0755", kind: "folder" },
    { name: "assets/", size: "—", when: "yesterday", perm: "0755", kind: "folder" },
    { name: ".env", size: "380 B", when: "6 days ago", perm: "0600", kind: "secret" },
    { name: "README.md", size: "2.0 KB", when: "3 weeks ago", perm: "0644", kind: "doc" },
  ];
}

export const DEPLOYS: readonly DeployRow[] = [
  { message: "fix: cart totals with coupons", sha: "4f2a1c9", when: "2h ago", state: "Deployed" },
  { message: "chore: bump dependencies", sha: "9b71e40", when: "yesterday", state: "Deployed" },
  { message: "feat: order webhooks", sha: "c02d8aa", when: "3 days ago", state: "Deployed" },
  { message: "wip: experimental cache", sha: "77e1b3f", when: "5 days ago", state: "Failed" },
];

export const ENV_VARS: readonly KeyValue[] = [
  { label: "NODE_ENV", value: "production" },
  { label: "DATABASE_URL", value: "mysql://••••••••@localhost/nexp" },
  { label: "STRIPE_SECRET", value: "••••••••••••4f21" },
  { label: "REDIS_URL", value: "redis://127.0.0.1:6379" },
];

export const WP_ITEMS = [
  { name: "WooCommerce", version: "9.2.1", state: "active" as const },
  { name: "Rank Math SEO", version: "1.0.2", state: "active" as const },
  { name: "Contact Form 7", version: "5.9", state: "update" as const },
  { name: "Theme — Storefront", version: "4.6", state: "active" as const },
];

export const SITE_BACKUPS = [
  { when: "14 Aug 2026, 02:00", kind: "Files + database", size: "512 MB" },
  { when: "13 Aug 2026, 02:00", kind: "Files + database", size: "509 MB" },
  { when: "12 Aug 2026, 02:00", kind: "Files + database", size: "505 MB" },
  { when: "11 Aug 2026, 02:00", kind: "Files only", size: "318 MB" },
  { when: "10 Aug 2026, 02:00", kind: "Files + database", size: "501 MB" },
];

export const SITE_LOG_LINES = [
  { time: "12:04:11", text: "GET /  200  34ms" },
  { time: "12:04:12", text: "GET /assets/app.css  200  8ms" },
  { time: "12:04:19", text: "POST /api/orders  201  112ms", color: "var(--nx-success-on-dark)" },
  { time: "12:05:02", text: "GET /favicon.ico  404  3ms", color: "var(--nx-warning-on-dark)" },
  { time: "12:06:44", text: "worker: 3 jobs processed" },
  { time: "12:08:01", text: "GET /checkout  200  67ms" },
  { time: "12:09:23", text: "warn: slow query 812ms (orders)", color: "var(--nx-warning-on-dark)" },
  { time: "12:10:00", text: "deploy hook received · main@4f2a1c9", color: "var(--nx-success-on-dark)" },
];

// ---- PHP settings ----------------------------------------------------------

export const PHP_VERSIONS = [
  { version: "8.5", note: "latest" },
  { version: "8.4", note: "recommended" },
  { version: "8.3", note: "security fixes only" },
  { version: "8.2", note: "security fixes only" },
  { version: "8.1", note: "end of life" },
  { version: "8.0", note: "end of life" },
  { version: "7.4", note: "end of life" },
];

export const PHP_EXT_ENABLED = [
  "opcache", "redis", "imagick", "curl", "mbstring", "pdo_mysql", "gd", "zip",
  "intl", "json", "xml", "bcmath", "exif", "fileinfo", "sodium",
];

export const PHP_EXT_AVAILABLE = [
  "xdebug", "memcached", "imap", "ldap", "soap", "pgsql", "mongodb", "apcu",
  "ffi", "gmp", "tidy", "yaml", "swoole", "oci8", "snmp",
];

export const PHP_INI_FLAGS: readonly { key: string; on: boolean }[] = [
  { key: "allowUrlFopen", on: false },
  { key: "exposePhp", on: false },
  { key: "logErrors", on: true },
  { key: "opcache.enable", on: true },
  { key: "fileUploads", on: true },
  { key: "zlib.outputCompression", on: true },
  { key: "opcache.enableCli", on: false },
  { key: "session.useStrictMode", on: true },
  { key: "shortOpenTag", on: false },
  { key: "displayErrors", on: false },
  { key: "session.cookieSecure", on: true },
  { key: "session.cookieHttponly", on: true },
  { key: "allowUrlInclude", on: false },
  { key: "outputBuffering", on: true },
  { key: "opcache.validateTimestamps", on: true },
];

export function phpIniRows(s: Site): readonly KeyValue[] {
  return [
    { label: "date.timezone", value: "Asia/Kolkata" },
    { label: "disableFunctions", value: "exec, passthru, shell_exec, system" },
    { label: "includePath", value: ".:/usr/share/php" },
    { label: "mail.forceExtraParameters", value: "-fnoreply@" + s.domain },
    { label: "maxExecutionTime", value: "120" },
    { label: "maxFileUploads", value: "20" },
    { label: "maxInputTime", value: "120" },
    { label: "maxInputVars", value: "5000" },
    { label: "memoryLimit", value: "512M" },
    { label: "opcache.internedStringsBuffer", value: "16" },
    { label: "opcache.maxAcceleratedFiles", value: "10000" },
    { label: "opcache.maxFileSize", value: "0" },
    { label: "opcache.memoryConsumption", value: "256" },
    { label: "opcache.revalidateFreq", value: "2" },
    { label: "openBasedir", value: "/home/nexp/" + s.domain },
    { label: "postMaxSize", value: "256M" },
    { label: "session.cookieLifetime", value: "0" },
    { label: "session.gcMaxlifetime", value: "1440" },
    { label: "session.savePath", value: "/var/lib/php/sessions" },
    { label: "uploadMaxFilesize", value: "256M" },
    { label: "uploadTmpDir", value: "/tmp" },
    { label: "errorReporting", value: "E_ALL & ~E_DEPRECATED & ~E_STRICT" },
    { label: "errorLog", value: "/home/nexp/" + s.domain + "/logs/php-error.log" },
    { label: "session.cookieSamesite", value: "Lax" },
    { label: "defaultCharset", value: "UTF-8" },
    { label: "realpathCacheSize", value: "4096k" },
  ];
}

export const PHP_INI_RAW = [
  "; managed by NexPanel — edits here override the panel fields",
  "memory_limit = 512M",
  "max_execution_time = 120",
  "upload_max_filesize = 256M",
  "post_max_size = 256M",
  "display_errors = Off",
  "opcache.enable = 1",
  "opcache.memory_consumption = 256",
];
