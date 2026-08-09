import { describe, expect, it } from "vitest";
import { classifyDomain, normalizeDomain, siteNameFrom } from "./newsite";
import { filterOptions } from "@/components/ui";

const free = ["acme.com", "staging.acme.co.uk", "example.org"];
// example.org is trusted but *not* free — something already serves it.
const trusted = ["acme.com", "staging.acme.co.uk", "example.org", "inuse.com"];

describe("normalizeDomain", () => {
  it("lowercases and trims", () => {
    expect(normalizeDomain("  Acme.COM  ")).toBe("acme.com");
  });

  // Mirrors the Go-side fix in NormalizeFQDN: the root dot has to come off
  // after the padding, or a pasted value keeps it and fails validation.
  it("strips a trailing root dot even behind whitespace", () => {
    expect(normalizeDomain("  Acme.COM.  ")).toBe("acme.com");
  });

  it("returns empty for blank input", () => {
    expect(normalizeDomain("   ")).toBe("");
  });
});

describe("classifyDomain", () => {
  it("reports empty input", () => {
    expect(classifyDomain("", free, trusted)).toEqual({ kind: "empty" });
  });

  it("recognises a free domain exactly", () => {
    expect(classifyDomain("acme.com", free, trusted)).toEqual({ kind: "free", fqdn: "acme.com" });
  });

  it("normalizes before matching", () => {
    expect(classifyDomain(" ACME.com. ", free, trusted)).toEqual({ kind: "free", fqdn: "acme.com" });
  });

  it("recognises a subdomain of a trusted domain", () => {
    expect(classifyDomain("blog.acme.com", free, trusted)).toEqual({ kind: "subdomain", parent: "acme.com" });
  });

  // The case the whole `trusted` list exists for: the parent is already
  // serving a site, so it is not free, but a subdomain still needs no
  // verification. Reporting "unknown" here would contradict the server.
  it("recognises a subdomain of a domain already in use", () => {
    expect(classifyDomain("shop.inuse.com", free, trusted)).toEqual({ kind: "subdomain", parent: "inuse.com" });
  });

  it("picks the closest parent when several match", () => {
    const t = ["acme.co.uk", "staging.acme.co.uk"];
    expect(classifyDomain("api.staging.acme.co.uk", [], t)).toEqual({
      kind: "subdomain",
      parent: "staging.acme.co.uk",
    });
  });

  it("reports an unrelated domain as unknown", () => {
    expect(classifyDomain("somewhere-else.net", free, trusted)).toEqual({ kind: "unknown" });
  });

  // A suffix match is not a subdomain: "notacme.com" merely ends with the
  // same characters as "acme.com".
  it("does not treat a suffix collision as a subdomain", () => {
    expect(classifyDomain("notacme.com", free, trusted)).toEqual({ kind: "unknown" });
  });
});

describe("siteNameFrom", () => {
  it("takes the first label", () => {
    expect(siteNameFrom("acme.com")).toBe("acme");
  });

  it("skips a leading www", () => {
    expect(siteNameFrom("www.acme.com")).toBe("acme");
  });

  it("handles a subdomain", () => {
    expect(siteNameFrom("blog.acme.com")).toBe("blog");
  });

  it("returns empty for empty input", () => {
    expect(siteNameFrom("")).toBe("");
  });
});

describe("filterOptions", () => {
  it("returns everything for an empty query — the click-to-see-all case", () => {
    expect(filterOptions(free, "")).toEqual(free);
    expect(filterOptions(free, "   ")).toEqual(free);
  });

  // Substring, not prefix: people type the memorable middle of a hostname.
  it("matches anywhere in the option", () => {
    expect(filterOptions(free, "acme")).toEqual(["acme.com", "staging.acme.co.uk"]);
  });

  it("is case-insensitive", () => {
    expect(filterOptions(free, "ACME.CO.UK")).toEqual(["staging.acme.co.uk"]);
  });

  it("returns nothing when there is no match, leaving the typed value to stand", () => {
    expect(filterOptions(free, "zzz")).toEqual([]);
  });
});
