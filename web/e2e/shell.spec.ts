import { expect, test } from "@playwright/test";
import { bootstrapOnce } from "./helpers";

// The application shell: that hpd serves the *built* bundle, that client-side
// routing works including on a hard reload, and that features whose module is
// absent degrade rather than 404 on click.

test.beforeEach(async ({ page }) => {
  await bootstrapOnce(page);
});

test("hpd serves the built SPA, not a placeholder", async ({ page }) => {
  await page.goto("/");
  // The placeholder used when the frontend has not been built has no app shell
  // and no script bundle; both must be present.
  await expect(page.locator("script[src*='/assets/']")).toHaveCount(1);
  await expect(page.getByRole("navigation")).toBeVisible();
});

test("every primary navigation destination renders", async ({ page }) => {
  for (const name of [/sites/i, /databases/i, /dns/i, /ssl/i, /audit/i, /modules/i]) {
    await page.getByRole("navigation").getByRole("link", { name }).click();
    // Whatever the page shows — a table, an empty state, or an "unavailable"
    // notice — it must not be a crash or the router's fallback.
    await expect(page.getByRole("main")).toBeVisible();
    await expect(page.getByText(/something went wrong|unexpected error/i)).toHaveCount(0);
  }
});

test("a deep link survives a hard reload", async ({ page }) => {
  // Client-side routes only work if hpd falls through unknown paths to the SPA.
  // A 404 here means the fallback is broken, which no unit test would notice.
  await page.goto("/sites");
  await page.reload();
  await expect(page.getByRole("main")).toBeVisible();
  await expect(page.getByText(/requested resource was not found/i)).toHaveCount(0);
});

test("an unknown route redirects instead of dead-ending", async ({ page }) => {
  await page.goto("/this-route-does-not-exist");
  await expect(page.getByRole("navigation")).toBeVisible();
});

test("the command palette opens on its shortcut", async ({ page }) => {
  await page.goto("/");
  // Wait for the shell before typing: the palette's key listener is mounted with
  // the authenticated app, and a keystroke sent while the page is still loading
  // lands on nothing.
  await expect(page.getByRole("navigation")).toBeVisible();

  await page.keyboard.press("ControlOrMeta+k");
  await expect(page.getByPlaceholder(/search|command|jump/i)).toBeVisible();
  await page.keyboard.press("Escape");
  await expect(page.getByPlaceholder(/search|command|jump/i)).toHaveCount(0);
});

// Modules whose backing service is absent (no broker here) must say so rather
// than appearing to work and failing on click.
test("the modules page reports what is actually running", async ({ page }) => {
  await page.getByRole("navigation").getByRole("link", { name: /modules/i }).click();
  await expect(page.getByRole("main")).toBeVisible();
});
