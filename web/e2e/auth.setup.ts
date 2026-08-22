import { expect, test as setup } from "@playwright/test";
import { ADMIN, STORAGE_STATE } from "./helpers";

/**
 * Brings a blank panel to the state every other spec assumes: an administrator
 * exists, they are signed in, and the first-run wizard is finished.
 *
 * This runs once per suite and saves the session cookie, rather than each spec
 * signing in for itself. That is not only about speed — bootstrapping is a
 * one-time act, so a second spec trying it would be refused, and the failure
 * would look like a bug in whatever that spec was actually testing.
 *
 * It doubles as the only end-to-end proof of the first-run path. Nothing else
 * exercises bootstrap → login → setup against a real npd, and it is the one
 * sequence every single installation runs exactly once, with no way to retry it
 * if it is broken.
 */
setup("bootstrap the panel and sign in", async ({ page }) => {
  // A blank panel sends every route to the bootstrap screen, so asking for the
  // dashboard is a fair test of the guard as well as a way to get there.
  await page.goto("/");
  await expect(page).toHaveURL(/\/welcome$/);

  await page.getByLabel("Email").fill(ADMIN.email);
  await page.getByLabel("Username").fill(ADMIN.username);
  await page.getByLabel("Password", { exact: true }).fill(ADMIN.password);
  await page.getByLabel("Confirm password").fill(ADMIN.password);
  await page.getByRole("button", { name: "Create administrator" }).click();

  // Bootstrap signs the new administrator in, and the guard then sends them to
  // the wizard — the panel is running but manages nothing yet.
  await expect(page).toHaveURL(/\/setup$/);

  // The stack is not a question: exactly one web server choice is on offer
  // besides the licensed one, and the baseline is listed rather than asked.
  await expect(page.getByRole("radio", { name: /OpenLiteSpeed/ })).toBeChecked();
  await expect(page.getByText("phpMyAdmin")).toBeVisible();
  await expect(page.getByText("ModSecurity + OWASP CRS")).toBeVisible();

  await page.getByRole("button", { name: "Install and finish" }).click();

  // With no broker wired, provisioning is record-only — the selection persists
  // and the gate lifts, which is exactly what this suite needs.
  await expect(page).toHaveURL(/\/$/);
  await expect(page.locator(".nx-sidebar")).toBeVisible();

  await page.context().storageState({ path: STORAGE_STATE });
});
