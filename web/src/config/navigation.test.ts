import { describe, expect, it } from "vitest";

import { MOBILE_TABS, NAV_GROUPS, RAIL_ENTRIES } from "./navigation";
import { buildSiteNav, isGroup, navExplain, type SiteNavNode } from "./siteNavigation";
import { ICONS } from "@/components/ui/icons";
import { APP_CATEGORIES } from "@/data/apps";
import { ATTENTION, NOTIFICATIONS, QUICK_ACTIONS, SCAN_TILES } from "@/data/dashboard";
import { quickActions } from "@/data/siteDetail";
import type { Site } from "@/stores/sites";

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

/**
 * Every icon name in the app resolves to a real glyph.
 *
 * This is the one class of bug that is invisible in review and in a screenshot
 * taken by whoever wrote the screen: a name that is not in the registry renders
 * nothing at all, so the layout still looks deliberate. Collecting the names
 * from the data modules and checking them here catches it at test time.
 */
describe("icon registry", () => {
  const named: [string, string][] = [];

  const collect = (where: string, name: string | undefined) => {
    if (name) named.push([where, name]);
  };

  for (const group of NAV_GROUPS) {
    for (const entry of group.entries) {
      collect("nav:" + entry.label, entry.icon);
      for (const child of entry.children ?? []) collect("nav:" + child.label, child.icon);
    }
  }
  for (const r of RAIL_ENTRIES) collect("rail:" + r.label, r.icon);
  for (const t of MOBILE_TABS) collect("tab:" + t.label, t.icon);
  for (const c of APP_CATEGORIES) {
    collect("cat:" + c.label, c.icon);
    for (const a of c.apps) collect("app:" + a.name, a.icon);
    for (const child of c.children ?? []) {
      collect("cat:" + child.label, child.icon);
      for (const a of child.apps) collect("app:" + a.name, a.icon);
    }
  }
  for (const t of SCAN_TILES) collect("scan:" + t.label, t.icon);
  for (const q of QUICK_ACTIONS) collect("quick:" + q.label, q.icon);
  for (const n of NOTIFICATIONS) collect("notif:" + n.label, n.icon);
  for (const stack of ["static", "php", "node", "python", "wp"] as const) {
    for (const q of quickActions(site({ stackKey: stack, deploy: "GitHub" }))) {
      collect("site-quick:" + q.label, q.icon);
    }
  }
  // Recursive, not two levels: the drawer nests Git inside Advanced, and a
  // shallow walk silently skipped every icon at that depth.
  const walkDrawer = (nodes: readonly SiteNavNode[]) => {
    for (const n of nodes) {
      collect("drawer:" + n.label, n.icon);
      if (isGroup(n)) walkDrawer(n.children);
    }
  };
  for (const stack of ["static", "php", "node", "python", "wp"] as const) {
    for (const deploy of ["Manual", "GitHub"]) walkDrawer(buildSiteNav({ stackKey: stack, deploy }));
  }

  it("collects icon names from every source that stores them as strings", () => {
    expect(named.length).toBeGreaterThan(60);
  });

  it.each(named)("%s uses a glyph that exists (%s)", (_where, name) => {
    expect(ICONS[name]).toBeDefined();
  });
});

describe("navigation model", () => {
  it("has four groups, each with a caption and entries", () => {
    expect(NAV_GROUPS).toHaveLength(4);
    for (const g of NAV_GROUPS) {
      expect(g.label, g.id).toBeTruthy();
      expect(g.entries.length, g.id).toBeGreaterThan(0);
    }
  });

  it("never nests more than one level deep", () => {
    for (const g of NAV_GROUPS) {
      for (const e of g.entries) {
        for (const c of e.children ?? []) {
          expect(c, e.label).not.toHaveProperty("children");
        }
      }
    }
  });

  it("gives every entry a label, an icon and a destination", () => {
    for (const g of NAV_GROUPS) {
      for (const e of [...g.entries, ...g.entries.flatMap((x) => x.children ?? [])]) {
        expect(e.label).toBeTruthy();
        expect(e.icon).toBeTruthy();
        expect(e.to).toBeTruthy();
      }
    }
  });
});

/** Every label in the drawer, at any depth. */
function flatten(groups: readonly { label: string; children?: readonly { label: string; children?: readonly { label: string }[] }[] }[]): string[] {
  const out: string[] = [];
  const walk = (nodes: readonly { label: string; children?: readonly never[] }[]) => {
    for (const n of nodes) {
      out.push(n.label);
      if (n.children) walk(n.children as never);
    }
  };
  walk(groups as never);
  return out;
}

describe("site drawer", () => {
  it("offers the WordPress manager only to a WordPress site", () => {
    const wp = buildSiteNav({ stackKey: "wp", deploy: "Manual" }).map((g) => g.label);
    expect(wp).toContain("WordPress manager");

    const node = buildSiteNav({ stackKey: "node", deploy: "Manual" }).map((g) => g.label);
    expect(node).not.toContain("WordPress manager");
  });

  it("offers Git deployments only when a repository is connected", () => {
    // The drawer nests two levels: Advanced → Git → Git deployments, so a
    // one-level flatten would miss the entry entirely and pass by accident.
    const labels = (deploy: string) => flatten(buildSiteNav({ stackKey: "node", deploy }));

    expect(labels("GitHub")).toContain("Git deployments");
    expect(labels("Manual")).not.toContain("Git deployments");
    // "Setup Git" is always reachable — that is how you connect one.
    expect(labels("Manual")).toContain("Setup Git");
  });

  it("explains the shape of the menu for every stack", () => {
    // The footer card exists so a missing section reads as inapplicable rather
    // than broken; an empty or duplicated sentence would defeat that.
    const seen = new Set<string>();
    for (const stackKey of ["static", "php", "node", "python", "wp"] as const) {
      for (const deploy of ["Manual", "GitHub"]) {
        const text = navExplain({ stackKey, deploy });
        expect(text.length, stackKey).toBeGreaterThan(40);
        seen.add(text);
      }
    }
    // PHP and WordPress each say one thing regardless of deploy mode; static,
    // Node and Python each say two. Ten combinations, eight distinct sentences.
    expect(seen.size).toBe(8);
  });

  it("always offers the overview and the danger zone", () => {
    for (const stackKey of ["static", "php", "node", "python", "wp"] as const) {
      const labels = flatten(buildSiteNav({ stackKey, deploy: "Manual" }));
      expect(labels, stackKey).toContain("Overview");
      expect(labels, stackKey).toContain("Danger zone");
    }
  });
});

describe("dashboard fixtures", () => {
  it("gives every attention item a severity the styles cover", () => {
    for (const a of ATTENTION) {
      expect(["critical", "warning", "info"]).toContain(a.severity);
      expect(a.action).toBeTruthy();
      expect(a.to).toBeTruthy();
    }
  });

  it("keeps notifications distinct from routine activity", () => {
    // Notifications are the subset that needs a decision; if they were the same
    // list, the backup failure would be buried among successful deploys.
    for (const n of NOTIFICATIONS) {
      expect(["critical", "warning"]).toContain(n.severity);
    }
  });
});
