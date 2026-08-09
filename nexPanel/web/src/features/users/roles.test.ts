import { describe, expect, it } from "vitest";
import type { Principal } from "@/lib/api";
import { canGrantRole, grantableRoles, type Role } from "./users";

function role(slug: string, permissions: string[]): Role {
  return { uid: slug, slug, name: slug, description: "", system: false, permissions };
}

function principal(permissions: string[]): Principal {
  return {
    user_uid: "u",
    username: "u",
    email: "u@h.io",
    display_name: "u",
    permissions,
  } as Principal;
}

describe("canGrantRole", () => {
  const developer = role("developer", ["site.read", "site.write"]);
  const admin = role("admin", ["*"]);

  it("lets a superuser grant anything, including the wildcard role", () => {
    const me = principal(["*"]);
    expect(canGrantRole(me, developer)).toBe(true);
    expect(canGrantRole(me, admin)).toBe(true);
  });

  it("refuses the wildcard role to a non-superuser", () => {
    expect(canGrantRole(principal(["site.read", "site.write"]), admin)).toBe(false);
  });

  it("grants a role only when the actor holds every permission it carries", () => {
    expect(canGrantRole(principal(["site.read", "site.write"]), developer)).toBe(true);
    expect(canGrantRole(principal(["site.read"]), developer)).toBe(false); // missing site.write
  });

  it("treats a null principal as unable to grant", () => {
    expect(canGrantRole(null, developer)).toBe(false);
  });
});

describe("grantableRoles", () => {
  it("filters to the roles the actor can assign", () => {
    const roles = [
      role("developer", ["site.read", "site.write"]),
      role("dnsonly", ["dns.read"]),
      role("admin", ["*"]),
    ];
    const me = principal(["site.read", "site.write"]); // no dns, not superuser
    expect(grantableRoles(me, roles).map((r) => r.slug)).toEqual(["developer"]);
  });
});
