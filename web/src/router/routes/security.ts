import type { RouteRecordRaw } from "vue-router";

/** Server-wide security. Site-scoped scanning lives under the site drawer. */
export const securityRoutes: RouteRecordRaw[] = [
  {
    path: "/security",
    component: () => import("@/views/security/SecurityLayout.vue"),
    children: [
      { path: "", name: "security", redirect: { name: "security-overview" } },
      { path: "overview", name: "security-overview", component: () => import("@/views/security/SecurityOverviewView.vue"), meta: { title: "Security overview" } },
      { path: "firewall", name: "security-firewall", component: () => import("@/views/security/FirewallView.vue"), meta: { title: "Firewall" } },
      { path: "waf", name: "security-waf", component: () => import("@/views/security/WafView.vue"), meta: { title: "WAF" } },
      { path: "malware", name: "security-malware", component: () => import("@/views/security/MalwareView.vue"), meta: { title: "Malware scanner" } },
      { path: "ssh", name: "security-ssh", component: () => import("@/views/security/SshSecurityView.vue"), meta: { title: "SSH security" } },
      { path: "updates", name: "security-updates", component: () => import("@/views/security/SecurityUpdatesView.vue"), meta: { title: "Security updates" } },
      { path: "login", name: "security-login", component: () => import("@/views/security/LoginProtectionView.vue"), meta: { title: "Login protection" } },
      { path: "logs", name: "security-logs", component: () => import("@/views/security/SecurityLogsView.vue"), meta: { title: "Security logs" } },
      { path: "settings", name: "security-settings", component: () => import("@/views/security/SecuritySettingsView.vue"), meta: { title: "Security settings" } },
    ],
  },
];
