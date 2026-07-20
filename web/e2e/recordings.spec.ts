import { expect, test } from "@playwright/test";
import { bootstrapOnce } from "./helpers";

// Session recordings must be reachable *without* holding terminal.use.
//
// This is the regression these tests exist for: the recordings list originally
// lived only inside a site's Terminal tab, which is gated on `terminal.use`. The
// permissions were correct and every backend test passed — the feature was
// simply unreachable for the one role it was designed for, an auditor with
// `terminal.recordings.read` and deliberately no shell access. Nothing but a
// browser test can catch a permission that is right in the API and wrong in the
// navigation.

test.beforeEach(async ({ page }) => {
  await bootstrapOnce(page);
});

test("recordings have a top-level destination of their own", async ({ page }) => {
  await page.goto("/");
  await expect(page.getByRole("navigation")).toBeVisible();

  const link = page.getByRole("navigation").getByRole("link", { name: /recordings/i });
  await expect(link).toBeVisible();
  await link.click();

  await expect(page).toHaveURL(/\/recordings$/);
  await expect(page.getByRole("heading", { name: /session recordings/i })).toBeVisible();
  await expect(page.getByText(/something went wrong|unexpected error/i)).toHaveCount(0);
});

test("the recordings page is reached without ever opening a site", async ({ page }) => {
  // The whole point: no site, no Terminal tab, no terminal.use — straight to the
  // transcripts. A deep link must work on a hard load too, not only via the SPA.
  await page.goto("/recordings");
  await expect(page.getByRole("heading", { name: /session recordings/i })).toBeVisible();
  await expect(page.getByText(/requested resource was not found/i)).toHaveCount(0);
});

test("the empty state explains retention rather than looking broken", async ({ page }) => {
  await page.goto("/recordings");
  // No terminal can be opened in this harness (no broker), so the list is empty.
  // An empty audit view has to say why it is empty; a blank page reads as a
  // failure to load, which is the wrong thing to conclude about an audit trail.
  await expect(page.getByText(/no recorded sessions/i)).toBeVisible();
  await expect(page.getByText(/recorded automatically once a terminal is opened/i)).toBeVisible();
});

test("the command palette can jump to recordings", async ({ page }) => {
  await page.goto("/");
  await expect(page.getByRole("navigation")).toBeVisible();

  await page.keyboard.press("ControlOrMeta+k");
  const input = page.getByPlaceholder(/search|command|jump/i);
  await expect(input).toBeVisible();
  await input.fill("recording");
  await page.keyboard.press("Enter");

  await expect(page).toHaveURL(/\/recordings$/);
});
