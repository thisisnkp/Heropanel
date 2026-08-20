/**
 * The app marketplace, from the design.
 *
 * Categories are nested one level deep (Server side scripting → PHP / Node.js /
 * Python) because that is how the design grouped them and because a flat list
 * of forty entries is not browsable. The nesting lives in the data, so the
 * catalogue screen renders a tree it is given rather than hard-coding which
 * category happens to have children.
 */

export type AppBadge = "Installed" | "Free" | string;

export interface CatalogApp {
  readonly name: string;
  readonly icon: string;
  readonly desc: string;
  /** "Installed", "Free", or a price like "₹149/mo". */
  readonly badge: AppBadge;
}

export interface AppCategory {
  readonly key: string;
  readonly label: string;
  readonly icon: string;
  readonly sub: string;
  readonly apps: readonly CatalogApp[];
  readonly children?: readonly AppCategory[];
}

const app = (name: string, icon: string, desc: string, badge: AppBadge): CatalogApp => ({ name, icon, desc, badge });

export const APP_CATEGORIES: readonly AppCategory[] = [
  {
    key: "webservers",
    label: "Webservers",
    icon: "dns",
    sub: "The server that answers requests. Switching keeps your files untouched.",
    apps: [
      app("Nginx", "dns", "Fast reverse proxy and static file server. Panel default.", "Installed"),
      app("OpenLiteSpeed", "bolt", "Built-in cache, excellent for WordPress.", "Free"),
      app("Apache", "lan", "Widest .htaccess compatibility for legacy PHP apps.", "Free"),
      app("Caddy", "https", "Automatic HTTPS with the simplest config.", "Free"),
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
        apps: [
          app("PHP 8.3", "php", "Current stable. Recommended for new sites.", "Installed"),
          app("PHP 8.2", "php", "Keep alongside 8.3 for older plugins.", "Free"),
          app("PHP 7.4", "php", "Legacy only — no security patches upstream.", "Free"),
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
        apps: [
          app("Node.js 20 LTS", "javascript", "Long-term support. Panel default.", "Installed"),
          app("Node.js 22", "javascript", "Latest features, shorter support window.", "Free"),
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
        apps: [
          app("Python 3.12", "terminal", "Current stable interpreter.", "Installed"),
          app("Python 3.11", "terminal", "For projects pinned to an older release.", "Free"),
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
    apps: [
      app("MariaDB 11", "database", "MySQL-compatible, panel default.", "Installed"),
      app("PostgreSQL 16", "database", "For apps that need JSONB and strict types.", "Free"),
      app("MongoDB 7", "database", "Document store for Node projects.", "Free"),
      app("phpMyAdmin", "table-view", "Browser UI for MySQL and MariaDB.", "Installed"),
      app("pgAdmin", "table-view", "Browser UI for PostgreSQL.", "Free"),
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
      app("LiteSpeed Cache", "speed", "Page cache for WordPress on OpenLiteSpeed.", "Free"),
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
    apps: [
      app("ModSecurity WAF", "verified-user", "OWASP core rule set at the webserver.", "Installed"),
      app("Fail2Ban", "gpp-maybe", "Ban IPs after repeated failed logins.", "Installed"),
      app("ClamAV", "bug-report", "Open-source malware signatures.", "Free"),
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
