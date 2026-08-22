import { createRouter, createWebHistory } from "vue-router";

import { authRoutes } from "./routes/auth";
import { mainRoutes } from "./routes/main";
import { appsRoutes } from "./routes/apps";
import { securityRoutes } from "./routes/security";
import { siteRoutes } from "./routes/site";
import { installAuthGuard } from "./guard";

export const router = createRouter({
  history: createWebHistory(),
  routes: [
    ...authRoutes,
    ...mainRoutes,
    ...appsRoutes,
    ...securityRoutes,
    ...siteRoutes,
    { path: "/:pathMatch(.*)*", name: "not-found", component: () => import("@/views/NotFoundView.vue") },
  ],
  // Returning to a list should return to where you were in it; opening a new
  // screen should start at the top.
  scrollBehavior: (_to, _from, saved) => saved ?? { top: 0 },
});

installAuthGuard(router);

router.afterEach((to) => {
  const title = to.meta.title as string | undefined;
  document.title = title ? `${title} · NexPanel` : "NexPanel";
});
