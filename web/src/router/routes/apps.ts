import type { RouteRecordRaw } from "vue-router";

/**
 * Apps is a three-tab screen, and the tab is in the URL rather than in component
 * state so "install new app" is a link someone can be sent. The catalogue
 * category rides along as a query parameter for the same reason.
 */
export const appsRoutes: RouteRecordRaw[] = [
  {
    path: "/apps",
    component: () => import("@/views/apps/AppsLayout.vue"),
    children: [
      { path: "", name: "apps", redirect: { name: "apps-installed" } },
      { path: "installed", name: "apps-installed", component: () => import("@/views/apps/InstalledAppsView.vue"), meta: { title: "Installed apps" } },
      { path: "install", name: "apps-install", component: () => import("@/views/apps/InstallAppView.vue"), meta: { title: "Install new app" } },
      { path: "licenses", name: "apps-licenses", component: () => import("@/views/apps/LicensesView.vue"), meta: { title: "Paid app licenses" } },
    ],
  },
];
