import AxeBuilder from "@axe-core/playwright";
import { test, expect, answerConfirm } from "./fixture";

test("switch paint and active navigation reflect actual state", async ({
  page,
  fixture,
}) => {
  void fixture;
  await page.goto("/admin/workspaces/demo/settings");
  const toggle = page.getByRole("switch", {
    name: "Enable Web Map Service (WMS)",
  });
  await expect(toggle).toBeChecked();
  await expect
    .poll(() => toggle.evaluate((e) => getComputedStyle(e).backgroundColor))
    .not.toBe("rgba(0, 0, 0, 0)");
  const checked = await toggle.evaluate(
    (e) => getComputedStyle(e.firstElementChild!).translate,
  );
  await toggle.click();
  await expect(toggle).not.toBeChecked();
  await expect
    .poll(() =>
      toggle.evaluate((e) => getComputedStyle(e.firstElementChild!).translate),
    )
    .not.toBe(checked);
  const active = page.getByRole("link", {
    name: "Service settings",
    exact: true,
  });
  const inactive = page.getByRole("link", { name: "Stores", exact: true });
  expect(
    await active.evaluate((e) => getComputedStyle(e).backgroundColor),
  ).not.toBe(
    await inactive.evaluate((e) => getComputedStyle(e).backgroundColor),
  );
});

test("query failures are not empty catalogs and can be retried", async ({
  page,
  fixture,
}) => {
  fixture.failStores = true;
  await page.goto("/admin/workspaces/demo/stores");
  await expect(page.getByRole("alert")).toContainText(
    "Store connection unavailable",
  );
  await expect(page.getByText("No stores yet.")).toHaveCount(0);
  fixture.failStores = false;
  await page.getByRole("button", { name: "Retry", exact: true }).click();
  await expect(page.getByText("Primary data", { exact: true })).toBeVisible();
});

test("settings forms preserve unrendered options and independent drafts", async ({
  page,
  fixture,
}) => {
  await page.goto("/admin/workspaces/demo/settings");
  await page
    .getByText("Configure Web Map Service (WMS)", { exact: true })
    .click();
  await page.getByText("Configure OGC API – Features", { exact: true }).click();
  const wms = page.locator("details").filter({
    has: page.locator("summary", {
      hasText: "Configure Web Map Service (WMS)",
    }),
  });
  const features = page.locator("details").filter({
    has: page.locator("summary", { hasText: "Configure OGC API – Features" }),
  });
  await wms.getByLabel("Title", { exact: true }).fill("Updated maps");
  await features
    .getByLabel("Title", { exact: true })
    .fill("Unsubmitted features");
  await wms.getByRole("button", { name: "Save settings", exact: true }).click();
  await expect.poll(() => fixture.settings.wms.title).toBe("Updated maps");
  expect(fixture.settings.wms.unknown_option).toBe("preserve me");
  await expect(features.getByLabel("Title", { exact: true })).toHaveValue(
    "Unsubmitted features",
  );
});

test("failed mutation is reported once", async ({ page, fixture }) => {
  fixture.failSettings = true;
  await page.goto("/admin/workspaces/demo/settings");
  await page
    .getByRole("switch", { name: "Enable Web Map Service (WMS)" })
    .click();
  await expect(
    page.getByText("Settings could not be saved", { exact: true }),
  ).toHaveCount(1);
  await expect(page.locator("[data-sonner-toast]")).toHaveCount(0);
});

test("creating a workspace refreshes the workspace selector", async ({
  page,
  fixture,
}) => {
  await page.goto("/admin/workspaces");
  await page
    .getByRole("button", { name: "New workspace", exact: true })
    .click();
  await page.getByLabel("Name", { exact: true }).fill("created");
  await page
    .getByRole("button", { name: "Create workspace", exact: true })
    .click();
  await page.getByRole("combobox", { name: "Workspace" }).click();
  await expect(
    page.getByRole("option", { name: "created · admin" }),
  ).toBeVisible();
  expect(fixture.authReads).toBeGreaterThan(1);
});

test("style drafts survive cancelled selection and save refreshes portrayal", async ({
  page,
  fixture,
}) => {
  await page.goto("/admin/workspaces/demo/styles");
  const editor = page.getByRole("textbox", { name: "SLD XML" });
  await expect(editor).toContainText("ORIGINAL_ONE");
  await editor.fill("<StyledLayerDescriptor>CHANGED</StyledLayerDescriptor>");
  await page.getByRole("combobox", { name: "Style", exact: true }).click();
  await page.getByRole("option", { name: "two", exact: true }).click();
  await answerConfirm(page, "keep");
  await expect(editor).toContainText("CHANGED");
  await page.getByLabel("Preview layer", { exact: true }).selectOption("roads");
  await expect(
    page.getByRole("img", { name: "WMS preview of roads using one" }),
  ).toBeVisible();
  const beforeSave = fixture.wmsRequests.length;
  await page.getByRole("button", { name: /Save style/ }).click();
  await expect
    .poll(() => fixture.wmsRequests.length)
    .toBeGreaterThan(beforeSave);
  expect(fixture.styles.one).toContain("CHANGED");
});

test("publication form preserves fields and guards dialog dismissal", async ({
  page,
  fixture,
}) => {
  await page.goto("/admin/workspaces/demo/layers");
  await page.getByRole("button", { name: "Edit roads", exact: true }).click();
  await page.getByLabel("Title", { exact: true }).fill("Edited road title");
  await page.keyboard.press("Escape");
  await answerConfirm(page, "keep");
  await expect(page.getByRole("dialog")).toBeVisible();
  await page.getByRole("button", { name: "Save publication" }).click();
  await expect.poll(() => fixture.layer.title).toBe("Edited road title");
  expect(fixture.layer.unknown_option).toBe("preserve me");
});

test("large discovery is bounded, editable, and validates IDs", async ({
  page,
  fixture,
}) => {
  fixture.discoveryCount = 5000;
  await page.goto("/admin/workspaces/demo/layers");
  await page.getByRole("button", { name: "Add layer", exact: true }).click();
  await page
    .getByRole("button", { name: "Discover layers", exact: true })
    .click();
  await expect(
    page.getByRole("checkbox", { name: /Publish public/ }),
  ).toHaveCount(50);
  await page.getByLabel("Filter discovered layers").fill("source_4999");
  await page
    .getByRole("checkbox", { name: "Publish public.source_4999" })
    .check();
  await page.getByLabel("Public ID for public.source_4999").fill("invalid id");
  await expect(
    page.getByRole("button", { name: "Publish 1 layer", exact: true }),
  ).toBeDisabled();
  await page.getByLabel("Public ID for public.source_4999").fill("custom_id");
  await page.getByLabel("Title for public.source_4999").fill("Custom title");
  await page
    .getByRole("button", { name: "Publish 1 layer", exact: true })
    .click();
  await expect.poll(() => fixture.published.length).toBe(1);
  expect(fixture.published[0]).toMatchObject({
    public_id: "custom_id",
    title: "Custom title",
    public: false,
  });
});

test("import plan uses fields and renders a map plus the same sample table", async ({
  page,
  fixture,
}) => {
  await page.goto("/admin/workspaces/demo/imports");
  await page.getByRole("link", { name: "Import roads", exact: true }).click();
  await page
    .getByRole("button", { name: "Validate plan", exact: true })
    .click();
  await expect(page.getByRole("alert")).toContainText("already published");
  expect(fixture.plan).toBeUndefined();
  await page.getByLabel("Public ID *", { exact: true }).fill("imported_roads");
  await page.locator("summary", { hasText: /^Advanced options/ }).click();
  await page.getByLabel("Target CRS (EPSG code)", { exact: true }).fill("");
  await expect(
    page.getByLabel("Target CRS (EPSG code)", { exact: true }),
  ).toHaveValue("");
  await page.getByLabel("Target name for roads.name").fill("road_name");
  await page
    .getByRole("button", { name: "Validate plan", exact: true })
    .click();
  await expect.poll(() => fixture.plan).toBeDefined();
  await expect(
    page.getByRole("region", { name: "Feature preview map" }),
  ).toBeVisible();
  await expect(
    page.getByRole("cell", { name: "Sample road", exact: true }),
  ).toBeVisible();
  await expect(
    page.getByText("Sample rendered on map", { exact: true }),
  ).toBeVisible();
});

test("map source controls render a bounded sample and retain shareable view state", async ({
  page,
  fixture,
}) => {
  void fixture;
  await page.goto("/admin/workspaces/demo/preview");
  // With nothing in the URL, Preview shows a publication by default.
  await expect(
    page.getByRole("switch", { name: "Show roads", exact: true }),
  ).toBeChecked();
  await page.getByLabel("Source for roads").selectOption("geojson");
  await expect(
    page.getByText("1 sampled features (maximum 1,000; not the full dataset)."),
  ).toBeVisible();
  const map = page.getByRole("region", { name: "Workspace map preview" });
  await expect(map).toHaveAttribute("aria-busy", "false");
  await expect(async () => {
    await map.click();
    await expect(page.locator("pre")).toContainText("Sample road");
  }).toPass({ timeout: 10000 });
  await expect(page).toHaveURL(/sources=geojson/);
  await page.reload();
  await expect(page.getByLabel("Source for roads")).toHaveValue("geojson");
  await expect(
    page.getByRole("switch", { name: "Show roads", exact: true }),
  ).toBeChecked();
});

for (const theme of ["light", "dark"] as const)
  test("shell accessibility in " + theme, async ({ page, fixture }) => {
    void fixture;
    await page.addInitScript(
      (theme) => localStorage.setItem("theme", theme),
      theme,
    );
    await page.goto("/admin/workspaces/demo/settings");
    await expect(
      page.getByRole("heading", { name: "Service settings", exact: true }),
    ).toBeVisible();
    const scan = async () => {
      const results = await new AxeBuilder({ page })
        .withTags(["wcag2a", "wcag2aa", "wcag21aa"])
        .analyze();
      expect(
        results.violations.filter(
          (v) => v.impact === "critical" || v.impact === "serious",
        ),
      ).toEqual([]);
      // Every piece of content sits inside a landmark.
      const landmarks = await new AxeBuilder({ page })
        .withRules(["region"])
        .analyze();
      expect(
        landmarks.violations.flatMap((v) => v.nodes.map((n) => n.target)),
      ).toEqual([]);
    };
    await scan();
    await page
      .getByRole("button", { name: "Toggle Sidebar", exact: true })
      .first()
      .click();
    await scan();
  });
