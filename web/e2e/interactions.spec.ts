import { expect, test } from "@playwright/test";

/**
 * The flows a screenshot cannot prove: the create-site wizard's gating, the
 * typed-confirmation delete, the file manager's trash round-trip, and the
 * responsive shell swap.
 *
 * These are the places where getting it wrong is expensive — a delete that
 * fires on the wrong row, a wizard that produces a half-configured site — so
 * they are driven end to end rather than asserted on markup.
 */

test.describe("create a website", () => {
  test("each step is gated on being answerable", async ({ page }) => {
    await page.goto("/websites");

    await page.getByRole("button", { name: /Add Website/ }).click();
    await page.locator(".list__menu-item").filter({ hasText: "Node.js" }).click();

    const dialog = page.getByRole("dialog");
    await expect(dialog).toContainText("New Node.js website");

    const next = page.getByRole("button", { name: "Next" });
    await expect(next).toBeDisabled();

    await dialog.locator("input").first().fill("shop.example.com");
    await expect(next).toBeEnabled();
    await next.click();

    const create = page.getByRole("button", { name: "Create website" });
    await expect(dialog).toContainText("How will files get here?");
    await expect(create).toBeDisabled();

    await page.locator(".wiz__source").filter({ hasText: "GitHub deploy" }).click();
    // A GitHub source with no repository is not a site anyone can deploy.
    await expect(create).toBeDisabled();

    await dialog.locator("input").first().fill("aaravrao/shop");
    await expect(create).toBeEnabled();
    await create.click();

    await expect(page.getByRole("dialog")).toHaveCount(0);
    await expect(page.locator(".nx-table__row").first()).toContainText("shop.example.com");
  });

  test("a WordPress site is not offered a repository", async ({ page }) => {
    await page.goto("/websites");
    await page.getByRole("button", { name: /Add Website/ }).click();
    await page.locator(".list__menu-item").filter({ hasText: "WordPress" }).click();

    const dialog = page.getByRole("dialog");
    await dialog.locator("input").first().fill("blog.example.com");
    await page.getByRole("button", { name: "Next" }).click();

    // WordPress is installed by the panel, so "GitHub deploy" would be a dead end.
    await expect(dialog).toContainText("Manual upload");
    await expect(dialog).not.toContainText("GitHub deploy");
  });

  test("Escape closes the add menu", async ({ page }) => {
    await page.goto("/websites");
    await page.getByRole("button", { name: /Add Website/ }).click();
    await expect(page.locator(".nx-menu__panel")).toBeVisible();
    await page.keyboard.press("Escape");
    await expect(page.locator(".nx-menu__panel")).toHaveCount(0);
  });
});

test.describe("deleting a website", () => {
  test("requires the domain typed back, exactly", async ({ page }) => {
    await page.goto("/websites");

    const rowCount = await page.locator(".nx-table__row").count();
    await page.locator(".nx-table__row").first().getByRole("button", { name: "Delete" }).click();

    const confirm = page.getByRole("button", { name: "Delete website" });
    await expect(confirm).toBeDisabled();

    const input = page.getByRole("dialog").locator("input").first();
    await input.fill("novaretail");
    await expect(confirm).toBeDisabled();

    await input.fill("novaretail.in");
    await expect(confirm).toBeEnabled();
    await confirm.click();

    await expect(page.locator(".nx-table__row")).toHaveCount(rowCount - 1);
    const names = await page.locator(".nx-table__row .list__name").allInnerTexts();
    expect(names.map((n) => n.trim())).not.toContain("novaretail.in");
  });
});

test.describe("file manager", () => {
  test("navigates, searches across folders, and edits", async ({ page }) => {
    await page.goto("/sites/1/files");
    await expect(page.locator(".fm__row")).toHaveCount(9);

    // Tools stay disabled with nothing selected — no silent no-ops.
    const rename = page.locator(".fm__tool").filter({ hasText: "Rename" });
    await expect(rename).toBeDisabled();
    await page.locator(".fm__row").first().click();
    await expect(rename).toBeEnabled();

    await page.locator(".fm__row").filter({ hasText: "wp-content" }).dblclick();
    await expect(page.locator(".fm__crumb")).toHaveCount(3);
    await page.locator(".fm__crumb").nth(1).click();
    await expect(page.locator(".fm__row")).toHaveCount(9);

    // A name that lives in another folder is still findable.
    await page.locator(".fm__search input").fill("plugins");
    await expect(page.locator(".fm__row").first().locator(".fm__tag")).toContainText("in ");
    await page.locator(".fm__search input").fill("");
    await expect(page.locator(".fm__row")).toHaveCount(9);

    // Wait for the specific row, not just the count: the count assertion can
    // resolve on the first poll after the list re-renders, and a double-click
    // whose two clicks straddle Vue replacing the node is not a double-click.
    const config = page.locator(".fm__row").filter({ hasText: "wp-config.php" });
    await expect(config).toBeVisible();
    await config.dblclick();
    await expect(page.locator(".ed")).toBeVisible();
    await expect(page.locator("body")).toContainText("read-only");
    await page.getByRole("button", { name: /Back to files/ }).click();
    await expect(page.locator(".fm__row")).toHaveCount(9);
  });

  test("delete goes to trash and restore puts it back", async ({ page }) => {
    await page.goto("/sites/1/files");

    await page.locator(".fm__row").filter({ hasText: "robots.txt" }).click();
    await page.getByRole("button", { name: "Move to trash" }).first().click();
    await page.getByRole("dialog").getByRole("button", { name: "Move to trash" }).click();
    await expect(page.locator(".fm__row")).toHaveCount(8);

    await page.locator(".fm__side-item").filter({ hasText: "Trash" }).click();
    await expect(page.locator("body")).toContainText("robots.txt");

    await page.locator(".fm__row").filter({ hasText: "robots.txt" })
      .getByRole("button", { name: "Restore" }).click();

    await page.locator(".fm__side-item").filter({ hasText: "public_html" }).click();
    // Back in the folder it came from, not wherever happened to be open.
    await expect(page.locator(".fm__row")).toHaveCount(9);
    await expect(page.locator("body")).toContainText("robots.txt");
  });
});

test.describe("per-stack screens", () => {
  test("a WordPress site and a Node site get different overviews", async ({ page }) => {
    await page.goto("/sites/1/overview");
    await expect(page.locator(".shead__wp")).toHaveCount(1);
    await expect(page.locator("body")).toContainText("Pending updates");

    await page.goto("/sites/2/overview");
    await expect(page.locator(".shead__wp")).toHaveCount(0);
    await expect(page.locator("body")).toContainText("Last deploy");
  });

  test("the version screen offers the right runtime", async ({ page }) => {
    await page.goto("/sites/2/version");
    await expect(page.locator("body")).toContainText("Node.js version");
    await expect(page.locator("body")).toContainText("22.4");

    await page.goto("/sites/1/version");
    await expect(page.locator("body")).toContainText("PHP version");
  });

  test("a site with no repository is offered the setup screen, not an empty table", async ({ page }) => {
    await page.goto("/sites/1/git/deployments");
    await expect(page.locator("body")).toContainText("No repository connected");
  });
});

test.describe("responsive shell", () => {
  test("swaps to the mobile chrome and back", async ({ page }) => {
    await page.setViewportSize({ width: 390, height: 844 });
    await page.goto("/");
    await expect(page.locator(".nx-tabbar")).toBeVisible();
    await expect(page.locator(".nx-sidebar")).toHaveCount(0);

    const tabs = await page.locator(".nx-tabbar__label").allInnerTexts();
    expect(tabs).toEqual(["Home", "Websites", "Security", "Activity", "More"]);

    // Every tab target is at least 44px tall.
    const short = await page.evaluate(
      () => [...document.querySelectorAll(".nx-tabbar__tab")].filter((e) => e.getBoundingClientRect().height < 44).length,
    );
    expect(short).toBe(0);

    await page.setViewportSize({ width: 1440, height: 900 });
    await expect(page.locator(".nx-sidebar")).toBeVisible();
    await expect(page.locator(".nx-tabbar")).toHaveCount(0);
  });

  test("no screen scrolls horizontally at 360px", async ({ page }) => {
    await page.setViewportSize({ width: 360, height: 900 });
    for (const path of ["/", "/websites", "/security/firewall", "/sites/1/files", "/sites/1/php", "/dns"]) {
      await page.goto(path);
      const blown = await page.evaluate(() => {
        const bad: string[] = [];
        for (const el of document.querySelectorAll("body *")) {
          if (el.scrollWidth <= el.clientWidth + 1) continue;
          let clipped = false;
          for (let a: Element | null = el; a; a = a.parentElement) {
            const ox = getComputedStyle(a).overflowX;
            if (ox === "auto" || ox === "scroll" || ox === "hidden") {
              clipped = true;
              break;
            }
          }
          if (!clipped) bad.push(el.tagName + "." + String(el.className).slice(0, 40));
        }
        return bad;
      });
      expect(blown, `overflow at ${path}`).toEqual([]);
    }
  });
});
