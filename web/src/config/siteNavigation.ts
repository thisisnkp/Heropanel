import type { StackKey } from "./stacks";
import { LANG_VERSION_LABEL } from "./stacks";

/**
 * The per-site drawer navigation.
 *
 * A function rather than a constant because the drawer is genuinely different
 * per site: a static site has no runtime, no logs and no PHP settings; only
 * WordPress gets the WordPress manager; Git deployments only appear once a repo
 * is actually connected. Rendering every entry and disabling the ones that do
 * not apply would leave a WordPress menu on a Python site, which reads as a
 * missing feature rather than an inapplicable one.
 */

export interface SiteNavLeaf {
  readonly to: string;
  readonly label: string;
  readonly icon: string;
  /** Route params merged in — used by entries that jump out of the site scope. */
  readonly jump?: string;
  /**
   * Opens in its own window rather than replacing this one.
   *
   * Only the file manager: the design ships it as a separate document the panel
   * opens with window.open(), because you keep it open beside the panel while
   * you work rather than visiting it and coming back.
   */
  readonly newTab?: boolean;
}

export interface SiteNavGroup {
  readonly id: string;
  readonly label: string;
  readonly icon: string;
  readonly children: readonly (SiteNavLeaf | SiteNavGroup)[];
}

export type SiteNavNode = SiteNavLeaf | SiteNavGroup;

export function isGroup(n: SiteNavNode): n is SiteNavGroup {
  return "children" in n;
}

export interface SiteNavInput {
  readonly stackKey: StackKey;
  /** "GitHub" once a repository is connected, "Manual" otherwise. */
  readonly deploy: string;
}

export function buildSiteNav(site: SiteNavInput): SiteNavNode[] {
  const { stackKey, deploy } = site;
  const nodes: SiteNavNode[] = [];

  nodes.push({ to: "site-overview", label: "Overview", icon: "space-dashboard" });

  nodes.push({
    id: "perf",
    label: "Performance",
    icon: "speed",
    children: [
      { to: "site-ai-troubleshooter", label: "AI troubleshooter", icon: "auto-awesome" },
      { to: "site-pagespeed", label: "PageSpeed", icon: "trending-up" },
      { to: "site-cdn", label: "Cloudflare CDN", icon: "cloud" },
    ],
  });

  nodes.push({ to: "site-analytics", label: "Analytics", icon: "bar-chart" });

  nodes.push({
    id: "sec",
    label: "Security",
    icon: "shield",
    children: [
      { to: "site-malware", label: "Malware scanner", icon: "bug-report" },
      { to: "site-ssl", label: "SSL", icon: "lock" },
    ],
  });

  nodes.push({
    id: "dom",
    label: "Domains",
    icon: "dns",
    children: [
      { to: "site-subdomains", label: "Subdomains", icon: "account-tree" },
      { to: "site-parked", label: "Parked domains", icon: "local-parking" },
      { to: "site-redirects", label: "Redirections", icon: "call-split" },
    ],
  });

  if (stackKey === "wp") {
    nodes.push({
      id: "wpm",
      label: "WordPress manager",
      icon: "extension",
      children: [
        { to: "site-wp-install", label: "Install", icon: "download" },
        { to: "site-wp-migrate", label: "Migrate website", icon: "swap-horiz" },
        { to: "site-wp-staging", label: "Create staging", icon: "content-copy" },
        { to: "site-wp-plugins", label: "Plugins and updates", icon: "widgets" },
      ],
    });
  }

  nodes.push({
    id: "fls",
    label: "Files",
    icon: "folder-open",
    children: [
      { to: "site-files", label: "File manager", icon: "folder", newTab: true },
      { to: "site-ftp", label: "FTP", icon: "swap-vert" },
      { to: "site-ssh", label: "SSH", icon: "terminal" },
    ],
  });

  nodes.push({
    id: "dbs",
    label: "Databases",
    icon: "database",
    children: [
      { to: "site-db", label: "Manage", icon: "table-view" },
      { to: "site-phpmyadmin", label: "phpMyAdmin", icon: "open-in-new" },
      { to: "site-remote-db", label: "Remote DB", icon: "lan" },
    ],
  });

  if (stackKey === "node") nodes.push({ to: "site-runtime", label: "Node.js runtime", icon: "terminal" });
  if (stackKey === "python") nodes.push({ to: "site-runtime", label: "Python runtime", icon: "terminal" });
  if (stackKey === "node" || stackKey === "python" || deploy === "GitHub") {
    nodes.push({ to: "site-env", label: "Environment", icon: "key" });
  }

  nodes.push({ to: "site-backups", label: "Backups", icon: "backup" });

  const advanced: SiteNavNode[] = [];
  if (stackKey === "php" || stackKey === "wp") advanced.push({ to: "site-php", label: "PHP settings", icon: "tune" });
  if (stackKey !== "static") advanced.push({ to: "site-logs", label: "Logs", icon: "receipt-long" });

  const langLabel = LANG_VERSION_LABEL[stackKey];
  if (langLabel) advanced.push({ to: "site-lang-version", label: langLabel, icon: "code" });

  advanced.push({ to: "site-cron", label: "Cron jobs", icon: "schedule" });
  // Leaves the site scope on purpose: the zone belongs to the domain, not the site.
  advanced.push({ to: "dns", label: "DNS zone editor", icon: "dns", jump: "dns" });

  const gitChildren: SiteNavLeaf[] = [];
  if (stackKey !== "wp" && deploy === "GitHub") {
    gitChildren.push({ to: "site-git-deployments", label: "Git deployments", icon: "cloud-sync" });
  }
  gitChildren.push({ to: "site-git-setup", label: "Setup Git", icon: "settings" });
  advanced.push({ id: "git", label: "Git", icon: "commit", children: gitChildren });

  advanced.push({ to: "site-ip-manage", label: "IP manage", icon: "shield-lock" });
  advanced.push({ to: "site-hotlink", label: "Hotlink protection", icon: "link-off" });
  advanced.push({ to: "site-cache", label: "Cache manager", icon: "bolt" });
  advanced.push({ to: "site-activity", label: "Activity logs", icon: "history" });

  nodes.push({ id: "adv", label: "Advanced", icon: "tune", children: advanced });
  nodes.push({ to: "site-danger", label: "Danger zone", icon: "delete" });

  return nodes;
}

/**
 * Why this drawer looks the way it does, in one sentence.
 *
 * Shown in the drawer's footer card. A menu that silently differs between two
 * sites reads as a bug or a missing feature; saying "a static site has no
 * runtime" turns an absence into an explanation. It belongs beside buildSiteNav
 * because the two answer the same question and would otherwise disagree.
 */
export function navExplain(site: SiteNavInput): string {
  const manual = site.deploy === "GitHub" ? "Deploys come from Git." : "Files are uploaded manually.";
  switch (site.stackKey) {
    case "static":
      return (
        "A static site has no runtime, so those panels are hidden. " +
        (site.deploy === "GitHub" ? "Git deployments are shown." : "Files are uploaded manually.")
      );
    case "node":
      return "Node.js site: runtime version, start command and environment variables are shown. " + manual;
    case "python":
      return "Python site: interpreter version, WSGI server and environment variables are shown. " + manual;
    case "wp":
      return "WordPress site: plugin, theme and database panels are shown; runtime settings are managed for you.";
    default:
      return "PHP site: PHP version, extensions and MySQL databases are shown.";
  }
}
