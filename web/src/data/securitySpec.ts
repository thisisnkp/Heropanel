/**
 * Server-wide security screens, transcribed from the design.
 *
 * These read live flag state — "Firewall: Active" has to say Off the moment
 * someone turns the switch off two panels down the same page, or the stat row
 * is decoration rather than status.
 */
import { row, type Spec, type SpecLogLine } from "@/data/spec";
import type { useFlagsStore } from "@/stores/flags";

export type SecurityKey =
  | "overview" | "firewall" | "waf" | "malware" | "ssh" | "updates" | "login" | "logs" | "settings";

type Flags = ReturnType<typeof useFlagsStore>;

export const WAF_LEVELS = ["Basic", "Advanced"] as const;
export const SECURITY_PROFILES = ["Lite", "Standard", "Maximum"] as const;

export const LOG_SOURCES = [
  "Authentication", "Firewall", "WAF", "File changes", "Security events", "Audit trail",
] as const;

export type LogSource = (typeof LOG_SOURCES)[number];

const LOG_NAMES: Record<LogSource, string> = {
  Authentication: "auth/panel-auth.log",
  Firewall: "firewall/nftables.log",
  WAF: "waf/modsec-audit.log",
  "File changes": "integrity/file-changes.log",
  "Security events": "events/security.log",
  "Audit trail": "audit/panel-actions.log",
};

const LOG_LINES: Record<LogSource, readonly SpecLogLine[]> = {
  Authentication: [
    { time: "12:04:11", text: "login success user=aarav ip=49.36.12.5 2fa=totp", tone: "success" },
    { time: "11:58:02", text: "login failed user=admin ip=45.148.10.72", tone: "warning" },
    { time: "11:57:51", text: "login failed user=admin ip=45.148.10.72", tone: "warning" },
    { time: "11:40:19", text: "session revoked id=sess_9b71 by=aarav" },
    { time: "10:12:44", text: "2fa enrolled user=priya method=totp", tone: "success" },
  ],
  Firewall: [
    { time: "12:04:02", text: "DROP src=45.148.10.72 dpt=22 proto=tcp", tone: "warning" },
    { time: "12:06:10", text: "DROP src=185.220.101.4 dpt=3306 proto=tcp", tone: "warning" },
    { time: "12:09:31", text: "rule updated 22/tcp source=allowlist by=aarav", tone: "success" },
  ],
  WAF: [
    { time: "12:01:08", text: "block rule=942100 sqli uri=/index.php?id=1' site=novaretail.in", tone: "warning" },
    { time: "11:52:37", text: "block rule=941110 xss uri=/search site=brightlabs.dev", tone: "warning" },
    { time: "11:31:02", text: "challenge bot-score=88 uri=/api/orders" },
  ],
  "File changes": [
    { time: "12:00:12", text: "modified /novaretail.in/public/wp-config.php", tone: "warning" },
    { time: "09:41:55", text: "created /api.novaretail.in/.env.backup" },
    { time: "08:22:03", text: "deleted /brightlabs.dev/public/old-index.html" },
  ],
  "Security events": [
    { time: "12:02:24", text: "fail2ban banned 45.148.10.72 (5 attempts)", tone: "success" },
    { time: "06:00:00", text: "malware scan completed threats=2", tone: "warning" },
    { time: "04:00:00", text: "unattended-upgrade installed openssl 3.0.14", tone: "success" },
  ],
  "Audit trail": [
    { time: "12:09:31", text: "aarav updated firewall rule 22/tcp" },
    { time: "11:20:14", text: "aarav deployed api.novaretail.in main@4f2a1c9" },
    { time: "10:02:41", text: "priya created database nexp_queue" },
  ],
};

/** The nine sections, in the order the sidebar lists them. */
export const SECURITY_SECTIONS: readonly { key: SecurityKey; label: string; icon: string }[] = [
  { key: "overview", label: "Security overview", icon: "shield" },
  { key: "firewall", label: "Firewall", icon: "local-fire-department" },
  { key: "waf", label: "WAF", icon: "verified-user" },
  { key: "malware", label: "Malware scanner", icon: "bug-report" },
  { key: "ssh", label: "SSH security", icon: "terminal" },
  { key: "updates", label: "Security updates", icon: "system-update-alt" },
  { key: "login", label: "Login protection", icon: "lock" },
  { key: "logs", label: "Security logs", icon: "receipt-long" },
  { key: "settings", label: "Security settings", icon: "tune" },
];

export interface SecurityIssue {
  readonly severity: "critical" | "warning";
  readonly label: string;
  readonly key: SecurityKey;
}

/**
 * What is currently wrong, as a list rather than a count.
 *
 * The design's sidebar shows fixed "1 critical / 3 warnings" chips. Derived
 * numbers are the only ones worth showing: a chip that reads "1 critical" while
 * every switch on the page is green is worse than no chip, and turning root
 * login on has to make the count move. Returning the issues rather than totals
 * means the chip can say which ones without a second source of truth.
 */
export function securityIssues(flags: Flags): readonly SecurityIssue[] {
  const issues: SecurityIssue[] = [];

  if (flags.isOn("rootLogin")) {
    issues.push({ severity: "critical", label: "Root SSH login is enabled", key: "ssh" });
  }
  if (flags.isOn("passwordLogin")) {
    issues.push({ severity: "critical", label: "SSH password login is enabled", key: "ssh" });
  }
  if (!flags.isOn("fw")) issues.push({ severity: "critical", label: "Firewall is off", key: "firewall" });

  if (!flags.isOn("waf")) issues.push({ severity: "warning", label: "WAF is off", key: "waf" });
  if (!flags.isOn("fail2ban")) issues.push({ severity: "warning", label: "Fail2Ban is off", key: "login" });
  if (!flags.isOn("twofa")) issues.push({ severity: "warning", label: "Two-factor is not enforced", key: "login" });
  if (!flags.isOn("schedScan")) issues.push({ severity: "warning", label: "No scheduled malware scan", key: "malware" });
  if (!flags.isOn("realtime")) issues.push({ severity: "warning", label: "Real-time protection is off", key: "malware" });

  return issues;
}

/** The one-line summary under the profile name, in the sidebar footer. */
export function profileNote(profile: string): string {
  switch (profile) {
    case "Lite":
      return "Lite — the minimum that still blocks the obvious, for low-traffic sites.";
    case "Maximum":
      return "Maximum — strictest rules and shortest sessions, applied to every website.";
    default:
      return "Standard — balanced defaults, applied to every website.";
  }
}

export interface SecurityContext {
  readonly flags: Flags;
  readonly wafLevel: string;
  readonly profile: string;
  readonly logSource: LogSource;
  readonly scanning: boolean;
}

export function buildSecuritySpec(key: SecurityKey, ctx: SecurityContext): Spec {
  const { flags } = ctx;

  switch (key) {
    case "overview":
      return {
        kicker: "Security",
        title: "Security overview",
        sub: "One score, the checks behind it, and the fixes worth doing today.",
        hero: {
          score: 82,
          grade: "Good",
          note: "1 critical and 3 warnings are pulling this down.",
          critical: "1",
          warning: "3",
          healthy: "24",
        },
        stats: [
          { label: "Firewall", value: flags.isOn("fw") ? "Active" : "Off", sub: "18 rules · 6 ports open" },
          { label: "WAF", value: flags.isOn("waf") ? ctx.wafLevel : "Off", sub: "1,284 requests blocked (7d)" },
          {
            label: "SSH",
            value: flags.isOn("rootLogin") ? "Root login on" : "Hardened",
            sub: "keys only · Fail2Ban " + flags.label("fail2ban").toLowerCase(),
          },
          { label: "Failed logins", value: "47", sub: "last 24 hours · 4 IPs blocked" },
        ],
        quickFixes: [
          { label: "Disable root SSH login", sub: "Critical · one click, keys keep working", severity: "critical", to: "security-ssh" },
          { label: "Install 3 pending security updates", sub: "Warning · OS 2, PHP 1", severity: "warning", to: "security-updates" },
          { label: "Require 2FA for all team members", sub: "Warning · 1 of 3 members without 2FA", severity: "warning", to: "security-login" },
          { label: "Run a malware scan", sub: "Warning · last scan 6 days ago", severity: "warning", to: "security-malware" },
        ],
      };

    case "firewall":
      return {
        kicker: "Security",
        title: "Firewall",
        sub: "What can reach your server, and what gets dropped at the edge.",
        stats: [
          { label: "Open ports", value: "6", sub: "22, 80, 443, 3306, 6379, 3000" },
          { label: "Blocked IPs", value: "128", sub: "11 added today" },
          { label: "Allowed IPs", value: "4", sub: "office + 2 team members" },
          { label: "Dropped (24h)", value: "9.4k", sub: "packets rejected" },
        ],
        toggleTitle: "Firewall",
        toggles: [
          { label: "Firewall enabled", sub: "Default policy: deny incoming, allow outgoing", flag: "fw" },
          { label: "Auto-block repeat offenders", sub: "Block an IP for 24h after 10 rejected attempts", flag: "autoBlock" },
        ],
        sideTitle: "Add a rule",
        sideNote: "Open a port for everyone, or only for the IPs you trust. Changes apply instantly and are logged.",
        sideActions: [{ label: "Add port rule", primary: true }, { label: "Add IP to allowlist" }],
        table1: {
          title: "Port rules",
          action: "Add rule",
          columns: ["Port", "Protocol", "Source"],
          rows: [
            row("22 · SSH", "TCP", "allowlist only", "Edit", "Remove"),
            row("80 · HTTP", "TCP", "anywhere", "Edit", "Remove"),
            row("443 · HTTPS", "TCP", "anywhere", "Edit", "Remove"),
            row("3306 · MySQL", "TCP", "localhost", "Edit", "Remove"),
            row("6379 · Redis", "TCP", "localhost", "Edit", "Remove"),
          ],
        },
        table2: {
          title: "Blocked IPs",
          action: "Block an IP",
          columns: ["IP address", "Reason", "Blocked"],
          rows: [
            row("45.148.10.72", "SSH brute force", "18 min ago", "Unblock"),
            row("185.220.101.4", "WAF · SQL injection", "2 hours ago", "Unblock"),
            row("103.76.44.19", "Rate limit exceeded", "5 hours ago", "Unblock"),
            row("91.219.236.8", "Manual block", "yesterday", "Unblock"),
          ],
        },
        logs: [
          { time: "12:04:02", text: "DROP in=eth0 src=45.148.10.72 dpt=22", tone: "warning" },
          { time: "12:04:44", text: "ACCEPT in=eth0 src=49.36.12.5 dpt=443" },
          { time: "12:06:10", text: "DROP in=eth0 src=185.220.101.4 dpt=3306", tone: "warning" },
          { time: "12:09:31", text: "rule updated: 22/tcp source=allowlist", tone: "success" },
        ],
        logName: "firewall/nftables.log",
      };

    case "waf":
      return {
        kicker: "Security",
        title: "Web application firewall",
        sub: "Blocks injection, XSS and bot traffic before it reaches your sites.",
        stats: [
          { label: "Status", value: flags.isOn("waf") ? "Protecting" : "Off", sub: "all 5 websites" },
          { label: "Blocked (7d)", value: "1,284", sub: "0.4% of all requests" },
          { label: "OWASP rules", value: "942", sub: "core rule set 4.3" },
          { label: "False positives", value: "2", sub: "review suggested" },
        ],
        toggleTitle: "WAF",
        toggles: [
          { label: "WAF enabled", sub: "Applies to every website on this account", flag: "waf" },
          { label: "Auto-quarantine hostile requests", sub: "Store blocked payloads for 7 days for review", flag: "autoQuarantine" },
        ],
        choiceTitle: "Protection level",
        choiceDefault: ctx.wafLevel,
        choices: [
          { label: "Basic", sub: "Blocks known attacks only. Zero false positives." },
          { label: "Advanced", sub: "Full OWASP set + bot scoring. Recommended." },
        ],
        table1: {
          title: "OWASP rule groups",
          action: "Tune rules",
          columns: ["Group", "Status", "Blocked (7d)"],
          rows: [
            row("SQL injection", "Enabled", "612", "Disable"),
            row("Cross-site scripting", "Enabled", "318", "Disable"),
            row("Remote code execution", "Enabled", "164", "Disable"),
            row("Scanner / bot detection", ctx.wafLevel === "Advanced" ? "Enabled" : "Off", "190", "Disable"),
            row("Protocol violations", "Log only", "0", "Enable"),
          ],
        },
        table2: {
          title: "Custom rules",
          action: "Add custom rule",
          columns: ["Rule", "Match", "Action"],
          rows: [
            row("Block /wp-login.php outside India", "geo != IN", "Block", "Edit", "Remove"),
            row("Rate limit /api/*", "60 req/min per IP", "Challenge", "Edit", "Remove"),
            row("Allow uptime monitor", "ua = NexPing", "Allow", "Edit", "Remove"),
          ],
        },
      };

    case "malware":
      return {
        kicker: "Security",
        title: "Malware scanner",
        sub: "Signature and heuristic scan across every document root.",
        stats: [
          { label: "Last scan", value: ctx.scanning ? "Running…" : "6 days ago", sub: "184,201 files checked" },
          { label: "Threats found", value: "2", sub: "1 quarantined, 1 pending" },
          { label: "Quarantine", value: "3", sub: "files isolated" },
          { label: "Clean sites", value: "4 of 5", sub: "novaretail.in needs review" },
        ],
        sideTitle: "Scan now",
        sideNote:
          "A full scan takes about 4 minutes and does not slow your sites. Threats are isolated, never deleted without you.",
        sideActions: [
          { label: ctx.scanning ? "Scanning…" : "Run full scan", primary: true },
          { label: "Scan a single site" },
        ],
        toggleTitle: "Automation",
        toggles: [
          { label: "Scheduled daily scan", sub: "Paid add-on · runs at 03:00, alerts on findings", flag: "schedScan", paid: true },
          { label: "Real-time protection", sub: "Paid add-on · blocks malicious writes as they happen", flag: "realtime", paid: true },
        ],
        table1: {
          title: "Detected threats",
          action: "Review all",
          columns: ["File", "Signature", "Site"],
          rows: [
            row("/novaretail.in/public/wp-content/uploads/x.php", "PHP.Backdoor.Web-Shell", "novaretail.in", "Quarantine", "Ignore"),
            row("/billing-portal.co/public/vendor/old.js", "JS.Injected.Redirect", "billing-portal.co", "Quarantine", "Ignore"),
          ],
        },
        table2: {
          title: "Quarantine",
          action: "Empty quarantine",
          columns: ["File", "Isolated", "Size"],
          rows: [
            row("uploads/2026/07/theme.php", "6 days ago", "14 KB", "Restore", "Delete"),
            row("cache/ads-loader.js", "2 weeks ago", "3 KB", "Restore", "Delete"),
            row("tmp/.sess_ab12", "3 weeks ago", "1 KB", "Restore", "Delete"),
          ],
        },
      };

    case "ssh":
      return {
        kicker: "Security",
        title: "SSH security",
        sub: "Keys, ports and brute-force defence for shell access.",
        stats: [
          { label: "SSH", value: flags.label("ssh") === "On" ? "Enabled" : "Disabled", sub: "port 2202" },
          { label: "Keys", value: "2", sub: "both used in last 7 days" },
          { label: "Brute-force attempts", value: "312", sub: "last 24 hours" },
          { label: "Fail2Ban bans", value: "18", sub: "active bans" },
        ],
        toggleTitle: "Access rules",
        toggles: [
          { label: "SSH enabled", sub: "Turn off entirely if you only use the file manager", flag: "ssh" },
          { label: "Root login", sub: "Critical if on — use a sudo user instead", flag: "rootLogin", warn: true },
          { label: "Password login", sub: "Keys only is far safer", flag: "passwordLogin", warn: true },
          { label: "Fail2Ban", sub: "Ban an IP for 1 hour after 5 failed attempts", flag: "fail2ban" },
        ],
        sideTitle: "Connection",
        sideNote: "Non-standard ports cut automated scanning noise by roughly 90%.",
        fields: [
          { label: "SSH port", value: "2202" },
          { label: "Allowed users", value: "nexp, deploy" },
          { label: "Idle timeout", value: "300s" },
        ],
        sideActions: [{ label: "Change port", primary: true }],
        table1: {
          title: "SSH keys",
          action: "Add key",
          columns: ["Name", "Fingerprint", "Last used"],
          rows: [
            row("macbook-aarav", "SHA256:4f2a…c91", "2 hours ago", "Revoke"),
            row("deploy-runner", "SHA256:9b71…e40", "yesterday", "Revoke"),
          ],
        },
        logs: [
          { time: "12:02:19", text: "Failed password for root from 45.148.10.72 port 51122", tone: "warning" },
          { time: "12:02:24", text: "fail2ban: banned 45.148.10.72 (5 attempts)", tone: "success" },
          { time: "11:48:03", text: "Accepted publickey for nexp from 49.36.12.5" },
          { time: "11:20:41", text: "Invalid user admin from 103.76.44.19", tone: "warning" },
        ],
        logName: "auth/sshd.log",
      };

    case "updates":
      return {
        kicker: "Security",
        title: "Security updates",
        sub: "Patches for the OS, PHP and your WordPress installs.",
        stats: [
          { label: "Pending", value: "3", sub: "1 marked urgent" },
          { label: "OS patches", value: "2", sub: "Ubuntu 24.04 LTS" },
          { label: "PHP", value: "1", sub: "8.3.9 → 8.3.11" },
          { label: "WordPress", value: "Up to date", sub: "core 6.7.1 · 1 plugin update" },
        ],
        toggleTitle: "Automatic updates",
        toggles: [
          { label: "OS security updates", sub: "Unattended upgrades, nightly at 04:00", flag: "autoOs" },
          { label: "PHP patch releases", sub: "Minor versions only, never a major jump", flag: "autoPhp" },
          { label: "WordPress core & plugins", sub: "Security releases installed within 6 hours", flag: "autoWp" },
        ],
        sideTitle: "Install now",
        sideNote:
          "Security-only patches need no downtime. A backup is taken automatically before anything is applied.",
        sideActions: [{ label: "Install 3 updates", primary: true }, { label: "Schedule for tonight" }],
        table1: {
          title: "Pending updates",
          action: "Install all",
          columns: ["Package", "Current → new", "Severity"],
          rows: [
            row("openssl", "3.0.13 → 3.0.14", "Urgent", "Install"),
            row("libxml2", "2.9.14 → 2.9.16", "Moderate", "Install"),
            row("php8.3-fpm", "8.3.9 → 8.3.11", "Moderate", "Install"),
          ],
        },
        table2: {
          title: "Update history",
          action: "Export",
          columns: ["What", "When", "Result"],
          rows: [
            row("WordPress 6.7 → 6.7.1", "2 days ago", "Success", "Details"),
            row("nginx 1.26.1 → 1.26.2", "9 days ago", "Success", "Details"),
            row("php8.3 8.3.8 → 8.3.9", "3 weeks ago", "Success", "Details"),
          ],
        },
      };

    case "login":
      return {
        kicker: "Security",
        title: "Login protection",
        sub: "Who can sign in to NexPanel, and from where.",
        stats: [
          { label: "2FA", value: flags.isOn("twofa") ? "Required" : "Optional", sub: "2 of 3 members enrolled" },
          { label: "Failed attempts", value: "47", sub: "last 24 hours" },
          { label: "Blocked IPs", value: "4", sub: "auto-unblock in 24h" },
          { label: "Active sessions", value: "3", sub: "2 devices, 1 API token" },
        ],
        toggleTitle: "Rules",
        toggles: [
          { label: "Require 2FA for all members", sub: "Members without 2FA are prompted at next login", flag: "twofa" },
          { label: "Block IP after failed attempts", sub: "5 failures in 10 minutes = 24h block", flag: "ipBlock" },
        ],
        sideTitle: "Trusted IPs",
        sideNote: "Panel logins from outside this list get an email challenge, even with the right password.",
        fields: [{ label: "Office", value: "49.36.12.5" }, { label: "Aarav home", value: "106.51.88.240" }],
        sideActions: [{ label: "Add trusted IP", primary: true }],
        table1: {
          title: "Active sessions",
          action: "Revoke all others",
          columns: ["Device", "Location", "Last seen"],
          rows: [
            row("Chrome · macOS (this device)", "Bengaluru, IN", "now", "Current"),
            row("Safari · iPhone", "Bengaluru, IN", "3 hours ago", "Revoke"),
            row("API token nxp_live_••••4f21", "server-side", "11 min ago", "Revoke"),
          ],
        },
        table2: {
          title: "Recent login attempts",
          action: "View all",
          columns: ["Account", "IP", "Result"],
          rows: [
            row("aarav@novaretail.in", "49.36.12.5", "Success · 2FA", "Details"),
            row("admin", "45.148.10.72", "Failed · blocked", "Block IP"),
            row("aarav@novaretail.in", "103.76.44.19", "Failed · wrong 2FA", "Details"),
          ],
        },
      };

    case "logs":
      return {
        kicker: "Security",
        title: "Security logs",
        sub: "Every authentication, block and file change, in one searchable trail.",
        choiceTitle: "Log source",
        choiceDefault: ctx.logSource,
        choices: LOG_SOURCES.map((label) => ({ label })),
        logName: LOG_NAMES[ctx.logSource],
        logs: LOG_LINES[ctx.logSource],
        table1: {
          title: "Retention & export",
          action: "Download logs",
          columns: ["Source", "Retention", "Size"],
          rows: [
            row("Authentication", "90 days", "42 MB", "Export"),
            row("Firewall", "30 days", "318 MB", "Export"),
            row("WAF", "30 days", "96 MB", "Export"),
            row("Audit trail", "1 year", "12 MB", "Export"),
          ],
        },
      };

    case "settings":
      return {
        kicker: "Security",
        title: "Security settings",
        sub: "Pick a profile, then fine-tune what NexPanel does on its own.",
        choiceTitle: "Security profile",
        choiceDefault: ctx.profile,
        choices: [
          { label: "Lite", sub: "Fewest blocks. For legacy apps that break easily." },
          { label: "Standard", sub: "Balanced defaults. Right for almost everyone." },
          { label: "Maximum", sub: "Strict WAF, short sessions, 2FA enforced." },
        ],
        toggleTitle: "Automatic actions",
        toggles: [
          { label: "Block attacking IPs automatically", sub: "Firewall drops the source after repeated blocks", flag: "autoBlock" },
          { label: "Quarantine infected files", sub: "Isolate on detection instead of waiting for review", flag: "autoQuarantine" },
          { label: "Email me security alerts", sub: "aarav@novaretail.in · critical and warning events", flag: "notifyEmail" },
          { label: "Send alerts to Slack", sub: 'Via your n8n workflow "Failed deploy → Slack alert"', flag: "notifySlack" },
        ],
        sideTitle: "Resource limits",
        sideNote: "Caps that stop one compromised site from taking the server down.",
        fields: [
          { label: "Requests per IP", value: "120 / minute" },
          { label: "PHP processes per site", value: "12" },
          { label: "Max upload size", value: "256 MB" },
          { label: "Panel session timeout", value: ctx.profile === "Maximum" ? "15 minutes" : "8 hours" },
        ],
        sideActions: [{ label: "Edit limits", primary: true }],
      };
  }
}
