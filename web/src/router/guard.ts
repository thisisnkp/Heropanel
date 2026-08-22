import type { Router } from "vue-router";
import { useSessionStore } from "@/stores/session";

/**
 * The one gate in front of every route.
 *
 * It runs before navigation rather than inside the shell so that a deep link to
 * a screen the operator may not see never mounts that screen at all — no flash
 * of a dashboard, no request fired on behalf of someone who is not signed in.
 *
 * Order matters, and it is the order of what is *blocking*:
 *
 *   1. no datastore   — nothing else is possible; a login form would be a lie
 *   2. no admin       — the login form would reject everyone
 *   3. no session     — sign in, remembering where they were going
 *   4. setup unfinished — the panel is running but manages nothing yet
 *
 * Checking session before setup is deliberate: completing setup installs
 * packages on the host, so it needs an administrator, not just a browser.
 *
 * The session is loaded once, lazily. Every later navigation reads the store,
 * so this costs one round trip per page load rather than one per route change.
 */
export function installAuthGuard(router: Router) {
  router.beforeEach(async (to) => {
    const session = useSessionStore();

    // First navigation of the page: find out where we stand before deciding.
    if (session.loading) await session.load();

    // The panel could not be reached at all. Bouncing to /login would blame the
    // operator for npd being down, so every route is left alone and the shell
    // shows the transport error instead.
    if (session.unreachable) return true;

    if (!session.configured) {
      return to.name === "unconfigured" ? true : { name: "unconfigured" };
    }
    if (to.name === "unconfigured") return { name: "login" };

    if (session.needsBootstrap) {
      return to.name === "bootstrap" ? true : { name: "bootstrap" };
    }
    if (to.name === "bootstrap") return { name: "login" };

    if (!session.isAuthenticated) {
      if (to.meta.public) return true;
      // `next` is a full path so a deep link survives the round trip through
      // the login form. LoginView refuses anything that is not a local path.
      return { name: "login", query: to.fullPath === "/" ? {} : { next: to.fullPath } };
    }

    // Signed in: the login form has nothing left to offer.
    if (to.name === "login") return { path: "/" };

    if (!session.setupComplete) {
      return to.name === "setup" ? true : { name: "setup" };
    }
    if (to.name === "setup") return { path: "/" };

    return true;
  });
}
