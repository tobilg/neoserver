import { test, expect } from "./fixture";

test("an all-null geometry sample finishes and retains attributes", async ({
  page,
  fixture,
}) => {
  await page.route("**/api/v1/workspaces/demo/imports/i1/preview**", (route) =>
    route.fulfill({
      json: {
        type: "FeatureCollection",
        features: [
          {
            type: "Feature",
            id: "null-1",
            geometry: null,
            properties: { name: "Non-spatial record" },
          },
        ],
      },
    }),
  );
  await page.goto("/admin/workspaces/demo/imports");
  await page.getByRole("link", { name: "Import roads", exact: true }).click();
  await page.getByLabel("Public ID *", { exact: true }).fill("nonspatial");
  await page
    .getByRole("button", { name: "Validate plan", exact: true })
    .click();
  await expect.poll(() => fixture.plan).toBeDefined();
  await expect(
    page.getByRole("cell", { name: "Non-spatial record", exact: true }),
  ).toBeVisible();
  await expect(
    page.getByRole("status").filter({ hasText: "no drawable geometry" }),
  ).toBeVisible();
  await expect(
    page.getByText("Rendering sample…", { exact: true }),
  ).toHaveCount(0);
});
