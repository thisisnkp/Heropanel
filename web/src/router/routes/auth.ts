import type { RouteRecordRaw } from "vue-router";

/**
 * The screens that exist before a session does.
 *
 * All four are `standalone` — they render with no rail, no sidebar and no
 * search, because there is nothing yet to navigate to — and `public`, which is
 * what stops the guard from bouncing them to themselves.
 *
 * The setup wizard sits here rather than under /settings for the same reason:
 * it runs once, before the panel is usable, and it is the only screen a
 * half-configured install may show.
 */
export const authRoutes: RouteRecordRaw[] = [
  {
    path: "/login",
    name: "login",
    component: () => import("@/views/auth/LoginView.vue"),
    meta: { title: "Sign in", standalone: true, public: true },
  },
  {
    path: "/welcome",
    name: "bootstrap",
    component: () => import("@/views/auth/BootstrapView.vue"),
    meta: { title: "Create the administrator", standalone: true, public: true },
  },
  {
    path: "/unconfigured",
    name: "unconfigured",
    component: () => import("@/views/auth/UnconfiguredView.vue"),
    meta: { title: "No database", standalone: true, public: true },
  },
  {
    path: "/setup",
    name: "setup",
    component: () => import("@/views/setup/SetupWizardView.vue"),
    // Not public: finishing setup installs packages on the host, so it needs a
    // signed-in administrator. It is standalone because the panel behind it is
    // not usable yet.
    meta: { title: "Set up this server", standalone: true },
  },
];
