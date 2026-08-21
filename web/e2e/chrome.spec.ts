import { expect, test } from "@playwright/test";

/**
 * The shell's scoped contexts and its global overlays.
 *
 * These are the parts of the design that a per-screen test cannot reach: which
 * left column a route gets, whether the site switcher actually switches, and
 * whether the three things anchored to the shell — search, the assistant, the
 * job tray — are wired to anything. Every one of them was either missing or a
 * dead control before, which is exactly the class of gap a screenshot hides.
 */

test.describe("scoped sidebars", () => {
  test("each context replaces the global sidebar and keeps the rail", async ({ page }) => {
    for (const [path, label] of [
      ["/sites/1/cron", "Site sections"],
      ["/security/firewall", "Security sections"],
      ["/apps/installed", "Apps sections"],
      ["/dns?domain=novaretail.in", "Domain sections"],
    ] as const) {
      await page.goto(path);
      await expect(page.locator(`aside[aria-label="${label}"]`), path).toBeVisible();
      // The global sidebar is gone, and the rail is there instead — otherwise a
      // scoped context is a dead end with no way back to the rest of the panel.
      await expect(page.locator(".nx-sidebar"), path).toHaveCount(0);
      await expect(page.locator(".nx-rail"), path).toBeVisible();
    }
  });

  test("the zone picker is not a context, but a picked zone is", async ({ page }) => {
    // With no domain chosen there is nothing to name in a sidebar; claiming a
    // context here would put a switcher above an empty editor.
    await page.goto("/dns");
    await expect(page.locator(".nx-sidebar")).toBeVisible();
    await expect(page.locator('aside[aria-label="Domain sections"]')).toHaveCount(0);

    await page.goto("/dns?domain=brightlabs.dev");
    await expect(page.locator('aside[aria-label="Domain sections"]')).toBeVisible();
  });

  test("the security chips count live state, not a constant", async ({ page }) => {
    await page.goto("/security/ssh");
    const sidebar = page.locator('aside[aria-label="Security sections"]');
    // Two warnings out of the box: scheduled scanning and real-time protection
    // are both off in the defaults, and the chip says so rather than saying the
    // design's constant.
    await expect(sidebar).toContainText("2 warnings");
    await expect(sidebar).not.toContainText("critical");

    // The switch is a real checkbox, visually hidden behind its track — so it is
    // named for assistive tech but clicked through its label, exactly as a user
    // does. Asserting the name separately is the point: until NxToggle forwarded
    // attributes to the input, every security switch was an unnamed checkbox.
    await expect(page.getByRole("switch", { name: "Root login" })).toHaveCount(1);

    // Turning root login on has to move the number, or the chip is decoration.
    await page.locator(".spec__toggle").filter({ hasText: "Root login" }).locator("label").click();
    await expect(sidebar).toContainText("1 critical");
    await expect(sidebar.locator(".sec__chip.is-critical")).toHaveAttribute(
      "title",
      /Root SSH login is enabled/,
    );
  });

  test("the apps sidebar carries the catalogue tree", async ({ page }) => {
    await page.goto("/apps/install?category=php");
    const sidebar = page.locator('aside[aria-label="Apps sections"]');
    // The PHP category sits under "Server side scripting", which must arrive
    // expanded when the link points inside it.
    await expect(sidebar).toContainText("Server side scripting");
    await expect(sidebar.getByRole("button", { name: "PHP" })).toHaveAttribute("aria-current", "page");

    await sidebar.getByRole("button", { name: "Databases" }).click();
    await expect(page).toHaveURL(/category=databases/);
    await expect(page.locator("h1")).toContainText("Databases");
  });
});

test.describe("website switcher", () => {
  test("switches site and stays on the same section", async ({ page }) => {
    await page.goto("/sites/1/cron");
    await expect(page.locator("body")).toContainText("Cron jobs");

    await page.getByRole("button", { name: "Switch website" }).click();
    await page.getByPlaceholder("Search websites").fill("api");
    await page.locator(".nx-sw__item").first().click();

    // Same section, different site: comparing two sites' cron jobs should not
    // mean going back to the list and drilling in again.
    await expect(page).toHaveURL(/\/sites\/2\/cron$/);
    await expect(page.locator("body")).toContainText("Cron jobs");
  });

  test("falls back to the overview when the section cannot exist there", async ({ page }) => {
    // Site 1 is WordPress; site 2 is not, and has no plugins screen.
    await page.goto("/sites/1/wp/plugins");
    await page.getByRole("button", { name: "Switch website" }).click();
    await page.getByPlaceholder("Search websites").fill("api");
    await page.locator(".nx-sw__item").first().click();

    await expect(page).toHaveURL(/\/sites\/2\/overview$/);
  });

  test("says so when nothing matches, rather than showing an empty list", async ({ page }) => {
    await page.goto("/sites/1/overview");
    await page.getByRole("button", { name: "Switch website" }).click();
    await page.getByPlaceholder("Search websites").fill("zzzz");
    await expect(page.locator(".nx-sw__empty")).toContainText("No website matches");

    await page.keyboard.press("Escape");
    await expect(page.locator(".nx-sw__panel")).toHaveCount(0);
  });

  test("explains why this site's menu looks the way it does", async ({ page }) => {
    await page.goto("/sites/1/overview");
    const drawer = page.locator('aside[aria-label="Site sections"]');
    await expect(drawer).toContainText("SHOWN FOR THIS SITE");
    await expect(drawer).toContainText("WordPress site");

    await page.goto("/sites/2/overview");
    await expect(drawer).toContainText("Node.js site");
  });
});

test.describe("shell overlays", () => {
  test("⌘K opens search and Enter navigates", async ({ page }) => {
    await page.goto("/");
    await page.keyboard.press("ControlOrMeta+k");

    const field = page.getByPlaceholder("Search sites, files, domains");
    await expect(field).toBeFocused();

    await field.fill("brightlabs");
    await expect(page.locator(".pal__hit").first()).toContainText("brightlabs.dev");
    await page.keyboard.press("Enter");
    await expect(page).toHaveURL(/\/sites\/\d+\/overview$/);
  });

  test("search finds screens as well as sites", async ({ page }) => {
    await page.goto("/");
    await page.keyboard.press("ControlOrMeta+k");
    await page.getByPlaceholder("Search sites, files, domains").fill("Containers");
    await page.locator(".pal__hit").first().click();
    await expect(page).toHaveURL(/\/docker$/);
  });

  test("Ask AI opens a panel that states its work before doing it", async ({ page }) => {
    await page.goto("/");
    await page.getByRole("button", { name: "Ask AI" }).click();

    const panel = page.getByRole("complementary", { name: "NexPanel AI" });
    await expect(panel).toBeVisible();
    // The four things that make this safe to ship on a control panel.
    for (const caption of ["WHAT WILL CHANGE", "WHAT IT TOUCHES", "RISK LEVEL", "HOW TO UNDO"]) {
      await expect(panel, caption).toContainText(caption);
    }
    await expect(panel.getByRole("button", { name: "Preview only" })).toBeVisible();

    await panel.getByRole("button", { name: "Close AI panel" }).click();
    await expect(panel).toHaveCount(0);
  });

  test("applying a proposal reports into the job tray, not a toast", async ({ page }) => {
    await page.goto("/");
    await page.getByRole("button", { name: "Ask AI" }).click();
    await page.getByRole("button", { name: "Apply", exact: true }).click();

    const tray = page.locator(".jt");
    await expect(tray).toBeVisible();
    await expect(tray).toContainText("1 job running");
    await expect(tray).toContainText("Snapshotting database");

    // Still there after navigating: that is the whole reason it is not a toast.
    // Clicked, not page.goto — a full reload would drop the store and prove
    // nothing about whether the tray survives moving around the app.
    await page.locator(".nx-sidebar").getByRole("link", { name: /Websites/ }).first().click();
    await expect(page).toHaveURL(/\/websites$/);
    await expect(page.locator(".jt")).toBeVisible();

    await page.locator(".jt__cancel").first().click();
    await expect(page.locator(".jt")).toHaveCount(0);
  });

  test("a deploy is a job, not a message that disappears", async ({ page }) => {
    await page.goto("/sites/2/overview");
    await page.getByRole("button", { name: /Redeploy/ }).first().click();
    await expect(page.locator(".jt")).toContainText("api.novaretail.in");
  });
});

test.describe("the file manager is its own window", () => {
  test("renders with no panel chrome at all", async ({ page }) => {
    await page.goto("/sites/1/files");

    // Not "the drawer is hidden" — none of the panel chrome is rendered. The
    // design opens this as a separate document precisely so the tool gets the
    // whole window; a hidden-but-mounted sidebar would still take the space.
    for (const chrome of [".nx-rail", ".nx-sidebar", ".nx-ctx", ".nx-topbar", ".nx-tabbar"]) {
      await expect(page.locator(chrome), chrome).toHaveCount(0);
    }

    // It brings its own: brand, the site it is browsing, and the file list.
    await expect(page.locator("h1")).toHaveText("File manager");
    await expect(page.locator(".fm__domain")).toHaveText("novaretail.in");
    await expect(page.locator(".fm__row")).toHaveCount(9);

    // And it fills the window rather than sitting in a page's content column.
    const height = await page.locator(".fm").evaluate((el) => el.getBoundingClientRect().height);
    const viewport = page.viewportSize()?.height ?? 0;
    expect(height).toBe(viewport);
  });

  test("is opened in a new tab from everywhere that links to it", async ({ page }) => {
    await page.goto("/sites/1/overview");

    const quick = page.locator(".ov__quick").filter({ hasText: "File manager" }).first();
    await expect(quick).toHaveAttribute("target", "_blank");
    // noopener, not just _blank: the opened window must not get a handle back
    // to the panel through window.opener.
    await expect(quick).toHaveAttribute("rel", /noopener/);

    await page.locator("aside .nx-row").filter({ hasText: "Files" }).first().click();
    const drawer = page.locator("aside a.nx-row").filter({ hasText: "File manager" }).first();
    await expect(drawer).toHaveAttribute("target", "_blank");
    await expect(drawer).toHaveAttribute("href", "/sites/1/files");
  });

  test("a direct link is not a dead end", async ({ page }) => {
    // Reached from a bookmark, with no panel tab to go back to.
    await page.goto("/sites/2/files");
    await page.getByRole("link", { name: "Back to panel" }).click();
    await expect(page).toHaveURL(/\/sites\/2\/overview$/);
    // Full panel chrome again on the way out.
    await expect(page.locator(".nx-rail")).toBeVisible();
  });
});

test.describe("chrome details the design specifies", () => {
  test("the sidebar names who is signed in and on what plan", async ({ page }) => {
    await page.goto("/");
    const sidebar = page.locator(".nx-sidebar");
    await expect(sidebar).toContainText("Aarav Rao");
    await expect(sidebar).toContainText("Business plan");
    await expect(sidebar.locator(".nx-sidebar__avatar")).toHaveText("AR");
  });

  test("counts and badges come from what they count", async ({ page }) => {
    await page.goto("/");
    const sidebar = page.locator(".nx-sidebar");
    // Four mailboxes in the fixture, four beside Mail.
    await expect(sidebar.getByRole("link", { name: /Mail/ })).toContainText("4");
    await expect(sidebar.getByRole("link", { name: /OpenClaw/ })).toContainText("running");
    // Nothing critical by default, so no count at all rather than a "0".
    await expect(sidebar.getByRole("link", { name: /^Security/ })).not.toContainText("0");
  });

  test("icons are the outlined face the design draws", async ({ page }) => {
    // The design sets FILL 0 on Material Symbols, so its icons are outlined.
    // Iconify's bare `material-symbols:home` is the FILLED house — a visibly
    // different drawing, and what this build shipped until now. Both exact paths
    // are pinned here because "an icon changed" is otherwise invisible: a filled
    // glyph in an outlined set still looks like a deliberate icon.
    const FILLED_HOME = "M4 21V9l8-6l8 6v12h-6v-7h-4v7z";
    const OUTLINE_HOME = "M6 19h3v-6h6v6h3v-9l-6-4.5L6 10zm-2 2V9l8-6l8 6v12h-7v-6h-2v6zm8-8.75";

    await page.goto("/");
    const home = page.locator(".nx-sidebar").getByRole("link", { name: /^Home/ }).locator("svg path");
    const d = await home.first().getAttribute("d");
    expect(d).toBe(OUTLINE_HOME);
    expect(d).not.toBe(FILLED_HOME);
  });
});
