import { test, expect } from "./fixture";

test.beforeEach(async ({ fixture }) => {
  expect(fixture.services.length).toBeGreaterThan(0);
});

test("actual ready response is shown as healthy in the shell", async ({
  page,
}) => {
  await page.goto("/admin/workspaces/demo/stores");
  await expect(page.getByRole("status", { name: "Server ready" })).toHaveClass(
    /text-success/,
  );
});

test("SQL view actions match datasource capabilities", async ({
  page,
  fixture,
}) => {
  fixture.services.push({
    ...fixture.services[0],
    id: "parquet",
    name: "Parquet data",
    type: "geoparquet",
  });
  fixture.services.push({
    ...fixture.services[0],
    id: "vector",
    name: "Vector files",
    type: "vectorfile",
  });
  await page.goto("/admin/workspaces/demo/stores");
  const sqlItem = page.getByRole("menuitem", {
    name: "Add SQL view…",
    exact: true,
  });
  await page
    .getByRole("button", { name: "More actions for Primary data" })
    .click();
  await expect(sqlItem).toBeVisible();
  await page.keyboard.press("Escape");
  for (const name of ["Parquet data", "Vector files"]) {
    await page
      .getByRole("button", { name: `More actions for ${name}`, exact: true })
      .click();
    await expect(
      page.getByRole("menuitem", { name: "Test connection" }),
    ).toBeVisible();
    await expect(sqlItem).toHaveCount(0);
    await page.keyboard.press("Escape");
  }
});

test("PostGIS appears in coverage discovery", async ({ page }) => {
  await page.goto("/admin/workspaces/demo/coverages");
  await page.getByRole("button", { name: "Add coverage", exact: true }).click();
  await page.getByLabel("Raster store", { exact: true }).selectOption("s1");
  await expect(
    page.getByRole("button", { name: "Discover coverages", exact: true }),
  ).toBeEnabled();
  await page
    .getByRole("button", { name: "Discover coverages", exact: true })
    .click();
  await expect(page.getByLabel("Public coverage ID")).toBeVisible();
});

test("published PostGIS coverage appears in preview and style choices with compatible sources", async ({
  page,
  fixture,
}) => {
  fixture.coverages.push({
    id: "raster-pg",
    service_id: "s1",
    public_id: "terrain",
    source_coverage: "public.elevation",
    title: "Terrain",
    enabled: true,
    public: true,
    native_extent: { srid: 4326, min_x: 1, min_y: 1, max_x: 2, max_y: 2 },
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
  });
  await page.goto("/admin/workspaces/demo/preview");
  await page.getByRole("switch", { name: "Show terrain", exact: true }).click();
  const source = page.getByLabel("Source for terrain");
  await expect(source).toHaveValue("wms");
  // Coverages can only render through WMS, so no other source is offered.
  await expect(source.locator('option[value="geojson"]')).toHaveCount(0);
  await expect(source.locator('option[value="tiles"]')).toHaveCount(0);
  await expect(source.locator('option[value="wms"]')).toHaveJSProperty(
    "disabled",
    false,
  );
  await page.goto("/admin/workspaces/demo/styles");
  await expect(
    page.locator('#preview-layer option[value="terrain"]'),
  ).toHaveCount(1);
  await page.goto("/admin/workspaces/demo/tile-cache");
  await page.getByRole("button", { name: "New job" }).click();
  const resource = page.getByLabel("Resource", { exact: true });
  await expect(resource.locator('option[value="terrain"]')).toHaveCount(0);
  await page.getByLabel("Tile type", { exact: true }).selectOption("map");
  await expect(resource.locator('option[value="terrain"]')).toHaveCount(1);
});
