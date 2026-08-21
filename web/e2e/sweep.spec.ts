import { expect, test, type Page } from "@playwright/test";

/**
 * Every screen the panel links to, found by crawling rather than by a list.
 *
 * screens.spec.ts asserts named screens render their own content; that list is
 * written by hand, so a screen nobody remembers to add is a screen nobody tests.
 * This walks the navigation the way a user would and checks every destination it
 * can actually reach — which also means a route that exists but is linked from
 * nowhere shows up as absent from the count below.
 *
 * The narrow-viewport pass is here because horizontal overflow is the failure
 * that never appears on the machine the screen was written on.
 */

const SEEDS = [
  "/",
  "/websites",
  "/sites/1/overview",
  "/sites/2/overview",
  "/security/overview",
  "/apps/installed",
  "/apps/install",
  "/dns?domain=novaretail.in",
];

/** Every in-app link on the page, normalised to a path. */
async function linksOn(page: Page): Promise<string[]> {
  return page.evaluate(() =>
    [...document.querySelectorAll<HTMLAnchorElement>('a[href^="/"]')]
      .map((a) => a.getAttribute("href") ?? "")
      .filter((h) => h && !h.startsWith("//")),
  );
}

/**
 * Expands every disclosure in the current sidebar first — a collapsed group
 * hides its children's links, and those are exactly the screens least likely to
 * have been looked at.
 */
async function expandAll(page: Page) {
  // Scoped to the nav rows, not to every [aria-expanded] in the aside: the site
  // switcher is also a collapsed disclosure, and opening it drops a panel over
  // the drawer that swallows every click after it.
  //
  // Re-query every time and always click the first still-collapsed disclosure.
  // Clicking a cached list by index toggles the ones already opened back shut.
  for (let i = 0; i < 40; i++) {
    const next = page.locator('aside .nx-row[aria-expanded="false"], aside .nx-nav-item[aria-expanded="false"]').first();
    if (!(await next.count())) return;
    await next.click({ timeout: 2000 }).catch(() => {});
  }
}

async function crawl(page: Page): Promise<string[]> {
  const found = new Set<string>();
  for (const seed of SEEDS) {
    await page.goto(seed);
    await expandAll(page);
    for (const href of await linksOn(page)) found.add(href);
    found.add(seed);
  }
  return [...found].sort();
}

test("every linked screen renders its own content, with one heading and no errors", async ({ page }) => {
  test.setTimeout(240_000);

  const paths = await crawl(page);
  // A floor rather than an exact number: adding a screen should not fail this,
  // but losing a third of the navigation should.
  expect(paths.length, "crawl found too few destinations").toBeGreaterThan(80);

  const failures: string[] = [];

  for (const path of paths) {
    const errors: string[] = [];
    page.on("pageerror", (e) => errors.push(e.message));

    await page.goto(path);

    const headings = await page.locator("h1").count();
    if (headings !== 1) failures.push(`${path}: ${headings} <h1> (want exactly 1)`);

    const body = (await page.locator("body").innerText()).trim();
    if (body.length < 40) failures.push(`${path}: renders almost nothing`);
    if (body.includes("Not ported yet")) failures.push(`${path}: still a placeholder`);
    if (body.includes("Page not found")) failures.push(`${path}: linked but 404s`);
    if (errors.length) failures.push(`${path}: ${errors[0]}`);

    page.removeAllListeners("pageerror");
  }

  expect(failures.join("\n")).toBe("");
});

/**
 * 360px is the phone shell; 901px is the *tightest desktop* — one pixel above
 * the breakpoint, where the icon rail and a 236px context sidebar both appear
 * and leave the least room for content. That second width is where the scoped
 * sidebars can actually push a page sideways, and it is not a size anyone
 * develops at.
 */
for (const width of [360, 901]) {
  test(`no screen scrolls sideways at ${width}px`, async ({ page }) => {
    test.setTimeout(240_000);

    const paths = await crawl(page);
    await page.setViewportSize({ width, height: 900 });

    const failures: string[] = [];

    for (const path of paths) {
      await page.goto(path);

      const blown = await page.evaluate(() => {
        const bad: string[] = [];
        for (const el of document.querySelectorAll("body *")) {
          if (el.scrollWidth <= el.clientWidth + 1) continue;
          // An element wider than its box is fine when something above it
          // scrolls or clips — a deliberate overflow container, not a break.
          let contained = false;
          for (let a: Element | null = el; a; a = a.parentElement) {
            const ox = getComputedStyle(a).overflowX;
            if (ox === "auto" || ox === "scroll" || ox === "hidden") {
              contained = true;
              break;
            }
          }
          if (!contained) bad.push(el.tagName + "." + String(el.className).slice(0, 40));
        }
        return bad;
      });

      if (blown.length) failures.push(path + ": " + blown.join(", "));
    }

    expect(failures.join(" | ")).toBe("");
  });
}
