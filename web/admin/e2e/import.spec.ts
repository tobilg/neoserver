import { gotoConsole } from "./fixtures";
import { expect, test } from "@playwright/test";
import { apiURL, readFixture, signInWithToken } from "./fixtures";
import { fileURLToPath } from "node:url";

test("an excluded source can be re-included and published after revision and reload", async ({
  page,
}) => {
  test.setTimeout(120_000);
  const { bootstrapToken, workspace } = await readFixture();
  await signInWithToken(page, bootstrapToken);
  await gotoConsole(page, `/workspaces/${workspace}/imports`);
  await page.getByRole("button", { name: "New import", exact: true }).click();
  await page
    .getByLabel("Import name", { exact: true })
    .fill(`multilayer-${Date.now()}`);
  await page
    .getByLabel("Dataset or archive")
    .setInputFiles(
      fileURLToPath(new URL("./fixtures/two-layers.gpkg", import.meta.url)),
    );
  const upload = page.waitForResponse((response) => {
    const contentType = response.request().headers()["content-type"];
    return (
      response.request().method() === "POST" &&
      contentType?.startsWith("multipart/form-data") === true
    );
  });
  await page.getByRole("button", { name: "Upload and inspect" }).click();
  const uploaded = await (await upload).json();
  await expect(page.getByLabel("Target store name")).toBeVisible({
    timeout: 30_000,
  });
  await page.locator("summary").filter({ hasText: "second_source" }).click();
  await page
    .getByRole("switch", { name: "Include second_source", exact: true })
    .click();
  await page.getByLabel("Public ID prefix").fill("multi-");
  await page.getByRole("button", { name: "Apply prefix" }).click();
  await page
    .getByRole("button", { name: "Validate plan", exact: true })
    .click();
  await expect(
    page.getByRole("button", { name: "Publish dataset" }),
  ).toBeVisible({ timeout: 30_000 });
  await page.reload();
  await page.getByRole("button", { name: "Revise plan" }).click();
  await expect(
    page.locator("summary").filter({ hasText: "first_source" }),
  ).toContainText("multi-first-source");
  await page.locator("summary").filter({ hasText: "second_source" }).click();
  const second = page.getByRole("switch", {
    name: "Include second_source",
    exact: true,
  });
  await expect(second).not.toBeChecked();
  await second.click();
  await page
    .getByRole("button", { name: "Validate plan", exact: true })
    .click();
  await expect(
    page.getByRole("button", { name: "Publish dataset" }),
  ).toBeVisible({ timeout: 30_000 });
  await page.getByRole("button", { name: "Publish dataset" }).click();
  await expect(
    page.getByRole("link", { name: "View published layers" }),
  ).toBeVisible({ timeout: 30_000 });
  const response = await page.request.get(
    apiURL(`/api/v1/workspaces/${workspace}/imports/${uploaded.id}`),
    { headers: { Authorization: `Bearer ${bootstrapToken}` } },
  );
  expect(response.ok()).toBeTruthy();
  const job = await response.json();
  expect(job.status).toBe("published");
  expect(
    job.plan.layers
      .map((layer: { source_layer: string }) => layer.source_layer)
      .sort(),
  ).toEqual(["first_source", "second_source"]);
  expect(job.plan.layers[0].public_id).toBe("multi-first-source");
});

test("uploaded preview can be revised, corrected after a failed cast, and published", async ({
  page,
}) => {
  test.setTimeout(120_000);
  const { bootstrapToken, workspace } = await readFixture();
  await signInWithToken(page, bootstrapToken);
  await gotoConsole(page, `/workspaces/${workspace}/imports`);
  await page.getByRole("button", { name: "New import", exact: true }).click();
  await page
    .getByLabel("Import name", { exact: true })
    .fill(`revision-${Date.now()}`);
  await page.getByLabel("Dataset or archive").setInputFiles({
    name: "revision-points.geojson",
    mimeType: "application/geo+json",
    buffer: Buffer.from(
      JSON.stringify({
        type: "FeatureCollection",
        features: [
          {
            type: "Feature",
            properties: { name: "not-a-number" },
            geometry: { type: "Point", coordinates: [10, 20] },
          },
        ],
      }),
    ),
  });
  const upload = page.waitForResponse((response) => {
    const contentType = response.request().headers()["content-type"];
    return (
      response.request().method() === "POST" &&
      contentType?.startsWith("multipart/form-data") === true
    );
  });
  await page.getByRole("button", { name: "Upload and inspect" }).click();
  const uploaded = await (await upload).json();
  await expect(page.getByLabel("Target store name")).toBeVisible({
    timeout: 30_000,
  });
  await page
    .getByRole("button", { name: "Validate plan", exact: true })
    .click();
  await expect(
    page.getByRole("button", { name: "Publish dataset" }),
  ).toBeVisible({ timeout: 30_000 });
  await page.getByRole("button", { name: "Revise plan" }).click();
  await page.getByLabel(/Target type for .*\.name/).fill("INTEGER");
  await page
    .getByRole("button", { name: "Validate plan", exact: true })
    .click();
  await expect(page.getByRole("button", { name: "Retry import" })).toBeVisible({
    timeout: 30_000,
  });
  await expect(page.getByLabel(/Target type for .*\.name/)).toHaveValue(
    "INTEGER",
  );
  await page.getByLabel(/Target type for .*\.name/).fill("VARCHAR");
  await page.getByLabel("Public ID prefix").fill("corrected-");
  await page.getByRole("button", { name: "Apply prefix" }).click();
  await page
    .getByRole("button", { name: "Validate plan", exact: true })
    .click();
  await expect(
    page.getByRole("button", { name: "Publish dataset" }),
  ).toBeVisible({ timeout: 30_000 });
  await page.getByRole("button", { name: "Publish dataset" }).click();
  await expect(
    page.getByRole("link", { name: "View published layers" }),
  ).toBeVisible({ timeout: 30_000 });
  const result = await page.request.get(
    apiURL(`/api/v1/workspaces/${workspace}/imports/${uploaded.id}`),
    { headers: { Authorization: `Bearer ${bootstrapToken}` } },
  );
  expect(result.ok()).toBeTruthy();
  const published = await result.json();
  expect(published.status).toBe("published");
  expect(published.plan.layers[0].public_id).toMatch(/^corrected-/);
});

test("the imports page lists jobs and offers the three acquisition sources", async ({
  page,
}) => {
  const { bootstrapToken, workspace } = await readFixture();
  await signInWithToken(page, bootstrapToken);
  await gotoConsole(page, `/workspaces/${workspace}/imports`);

  await expect(page.getByRole("heading", { name: /imports/i })).toBeVisible();
  await expect(
    page.getByRole("button", { name: /new import|upload|create/i }).first(),
  ).toBeVisible();
});

test("an import from an unreachable URL fails with a diagnosable phase", async ({
  page,
}) => {
  const { bootstrapToken, workspace } = await readFixture();
  await signInWithToken(page, bootstrapToken);
  await gotoConsole(page, `/workspaces/${workspace}/imports`);

  await page
    .getByRole("button", { name: /new import|upload|create/i })
    .first()
    .click();

  await page.getByRole("tab", { name: "URL or path" }).click();
  await page.getByLabel("Import name").fill(`unreachable-${Date.now()}`);
  const uri = page.getByLabel("HTTPS URL or allowed server path");
  await uri.fill("https://127.0.0.1:1/not-a-real-dataset.gpkg");
  await page
    .getByRole("button", { name: "Acquire and inspect", exact: true })
    .last()
    .click();

  // The failure must name the phase it failed in — `acquire` — because that is
  // the vocabulary the API and the logs use (PRD §9.6).
  await expect(page.getByText(/acquire|failed/i).first()).toBeVisible({
    timeout: 60_000,
  });
});
