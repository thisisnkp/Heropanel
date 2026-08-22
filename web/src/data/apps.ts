/**
 * The app catalogue.
 *
 * Categories are nested one level deep (Server side scripting → PHP / Node.js /
 * Python) because that is how the design grouped them and because a flat list
 * of forty entries is not browsable. The nesting lives in the data, so the
 * catalogue screen renders a tree it is given rather than hard-coding which
 * category happens to have children.
 *
 * The stack categories — webservers, the language runtimes, databases — describe
 * **this panel's stack**, not a marketplace. There is no marketplace: nothing
 * here is fetched from anywhere, so what a card offers has to be something
 * NexPanel actually manages or means to. Nginx, Apache, Caddy and MongoDB were
 * removed for that reason — the panel deleted its nginx/apache renderers in the
 * opinionated-stack change and never had a line of MongoDB, so a card offering
 * them promised a thing that could not happen.
 *
 * Where the catalogue is wider than what the panel manages, the category says so
 * in `note` rather than the card pretending otherwise. PostgreSQL is the case
 * that matters: it installs and runs, and the panel's Databases screen does not
 * manage it — which is exactly why pgAdmin is in the list beside it.
 */

export type AppBadge = "Installed" | "Free" | string;

/**
 * Support status for versioned software, shown as a chip beside the name.
 *
 * `eol` is the one that earns its place: a panel that lists PHP 7.4 and Node 14
 * without marking them is inviting someone to put an unpatched interpreter in
 * front of the internet. They stay listed because legacy apps are real and the
 * operator may have no choice — but not silently.
 */
export type AppTag = "current" | "active" | "lts" | "security-only" | "eol";

export interface CatalogApp {
  readonly name: string;
  readonly icon: string;
  readonly desc: string;
  /** "Installed", "Free", "Licence", or a price like "₹149/mo". */
  readonly badge: AppBadge;
  readonly tag?: AppTag;
}

export interface AppCategory {
  readonly key: string;
  readonly label: string;
  readonly icon: string;
  readonly sub: string;
  readonly apps: readonly CatalogApp[];
  readonly children?: readonly AppCategory[];
  /**
   * The constraint that governs the whole category — mutual exclusion, or a
   * limit on what the panel manages. Shown above the grid, because it applies
   * to every card and repeating it on each one would bury it.
   */
  readonly note?: string;
  readonly noteTone?: "info" | "warning";
}

const app = (name: string, icon: string, desc: string, badge: AppBadge, tag?: AppTag): CatalogApp => ({
  name,
  icon,
  desc,
  badge,
  ...(tag ? { tag } : {}),
});

export const APP_CATEGORIES: readonly AppCategory[] = [
  {
    key: "webservers",
    label: "Webservers",
    icon: "dns",
    sub: "The server that answers requests. Switching keeps your files untouched.",
    note:
      "These two are alternatives, not additions — LiteSpeed Enterprise is a drop-in replacement " +
      "that reads the same configuration, so a host runs one of them. Nginx, Apache and Caddy are " +
      "no longer offered: the panel no longer has a renderer for them.",
    apps: [
      app(
        "OpenLiteSpeed",
        "bolt",
        "The panel default, installed on every host. Event-driven, LiteSpeed Cache built in, .htaccess understood.",
        "Installed",
      ),
      app(
        "LiteSpeed Enterprise",
        "dns",
        "The same server under licence: LSAPI instead of FastCGI for PHP, ESI, and vendor support. Replaces OpenLiteSpeed in place.",
        "Licence",
      ),
    ],
  },
  {
    key: "scripting",
    label: "Server side scripting",
    icon: "code",
    sub: "Runtimes and tooling for the languages your sites run on.",
    apps: [],
    children: [
      {
        key: "php",
        label: "PHP",
        icon: "php",
        sub: "Versions, extensions and package tooling for PHP sites.",
        note:
          "Versions live side by side: each site picks one and gets its own FPM pool, so an old " +
          "app can stay on 7.4 while everything else runs 8.4. Extensions and OPcache are the " +
          "exception — those belong to the version's FPM master and change for every site on it.",
        apps: [
          app("PHP 8.5", "php", "Newest release. Ahead of what most plugins and frameworks have been tested against.", "Free", "current"),
          app("PHP 8.4", "php", "The panel default: a new PHP site runs here unless you choose otherwise.", "Installed", "active"),
          app("PHP 8.3", "php", "Still patched, and what a lot of production code was written for.", "Free", "security-only"),
          app("PHP 8.2", "php", "Security fixes only. Fine for an app you are not moving yet.", "Free", "security-only"),
          app("PHP 8.1", "php", "No upstream patches. Only for an app that has not been ported.", "Free", "eol"),
          app("PHP 8.0", "php", "No upstream patches. Only for an app that has not been ported.", "Free", "eol"),
          app("PHP 7.4", "php", "No upstream patches since 2022. Legacy apps only — do not start here.", "Free", "eol"),
          app("Composer", "inventory-2", "Dependency manager for PHP projects.", "Installed"),
          app("Imagick extension", "image", "Image processing for WordPress and Laravel.", "Free"),
          app("OPcache tuner", "speed", "Preload and tune the PHP bytecode cache.", "Free"),
        ],
      },
      {
        key: "node",
        label: "Node.js",
        icon: "javascript",
        sub: "Runtimes and process managers for Node apps.",
        note:
          "Even-numbered releases are the long-term ones; the odd ones are short-lived and are not " +
          "offered. An app site runs the version its Linux user finds on PATH, so a host can carry " +
          "several.",
        apps: [
          app("Node.js 24", "javascript", "The panel default for new app sites. Long-term support.", "Installed", "lts"),
          app("Node.js 22", "javascript", "The previous long-term line. Still patched.", "Free", "lts"),
          app("Node.js 20", "javascript", "Support ended. Move an app off it before the next dependency audit does it for you.", "Free", "eol"),
          app("Node.js 18", "javascript", "Support ended in 2025.", "Free", "eol"),
          app("Node.js 16", "javascript", "Support ended in 2023. Legacy apps only.", "Free", "eol"),
          app("Node.js 14", "javascript", "Support ended in 2023. Legacy apps only.", "Free", "eol"),
          app("PM2", "dashboard", "Cluster mode, auto-restart, log rotation.", "Installed"),
          app("pnpm", "inventory-2", "Faster installs with a shared store.", "Free"),
          app("Bun", "bolt", "Drop-in fast runtime for scripts and APIs.", "Free"),
        ],
      },
      {
        key: "python",
        label: "Python",
        icon: "terminal",
        sub: "Interpreters and WSGI servers for Python apps.",
        note:
          "Not part of the default stack — a host gets an interpreter when a Python site needs one. " +
          "Anything below 3.8 is missing from every current distribution and is not offered; " +
          "Python 2 is not offered at all.",
        apps: [
          app("Python 3.14", "terminal", "Newest release.", "Free", "current"),
          app("Python 3.13", "terminal", "A safe default for a new project.", "Free", "active"),
          app("Python 3.12", "terminal", "Widely tested against by frameworks and wheels.", "Free", "security-only"),
          app("Python 3.11", "terminal", "For a project pinned to it.", "Free", "security-only"),
          app("Python 3.10", "terminal", "Security fixes only.", "Free", "security-only"),
          app("Python 3.9", "terminal", "No upstream patches.", "Free", "eol"),
          app("Python 3.8", "terminal", "No upstream patches. Legacy apps only.", "Free", "eol"),
          app("Gunicorn", "settings-ethernet", "WSGI server for Django and Flask.", "Free"),
          app("uWSGI", "settings-ethernet", "Alternative WSGI server with fine tuning.", "Free"),
          app("Poetry", "inventory-2", "Dependency and virtualenv management.", "Free"),
        ],
      },
    ],
  },
  {
    key: "databases",
    label: "Databases",
    icon: "database",
    sub: "Engines and browsers for your data.",
    noteTone: "warning",
    note:
      "Two things to know before you install one. MariaDB and MySQL are the same seat — both want " +
      "port 3306 and /var/lib/mysql, so installing MySQL replaces MariaDB rather than joining it. " +
      "And the panel's Databases screen manages MariaDB only: PostgreSQL runs happily alongside on " +
      "5432, but creating and managing its databases is yours to do through pgAdmin.",
    apps: [
      app("MariaDB 11.4 LTS", "database", "The panel default. Site databases, backups and the phpMyAdmin hand-off are all built against it.", "Installed", "lts"),
      app("MariaDB 11.8 LTS", "database", "The newer long-term release.", "Free", "lts"),
      app("MariaDB 10.11 LTS", "database", "For a host that came from an older panel and is not ready to move.", "Free", "lts"),
      app("MySQL 8.4 LTS", "database", "Oracle's long-term release. Replaces MariaDB on this host; the panel drives it through the same tooling, untested.", "Free", "lts"),
      app("MySQL 8.0", "database", "Security fixes only. Same replacement rule as 8.4.", "Free", "security-only"),
      app("PostgreSQL 18", "database", "Newest release. Runs alongside MariaDB — the panel does not manage its databases.", "Free", "current"),
      app("PostgreSQL 17", "database", "A safe default for a new PostgreSQL app.", "Free"),
      app("PostgreSQL 16", "database", "For apps that need JSONB and strict types.", "Free"),
      app("PostgreSQL 15", "database", "For a project pinned to it.", "Free"),
      app("phpMyAdmin", "table-view", "Browser UI for MariaDB. The panel signs you in with a one-time ticket, so no database password is typed or stored in the browser.", "Installed"),
      app("pgAdmin 4", "table-view", "Browser UI for PostgreSQL — the way PostgreSQL is managed here, since the panel's own screen does not.", "Free"),
    ],
  },
  {
    key: "caches",
    label: "Caches",
    icon: "bolt",
    sub: "Object and page caching to cut response times.",
    apps: [
      app("Redis", "bolt", "Object cache and session store.", "Installed"),
      app("Memcached", "memory", "Simple key-value cache for PHP.", "Free"),
      app("Varnish", "speed", "Full-page HTTP cache in front of your site.", "Free"),
      app("LiteSpeed Cache", "speed", "Page cache built into the web server — there is nothing to install.", "Installed"),
    ],
  },
  {
    key: "queues",
    label: "Messages & queue",
    icon: "sync-alt",
    sub: "Background jobs and message passing between services.",
    apps: [
      app("RabbitMQ", "sync-alt", "Reliable AMQP broker with a management UI.", "Free"),
      app("BullMQ Dashboard", "view-timeline", "Inspect and retry Node job queues.", "Free"),
      app("Supervisor", "play-circle", "Keep PHP and Python workers alive.", "Free"),
      app("Mosquitto MQTT", "sensors", "Lightweight broker for IoT devices.", "Free"),
      app("Kafka Lite", "stream", "Event streaming for higher-volume pipelines.", "₹149/mo"),
    ],
  },
  {
    key: "security",
    label: "Security",
    icon: "shield",
    sub: "Add-ons that extend the panel Security section.",
    note:
      "The first five are the always-on baseline: every NexPanel host gets them and the setup " +
      "wizard does not offer to skip them. A fleet where some hosts have a firewall is worse than " +
      "either answer taken uniformly, because no statement about the fleet is true any more.",
    apps: [
      app("ModSecurity WAF", "verified-user", "OWASP core rule set at the webserver.", "Installed"),
      app("Fail2Ban", "gpp-maybe", "Ban IPs after repeated failed logins.", "Installed"),
      app("nftables", "security", "Host firewall. The panel writes the rules; the port it listens on is opened with a rollback timer.", "Installed"),
      app("ClamAV", "bug-report", "General antivirus signatures, scanned on demand and on a schedule.", "Installed"),
      app("maldet", "shield-lock", "Web shells and injected PHP, from shared-hosting intrusion data — the things a general antivirus corpus misses.", "Installed"),
      app("Malware Scanner Pro", "policy", "Scheduled scans and real-time protection.", "₹149/mo"),
      app("Wazuh Agent", "monitor-heart", "Host intrusion detection and file integrity.", "₹99/mo"),
    ],
  },
  {
    key: "other",
    label: "Other apps",
    icon: "apps",
    sub: "Everything else worth one click.",
    apps: [
      app("Grafana Lite", "monitoring", "Dashboards for CPU, memory and requests.", "Free"),
      app("Mailpit", "mail", "Catch and inspect outgoing mail.", "Free"),
      app("Uptime Monitor", "monitor-heart", "External checks every 60 seconds.", "Installed"),
      app("WP-CLI Console", "terminal", "Run WordPress commands in the browser.", "Free"),
      app("Backup Vault", "cloud-upload", "Off-site encrypted backups, 90 days.", "₹99/mo"),
      app("Edge CDN", "travel-explore", "Global caching and image optimisation.", "₹199/mo"),
    ],
  },
];

/** Flattens the tree to the leaves that actually carry apps. */
export function catalogLeaves(): readonly AppCategory[] {
  return APP_CATEGORIES.flatMap((c) => (c.children?.length ? c.children : [c]));
}

export const APP_STATS = [
  { label: "Installed", value: "6", sub: "5 running, 1 stopped" },
  { label: "Updates", value: "2", sub: "security patch for n8n" },
  { label: "Paid licenses", value: "2", sub: "1 renews in 24 days" },
  { label: "Resources used", value: "840 MB", sub: "across all apps" },
];

export interface InstalledApp {
  readonly name: string;
  readonly icon: string;
  readonly version: string;
  readonly state: "Running" | "Update ready" | "Stopped";
  readonly sub: string;
  readonly licensed?: boolean;
  /** Route name, when the app has a screen in the panel. */
  readonly to?: string;
}

export const INSTALLED_APPS: readonly InstalledApp[] = [
  { name: "OpenClaw", icon: "smart-toy", version: "2.4.0", state: "Running", sub: "Browser automation agents · port 7801", to: "openclaw" },
  { name: "n8n", icon: "account-tree", version: "1.62.1", state: "Update ready", sub: "Workflow automation · port 5678", to: "n8n" },
  { name: "phpMyAdmin", icon: "database", version: "5.2.1", state: "Running", sub: "MySQL browser for all databases" },
  { name: "Redis", icon: "bolt", version: "7.2", state: "Running", sub: "Object cache · 128 MB limit" },
  { name: "Malware Scanner Pro", icon: "bug-report", version: "3.1", state: "Running", sub: "Scheduled scans + real-time protection", licensed: true },
  { name: "Uptime Monitor", icon: "monitor-heart", version: "1.8", state: "Stopped", sub: "External checks every 60s" },
];

export interface License {
  readonly name: string;
  readonly key: string;
  readonly seats: string;
  readonly state: "Active" | "Expiring" | "Not licensed";
  readonly renew: string;
}

export const LICENSES: readonly License[] = [
  { name: "Malware Scanner Pro", key: "MSP-4F2A-9C81-••••", seats: "1 server", state: "Active", renew: "renews 7 Sep 2026" },
  { name: "Backup Vault", key: "BVT-7B10-2E44-••••", seats: "200 GB", state: "Expiring", renew: "renews in 24 days" },
  { name: "Edge CDN", key: "—", seats: "—", state: "Not licensed", renew: "trial available" },
];
