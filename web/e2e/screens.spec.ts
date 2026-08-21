import { expect, test } from "@playwright/test";

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
  ["/sites/1/overview", "Overview"],
  ["/sites/1/pagespeed", "PageSpeed"],
  ["/sites/1/ssl", "SSL"],
  ["/sites/1/redirects", "Redirections"],
  ["/sites/1/files", "File manager"],
  ["/sites/1/databases", "Databases"],
  ["/sites/1/php", "PHP settings"],
  ["/sites/1/logs", "Logs"],
  ["/sites/1/cron", "Cron jobs"],
  ["/sites/1/danger", "Delete website"],
  ["/sites/2/git/deployments", "Git deployments"],
  ["/sites/2/runtime", "Node.js configuration"],
];

for (const [path, expected] of SCREENS) {
  test(`${path} renders "${expected}"`, async ({ page }) => {
    const errors: string[] = [];
    page.on("pageerror", (e) => errors.push(e.message));

    await page.goto(path);
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
    await page.goto(path);
    await expect(page.locator("body")).not.toContainText("Not ported yet");
  }
});
