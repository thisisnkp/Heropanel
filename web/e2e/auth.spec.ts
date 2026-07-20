import { expect, test } from "@playwright/test";
import { ADMIN, bootstrapOnce, login } from "./helpers";

// First-run and sign-in, driven through the browser against a real hpd.
//
// These are the flows every other one depends on, and the ones most likely to
// break invisibly: a routing change, a cookie attribute, or an error shape that
// stops rendering. The API-level tests cannot catch a login form that posts to
// the wrong place or a session cookie the browser refuses to keep.

test.describe("first run and authentication", () => {
  test("a fresh panel offers first-run setup, not a login form", async ({ page }) => {
    await page.goto("/");
    // needs_bootstrap is true until an admin exists, and the app must show the
    // setup screen rather than a login form nobody could yet satisfy.
    await expect(page.getByRole("heading", { name: /set up|create.*admin|welcome/i })).toBeVisible();
  });

  test("bootstrap creates the first administrator and signs them in", async ({ page }) => {
    await bootstrapOnce(page);
    // Landing on the dashboard is the proof the session cookie was set and kept:
    // the app only renders the shell when /auth/me succeeds.
    await expect(page.getByRole("navigation")).toBeVisible();
  });

  test("a wrong password is refused with the server's own message", async ({ page }) => {
    await bootstrapOnce(page);
    await page.context().clearCookies();
    await page.goto("/");

    await page.getByLabel(/email/i).fill(ADMIN.email);
    await page.getByLabel(/password/i).fill("definitely-not-the-password");
    await page.getByRole("button", { name: /sign in|log in/i }).click();

    // The specific message matters: "Login failed." is the client's fallback for
    // a response that was not an error envelope at all, and seeing it here would
    // mean the API was unreachable rather than the credentials wrong.
    await expect(page.getByText(/invalid email or password/i)).toBeVisible();
    await expect(page.getByText(/^login failed\.$/i)).toHaveCount(0);
  });

  test("a signed-in operator can reload without being thrown out", async ({ page }) => {
    await login(page);
    await page.reload();
    await expect(page.getByRole("navigation")).toBeVisible();
  });

  test("signing out returns to the login form", async ({ page }) => {
    await login(page);
    await page.getByRole("button", { name: /sign out|log out/i }).click();
    await expect(page.getByRole("button", { name: /sign in|log in/i })).toBeVisible();
  });
});
