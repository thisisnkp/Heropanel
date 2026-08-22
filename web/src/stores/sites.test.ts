import { describe, expect, it } from "vitest";
import { fromApi } from "./sites";
import type { Site as ApiSite } from "@/lib/api";

/**
 * The mapping from npd's site to the screens' site.
 *
 * It is tested rather than trusted because it is where two vocabularies meet:
 * npd talks about provisioning state and vhost shape, the design talks about
 * what the operator sees. Every mistranslation here shows up as a badge or a
 * status that is confidently wrong.
 */

function api(over: Partial<ApiSite> = {}): ApiSite {
  return {
    uid: "ste_abc",
    name: "novaretail.in",
    primary_domain: "novaretail.in",
    type: "php",
    stack: "php",
    deploy_mode: "baremetal",
    status: "active",
    webserver: "openlitespeed",
    document_root: "/srv/nexpanel/sites/1/public",
    system_user: "nps1",
    created_at: "2026-03-14T00:00:00Z",
    ...over,
  };
}

describe("fromApi", () => {
  it("keeps the uid, which is what every request under /sites needs", () => {
    expect(fromApi(api()).uid).toBe("ste_abc");
  });

  it("falls back to the domain when a site has no name", () => {
    expect(fromApi(api({ name: "" })).name).toBe("novaretail.in");
  });

  // The stack comes from npd, not from `type`. A Node site and a Python site
  // are both type "proxy" — deriving the badge here would give both the same
  // one, and one of them would be wrong.
  it.each([
    ["static", "static"],
    ["php", "php"],
    ["node", "node"],
    ["python", "python"],
    ["app", "app"],
  ])("carries the %s stack through", (stack, expected) => {
    expect(fromApi(api({ stack, type: "proxy" })).stackKey).toBe(expected);
  });

  // A newer npd could report a stack this build has no badge for. Rendering
  // nothing at all would leave a blank cell in the list; falling back keeps the
  // row readable, and the site's own screens still work.
  it("falls back for a stack it does not know", () => {
    expect(fromApi(api({ stack: "ruby" })).stackKey).toBe("static");
  });

  describe("status", () => {
    it("shows an active site as live", () => {
      expect(fromApi(api({ status: "active" })).status).toBe("live");
    });

    it.each(["suspended", "disabled"])("shows %s as suspended", (status) => {
      expect(fromApi(api({ status })).status).toBe("suspended");
    });

    // A site that failed to provision still needs a row that says something.
    // Hiding it, or calling it live, are both worse than "building" plus the
    // site's own screen explaining what went wrong.
    it.each(["provisioning", "error"])("shows %s as building", (status) => {
      expect(fromApi(api({ status })).status).toBe("building");
    });
  });

  it("names the deploy source from the deploy mode", () => {
    expect(fromApi(api({ deploy_mode: "git" })).deploy).toBe("GitHub");
    expect(fromApi(api({ deploy_mode: "baremetal" })).deploy).toBe("Manual");
  });

  // The git fields belong to a different module and a different endpoint. An
  // em-dash is honest; "main" would be a guess that happens to be right often
  // enough to be believed.
  it("does not invent branch, repo or deploy time", () => {
    const s = fromApi(api({ deploy_mode: "git" }));
    expect([s.branch, s.repo, s.lastDeploy]).toEqual(["—", "—", "—"]);
  });
});
