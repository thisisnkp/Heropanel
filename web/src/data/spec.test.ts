import { describe, expect, it } from "vitest";
import { createPinia, setActivePinia } from "pinia";

import { buildSiteSpec, type SpecKey } from "./siteSpec";
import {
  buildSecuritySpec,
  LOG_SOURCES,
  profileNote,
  SECURITY_SECTIONS,
  securityIssues,
  type SecurityKey,
} from "./securitySpec";
import { quickActions, runtimeFields, runtimeTitle } from "./siteDetail";
import { catalogLeaves } from "./apps";
import { LANG_VERSIONS } from "./siteSpec";
import { buildSiteNav, isGroup, type SiteNavNode } from "@/config/siteNavigation";
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
    uid: "ste_novaretail",
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
    expect(node.choices?.map((c) => c.label)).toContain("24 LTS");
    expect(node.choices?.map((c) => c.label)).not.toContain("8.4");
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

describe("securityIssues", () => {
  const flags = () => {
    setActivePinia(createPinia());
    return useFlagsStore();
  };

  it("reports what is actually wrong, not a fixed number", () => {
    const f = flags();
    // Defaults: scheduled scanning and real-time protection are both off, and
    // nothing is critical. The sidebar chip shows exactly this.
    const issues = securityIssues(f);
    expect(issues.filter((i) => i.severity === "critical")).toHaveLength(0);
    expect(issues.filter((i) => i.severity === "warning")).toHaveLength(2);
  });

  it("promotes root login to critical the moment it is turned on", () => {
    const f = flags();
    f.set("rootLogin", true);
    const critical = securityIssues(f).filter((i) => i.severity === "critical");
    expect(critical.map((i) => i.label)).toContain("Root SSH login is enabled");
  });

  it("points every issue at the section that fixes it", () => {
    const f = flags();
    for (const key of ["fw", "waf", "fail2ban", "twofa", "rootLogin", "passwordLogin"] as const) {
      f.set(key, key === "rootLogin" || key === "passwordLogin");
    }
    const sections = new Set(SECURITY_SECTIONS.map((s) => s.key));
    for (const issue of securityIssues(f)) {
      expect(sections, issue.label).toContain(issue.key);
    }
  });

  it("says something different for each profile", () => {
    const notes = ["Lite", "Standard", "Maximum"].map(profileNote);
    expect(new Set(notes).size).toBe(3);
    for (const n of notes) expect(n.length).toBeGreaterThan(30);
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

  it("never offers a screen the site's own menu does not have", () => {
    // This is the invariant that a fall-through `else` broke: a Python site was
    // offered "PHP settings", linking to a screen its drawer does not list.
    for (const stackKey of ["static", "php", "node", "python", "wp"] as const) {
      for (const deploy of ["Manual", "GitHub"]) {
        const inDrawer = new Set<string>();
        const walk = (nodes: readonly SiteNavNode[]) => {
          for (const n of nodes) {
            if (isGroup(n)) walk(n.children);
            else inDrawer.add(n.to);
          }
        };
        walk(buildSiteNav({ stackKey, deploy }));

        for (const a of quickActions(site({ stackKey, deploy }))) {
          expect(inDrawer, `${stackKey}/${deploy}: ${a.label}`).toContain(a.to);
        }
      }
    }
  });

  it("leads with the two places files and data actually live", () => {
    for (const stackKey of ["static", "php", "node", "python", "wp"] as const) {
      const first = quickActions(site({ stackKey })).slice(0, 2).map((a) => a.to);
      expect(first, stackKey).toEqual(["site-files", "site-db"]);
    }
  });

  it("no longer duplicates the logs shortcut the drawer already carries", () => {
    for (const stackKey of ["static", "php", "node", "python", "wp"] as const) {
      expect(quickActions(site({ stackKey })).map((a) => a.to), stackKey).not.toContain("site-logs");
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

describe("the app catalogue", () => {
  const leaves = catalogLeaves();
  const every = leaves.flatMap((c) => c.apps);

  // The panel deleted its nginx and apache renderers and its PostgreSQL
  // capabilities. A card offering one is a promise nothing can keep, and this is
  // where such a card would come back — someone restoring a fixture from the
  // design, which still had all four webservers in it.
  it("offers no engine the panel no longer has code for", () => {
    const webservers = leaves.find((c) => c.key === "webservers");
    expect(webservers?.apps.map((a) => a.name)).toEqual([
      "OpenLiteSpeed",
      "LiteSpeed Enterprise",
    ]);
    for (const a of every) {
      expect(a.name, a.name).not.toMatch(/^(Nginx|Apache|Caddy)\b/);
    }
  });

  // "Installed" is the only badge that makes a claim about a host rather than
  // about the software. It is allowed exactly where the installer or the setup
  // baseline actually puts something, so a new card cannot quietly assert it.
  it("claims Installed only for the default stack", () => {
    const DEFAULT_STACK = new Set([
      "OpenLiteSpeed",
      "PHP 8.4",
      "Composer",
      "Node.js 24",
      "PM2",
      "MariaDB 11.4 LTS",
      "phpMyAdmin",
      "Redis",
      "LiteSpeed Cache",
      "ModSecurity WAF",
      "Fail2Ban",
      "nftables",
      "ClamAV",
      "maldet",
      "Uptime Monitor",
    ]);
    for (const a of every.filter((x) => x.badge === "Installed")) {
      expect(DEFAULT_STACK, `${a.name} claims to be installed`).toContain(a.name);
    }
  });

  // Python is deliberately absent from the default stack: an interpreter arrives
  // when a Python site needs one.
  it("installs no Python interpreter by default", () => {
    const python = leaves.find((c) => c.key === "python");
    expect(python?.apps.filter((a) => a.name.startsWith("Python")).map((a) => a.badge)).not.toContain(
      "Installed",
    );
  });

  // An unpatched interpreter is the one choice on this screen that can quietly
  // hurt someone, so every version card carries its support status and the dead
  // ones say so in words as well as in the chip.
  it("marks every end-of-life runtime", () => {
    for (const key of ["php", "node", "python"] as const) {
      const cat = leaves.find((c) => c.key === key);
      expect(cat, key).toBeDefined();
      // A version card ends in a number after a space ("PHP 8.4", "Node.js 24").
      // Matching a trailing digit alone caught PM2, which has no support line of
      // its own to state.
      const versions = cat!.apps.filter((a) => /\s\d+(\.\d+)?$/.test(a.name));
      expect(versions.length, key).toBeGreaterThan(3);
      for (const v of versions) {
        expect(v.tag, `${v.name} has no support status`).toBeTruthy();
      }
      for (const v of versions.filter((a) => a.tag === "eol")) {
        expect(v.desc, v.name).toMatch(/no upstream patches|support ended/i);
      }
    }
  });

  // Every category whose constraint governs the whole grid states it once, above
  // the cards. Losing the databases note is the expensive one: it is the only
  // place that says installing MySQL replaces MariaDB rather than joining it.
  it("states the constraint that governs each stack category", () => {
    for (const key of ["webservers", "php", "node", "python", "databases"] as const) {
      const cat = leaves.find((c) => c.key === key);
      expect(cat?.note, key).toBeTruthy();
    }
    const db = leaves.find((c) => c.key === "databases");
    expect(db?.note).toMatch(/3306/);
    expect(db?.note).toMatch(/pgAdmin/);
    expect(db?.noteTone).toBe("warning");
  });

  // The catalogue and the site's version selector are two views of one fact:
  // which versions this panel offers. They are separate fixtures, so the only
  // thing keeping them in step is this test — and a site screen offering a
  // version the catalogue does not is how an operator ends up selecting a pool
  // that was never installed.
  it("offers the site the same versions the catalogue lists", () => {
    const versionsOf = (key: string, prefix: string) =>
      leaves
        .find((c) => c.key === key)!
        .apps.filter((a) => a.name.startsWith(prefix + " "))
        .map((a) => a.name.slice(prefix.length + 1));

    expect(LANG_VERSIONS.PHP).toEqual(versionsOf("php", "PHP"));
    expect(LANG_VERSIONS.Python).toEqual(versionsOf("python", "Python"));
    // Node labels carry an "LTS" suffix on the supported lines, so compare the
    // major versions rather than the label text.
    const major = (v: string) => v.split(" ")[0];
    expect(LANG_VERSIONS["Node.js"].map(major)).toEqual(versionsOf("node", "Node.js").map(major));
  });
});
