import { expect, test } from "@playwright/test";
import { ADMIN } from "./helpers";

/**
 * The gate in front of the panel.
 *
 * Every test here runs **signed out** — `storageState: undefined` throws away
 * the session the setup project saved. That is deliberate and it is also the
 * only safe way to test signing out: the whole suite shares one session cookie,
 * and `POST /auth/logout` revokes the token server-side, so a sign-out on the
 * shared session would log out every test that runs after this file.
 */
test.use({ storageState: { cookies: [], origins: [] } });

test.describe("signed out", () => {
  test("sends a deep link to the login form and comes back to it", async ({ page }) => {
    await page.goto("/sites/e2e-php/databases");
    await expect(page).toHaveURL(/\/login\?next=\/sites\/e2e-php\/databases$/);

    await page.getByLabel("Email").fill(ADMIN.email);
    await page.getByLabel("Password").fill(ADMIN.password);
    await page.getByRole("button", { name: "Sign in" }).click();

    // The point of carrying `next`: someone who followed a link to one screen
    // and was asked to sign in should arrive at that screen, not the dashboard.
    await expect(page).toHaveURL(/\/sites\/e2e-php\/databases$/);
  });

  test("does not carry an off-site next", async ({ page }) => {
    // An open redirect on a login form is worth nothing to this panel and is
    // the classic phishing lever: sign in at the real address, land somewhere
    // else. LoginView accepts only a local path.
    await page.goto("/login?next=https://evil.example.com/");
    await page.getByLabel("Email").fill(ADMIN.email);
    await page.getByLabel("Password").fill(ADMIN.password);
    await page.getByRole("button", { name: "Sign in" }).click();

    await expect(page).toHaveURL(/127\.0\.0\.1:\d+\/$/);
  });

  test("says nothing about which half of the credentials was wrong", async ({ page }) => {
    await page.goto("/login");
    await page.getByLabel("Email").fill(ADMIN.email);
    await page.getByLabel("Password").fill("not-the-password");
    await page.getByRole("button", { name: "Sign in" }).click();

    const alert = page.locator(".nx-callout--danger");
    await expect(alert).toBeVisible();
    // Naming the wrong field turns the form into an account-enumeration oracle:
    // "no such account" and "wrong password" are different answers, and one of
    // them confirms an address exists.
    await expect(alert).toContainText("do not match an account");
    await expect(alert).not.toContainText(/password is|no such|not found/i);
  });

  test("the bootstrap screen is gone once an administrator exists", async ({ page }) => {
    await page.goto("/welcome");
    await expect(page).toHaveURL(/\/login$/);
  });

  test("signing out returns to the login form and stays there", async ({ page }) => {
    await page.goto("/login");
    await page.getByLabel("Email").fill(ADMIN.email);
    await page.getByLabel("Password").fill(ADMIN.password);
    await page.getByRole("button", { name: "Sign in" }).click();
    await expect(page.locator(".nx-sidebar")).toBeVisible();

    await page.getByRole("button", { name: "Sign out" }).click();
    await expect(page).toHaveURL(/\/login$/);

    // The session must be gone on the server too, not just forgotten by the
    // browser. Asking for a screen again is the honest check: a cookie the
    // server still honours would let this straight through.
    await page.goto("/websites");
    await expect(page).toHaveURL(/\/login\?next=\/websites$/);
  });
});
