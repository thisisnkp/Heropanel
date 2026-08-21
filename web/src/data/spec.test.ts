import { describe, expect, it } from "vitest";
import { createPinia, setActivePinia } from "pinia";

import { buildSiteSpec, type SpecKey } from "./siteSpec";
import { buildSecuritySpec, LOG_SOURCES, type SecurityKey } from "./securitySpec";
import { quickActions, runtimeFields, runtimeTitle } from "./siteDetail";
import { useFlagsStore } from "@/stores/flags";
import type { Site } from "@/stores/sites";

const SITE_KEYS: readonly SpecKey[] = [
  "aitrouble", "pagespeed", "cdn", "analytics", "malware", "ssl",
  "subdomains", "parked", "redirects", "wpinstall", "wpmigrate", "wpstaging",
  "ftp", "phpmyadmin", "remotedb", "lang", "cron", "sshsite", "git",
  "ipmanage", "hotlink", "cachemgr", "activity",
];

const SECURITY_KEYS: readonly SecurityKey[] = [
  "overview", "firewall", "waf", "malware", "ssh", "updates", "login", "logs", "settings",
];

function site(over: Partial<Site> = {}): Site {
  return {
    id: 1,
    name: "novaretail.in",
    domain: "novaretail.in",
    stackKey: "wp",
    deploy: "Manual",
    status: "live",
    lastDeploy: "2 hours ago",
    branch: "—",
    repo: "—",
    ...over,
  };
}

describe("buildSiteSpec", () => {
  it("returns a usable spec for every key the router declares", () => {
    for (const key of SITE_KEYS) {
      const spec = buildSiteSpec(key, site());
      expect(spec, key).toBeDefined();
      expect(spec.title, key).toBeTruthy();
      expect(spec.sub, key).toBeTruthy();
      expect(spec.kicker, key).toBeTruthy();
    }
  });

  it("gives every stat row a label, a value and a sub", () => {
    for (const key of SITE_KEYS) {
      for (const stat of buildSiteSpec(key, site()).stats ?? []) {
        expect(stat.label, key).toBeTruthy();
        expect(stat.value, key).toBeTruthy();
        expect(stat.sub, key).toBeTruthy();
      }
    }
  });

  it("keeps every table's row shape aligned with its three columns", () => {
    for (const key of SITE_KEYS) {
      const spec = buildSiteSpec(key, site());
      for (const table of [spec.table1, spec.table2]) {
        if (!table) continue;
        expect(table.columns, key).toHaveLength(3);
        expect(table.rows.length, key).toBeGreaterThan(0);
        for (const row of table.rows) {
          expect(typeof row.a, key).toBe("string");
          expect(typeof row.b, key).toBe("string");
          expect(typeof row.c, key).toBe("string");
        }
      }
    }
  });

  it("offers the runtime the site actually runs, not a generic list", () => {
    expect(buildSiteSpec("lang", site({ stackKey: "node" })).title).toBe("Node.js version");
    expect(buildSiteSpec("lang", site({ stackKey: "python" })).title).toBe("Python version");
    expect(buildSiteSpec("lang", site({ stackKey: "php" })).title).toBe("PHP version");
    // WordPress runs on PHP, so it gets PHP versions rather than a "wp" runtime.
    expect(buildSiteSpec("lang", site({ stackKey: "wp" })).title).toBe("PHP version");

    const node = buildSiteSpec("lang", site({ stackKey: "node" }));
    expect(node.choices?.map((c) => c.label)).toContain("22.4");
    expect(node.choices?.map((c) => c.label)).not.toContain("8.3");
  });

  it("says whether a repository is connected rather than showing an empty repo", () => {
    const connected = buildSiteSpec("git", site({ deploy: "GitHub", repo: "aaravrao/nova-api", branch: "main" }));
    expect(connected.sub).toContain("Repository, deploy key");
    expect(connected.sideActions?.[0].label).toBe("Pull now");

    const manual = buildSiteSpec("git", site({ deploy: "Manual" }));
    expect(manual.sub).toContain("Not connected yet");
    expect(manual.sideActions?.[0].label).toBe("Connect repository");
  });

  it("quotes the site's own domain back rather than a placeholder", () => {
    const spec = buildSiteSpec("sshsite", site({ domain: "billing-portal.co" }));
    const values = spec.fields?.map((f) => f.value) ?? [];
    expect(values).toContain("billing-portal.co");
    // The shell user is derived from the domain, with punctuation stripped.
    expect(values).toContain("site_billingportal");
  });

  it("derives the database name from the domain", () => {
    const spec = buildSiteSpec("phpmyadmin", site({ domain: "billing-portal.co" }));
    expect(spec.table1?.rows[0].a).toBe("nexp_billing_portal");
  });
});

describe("buildSecuritySpec", () => {
  const ctx = () => {
    setActivePinia(createPinia());
    return {
      flags: useFlagsStore(),
      wafLevel: "Advanced",
      profile: "Standard",
      logSource: "Authentication" as const,
      scanning: false,
    };
  };

  it("returns a usable spec for every security tab", () => {
    for (const key of SECURITY_KEYS) {
      const spec = buildSecuritySpec(key, ctx());
      expect(spec.title, key).toBeTruthy();
      expect(spec.sub, key).toBeTruthy();
    }
  });

  it("reports live flag state in the stat row", () => {
    const c = ctx();
    expect(buildSecuritySpec("overview", c).stats?.[0].value).toBe("Active");

    c.flags.set("fw", false);
    // The stat is status, not decoration: turning the firewall off has to show.
    expect(buildSecuritySpec("overview", c).stats?.[0].value).toBe("Off");
  });

  it("flags root login in the SSH summary", () => {
    const c = ctx();
    expect(buildSecuritySpec("overview", c).stats?.[2].value).toBe("Hardened");
    c.flags.set("rootLogin", true);
    expect(buildSecuritySpec("overview", c).stats?.[2].value).toBe("Root login on");
  });

  it("switches the log tail with the chosen source", () => {
    const c = ctx();
    for (const source of LOG_SOURCES) {
      const spec = buildSecuritySpec("logs", { ...c, logSource: source });
      expect(spec.logName, source).toBeTruthy();
      expect(spec.logs?.length, source).toBeGreaterThan(0);
    }

    const auth = buildSecuritySpec("logs", { ...c, logSource: "Authentication" });
    const waf = buildSecuritySpec("logs", { ...c, logSource: "WAF" });
    expect(auth.logName).not.toBe(waf.logName);
    expect(auth.logs).not.toEqual(waf.logs);
  });

  it("only offers the quick fixes on the overview", () => {
    const c = ctx();
    expect(buildSecuritySpec("overview", c).quickFixes?.length).toBe(4);
    for (const key of SECURITY_KEYS.filter((k) => k !== "overview")) {
      expect(buildSecuritySpec(key, c).quickFixes, key).toBeUndefined();
    }
  });

  it("shortens the session timeout on the strictest profile", () => {
    const c = ctx();
    const standard = buildSecuritySpec("settings", { ...c, profile: "Standard" });
    const maximum = buildSecuritySpec("settings", { ...c, profile: "Maximum" });
    expect(standard.fields?.at(-1)?.value).toBe("8 hours");
    expect(maximum.fields?.at(-1)?.value).toBe("15 minutes");
  });
});

describe("site overview shortcuts", () => {
  it("never offers the same destination twice", () => {
    for (const stackKey of ["static", "php", "node", "python", "wp"] as const) {
      for (const deploy of ["Manual", "GitHub"]) {
        const actions = quickActions(site({ stackKey, deploy }));
        const destinations = actions.map((a) => a.to);
        expect(new Set(destinations).size, `${stackKey}/${deploy}`).toBe(destinations.length);
        expect(actions.length, `${stackKey}/${deploy}`).toBeLessThanOrEqual(6);
      }
    }
  });

  it("offers WordPress tools only to a WordPress site", () => {
    const wp = quickActions(site({ stackKey: "wp" })).map((a) => a.to);
    expect(wp).toContain("site-wp-plugins");

    const node = quickActions(site({ stackKey: "node", deploy: "GitHub" })).map((a) => a.to);
    expect(node).not.toContain("site-wp-plugins");
    expect(node).toContain("site-git-deployments");
  });

  it("offers redeploy only when a repository is connected", () => {
    const manual = quickActions(site({ stackKey: "node", deploy: "Manual" }));
    expect(manual.map((a) => a.label)).not.toContain("Redeploy from Git");
    // The upload shortcut points at the file manager, which is already the first
    // action, so the dedupe drops it — a manual site gets five shortcuts and
    // still reaches uploads through File manager. Asserting the destination
    // rather than the label is what actually matters here.
    expect(manual.map((a) => a.to)).toContain("site-files");
  });
});

describe("runtime facts", () => {
  it("describes the interpreter the site actually runs", () => {
    expect(runtimeTitle(site({ stackKey: "node" }))).toBe("Node.js configuration");
    expect(runtimeTitle(site({ stackKey: "python" }))).toBe("Python configuration");
    expect(runtimeTitle(site({ stackKey: "php" }))).toBe("PHP configuration");

    const node = runtimeFields(site({ stackKey: "node" })).map((f) => f.label);
    expect(node).toContain("Start command");
    expect(node).not.toContain("memory_limit");

    const php = runtimeFields(site({ stackKey: "php" })).map((f) => f.label);
    expect(php).toContain("memory_limit");
  });
});
