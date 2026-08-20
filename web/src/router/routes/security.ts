import type { RouteRecordRaw } from "vue-router";
import type { SecurityKey } from "@/data/securitySpec";

/** Server-wide security. Site-scoped scanning lives under the site drawer. */

const SecuritySpecView = () => import("@/views/security/SecuritySpecView.vue");

function tab(path: string, name: string, securityKey: SecurityKey, title: string): RouteRecordRaw {
  return { path, name, component: SecuritySpecView, props: { securityKey }, meta: { title } };
}

export const securityRoutes: RouteRecordRaw[] = [
  {
    path: "/security",
    component: () => import("@/views/security/SecurityLayout.vue"),
    children: [
      { path: "", name: "security", redirect: { name: "security-overview" } },
      tab("overview", "security-overview", "overview", "Security overview"),
      tab("firewall", "security-firewall", "firewall", "Firewall"),
      tab("waf", "security-waf", "waf", "WAF"),
      tab("malware", "security-malware", "malware", "Malware scanner"),
      tab("ssh", "security-ssh", "ssh", "SSH security"),
      tab("updates", "security-updates", "updates", "Security updates"),
      tab("login", "security-login", "login", "Login protection"),
      tab("logs", "security-logs", "logs", "Security logs"),
      tab("settings", "security-settings", "settings", "Security settings"),
    ],
  },
];
