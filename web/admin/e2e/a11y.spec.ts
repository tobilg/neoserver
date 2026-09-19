import { apiURL, gotoConsole, sessionHeaders } from "./fixtures";
import { styleTemplate } from "../src/features/styles/style-templates";
import AxeBuilder from "@axe-core/playwright";
import { expect, test, type Page } from "@playwright/test";
import { readFixture, signInWithToken } from "./fixtures";

// Sign-in uses a real bootstrap credential. Keep it out of recordings.
test.use({ trace: "off", video: "off", screenshot: "off" });

/**
 * PRD §11.6 quality floor: no critical accessibility violations on any
 * top-level route, in both themes.
 */

async function scan(page: Page) {
  const results = await new AxeBuilder({ page })
    .withTags(["wcag2a", "wcag2aa", "wcag21a", "wcag21aa", "wcag22aa"])
    .analyze();
  return results.violations.filter(
    (violation) =>
      violation.impact === "critical" || violation.impact === "serious",
  );
}

async function setTheme(page: Page, theme: "light" | "dark") {
  await page.emulateMedia({ colorScheme: theme, reducedMotion: "reduce" });
  await page.evaluate((value) => {
    // Inspect final colors/layout. Do not wait on decorative/paused animations
    // or background-frame callbacks after axe's auxiliary pages have run.
    if (!document.getElementById("axe-static-theme")) {
      const style = document.createElement("style");
      style.id = "axe-static-theme";
      style.textContent =
        "*, *::before, *::after { transition: none !important; animation: none !important; }";
      document.head.append(style);
    }
    document.documentElement.classList.toggle("dark", value === "dark");
    void getComputedStyle(document.body).color;
  }, theme);
}

test.describe("accessibility", () => {
  test("populated editor and Identity wrap and remain keyboard accessible on a narrow screen", async ({
    page,
  }) => {
    const { bootstrapToken, workspace } = await readFixture();
    await signInWithToken(page, bootstrapToken);
    const name = `accessible-points-${Date.now()}`;
    const response = await page.request.post(
      apiURL(`/api/v1/workspaces/${workspace}/styles`),
      {
        headers: await sessionHeaders(page),
        data: {
          name,
          format: "sld_1.0.0",
          body: styleTemplate("sld_1.0.0", "point"),
        },
      },
    );
    expect(response.status()).toBe(201);
    await page.setViewportSize({ width: 390, height: 844 });
    await gotoConsole(page, `/workspaces/${workspace}/styles`);
    await page.getByLabel("Style", { exact: true }).click();
    await page.getByRole("option", { name, exact: true }).click();
    await expect(page.getByRole("textbox", { name: "SLD XML" })).toContainText(
      "PointSymbolizer",
    );
    // Assign to layer, Apply preview and Layout sit between the style choosers
    // and the panels; the editor stays reachable from the last of them.
    await page.getByLabel("Layout", { exact: true }).focus();
    await page.keyboard.press("Tab");
    await expect(
      page.getByRole("tab", { name: "Editor", exact: true }),
    ).toBeFocused();
    await page.keyboard.press("Tab");
    await expect(page.getByRole("textbox", { name: "SLD XML" })).toBeFocused();
    for (const theme of ["light", "dark"] as const) {
      await setTheme(page, theme);
      expect(await scan(page)).toEqual([]);
    }
    const assets = page.getByRole("table", {
      name: "Style assets, scroll to see all columns",
    });
    await assets.focus();
    await expect(assets).toBeFocused();
    await gotoConsole(page, "/identity");
    await expect(page.getByText("Redirect URI", { exact: true })).toBeVisible();
    const uri = page
      .getByText("Redirect URI", { exact: true })
      .locator("..")
      .locator("code");
    expect(
      await uri.evaluate((node) => node.scrollWidth <= node.clientWidth),
    ).toBe(true);
    for (const theme of ["light", "dark"] as const) {
      await setTheme(page, theme);
      expect(await scan(page)).toEqual([]);
    }
  });
  for (const theme of ["light", "dark"] as const) {
    test(`login page has no critical violations (${theme})`, async ({
      page,
    }) => {
      await gotoConsole(page, "/login");
      await expect(page.getByRole("heading", { level: 1 })).toBeVisible();
      await setTheme(page, theme);
      expect(await scan(page)).toEqual([]);
    });
  }

  // 820px is the width where the shell keeps the sidebar but tables have
  // already folded into cards -- the layout neither desktop nor phone covers.
  for (const width of [undefined, 820] as const) {
    test(`authenticated routes have no critical violations${width ? ` at ${width}px` : ""}`, async ({
      page,
    }) => {
      test.setTimeout(90_000); // 19 routes × two complete axe scans.
      const { bootstrapToken, workspace } = await readFixture();
      await signInWithToken(page, bootstrapToken);
      if (width) await page.setViewportSize({ width, height: 1180 });

      const routes = [
        "/workspaces",
        "/operations",
        "/audit",
        "/roles",
        "/identity",
        "/tile-matrix-sets",
        `/workspaces/${workspace}`,
        `/workspaces/${workspace}/stores`,
        `/workspaces/${workspace}/layers`,
        `/workspaces/${workspace}/layer-groups`,
        `/workspaces/${workspace}/styles`,
        `/workspaces/${workspace}/imports`,
        `/workspaces/${workspace}/settings`,
        `/workspaces/${workspace}/endpoints`,
        `/workspaces/${workspace}/caching`,
        `/workspaces/${workspace}/deletions`,
        "/deletions",
        `/workspaces/${workspace}/api-keys`,
        `/workspaces/${workspace}/claim-mappings`,
      ];

      const failures: string[] = [];
      for (const route of routes) {
        await gotoConsole(page, route);
        for (const theme of ["light", "dark"] as const) {
          await setTheme(page, theme);
          const violations = await scan(page);
          if (violations.length > 0) {
            failures.push(
              `${route} (${theme}): ${violations
                .map(
                  (v) =>
                    `${v.id}: ${v.nodes.map((node) => `${node.target.join(" ")}: ${node.failureSummary}`).join("; ")}`,
                )
                .join("\n")}`,
            );
          }
        }
      }
      expect(failures, failures.join("\n")).toEqual([]);
    });
  }
});
