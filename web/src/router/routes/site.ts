import type { RouteRecordRaw } from "vue-router";
import type { SpecKey } from "@/data/siteSpec";

/**
 * Everything under a single site.
 *
 * Nested under `/sites/:id` so the site id is a route param rather than store
 * state: a deep link to one site's cron jobs has to survive a reload, and the
 * shell needs to know which site the drawer is for before any component mounts.
 */

const SiteSpecView = () => import("@/views/site/SiteSpecView.vue");

/**
 * A screen that renders through the shared spec component. Twenty-two of the
 * site screens are the same layout with different content, so they are declared
 * as data here instead of as twenty-two near-identical component files.
 */
function spec(path: string, name: string, specKey: SpecKey, title: string): RouteRecordRaw {
  return { path, name, component: SiteSpecView, props: { specKey }, meta: { title } };
}

export const siteRoutes: RouteRecordRaw[] = [
  {
    path: "/sites/:id",
    component: () => import("@/views/site/SiteLayout.vue"),
    props: true,
    children: [
      { path: "", name: "site", redirect: { name: "site-overview" } },
      { path: "overview", name: "site-overview", component: () => import("@/views/site/SiteOverviewView.vue"), meta: { title: "Overview" } },

      // Performance
      spec("ai-troubleshooter", "site-ai-troubleshooter", "aitrouble", "AI troubleshooter"),
      spec("pagespeed", "site-pagespeed", "pagespeed", "PageSpeed"),
      spec("cdn", "site-cdn", "cdn", "Cloudflare CDN"),

      spec("analytics", "site-analytics", "analytics", "Analytics"),

      // Security
      spec("malware", "site-malware", "malware", "Malware scanner"),
      spec("ssl", "site-ssl", "ssl", "SSL"),

      // Domains
      spec("subdomains", "site-subdomains", "subdomains", "Subdomains"),
      spec("parked", "site-parked", "parked", "Parked domains"),
      spec("redirects", "site-redirects", "redirects", "Redirections"),

      // WordPress manager
      spec("wp/install", "site-wp-install", "wpinstall", "Install"),
      spec("wp/migrate", "site-wp-migrate", "wpmigrate", "Migrate website"),
      spec("wp/staging", "site-wp-staging", "wpstaging", "Create staging"),
      { path: "wp/plugins", name: "site-wp-plugins", component: () => import("@/views/site/wordpress/WpPluginsView.vue"), meta: { title: "Plugins and updates" } },

      // Files
      { path: "files", name: "site-files", component: () => import("@/views/site/files/FileManagerView.vue"), meta: { title: "File manager", fullBleed: true } },
      spec("ftp", "site-ftp", "ftp", "FTP"),
      spec("ssh", "site-ssh", "sshsite", "SSH"),

      // Databases
      { path: "databases", name: "site-db", component: () => import("@/views/site/databases/SiteDatabasesView.vue"), meta: { title: "Databases" } },
      spec("phpmyadmin", "site-phpmyadmin", "phpmyadmin", "phpMyAdmin"),
      spec("remote-db", "site-remote-db", "remotedb", "Remote DB"),

      { path: "runtime", name: "site-runtime", component: () => import("@/views/site/RuntimeView.vue"), meta: { title: "Runtime" } },
      { path: "environment", name: "site-env", component: () => import("@/views/site/EnvironmentView.vue"), meta: { title: "Environment" } },
      { path: "backups", name: "site-backups", component: () => import("@/views/site/SiteBackupsView.vue"), meta: { title: "Backups" } },

      // Advanced
      { path: "php", name: "site-php", component: () => import("@/views/site/advanced/PhpSettingsView.vue"), meta: { title: "PHP settings" } },
      { path: "logs", name: "site-logs", component: () => import("@/views/site/advanced/SiteLogsView.vue"), meta: { title: "Logs" } },
      spec("version", "site-lang-version", "lang", "Version"),
      spec("cron", "site-cron", "cron", "Cron jobs"),
      { path: "git/deployments", name: "site-git-deployments", component: () => import("@/views/site/advanced/GitDeploymentsView.vue"), meta: { title: "Git deployments" } },
      spec("git/setup", "site-git-setup", "git", "Setup Git"),
      spec("ip-manage", "site-ip-manage", "ipmanage", "IP manage"),
      spec("hotlink", "site-hotlink", "hotlink", "Hotlink protection"),
      spec("cache", "site-cache", "cachemgr", "Cache manager"),
      spec("activity", "site-activity", "activity", "Activity logs"),

      { path: "danger", name: "site-danger", component: () => import("@/views/site/DangerZoneView.vue"), meta: { title: "Danger zone" } },
    ],
  },
];
