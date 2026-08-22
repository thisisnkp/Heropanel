import { expect, type Page } from "@playwright/test";
import path from "node:path";
import { fileURLToPath } from "node:url";

/**
 * The administrator the suite creates and then signs in as.
 *
 * It lives here rather than in auth.setup.ts because Playwright refuses to let
 * one test file import another — and both the setup project and the signed-out
 * specs need these exact credentials.
 */
export const ADMIN = {
  email: "e2e@nexpanel.test",
  username: "e2e",
  // Long enough to clear the panel's minimum, fixed so a failure is reproducible.
  password: "e2e-panel-password-1",
};

/** Where the setup project saves the session every other spec runs with. */
export const STORAGE_STATE = path.join(
  path.dirname(fileURLToPath(import.meta.url)),
  ".auth",
  "state.json",
);

/**
 * Navigate, then wait for the panel to actually be there.
 *
 * `page.goto` resolves when the document has loaded, which is now earlier than
 * the panel exists: App.vue renders nothing until `/auth/status` (and, when
 * signed in, `/auth/me`) have answered, because painting a shell first would
 * flash a signed-in panel for the instant before the guard redirects.
 *
 * Assertions ride that out on their own — they retry for ten seconds. **Actions
 * do not.** A `keyboard.press` or a `click` fired in the gap goes to a document
 * with no listeners attached and is simply lost, and the failure that follows
 * points at the control rather than at the race.
 *
 * `#nx-main` is the marker because every shell renders it — desktop, mobile,
 * standalone and the pre-session screens alike — so this works for the login
 * form and the file-manager window as well as the panel proper.
 */
export async function gotoPanel(page: Page, url: string) {
  // The navigation response is handed back so callers can still assert on the
  // status npd answered with — the suite's core claim is that npd itself serves
  // the SPA, and that is only checkable from the response.
  const response = await page.goto(url);
  await expect(page.locator("#nx-main")).toBeVisible();
  return response;
}
