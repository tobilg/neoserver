import AxeBuilder from "@axe-core/playwright";
import type { Locator, Page } from "@playwright/test";
import { test, expect } from "./fixture";

async function inViewport(page: Page, element: Locator) {
  const box = await element.boundingBox();
  const viewport = page.viewportSize()!;
  expect(box).not.toBeNull();
  expect(box!.x).toBeGreaterThanOrEqual(0);
  expect(box!.y).toBeGreaterThanOrEqual(0);
  expect(box!.x + box!.width).toBeLessThanOrEqual(viewport.width + 1);
  expect(box!.y + box!.height).toBeLessThanOrEqual(viewport.height + 1);
}

for (const viewport of [
  { width: 1440, height: 1000 },
  { width: 320, height: 640 },
  { width: 844, height: 390 },
]) {
  test(`discovery and key actions stay inside ${viewport.width}x${viewport.height}`, async ({
    page,
    fixture,
  }) => {
    fixture.discoveryCount = 30;
    await page.setViewportSize(viewport);
    await page.goto("/admin/workspaces/demo/layers");
    await page.getByRole("button", { name: "Add layer", exact: true }).click();
    await page
      .getByRole("button", { name: "Discover layers", exact: true })
      .click();
    const dialog = page.getByRole("dialog");
    await dialog.getByRole("button", { name: "Select all" }).click();
    const publish = dialog.getByRole("button", {
      name: "Publish 30 layers",
      exact: true,
    });
    await expect(publish).toBeEnabled();
    await inViewport(page, dialog);
    await inViewport(page, publish);
    expect(
      await dialog.evaluate((node) => node.scrollWidth <= node.clientWidth + 1),
    ).toBe(true);
    if (viewport.width === 1440)
      expect((await dialog.boundingBox())!.width).toBeGreaterThan(600);
    await page.keyboard.press("Escape");
    await page.goto("/admin/workspaces/demo/api-keys");
    await page.getByRole("button", { name: "Create key", exact: true }).click();
    await inViewport(page, page.getByRole("dialog"));
    await inViewport(
      page,
      page
        .getByRole("dialog")
        .getByRole("button", { name: "Create key", exact: true }),
    );
    await inViewport(
      page,
      page.getByRole("button", { name: "Cancel", exact: true }),
    );
  });
}

for (const theme of ["light", "dark"] as const) {
  test(`published import history is keyboard scrollable and accessible (${theme})`, async ({
    page,
    fixture,
  }) => {
    fixture.job.status = "published";
    await page.emulateMedia({ colorScheme: theme, reducedMotion: "reduce" });
    await page.addInitScript(
      (value) => localStorage.setItem("theme", value),
      theme,
    );
    await page.route("**/imports/i1/history", (route) =>
      route.fulfill({
        json: {
          events: Array.from({ length: 15 }, (_, i) => ({
            id: String(i),
            import_id: "i1",
            workspace_id: "w1",
            created_at: "2026-09-16T12:00:00Z",
            status: "published",
            phase: `step-${i}`,
          })),
        },
      }),
    );
    await page.goto("/admin/workspaces/demo/imports?import=i1");
    const region = page.getByRole("region", { name: "Import event history" });
    await expect(region).toBeVisible();
    // The latest events show first; the rest expand in place, no inner scroll.
    await expect(region.getByText(/^step-/)).toHaveCount(8);
    await expect(region.getByText("step-14")).toBeVisible();
    await region.getByRole("button", { name: "Show all 15 events" }).click();
    await expect(region.getByText(/^step-/)).toHaveCount(15);
    await expect(
      page.getByRole("button", { name: "Roll back publication" }),
    ).toBeVisible();
    await page.waitForTimeout(400);
    expect(
      (
        await new AxeBuilder({ page })
          .withTags(["wcag2a", "wcag2aa", "wcag21aa"])
          .analyze()
      ).violations,
    ).toEqual([]);
  });
}

test("mobile stores and keys keep identity, state and actions reachable", async ({
  page,
  fixture,
}) => {
  void fixture;
  await page.setViewportSize({ width: 320, height: 844 });
  for (const [route, label] of [
    ["stores", "Primary data"],
    ["api-keys", "CI pipeline"],
  ]) {
    await page.goto(`/admin/workspaces/demo/${route}`);
    const action = page.getByRole("button", {
      name: `Actions for ${label}`,
      exact: true,
    });
    await inViewport(page, action);
    await action.press("Enter");
    await expect(action).toHaveAttribute("aria-expanded", "true");
    // One level: every action is an item of the same menu.
    const menu = page.getByRole("group", { name: `Actions for ${label}` });
    await inViewport(page, menu);
    await expect(
      menu.getByRole("button", {
        name: route === "stores" ? "Delete store…" : `Revoke ${label}`,
      }),
    ).toBeVisible();
    expect(
      await page
        .getByRole("table")
        .evaluate((node) => node.scrollWidth <= node.clientWidth + 1),
    ).toBe(true);
    await page.keyboard.press("Escape");
    await expect(action).toHaveAttribute("aria-expanded", "false");
    await expect(action).toBeFocused();
    await page.getByText("Details and status", { exact: true }).click();
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth,
      ),
    ).toBe(true);
  }
});

test("built-in grids expose read-only inspection without mutation controls", async ({
  page,
  fixture,
}) => {
  void fixture;
  await page.goto("/admin/tile-matrix-sets");
  await expect(
    page.getByRole("button", { name: "Delete WebMercatorQuad" }),
  ).toHaveCount(0);
  await page.getByRole("button", { name: "Inspect WebMercatorQuad" }).click();
  await expect(page.getByRole("dialog")).toContainText("read-only");
  await expect(page.getByRole("button", { name: "Save changes" })).toHaveCount(
    0,
  );
});

test("duplicate titles retain unique resource identities", async ({
  page,
  fixture,
}) => {
  fixture.published.push({
    ...fixture.layer,
    id: "l2",
    public_id: "other_roads",
  });
  await page.goto("/admin/workspaces/demo/tile-cache");
  await page.getByRole("button", { name: "New job", exact: true }).click();
  const resource = page.getByLabel("Resource", { exact: true });
  await expect(
    resource.locator("option", { hasText: "Roads (roads, feature)" }),
  ).toHaveCount(1);
  await expect(
    resource.locator("option", { hasText: "Roads (other_roads, feature)" }),
  ).toHaveCount(1);
});

test("style failures remain above the editor, layout follows available width, and assignment is explicit", async ({
  page,
  fixture,
}) => {
  await page.setViewportSize({ width: 1024, height: 900 });
  // An expanded sidebar leaves too little width for the split editor.
  await page.addInitScript(() => localStorage.setItem("sidebar", "open"));
  await page.goto("/admin/workspaces/demo/styles");
  await expect(
    page.getByRole("tab", { name: "Editor", exact: true }),
  ).toBeVisible();
  const assign = page.getByRole("button", { name: "Assign to layer" });
  await assign.click();
  await page.getByLabel("Layer", { exact: true }).selectOption("roads");
  await expect(page.getByRole("dialog")).toContainText("Replace default style");
  await page.getByRole("button", { name: "Confirm assignment" }).click();
  await expect(page.getByRole("dialog")).toBeHidden();
  expect(fixture.layer).toHaveProperty("default_style", "one");
  await page.route("**/styles/one", (route) =>
    route.request().method() === "PUT"
      ? route.fulfill({
          status: 400,
          json: { code: 400, message: "Invalid XML at line 1" },
        })
      : route.fallback(),
  );
  await page.getByLabel("SLD XML").fill("<broken>");
  await page.getByRole("button", { name: /^Save style/ }).click();
  await expect(page.getByRole("alert")).toContainText("Invalid XML");
  await inViewport(page, page.getByRole("alert"));
  await expect(page.getByLabel("SLD XML")).toContainText("<broken>");
  await page.setViewportSize({ width: 320, height: 844 });
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth,
    ),
  ).toBe(true);
});

test("client-key handoff returns to the same layer without putting secrets in URLs", async ({
  page,
  fixture,
}) => {
  void fixture;
  await page.goto("/admin/workspaces/demo/endpoints?layer=roads");
  await page.getByRole("link", { name: "Manage API keys" }).click();
  await expect(page).toHaveURL(/api-keys\?connect=roads/);
  await page.getByRole("button", { name: "Create key", exact: true }).click();
  await page.getByLabel("Key name").fill("Map client");
  await page
    .getByRole("dialog")
    .getByRole("button", { name: "Create key" })
    .click();
  await expect(
    page.getByRole("heading", { name: "Copy this key now" }),
  ).toBeVisible();
  await page.getByLabel("I have stored this key").check();
  await page.getByRole("button", { name: "Done", exact: true }).click();
  await page
    .getByRole("link", { name: "Return to connection examples" })
    .click();
  await expect(page.getByLabel("Published layer")).toHaveValue("roads");
  expect(page.url()).not.toContain("nsk_");
});

test("operations have summaries and one page heading, theme choices are explicit", async ({
  page,
  fixture,
}) => {
  void fixture;
  await page.goto("/admin/operations");
  await expect(page.getByText("Catalog healthy: 0 issues")).toBeVisible();
  await expect(page.getByText(/Using 0 B of 1 MiB/)).toBeVisible();
  await expect(page.getByRole("heading", { level: 1 })).toHaveCount(1);
  // Deletions live on their own page; Operations only summarises them.
  await expect(page.getByText("No deletions need attention.")).toBeVisible();
  await expect(
    page.getByRole("link", { name: "Review deletions" }),
  ).toHaveAttribute("href", "/admin/deletions");
  await page.getByRole("button", { name: /Color theme:/ }).click();
  await page.getByRole("menuitemradio", { name: "Dark", exact: true }).click();
  await expect(page.locator("html")).toHaveClass(/dark/);
  await page.goto("/admin/workspaces/demo/layers");
  await expect(page.locator('a[aria-current="page"]')).toHaveCount(1);
});

test("command palette jumps to pages and publications", async ({
  page,
  fixture,
}) => {
  void fixture;
  await page.goto("/admin/workspaces/demo");
  await expect(page.getByRole("heading", { level: 1 })).toBeVisible();
  await page.keyboard.press("ControlOrMeta+k");
  const palette = page.getByRole("dialog", { name: "Go to" });
  await palette.getByRole("combobox").fill("styles");
  await page.keyboard.press("Enter");
  await expect(page).toHaveURL(/\/workspaces\/demo\/styles$/);
  await expect(palette).toHaveCount(0);
  await page.getByRole("button", { name: /Go to/ }).click();
  await palette.getByRole("combobox").fill("roads");
  await palette.getByRole("option", { name: /Roads/ }).first().click();
  await expect(page).toHaveURL(/\/preview\?layers=roads/);
});

test("preview groups publications by store or by type", async ({
  page,
  fixture,
}) => {
  void fixture;
  await page.goto("/admin/workspaces/demo/preview");
  const controls = page.getByRole("complementary", { name: "Layer controls" });
  // Without a configured basemap the toggle is off-limits and says why.
  const basemap = controls.getByRole("button", { name: "Show basemap" });
  await expect(basemap).toBeDisabled();
  await expect(basemap).toHaveAccessibleDescription(
    /No basemap configured.*Website\.BasemapUrl/,
  );
  // What is drawn is pinned above the catalog.
  const pinned = controls.getByRole("region", { name: "On the map" });
  const roadsSwitch = controls.getByRole("switch", { name: "Show roads" });
  await expect(
    pinned.getByRole("switch", { name: "Show roads" }),
  ).toBeChecked();
  await expect(pinned.getByRole("button", { name: "Fit all" })).toBeVisible();
  await roadsSwitch.click();
  await expect(pinned).toHaveCount(0);
  const store = controls.getByRole("region", { name: "Primary data" });
  await expect(store.getByRole("switch", { name: "Show roads" })).toBeVisible();
  await controls.getByLabel("Find a publication").fill("primary");
  await expect(store).toBeVisible();
  await controls.getByLabel("Find a publication").fill("");
  await controls.getByLabel("Group publications by").selectOption("kind");
  await expect(store).toHaveCount(0);
  await expect(
    controls
      .getByRole("region", { name: "Layers" })
      .getByRole("switch", { name: "Show roads" }),
  ).toBeVisible();
});

test("a layer group previews with its member legends", async ({
  page,
  fixture,
}) => {
  await page.route("**/api/v1/workspaces/demo/layer-groups", (route) =>
    route.fulfill({
      json: {
        layer_groups: [
          {
            id: "g1",
            workspace_id: "w1",
            public_id: "stack",
            title: "Stack",
            enabled: true,
            public: false,
            members: [{ resource: "roads", style: "one" }],
            created_at: "2026-01-01T00:00:00Z",
            updated_at: "2026-01-01T00:00:00Z",
          },
        ],
      },
    }),
  );
  await page.goto("/admin/workspaces/demo/preview?layers=stack&sources=wms");
  const controls = page.getByRole("complementary", { name: "Layer controls" });
  await expect(
    controls.getByText("Legends of the group's member layers:"),
  ).toBeVisible();
  await expect(controls.getByAltText("Legend for roads")).toBeVisible();
  const legends = fixture.wmsRequests.filter((url) =>
    url.includes("GetLegendGraphic"),
  );
  expect(legends.some((url) => /LAYER=roads&STYLE=one/.test(url))).toBe(true);
  expect(legends.some((url) => /LAYER=stack/.test(url))).toBe(false);
});

test("the icon rail keeps collapsible sections reachable", async ({
  page,
  fixture,
}) => {
  void fixture;
  await page.setViewportSize({ width: 1280, height: 900 });
  await page.goto("/admin/workspaces/demo/api-keys");
  const nav = page.getByRole("navigation", { name: "Console" });
  // Collapsed sections stay closed while the sidebar is expanded.
  await expect(nav.getByRole("link", { name: "Audit" })).toBeHidden();
  await expect(nav.getByRole("link", { name: "Caching" })).toBeHidden();
  await page
    .getByRole("button", { name: "Toggle Sidebar", exact: true })
    .first()
    .click();
  for (const name of ["Audit", "Workspaces", "Caching"])
    await expect(nav.getByRole("link", { name })).toBeVisible();
  await expect(nav.getByText("Server administration")).toBeHidden();
  await nav.getByRole("link", { name: "Audit" }).click();
  await expect(page).toHaveURL(/\/admin\/audit$/);
});

test("sidebar and page headers share one bottom edge", async ({
  page,
  fixture,
}) => {
  void fixture;
  await page.setViewportSize({ width: 1280, height: 900 });
  await page.goto("/admin/workspaces/demo/api-keys");
  const bottom = (selector: string) =>
    page
      .locator(selector)
      .first()
      .evaluate((node) => Math.round(node.getBoundingClientRect().bottom));
  const aligned = async () =>
    expect(await bottom('[data-sidebar="header"]')).toBe(
      await bottom("header"),
    );
  await aligned();
  await expect(page.getByRole("combobox", { name: "Workspace" })).toBeVisible();
  await page
    .getByRole("button", { name: "Toggle Sidebar", exact: true })
    .first()
    .click();
  await expect(page.getByRole("combobox", { name: "Workspace" })).toBeHidden();
  await aligned();
});
