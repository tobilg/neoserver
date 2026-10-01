import { expect, test } from "@playwright/test";
import { apiURL, gotoConsole, readFixture, signInWithToken } from "./fixtures";

test("imports reviewed STAC metadata and serves it through workspace search", async ({
  page,
  request,
}) => {
  const { workspace, workspaceAdminKey } = await readFixture();
  const id = `stac-${Date.now()}`;
  await signInWithToken(page, workspaceAdminKey);
  await gotoConsole(page, `/workspaces/${workspace}/stac`);
  await page.getByRole("tab", { name: "Settings", exact: true }).click();
  await page.getByLabel("Enable this workspace catalog").check();
  await page
    .getByLabel("Allow public access (Collection visibility still applies)")
    .check();
  const saved = page.waitForResponse(
    (r) => r.request().method() === "PUT" && r.url().endsWith("/settings/stac"),
  );
  await page.getByRole("button", { name: "Save settings" }).click();
  expect((await saved).ok()).toBeTruthy();
  await page.getByRole("tab", { name: "Collections", exact: true }).click();
  await page.getByRole("button", { name: "New Collection" }).click();
  await page.getByLabel("Collection ID", { exact: true }).fill(id);
  await page.getByLabel("Title", { exact: true }).fill(id);
  await page
    .getByLabel("Description", { exact: true })
    .fill("Browser publication fixture");
  await page.getByLabel("Public Collection").check();
  await page.getByRole("button", { name: "Save Collection" }).click();
  await expect(
    page.getByRole("heading", { name: id, exact: true }),
  ).toBeVisible();
  await page.getByRole("tab", { name: "Import metadata", exact: true }).click();
  await page
    .getByRole("combobox", { name: "Collection", exact: true })
    .selectOption(id);
  const item = {
    type: "Feature",
    stac_version: "1.1.0",
    id: "scene",
    collection: id,
    geometry: { type: "Point", coordinates: [8, 48] },
    bbox: [8, 48, 8, 48],
    properties: {
      datetime: "2026-01-01T00:00:00.123456789Z",
      "custom:quality": "reviewed",
    },
    assets: {
      data: { href: "https://example.org/scene.tif", roles: ["data"] },
    },
    links: [],
  };
  await page.getByLabel("Metadata file").setInputFiles({
    name: "item.json",
    mimeType: "application/json",
    buffer: Buffer.from(JSON.stringify(item)),
  });
  await page.getByRole("button", { name: "Upload and validate" }).click();
  await expect(page.getByLabel("Import preview")).toHaveValue(/custom:quality/);
  const before = await request.get(
    apiURL(`/workspaces/${workspace}/stac/search?collections=${id}`),
  );
  expect(before.status()).toBe(200);
  expect((await before.json()).features).toHaveLength(0);
  await page.getByRole("button", { name: "Publish import" }).click();
  await expect(page.getByText(/1 records validated · succeeded/)).toBeVisible();
  const after = await request.post(
    apiURL(`/workspaces/${workspace}/stac/search`),
    { data: { collections: [id], bbox: [7, 47, 9, 49] } },
  );
  expect(after.status(), await after.text()).toBe(200);
  const body = await after.json();
  expect(body.features).toHaveLength(1);
  expect(body.features[0].properties["custom:quality"]).toBe("reviewed");
  expect(
    body.features[0].links.find((l: { rel: string }) => l.rel === "self").href,
  ).toContain(`/workspaces/${workspace}/stac/`);
  expect((await request.get(apiURL("/stac/search"))).status()).toBe(404);
  await page.getByRole("tab", { name: "Items", exact: true }).click();
  await page
    .getByRole("combobox", { name: "Collection", exact: true })
    .selectOption(id);
  await page.getByRole("button", { name: "Search", exact: true }).click();
  await page.getByRole("button", { name: "Inspect Item" }).click();
  await expect(page.getByLabel("Item JSON")).toHaveValue(/custom:quality/);
});
