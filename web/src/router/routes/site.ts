import type { RouteRecordRaw } from "vue-router";

/**
 * Everything under a single site.
 *
 * Nested under `/sites/:id` so the site id is a route param rather than store
 * state: a deep link to one site's cron jobs has to survive a reload, and the
 * shell needs to know which site the drawer is for before any component mounts.
 */
export const siteRoutes: RouteRecordRaw[] = [
  {
    path: "/sites/:id",
    component: () => import("@/views/site/SiteLayout.vue"),
    props: true,
    children: [
      { path: "", name: "site", redirect: { name: "site-overview" } },
      { path: "overview", name: "site-overview", component: () => import("@/views/site/SiteOverviewView.vue"), meta: { title: "Overview" } },

      // Performance
      { path: "ai-troubleshooter", name: "site-ai-troubleshooter", component: () => import("@/views/site/performance/AiTroubleshooterView.vue"), meta: { title: "AI troubleshooter" } },
      { path: "pagespeed", name: "site-pagespeed", component: () => import("@/views/site/performance/PageSpeedView.vue"), meta: { title: "PageSpeed" } },
      { path: "cdn", name: "site-cdn", component: () => import("@/views/site/performance/CdnView.vue"), meta: { title: "Cloudflare CDN" } },

      { path: "analytics", name: "site-analytics", component: () => import("@/views/site/AnalyticsView.vue"), meta: { title: "Analytics" } },

      // Security
      { path: "malware", name: "site-malware", component: () => import("@/views/site/security/SiteMalwareView.vue"), meta: { title: "Malware scanner" } },
      { path: "ssl", name: "site-ssl", component: () => import("@/views/site/security/SiteSslView.vue"), meta: { title: "SSL" } },

      // Domains
      { path: "subdomains", name: "site-subdomains", component: () => import("@/views/site/domains/SubdomainsView.vue"), meta: { title: "Subdomains" } },
      { path: "parked", name: "site-parked", component: () => import("@/views/site/domains/ParkedDomainsView.vue"), meta: { title: "Parked domains" } },
      { path: "redirects", name: "site-redirects", component: () => import("@/views/site/domains/RedirectsView.vue"), meta: { title: "Redirections" } },

      // WordPress manager
      { path: "wp/install", name: "site-wp-install", component: () => import("@/views/site/wordpress/WpInstallView.vue"), meta: { title: "Install" } },
      { path: "wp/migrate", name: "site-wp-migrate", component: () => import("@/views/site/wordpress/WpMigrateView.vue"), meta: { title: "Migrate website" } },
      { path: "wp/staging", name: "site-wp-staging", component: () => import("@/views/site/wordpress/WpStagingView.vue"), meta: { title: "Create staging" } },
      { path: "wp/plugins", name: "site-wp-plugins", component: () => import("@/views/site/wordpress/WpPluginsView.vue"), meta: { title: "Plugins and updates" } },

      // Files
      { path: "files", name: "site-files", component: () => import("@/views/site/files/FileManagerView.vue"), meta: { title: "File manager", fullBleed: true } },
      { path: "ftp", name: "site-ftp", component: () => import("@/views/site/files/FtpView.vue"), meta: { title: "FTP" } },
      { path: "ssh", name: "site-ssh", component: () => import("@/views/site/files/SiteSshView.vue"), meta: { title: "SSH" } },

      // Databases
      { path: "databases", name: "site-db", component: () => import("@/views/site/databases/SiteDatabasesView.vue"), meta: { title: "Databases" } },
      { path: "phpmyadmin", name: "site-phpmyadmin", component: () => import("@/views/site/databases/PhpMyAdminView.vue"), meta: { title: "phpMyAdmin" } },
      { path: "remote-db", name: "site-remote-db", component: () => import("@/views/site/databases/RemoteDbView.vue"), meta: { title: "Remote DB" } },

      { path: "runtime", name: "site-runtime", component: () => import("@/views/site/RuntimeView.vue"), meta: { title: "Runtime" } },
      { path: "environment", name: "site-env", component: () => import("@/views/site/EnvironmentView.vue"), meta: { title: "Environment" } },
      { path: "backups", name: "site-backups", component: () => import("@/views/site/SiteBackupsView.vue"), meta: { title: "Backups" } },

      // Advanced
      { path: "php", name: "site-php", component: () => import("@/views/site/advanced/PhpSettingsView.vue"), meta: { title: "PHP settings" } },
      { path: "logs", name: "site-logs", component: () => import("@/views/site/advanced/SiteLogsView.vue"), meta: { title: "Logs" } },
      { path: "version", name: "site-lang-version", component: () => import("@/views/site/advanced/LangVersionView.vue"), meta: { title: "Version" } },
      { path: "cron", name: "site-cron", component: () => import("@/views/site/advanced/CronView.vue"), meta: { title: "Cron jobs" } },
      { path: "git/deployments", name: "site-git-deployments", component: () => import("@/views/site/advanced/GitDeploymentsView.vue"), meta: { title: "Git deployments" } },
      { path: "git/setup", name: "site-git-setup", component: () => import("@/views/site/advanced/GitSetupView.vue"), meta: { title: "Setup Git" } },
      { path: "ip-manage", name: "site-ip-manage", component: () => import("@/views/site/advanced/IpManageView.vue"), meta: { title: "IP manage" } },
      { path: "hotlink", name: "site-hotlink", component: () => import("@/views/site/advanced/HotlinkView.vue"), meta: { title: "Hotlink protection" } },
      { path: "cache", name: "site-cache", component: () => import("@/views/site/advanced/CacheManagerView.vue"), meta: { title: "Cache manager" } },
      { path: "activity", name: "site-activity", component: () => import("@/views/site/advanced/ActivityLogView.vue"), meta: { title: "Activity logs" } },

      { path: "danger", name: "site-danger", component: () => import("@/views/site/DangerZoneView.vue"), meta: { title: "Danger zone" } },
    ],
  },
];
