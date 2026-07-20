import { expect, type Page } from "@playwright/test";

// Shared setup for the browser suite.
//
// Bootstrapping is a one-time act per datastore, and the specs share one hpd, so
// it has to be idempotent: the first spec that runs creates the admin, the rest
// sign in as them.

export const ADMIN = {
  email: "e2e-admin@heropanel.test",
  username: "e2eadmin",
  password: "playwright-e2e-password-1",
};

/** True when the panel is still showing first-run setup. */
async function needsBootstrap(page: Page): Promise<boolean> {
  const res = await page.request.get("/api/v1/auth/status");
  if (!res.ok()) return false;
  const body = (await res.json()) as { data?: { needs_bootstrap?: boolean } };
  return body.data?.needs_bootstrap === true;
}

/**
 * bootstrapOnce ensures the admin exists and the page is signed in as them.
 * Safe to call from every spec.
 */
export async function bootstrapOnce(page: Page): Promise<void> {
  if (await needsBootstrap(page)) {
    await page.goto("/");
    await page.getByLabel(/email/i).fill(ADMIN.email);
    // The bootstrap form asks for a username; the login form does not.
    const username = page.getByLabel(/username/i);
    if (await username.count()) await username.fill(ADMIN.username);
    await page.getByLabel(/^password/i).fill(ADMIN.password);
    const confirm = page.getByLabel(/confirm/i);
    if (await confirm.count()) await confirm.fill(ADMIN.password);
    await page.getByRole("button", { name: /create|set up|continue/i }).click();
    await expect(page.getByRole("navigation")).toBeVisible();
    return;
  }
  await login(page);
}

/** login signs in as the e2e admin and waits for the app shell. */
export async function login(page: Page): Promise<void> {
  await page.goto("/");
  // Already signed in from a previous navigation in this context.
  if (await page.getByRole("navigation").count()) return;

  await page.getByLabel(/email/i).fill(ADMIN.email);
  await page.getByLabel(/^password/i).fill(ADMIN.password);
  await page.getByRole("button", { name: /sign in|log in/i }).click();
  await expect(page.getByRole("navigation")).toBeVisible();
}
