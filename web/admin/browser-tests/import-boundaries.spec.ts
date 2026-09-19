import { test, expect } from "./fixture";

test("a revised plan can re-include a previously excluded source", async ({
  page,
  fixture,
}) => {
  const first = fixture.job.discovery.layers[0];
  fixture.job.discovery.layers.push({
    ...first,
    name: "second_source",
    title: "Second source",
  });
  Object.assign(fixture.job, {
    status: "ready_to_publish",
    phase: "preview",
    plan: {
      service_name: "revision",
      layers: [
        {
          source_layer: first.name,
          public_id: "selected_source",
          enabled: true,
          public: false,
        },
      ],
    },
  });
  await page.goto("/admin/workspaces/demo/imports");
  await page.getByRole("link", { name: "Import roads", exact: true }).click();
  await page.getByRole("button", { name: "Revise plan", exact: true }).click();
  await page.locator("summary").filter({ hasText: "second_source" }).click();
  const include = page.getByRole("switch", {
    name: "Include second_source",
    exact: true,
  });
  await expect(include).not.toBeChecked();
  await include.click();
  await expect(include).toBeChecked();
  await expect(page.getByLabel("Target store name")).toHaveValue("revision");
});

test("older import pages and direct job links remain usable", async ({
  page,
  fixture,
}) => {
  await page.route("**/api/v1/workspaces/demo/imports?*", async (route) => {
    const url = new URL(route.request().url());
    await route.fulfill({
      json: url.searchParams.has("cursor")
        ? { imports: [fixture.job] }
        : { imports: [], next_cursor: "older-page" },
    });
  });
  await page.route("**/api/v1/workspaces/demo/imports", (route) =>
    route.fulfill({ json: { imports: [], next_cursor: "older-page" } }),
  );
  await page.goto("/admin/workspaces/demo/imports");
  await page.getByRole("button", { name: "Older", exact: true }).click();
  await expect(page).toHaveURL(/cursor=older-page/);
  await page.getByRole("link", { name: "Import roads", exact: true }).click();
  await expect(page).toHaveURL(/\/imports\/[^/?]+$/);
  const detail = page.url();
  await page.reload();
  await expect(page.getByLabel("Target store name")).toBeVisible();
  await expect(
    page.getByRole("navigation", { name: "Breadcrumb" }),
  ).toContainText("Imports/Import roads");
  // Returning to the list keeps the page the user came from.
  await page.goto(
    detail.replace(/\/imports\/.*/, "/imports?cursor=older-page"),
  );
  await page.getByRole("link", { name: "Import roads", exact: true }).click();
  await page.getByRole("link", { name: "All imports" }).click();
  await expect(page).toHaveURL(/cursor=older-page/);
  // Links from before the detail route still open the job.
  const id = detail.split("/imports/")[1];
  await page.goto(`/admin/workspaces/demo/imports?import=${id}`);
  await expect(page).toHaveURL(new RegExp(`/imports/${id}$`));
  await expect(page.getByLabel("Target store name")).toBeVisible();
  await page.goto("/admin/workspaces/demo/imports?cursor=older-page");
  await page.getByRole("button", { name: "Newer", exact: true }).click();
  await expect(page).not.toHaveURL(/cursor=/);
  await page
    .getByLabel("Import status", { exact: true })
    .selectOption("actionable");
  await expect(page).toHaveURL(/status=actionable/);
});
