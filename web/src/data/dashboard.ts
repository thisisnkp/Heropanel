/**
 * Home screen fixtures, transcribed from the design's state model.
 *
 * The design computed tone/icon inline in each row; here the row carries the
 * *fact* (a check passed, a severity) and the component maps that to a colour.
 * A fixture that hard-codes `var(--nx-danger)` cannot be swapped for an API
 * response without the API also returning CSS.
 */

export type Severity = "critical" | "warning" | "info";

export interface AttentionItem {
  readonly id: string;
  readonly label: string;
  readonly sub: string;
  readonly severity: Severity;
  readonly action: string;
  /** Route name to send the user to. */
  readonly to: string;
}

export interface ActivityItem {
  readonly icon: string;
  readonly text: string;
  readonly when: string;
}

export interface ProtectionCheck {
  readonly label: string;
  readonly ok: boolean;
  readonly to: string;
}

export interface ScanTile {
  readonly icon: string;
  readonly label: string;
  readonly value: string;
  readonly sub: string;
  readonly tone: "success" | "warning" | "danger";
  readonly status?: string;
  readonly to: string;
}

export interface QuickAction {
  readonly icon: string;
  readonly label: string;
  readonly sub: string;
  readonly to: string;
}

export const ATTENTION: readonly AttentionItem[] = [
  { id: "ssh", label: "Root SSH login is enabled", sub: "Critical · disable it, keys keep working", severity: "critical", action: "Fix", to: "security-ssh" },
  { id: "updates", label: "3 security updates pending", sub: "Warning · openssl marked urgent", severity: "warning", action: "Install", to: "security-updates" },
  { id: "building", label: "queue.novaretail.in is still building", sub: "Started 2 minutes ago", severity: "warning", action: "View", to: "websites" },
];

export const ACTIVITY: readonly ActivityItem[] = [
  { icon: "cloud-sync", text: "api.novaretail.in deployed main@4f2a1c9", when: "18 min ago" },
  { icon: "shield", text: "Fail2Ban banned 45.148.10.72", when: "1 hour ago" },
  { icon: "smart-toy", text: 'OpenClaw agent "Price watch" finished 12 runs', when: "2 hours ago" },
  { icon: "backup", text: "Nightly backup completed · 512 MB", when: "today 02:00" },
  { icon: "system-update-alt", text: "openssl 3.0.14 security update available", when: "today 04:10" },
];

export const PROTECTION_CHECKS: readonly ProtectionCheck[] = [
  { label: "Firewall", ok: true, to: "security-firewall" },
  { label: "WAF", ok: true, to: "security-waf" },
  { label: "SSH", ok: false, to: "security-ssh" },
  { label: "2FA", ok: true, to: "security-login" },
];

export const PROTECTION_SCORE = 94;

/**
 * The grade bands the design used. Kept as a function so the thresholds are
 * stated once — the design repeated `94 < 70 ? … : 94 < 85 ? …` three times,
 * once per visual property, which is three chances to disagree.
 */
export function protectionGrade(score: number): { label: string; tone: "success" | "warning" | "danger" } {
  if (score < 70) return { label: "At risk", tone: "danger" };
  if (score < 85) return { label: "Needs work", tone: "warning" };
  return { label: "Protected", tone: "success" };
}

export const CPU_SERIES = [38, 42, 35, 50, 61, 47, 44, 58, 72, 64, 55, 49, 53, 66, 71, 58, 46, 41, 44, 52, 63, 57, 48, 43];
export const RAM_SERIES = [54, 56, 55, 58, 60, 59, 62, 64, 63, 66, 68, 67, 65, 69, 71, 70, 68, 66, 64, 63, 65, 67, 66, 64];

export const BANDWIDTH = { used: "146 GB", total: "2,000 GB", pct: 7.3, note: "1–15 Aug · 7.3% of monthly allowance" };
export const STORAGE = { used: "38 GB", total: "200 GB", pct: 19, note: "sites 24 GB · databases 9 GB · backups 5 GB" };

export const SCAN_TILES: readonly ScanTile[] = [
  { icon: "bug-report", label: "Malware detected", value: "2", sub: "1 quarantined, 1 pending review", tone: "danger", status: "Needs your attention", to: "security-malware" },
  { icon: "verified-user", label: "Attacks stopped", value: "1,842", sub: "WAF + firewall · 7 days", tone: "success", to: "security-waf" },
  { icon: "lock", label: "Failed logins blocked", value: "47", sub: "4 IPs banned · 24h", tone: "warning", to: "security-login" },
  { icon: "verified", label: "SSL certificates", value: "4 valid", sub: "all auto-renewing", tone: "success", to: "security-overview" },
];

export const LAST_SCAN = "Last full scan 6 days ago · 184,201 files";

export const QUICK_ACTIONS: readonly QuickAction[] = [
  { icon: "add-circle", label: "Add a website", sub: "Static, PHP, Node or WordPress", to: "websites" },
  { icon: "apps", label: "Install an app", sub: "Redis, n8n, PostgreSQL…", to: "apps-install" },
  { icon: "shield", label: "Security overview", sub: "Score 82 · 1 critical", to: "security-overview" },
  { icon: "mail", label: "Mailboxes", sub: "4 accounts · 3.2 GB used", to: "mail" },
];

export const AUTOMATION_TILES = [
  { name: "OpenClaw", tag: "OC", tone: "info" as const, sub: "4 agents · 128 runs today", to: "openclaw" },
  { name: "n8n", tag: "N8", tone: "danger" as const, sub: "11 workflows · 2,430 executions", to: "n8n" },
];

// ---- notifications ---------------------------------------------------------

/**
 * The alerts worth interrupting someone for, from the mobile design.
 *
 * Deliberately a different list from `ACTIVITY`: activity is everything that
 * happened, notifications are the subset that needs a decision. Merging them
 * would mean either burying the backup failure among routine deploys, or
 * pinging someone every time a cron job succeeds.
 */
export interface Notification {
  readonly icon: string;
  readonly label: string;
  readonly sub: string;
  readonly when: string;
  readonly severity: "critical" | "warning";
}

export const NOTIFICATIONS: readonly Notification[] = [
  { icon: "error", label: "Backup failed", sub: "billing-portal.co · destination full", when: "4 hours ago", severity: "critical" },
  { icon: "bug-report", label: "Malware detected", sub: "2 files on novaretail.in quarantined", when: "6 days ago", severity: "critical" },
  { icon: "gpp-maybe", label: "Security vulnerability", sub: "openssl 3.0.13 · patch available", when: "today 04:10", severity: "warning" },
  { icon: "cloud-sync", label: "Deployment failed", sub: "novaretail.in · build error in theme", when: "5 days ago", severity: "critical" },
  { icon: "memory", label: "CPU high", sub: "nexp-mum-01 hit 94% for 6 minutes", when: "yesterday", severity: "warning" },
];

export const NOTIFY_ABOUT =
  "Server or site down, SSL expiring, failed backups, malware, security vulnerabilities, failed deployments, disk nearly full, and critical CPU or memory. Routine events stay in Activity.";
