/**
 * Fixtures for the account and system screens: API tokens, temporary access,
 * containers, compose stacks, automation products, and the simple table pages
 * (domains, backups, panel settings).
 */

export interface ApiToken {
  readonly name: string;
  readonly key: string;
  readonly scope: string;
  readonly used: string;
  readonly state: "Active" | "Idle";
}

export interface Webhook {
  readonly url: string;
  readonly events: string;
  readonly state: string;
}

export const API_STATS = [
  { label: "Active tokens", value: "3", sub: "1 read-only" },
  { label: "Calls (24h)", value: "4,812", sub: "of 20k daily limit" },
  { label: "Webhooks", value: "2", sub: "both delivering" },
  { label: "Errors (24h)", value: "6", sub: "4× 401, 2× 429" },
];

export const API_TOKENS: readonly ApiToken[] = [
  { name: "CI deploy runner", key: "nxp_live_••••4f21", scope: "sites:write, deploy", used: "11 min ago", state: "Active" },
  { name: "Status page", key: "nxp_read_••••9b71", scope: "sites:read", used: "2 hours ago", state: "Active" },
  { name: "Old backup script", key: "nxp_live_••••c02d", scope: "backups:write", used: "3 months ago", state: "Idle" },
];

export const WEBHOOKS: readonly Webhook[] = [
  { url: "https://hooks.novaretail.in/nexpanel", events: "deploy.succeeded, deploy.failed", state: "Delivering" },
  { url: "https://n8n.novaretail.in/webhook/nexp", events: "security.alert", state: "Delivering" },
];

export const API_EXAMPLE = [
  "curl -X POST \\",
  "  https://api.nexpanel.app/v1/sites/4/deploy \\",
  '  -H "Authorization: Bearer nxp_live_…" \\',
  "  -d '{\"branch\":\"main\"}'",
];

// ---- temporary access ------------------------------------------------------

export interface Grant {
  readonly who: string;
  readonly scope: string;
  readonly expires: string;
  readonly state: "Active" | "Pending" | "Expired";
  readonly action: string;
}

export const TEMP_STATS = [
  { label: "Active grants", value: "1", sub: "expires in 3h 12m" },
  { label: "Pending", value: "1", sub: "awaiting accept" },
  { label: "Grants (30d)", value: "4", sub: "all auto-expired" },
  { label: "Default length", value: "8 hours", sub: "never longer than 7 days" },
];

export const GRANTS: readonly Grant[] = [
  { who: "support@nexpanel.app", scope: "Full panel, no billing", expires: "in 3h 12m", state: "Active", action: "Revoke" },
  { who: "dev@brightlabs.dev", scope: "api.novaretail.in only", expires: "invite valid 24h", state: "Pending", action: "Cancel" },
  { who: "priya@novaretail.in", scope: "Files + logs", expires: "expired 2 days ago", state: "Expired", action: "Renew" },
];

// ---- containers ------------------------------------------------------------

export interface Container {
  readonly name: string;
  readonly image: string;
  readonly ports: string;
  readonly cpu: string;
  readonly mem: string;
  readonly state: "Running" | "Stopped";
}

export const DOCKER_STATS = [
  { label: "Containers", value: "5", sub: "4 running, 1 stopped" },
  { label: "Images", value: "8", sub: "3.4 GB on disk" },
  { label: "Volumes", value: "6", sub: "1 unused" },
  { label: "Engine", value: "27.1", sub: "rootless mode" },
];

export const CONTAINERS: readonly Container[] = [
  { name: "nova-api", image: "node:20-alpine", ports: "3000 → 3000", cpu: "12%", mem: "210 MB", state: "Running" },
  { name: "redis-cache", image: "redis:7-alpine", ports: "6379 (internal)", cpu: "3%", mem: "64 MB", state: "Running" },
  { name: "pg-analytics", image: "postgres:16", ports: "5432 (internal)", cpu: "6%", mem: "320 MB", state: "Running" },
  { name: "n8n", image: "n8nio/n8n:1.62", ports: "5678 → 443", cpu: "4%", mem: "180 MB", state: "Running" },
  { name: "mailpit", image: "axllent/mailpit", ports: "8025", cpu: "—", mem: "—", state: "Stopped" },
];

export const IMAGES = [
  { name: "node:20-alpine", size: "142 MB", used: "nova-api", age: "2 weeks ago" },
  { name: "postgres:16", size: "412 MB", used: "pg-analytics", age: "3 weeks ago" },
  { name: "n8nio/n8n:1.62", size: "680 MB", used: "n8n", age: "6 days ago" },
  { name: "redis:7-alpine", size: "38 MB", used: "redis-cache", age: "2 months ago" },
  { name: "python:3.12-slim", size: "128 MB", used: "unused", age: "2 months ago" },
];

// ---- compose ---------------------------------------------------------------

export interface ComposeStack {
  readonly name: string;
  readonly path: string;
  readonly services: string;
  readonly state: "Running" | "Stopped";
  readonly action: string;
}

export const COMPOSE_STACKS: readonly ComposeStack[] = [
  { name: "novaretail-stack", path: "/home/nexp/compose/novaretail", services: "4 services", state: "Running", action: "Down" },
  { name: "analytics", path: "/home/nexp/compose/analytics", services: "3 services", state: "Running", action: "Down" },
  { name: "staging-api", path: "/home/nexp/compose/staging", services: "2 services", state: "Stopped", action: "Up" },
];

export const COMPOSE_YAML = [
  "services:",
  "  api:",
  "    image: node:20-alpine",
  "    command: npm run start",
  '    ports: ["3000:3000"]',
  "    env_file: .env",
  "  cache:",
  "    image: redis:7-alpine",
  '    volumes: ["redis-data:/data"]',
  "",
  "volumes:",
  "  redis-data:",
];

// ---- automation ------------------------------------------------------------

export interface AutomationRow {
  readonly name: string;
  readonly meta: string;
  readonly state: "active" | "paused" | "draft";
}

export interface AutomationProduct {
  readonly name: string;
  readonly tag: string;
  readonly tone: "info" | "danger";
  readonly cta: string;
  readonly sub: string;
  readonly stats: readonly { label: string; value: string }[];
  readonly listTitle: string;
  readonly rows: readonly AutomationRow[];
}

export const AUTOMATION: Readonly<Record<string, AutomationProduct>> = {
  openclaw: {
    name: "OpenClaw",
    tag: "OC",
    tone: "info",
    cta: "Open OpenClaw",
    sub: "Browser-automation agents, built into your panel. No install, no server to rent.",
    stats: [
      { label: "Agents", value: "4" },
      { label: "Runs today", value: "128" },
      { label: "Success rate", value: "97%" },
    ],
    listTitle: "Agents",
    rows: [
      { name: "Competitor price watch", meta: "every 6h", state: "active" },
      { name: "Order status scraper", meta: "every 30m", state: "active" },
      { name: "Lead form filler", meta: "on webhook", state: "active" },
      { name: "Weekly SEO audit", meta: "Mon 07:00", state: "paused" },
    ],
  },
  n8n: {
    name: "n8n",
    tag: "N8",
    tone: "danger",
    cta: "Open n8n editor",
    sub: "Connect your sites to 400+ services with visual workflows. Runs on your plan, no extra cost.",
    stats: [
      { label: "Workflows", value: "11" },
      { label: "Executions (24h)", value: "2,430" },
      { label: "Failed", value: "3" },
    ],
    listTitle: "Workflows",
    rows: [
      { name: "New order → WhatsApp + Sheets", meta: "webhook", state: "active" },
      { name: "Failed deploy → Slack alert", meta: "panel event", state: "active" },
      { name: "Nightly DB → S3 export", meta: "cron 02:00", state: "active" },
      { name: "Refund flow (draft)", meta: "manual", state: "draft" },
    ],
  },
};

// ---- simple table pages ----------------------------------------------------

export interface TablePage {
  readonly title: string;
  readonly sub: string;
  readonly action: string;
  readonly columns: readonly [string, string, string];
  /** Rows whose first column is an identifier get the monospace treatment. */
  readonly mono: boolean;
  readonly rows: readonly { a: string; b: string; c: string }[];
}

export const TABLE_PAGES: Readonly<Record<string, TablePage>> = {
  domains: {
    title: "Domains",
    sub: "DNS, SSL and redirects for every domain you point here.",
    action: "Add domain",
    columns: ["Domain", "Points to", "SSL"],
    mono: true,
    rows: [
      { a: "novaretail.in", b: "novaretail.in", c: "Active" },
      { a: "www.novaretail.in", b: "redirect → apex", c: "Active" },
      { a: "api.novaretail.in", b: "api.novaretail.in", c: "Active" },
      { a: "brightlabs.dev", b: "brightlabs.dev", c: "Active" },
    ],
  },
  backups: {
    title: "Backups",
    sub: "Daily snapshots of files and databases, kept for 14 days.",
    action: "Back up now",
    columns: ["Snapshot", "Site", "Size"],
    mono: false,
    rows: [
      { a: "14 Aug 2026, 02:00", b: "novaretail.in", c: "512 MB" },
      { a: "13 Aug 2026, 02:00", b: "novaretail.in", c: "509 MB" },
      { a: "14 Aug 2026, 02:10", b: "api.novaretail.in", c: "88 MB" },
      { a: "14 Aug 2026, 02:20", b: "billing-portal.co", c: "141 MB" },
    ],
  },
  settings: {
    title: "Panel settings",
    sub: "Account, security and team access.",
    action: "Invite member",
    columns: ["Item", "Value", "Status"],
    mono: false,
    rows: [
      { a: "Two-factor auth", b: "Authenticator app", c: "Enabled" },
      { a: "SSH keys", b: "2 keys", c: "Active" },
      { a: "Team members", b: "3 seats used", c: "OK" },
      { a: "API token", b: "nxp_live_••••4f21", c: "Active" },
    ],
  },
};

// ---- billing ---------------------------------------------------------------

export const PLAN = {
  name: "Business",
  price: "₹399",
  period: "/mo",
  renews: "renews 12 Sep 2026",
  includes: "100 websites · 200 GB NVMe · free SSL · daily backups · unlimited automations",
};

export const USAGE = [
  { label: "Websites", text: "5 / 100", pct: 5 },
  { label: "Storage", text: "38 / 200 GB", pct: 19 },
  { label: "Bandwidth", text: "146 / 2000 GB", pct: 7 },
  { label: "Automation runs", text: "18.4k / unlimited", pct: 34 },
];
