import { beforeEach, describe, expect, it } from "vitest";
import { createPinia, setActivePinia } from "pinia";
import { useSessionStore } from "./session";
import type { AuthStatus } from "@/lib/api";

/**
 * The session store's job is to tell four states apart, and the tests are about
 * exactly that — not about fetching.
 *
 * "Cannot reach npd", "npd has no database", "npd has no administrator" and
 * "you are signed out" all look like "not signed in" from a distance, and all
 * four need a different screen. Collapsing any two of them produces a panel
 * that blames the operator for a server problem, or offers a login form nobody
 * can ever pass.
 */

function status(over: Partial<AuthStatus> = {}): AuthStatus {
  return {
    needs_bootstrap: false,
    authenticated: false,
    configured: true,
    setup_complete: true,
    ...over,
  };
}

describe("session store", () => {
  beforeEach(() => setActivePinia(createPinia()));

  it("starts as loading, with nothing decided", () => {
    const s = useSessionStore();
    expect(s.loading).toBe(true);
    expect(s.isAuthenticated).toBe(false);
    expect(s.unreachable).toBeNull();
  });

  it("treats a panel with no datastore as unconfigured, not as signed out", () => {
    const s = useSessionStore();
    s.status = status({ configured: false });
    expect(s.configured).toBe(false);
    expect(s.isAuthenticated).toBe(false);
  });

  // An older npd does not send these flags at all. Reading a missing flag as
  // false would put every such panel behind a bootstrap screen or a setup
  // wizard it has no idea about.
  it("treats missing flags as the permissive answer", () => {
    const s = useSessionStore();
    s.status = { needs_bootstrap: false, authenticated: false };
    expect(s.configured).toBe(true);
    expect(s.setupComplete).toBe(true);
  });

  it("reports bootstrap and setup separately", () => {
    const s = useSessionStore();
    s.status = status({ needs_bootstrap: true, setup_complete: false });
    expect(s.needsBootstrap).toBe(true);
    expect(s.setupComplete).toBe(false);
  });

  describe("can()", () => {
    it("is false with no principal, whatever is asked", () => {
      const s = useSessionStore();
      expect(s.can("site.write")).toBe(false);
    });

    it("matches an exact permission", () => {
      const s = useSessionStore();
      s.set(principal(["site.read", "site.write"]));
      expect(s.can("site.write")).toBe(true);
      expect(s.can("system.write")).toBe(false);
    });

    // The wildcard is what npd issues an administrator. Missing it hides every
    // control in the panel from the one account allowed to use all of them.
    it("honours the administrator wildcard", () => {
      const s = useSessionStore();
      s.set(principal(["*"]));
      expect(s.can("site.write")).toBe(true);
      expect(s.can("anything.at.all")).toBe(true);
    });
  });
});

function principal(permissions: string[]) {
  return {
    user_id: 1,
    user_uid: "usr_1",
    email: "a@b.test",
    username: "a",
    display_name: "A",
    permissions,
  };
}
