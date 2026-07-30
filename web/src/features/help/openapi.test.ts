import { describe, expect, it } from "vitest";
import { countEndpoints, filterEndpoints, groupEndpoints, type OpenApiSpec } from "./openapi";

const spec: OpenApiSpec = {
  paths: {
    "/api/v1/sites": {
      get: { summary: "List sites", tags: ["Sites"], "x-required-permission": "site.read" },
      post: { summary: "Create a site", tags: ["Sites"], "x-required-permission": "site.write" },
      // A non-method key under the path item must be ignored, not treated as an op.
      parameters: {} as never,
    },
    "/api/v1/audit": {
      get: { summary: "List audit", tags: ["Audit"], "x-required-permission": "audit.read" },
    },
    "/api/v1/openapi.json": {
      get: { summary: "OpenAPI document" }, // no tag → "Other"
    },
  },
};

describe("groupEndpoints", () => {
  const groups = groupEndpoints(spec);

  it("groups by tag, sorted, ignoring non-method keys", () => {
    expect(groups.map((g) => g.tag)).toEqual(["Audit", "Other", "Sites"]);
    // parameters was not counted as an endpoint
    expect(countEndpoints(groups)).toBe(4);
  });

  it("uppercases the method and extracts the permission", () => {
    const sites = groups.find((g) => g.tag === "Sites")!;
    expect(sites.endpoints[0]).toMatchObject({ method: "GET", permission: "site.read" });
    expect(sites.endpoints[1]).toMatchObject({ method: "POST", permission: "site.write" });
  });

  it("defaults an untagged operation to Other with no permission", () => {
    const other = groups.find((g) => g.tag === "Other")!;
    expect(other.endpoints[0].permission).toBeUndefined();
  });

  it("tolerates a null/empty spec", () => {
    expect(groupEndpoints(null)).toEqual([]);
    expect(groupEndpoints({})).toEqual([]);
  });
});

describe("filterEndpoints", () => {
  const groups = groupEndpoints(spec);

  it("returns everything for an empty query", () => {
    expect(countEndpoints(filterEndpoints(groups, "  "))).toBe(4);
  });

  it("matches on path, summary, permission, method, and tag", () => {
    expect(countEndpoints(filterEndpoints(groups, "audit"))).toBe(1); // tag + path + perm
    expect(countEndpoints(filterEndpoints(groups, "create"))).toBe(1); // summary
    expect(countEndpoints(filterEndpoints(groups, "site.write"))).toBe(1); // permission
    expect(countEndpoints(filterEndpoints(groups, "post"))).toBe(1); // method
  });

  it("drops groups left empty by the filter", () => {
    const r = filterEndpoints(groups, "audit");
    expect(r.map((g) => g.tag)).toEqual(["Audit"]);
  });

  it("returns nothing when nothing matches", () => {
    expect(filterEndpoints(groups, "zzz-nomatch")).toEqual([]);
  });
});
