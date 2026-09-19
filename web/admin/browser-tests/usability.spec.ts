import AxeBuilder from "@axe-core/playwright";
import { test, expect } from "./fixture";

for (const width of [320, 390]) {
  test(`publication identity, state and keyboard actions fit at ${width}px`, async ({
    page,
    fixture,
  }) => {
    fixture.services[0].enabled = false;
    await page.setViewportSize({ width, height: 844 });
    await page.goto(
      "/admin/workspaces/demo/layers?layers_hide=public_id,actions",
    );
    const row = page
      .getByRole("row")
      .filter({ has: page.getByRole("button", { name: "Actions for roads" }) });
    await expect(row.getByText("roads", { exact: true })).toBeVisible();
    await expect(
      row.getByText("Restricted", { exact: true }).filter({ visible: true }),
    ).toBeVisible();
    await expect(
      row.getByText("Store off", { exact: true }).filter({ visible: true }),
    ).toBeVisible();
    expect(
      await page
        .getByRole("table")
        .evaluate((table) => table.scrollWidth <= table.clientWidth + 1),
    ).toBe(true);
    const trigger = row.getByRole("button", { name: "Actions for roads" });
    const box = await trigger.boundingBox();
    expect(box!.width).toBeGreaterThanOrEqual(44);
    expect(box!.x + box!.width).toBeLessThanOrEqual(width);
    await trigger.focus();
    await page.keyboard.press("Enter");
    await expect(
      page.getByRole("menuitem", { name: "Edit publication" }),
    ).toBeFocused();
    await page.keyboard.press("Escape");
    await expect(trigger).toBeFocused();
    await page.keyboard.press("Enter");
    await page.keyboard.press("Enter");
    await expect(
      page.getByRole("dialog", { name: "Edit roads" }),
    ).toBeVisible();
    await page.keyboard.press("Escape");
    await expect(trigger).toBeFocused();
    await page.keyboard.press("Enter");
    await page.getByRole("menuitem", { name: "Unpublish…" }).focus();
    await page.keyboard.press("Enter");
    await expect(
      page.getByRole("dialog", { name: "Unpublish roads?" }),
    ).toBeVisible();
    await page
      .getByRole("button", { name: "Cancel", exact: true })
      .press("Enter");
    await expect(trigger).toBeFocused();
  });
}

test("coverage actions use their WMS preview and do not offer a feature-only client link", async ({
  page,
  fixture,
}) => {
  fixture.coverages.push({
    id: "c1",
    service_id: "s1",
    public_id: "elevation",
    source_coverage: "elevation.tif",
    title: "Elevation",
    enabled: true,
    public: false,
  });
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto("/admin/workspaces/demo/coverages");
  await page.getByRole("button", { name: "Actions for elevation" }).click();
  await expect(
    page.getByRole("menuitem", { name: "Connect a client" }),
  ).toHaveCount(0);
  await expect(
    page.getByRole("menuitem", { name: "Preview", exact: true }),
  ).toHaveAttribute("href", /layers=elevation&sources=wms/);
});

test("publication disclosure and resizing preserve common, advanced and unknown fields", async ({
  page,
  fixture,
}) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto("/admin/workspaces/demo/layers");
  await page.getByRole("button", { name: "Actions for roads" }).click();
  await page.getByRole("menuitem", { name: "Edit publication" }).click();
  const dialog = page.getByRole("dialog", { name: "Edit roads" });
  await dialog.getByLabel("Title", { exact: true }).fill("Draft title");
  await expect(dialog.getByLabel("Default CRS (EPSG code)")).toBeHidden();
  await page.setViewportSize({ width: 1280, height: 900 });
  await expect(dialog.getByLabel("Title", { exact: true })).toHaveValue(
    "Draft title",
  );
  const summary = dialog.locator("summary", {
    hasText: "Advanced publication settings",
  });
  await summary.focus();
  await page.keyboard.press("Enter");
  await dialog.getByLabel("Default CRS (EPSG code)").fill("3857");
  await summary.press("Enter");
  await dialog.getByRole("button", { name: "Save publication" }).click();
  await expect(dialog).toBeHidden();
  expect(fixture.layer).toMatchObject({
    title: "Draft title",
    crs_default: 3857,
    unknown_option: "preserve me",
  });
  await expect(page.getByRole("button", { name: "Edit roads" })).toBeFocused();
});

test("publication save errors are announced and reveal advanced fields without losing the draft", async ({
  page,
  fixture,
}) => {
  void fixture;
  await page.route(
    "**/api/v1/workspaces/demo/services/s1/layers/l1",
    async (route) => {
      if (route.request().method() !== "PUT") return route.fallback();
      await route.fulfill({
        status: 400,
        json: { code: 400, message: "CRS is not supported" },
      });
    },
  );
  await page.goto("/admin/workspaces/demo/layers");
  await page.getByRole("button", { name: "Edit roads" }).click();
  const dialog = page.getByRole("dialog", { name: "Edit roads" });
  await dialog.getByLabel("Title", { exact: true }).fill("Keep this draft");
  await dialog.getByRole("button", { name: "Save publication" }).click();
  await expect(dialog.getByRole("alert")).toContainText("CRS is not supported");
  await expect(dialog.getByLabel("Default CRS (EPSG code)")).toBeVisible();
  await expect(dialog.getByLabel("Title", { exact: true })).toHaveValue(
    "Keep this draft",
  );
});

test("mobile publication load errors stay readable and can be retried or cancelled", async ({
  page,
  fixture,
}) => {
  void fixture;
  let fail = true;
  let release: (() => void) | undefined;
  await page.route(
    "**/api/v1/workspaces/demo/services/s1/layers/l1",
    async (route) => {
      if (fail)
        return route.fulfill({
          status: 503,
          json: { code: 503, message: "Publication temporarily unavailable" },
        });
      await new Promise<void>((resolve) => {
        release = resolve;
      });
      await route.fallback();
    },
  );
  await page.setViewportSize({ width: 320, height: 844 });
  await page.goto("/admin/workspaces/demo/layers");
  const trigger = page.getByRole("button", { name: "Actions for roads" });
  await trigger.click();
  await page.getByRole("menuitem", { name: "Edit publication" }).click();
  const dialog = page.getByRole("dialog", { name: "Edit roads" });
  await expect(dialog.getByRole("alert")).toContainText(
    "Publication temporarily unavailable",
  );
  expect(
    await dialog.evaluate((node) => node.scrollWidth <= node.clientWidth),
  ).toBe(true);
  await expect(
    dialog.getByRole("button", { name: "Save publication" }),
  ).toBeDisabled();
  fail = false;
  await dialog.getByRole("button", { name: "Retry", exact: true }).click();
  await expect(dialog.getByRole("status")).toContainText(
    "Loading publication editor",
  );
  await expect.poll(() => Boolean(release)).toBe(true);
  await page.keyboard.press("Escape");
  await expect(trigger).toBeFocused();
  release!();
  await expect(dialog).toBeHidden();
});

test("background catalog refresh retains an open publication draft", async ({
  page,
  fixture,
}) => {
  await page.clock.install();
  await page.goto("/admin/workspaces/demo/layers");
  await page.getByRole("button", { name: "Edit roads" }).click();
  const dialog = page.getByRole("dialog", { name: "Edit roads" });
  await dialog.getByLabel("Title", { exact: true }).fill("Unsaved title");
  fixture.layer.title = "Changed elsewhere";
  // The shared query client keeps data fresh for 20 seconds. Reconnecting
  // within that window deliberately does not refetch it.
  await page.clock.fastForward(21_000);
  const refresh = page.waitForResponse((response) =>
    response.url().endsWith("/services/s1/layers"),
  );
  await page.evaluate(() => {
    window.dispatchEvent(new Event("offline"));
    window.dispatchEvent(new Event("online"));
  });
  await refresh;
  await expect(dialog.getByLabel("Title", { exact: true })).toHaveValue(
    "Unsaved title",
  );
  await dialog.getByRole("button", { name: "Save publication" }).click();
  await expect(dialog).toBeHidden();
  expect(fixture.layer.title).toBe("Unsaved title");
});

for (const theme of ["light", "dark"] as const) {
  test(`mobile publication and style controls expose names, state and keyboard navigation (${theme})`, async ({
    page,
    fixture,
  }) => {
    void fixture;
    await page.setViewportSize({ width: 390, height: 844 });
    await page.addInitScript(
      (value) => localStorage.setItem("theme", value),
      theme,
    );
    await page.emulateMedia({ colorScheme: theme, reducedMotion: "reduce" });
    await page.goto("/admin/workspaces/demo/layers");
    await page.getByRole("button", { name: "Actions for roads" }).click();
    await page.getByRole("menuitem", { name: "Edit publication" }).click();
    const dialog = page.getByRole("dialog", { name: "Edit roads" });
    await expect(
      dialog.getByRole("switch", { name: "Enabled", exact: true }),
    ).toBeChecked();
    await expect(
      dialog.getByRole("switch", { name: "Public access", exact: true }),
    ).not.toBeChecked();
    expect(
      (
        await new AxeBuilder({ page })
          .withTags(["wcag2a", "wcag2aa", "wcag21aa", "wcag22aa"])
          .analyze()
      ).violations,
    ).toEqual([]);
    await page.bringToFront();
    await page.keyboard.press("Escape");
    await page.goto("/admin/workspaces/demo/styles");
    const editor = page.getByRole("textbox", { name: "SLD XML" });
    await expect(editor).toContainText("ORIGINAL_ONE");
    await editor.focus();
    await page.keyboard.press("ControlOrMeta+A");
    await page.keyboard.insertText(
      "<draft>Retain on tab and viewport changes</draft>",
    );
    await expect(editor).toContainText("Retain on tab");
    const editorTab = page.getByRole("tab", { name: "Editor (unsaved)" });
    await editorTab.focus();
    await page.keyboard.press("ArrowRight");
    await expect(
      page.getByRole("tab", { name: "Preview", exact: true }),
    ).toBeFocused();
    await expect(
      page.getByRole("tabpanel", { name: "Preview", exact: true }),
    ).toBeVisible();
    await expect(editor).toBeHidden();
    expect(
      (
        await new AxeBuilder({ page })
          .withTags(["wcag2a", "wcag2aa", "wcag21aa", "wcag22aa"])
          .analyze()
      ).violations,
    ).toEqual([]);
    await page.bringToFront();
    await page.getByRole("tab", { name: "Preview", exact: true }).focus();
    await page.keyboard.press("ArrowLeft");
    await expect(editor).toContainText("Retain on tab");
    await page.setViewportSize({ width: 1280, height: 900 });
    await expect(
      page.getByRole("region", { name: "Style editor", exact: true }),
    ).toBeVisible();
    await expect(
      page.getByRole("region", { name: "Style preview", exact: true }),
    ).toBeVisible();
    await page.setViewportSize({ width: 390, height: 844 });
    await expect(editor).toContainText("Retain on tab");
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth,
      ),
    ).toBe(true);
    await page
      .getByLabel("Preview layer", { exact: true })
      .selectOption("roads");
    await page.getByRole("button", { name: "Apply preview" }).click();
    await expect(
      page.getByRole("tab", { name: "Preview", exact: true }),
    ).toHaveAttribute("aria-selected", "true");
    await page.getByRole("tab", { name: "Editor (unsaved)" }).click();
    await expect(editor).toContainText("Retain on tab");
  });
}

test("initial preview frames data, supports feature inspection, and leaves edits and refreshes in place", async ({
  page,
  fixture,
}) => {
  void fixture;
  await page.goto("/admin/workspaces/demo/preview?layers=roads");
  const position = page.getByText(/^1\.5000, .*zoom/);
  await expect(position).toBeVisible();
  const before = await position.textContent();
  const canvas = page
    .getByRole("region", { name: "Workspace map preview" })
    .locator("canvas");
  await expect(
    page.getByText("1 sampled features", { exact: false }),
  ).toBeVisible();
  // The GeoJSON response precedes the GPU frame in WebKit. Exercise the real
  // hit-test after rendering rather than assuming the response painted it.
  await expect(async () => {
    await canvas.click();
    await expect(page.locator("pre", { hasText: "Sample road" })).toBeVisible({
      timeout: 500,
    });
  }).toPass({ timeout: 10000 });
  await page.getByRole("slider", { name: "Opacity for roads" }).fill("0.5");
  await page.getByRole("button", { name: "Refresh preview" }).click();
  await expect(position).toHaveText(before!);
});

test("an explicit shared preview bbox takes priority over data extent", async ({
  page,
  fixture,
}) => {
  void fixture;
  await page.goto(
    "/admin/workspaces/demo/preview?layers=roads&bbox=10,40,12,42",
  );
  await expect(page.getByText(/^11\.0000, .*zoom/)).toBeVisible();
  await expect(page.getByText("Select a publication to begin.")).toHaveCount(0);
});
