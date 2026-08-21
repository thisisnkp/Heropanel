import type { RouteRecordRaw } from "vue-router";

/** Top-level screens: the Manage, Automation, System and Account groups. */
export const mainRoutes: RouteRecordRaw[] = [
  { path: "/", name: "home", component: () => import("@/views/dashboard/HomeView.vue"), meta: { title: "Home" } },
  { path: "/websites", name: "websites", component: () => import("@/views/sites/SiteListView.vue"), meta: { title: "Websites" } },
  { path: "/mail", name: "mail", component: () => import("@/views/mail/MailView.vue"), meta: { title: "Mail" } },
  { path: "/domains", name: "domains", component: () => import("@/views/system/TablePageView.vue"), props: { pageKey: "domains" }, meta: { title: "Domains" } },
  { path: "/dns", name: "dns", component: () => import("@/views/domains/DnsView.vue"), meta: { title: "DNS & nameservers" } },
  { path: "/activity", name: "activity", component: () => import("@/views/activity/ActivityView.vue"), meta: { title: "Activity" } },
  { path: "/notifications", name: "notifications", component: () => import("@/views/activity/NotificationsView.vue"), meta: { title: "Notifications" } },
  { path: "/backups", name: "backups", component: () => import("@/views/system/TablePageView.vue"), props: { pageKey: "backups" }, meta: { title: "Backups" } },

  { path: "/openclaw", name: "openclaw", component: () => import("@/views/automation/AutomationView.vue"), props: { product: "openclaw" }, meta: { title: "OpenClaw" } },
  { path: "/n8n", name: "n8n", component: () => import("@/views/automation/AutomationView.vue"), props: { product: "n8n" }, meta: { title: "n8n" } },

  { path: "/docker", name: "docker", component: () => import("@/views/system/ContainersView.vue"), meta: { title: "Containers" } },
  { path: "/compose", name: "compose", component: () => import("@/views/system/ComposeView.vue"), meta: { title: "Compose" } },
  { path: "/settings", name: "settings", component: () => import("@/views/system/TablePageView.vue"), props: { pageKey: "settings" }, meta: { title: "Panel settings" } },

  { path: "/billing", name: "billing", component: () => import("@/views/account/BillingView.vue"), meta: { title: "License & billing" } },
  // Not "/api": the panel serves its own REST API under that prefix, so a UI
  // route there is shadowed by the backend in production and by the dev proxy
  // locally. The route *name* stays `api` so the navigation model is unchanged.
  { path: "/api-tokens", name: "api", component: () => import("@/views/account/ApiView.vue"), meta: { title: "API" } },
  { path: "/temp-access", name: "temp-access", component: () => import("@/views/account/TempAccessView.vue"), meta: { title: "Temp access" } },
];
