import { expect, test } from "@playwright/test";
import { gotoPanel } from "./helpers";

/**
 * Every screen, against a real npd.
 *
 * The claim is narrow and worth making: each route in the shipped bundle
 * renders its own content rather than a blank page, an error boundary, or
 * another screen's. A blank screen and a broken screen look identical in a
 * demo, and this is the suite that tells them apart.
 */

const SCREENS: readonly (readonly [string, string])[] = [
  ["/", "Good afternoon"],
  ["/websites", "Websites"],
  ["/mail", "Mail"],
  ["/domains", "Domains"],
  ["/dns", "DNS & nameservers"],
  ["/activity", "Activity"],
  ["/notifications", "Notifications"],
  ["/backups", "Backups"],
  ["/openclaw", "OpenClaw"],
  ["/n8n", "n8n"],
  ["/docker", "Containers"],
  ["/compose", "Compose"],
  ["/settings", "Panel settings"],
  ["/billing", "License & billing"],
  ["/api-tokens", "API"],
  ["/temp-access", "Temporary access"],
  ["/apps/installed", "Installed apps"],
  ["/apps/install", "Webservers"],
  ["/apps/licenses", "Paid app licenses"],
  ["/security/overview", "Security overview"],
  ["/security/firewall", "Firewall"],
  ["/security/waf", "Web application firewall"],
  ["/security/malware", "Malware scanner"],
  ["/security/ssh", "SSH security"],
  ["/security/updates", "Security updates"],
  ["/security/login", "Login protection"],
  ["/security/logs", "Security logs"],
  ["/security/settings", "Security settings"],
  ["/sites/e2e-php/overview", "Overview"],
  ["/sites/e2e-php/pagespeed", "PageSpeed"],
  ["/sites/e2e-php/ssl", "SSL"],
  ["/sites/e2e-php/redirects", "Redirections"],
  ["/sites/e2e-php/files", "File manager"],
  ["/sites/e2e-php/databases", "Databases"],
  ["/sites/e2e-php/php", "PHP settings"],
  ["/sites/e2e-php/logs", "Logs"],
  ["/sites/e2e-php/cron", "Cron jobs"],
  ["/sites/e2e-php/danger", "Delete website"],
  ["/sites/e2e-node/git/deployments", "Git deployments"],
  ["/sites/e2e-node/runtime", "Node.js configuration"],
];

for (const [path, expected] of SCREENS) {
  test(`${path} renders "${expected}"`, async ({ page }) => {
    const errors: string[] = [];
    page.on("pageerror", (e) => errors.push(e.message));

    await gotoPanel(page, path);
    await expect(page.locator("body")).toContainText(expected);

    // Exactly one <h1>: the layouts deliberately do not stack a generic heading
    // above a specific one, so there is a single answer to "where am I".
    await expect(page.locator("h1")).toHaveCount(1);
    expect(errors).toEqual([]);
  });
}

test("no screen is left unported", async ({ page }) => {
  // The port used a loud placeholder rather than an empty page. If one survives
  // into a build, this fails instead of shipping a screen that looks finished.
  for (const [path] of SCREENS) {
    await gotoPanel(page, path);
    await expect(page.locator("body")).not.toContainText("Not ported yet");
  }
});
