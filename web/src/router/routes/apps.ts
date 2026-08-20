import type { RouteRecordRaw } from "vue-router";

/**
 * Apps is a two-tab screen, and the tab is in the URL rather than in component
 * state so "install new app" is a link someone can be sent.
 */
export const appsRoutes: RouteRecordRaw[] = [
  {
    path: "/apps",
    component: () => import("@/views/apps/AppsLayout.vue"),
    children: [
      { path: "", name: "apps", redirect: { name: "apps-installed" } },
      { path: "installed", name: "apps-installed", component: () => import("@/views/apps/InstalledAppsView.vue"), meta: { title: "Installed apps" } },
      { path: "install", name: "apps-install", component: () => import("@/views/apps/InstallAppView.vue"), meta: { title: "Install new app" } },
    ],
  },
];
