import { gotoConsole } from "./fixtures";
import { expect, test } from "@playwright/test";
import { apiURL, readFixture, signInWithToken } from "./fixtures";

/**
 * Success criterion S1: empty catalog to a published, enabled layer that the
 * workspace's OGC API actually serves. The final assertion queries the OGC
 * endpoint rather than the console, so a passing run means the layer is real.
 */
test("publishes a PostGIS layer that OGC API Features then serves", async ({
  page,
}) => {
  const { workspaceAdminKey, workspace } = await readFixture();
  const storeName = `postgis-${Date.now()}`;

  await signInWithToken(page, workspaceAdminKey);
  await gotoConsole(page, `/workspaces/${workspace}/stores`);

  await page.getByRole("button", { name: "Add store" }).click();
  await page.getByLabel("Store name").fill(storeName);
  await page.getByLabel("Host").fill("db");
  await page.getByLabel("Port").fill("5432");
  await page.getByLabel("Database").fill("postgis");
  await page.getByLabel("User").fill("postgres");
  await page.getByLabel("Password").fill("postgres");
  await page.getByLabel("Schemas (comma separated)").fill("cite");

  await page.getByRole("button", { name: "Test connection" }).click();
  await expect(page.getByText(/Connected in/)).toBeVisible({ timeout: 30_000 });

  await page.getByRole("button", { name: "Add store" }).last().click();
  await expect(page.getByText(storeName)).toBeVisible();

  // Discover and publish a deterministic CITE fixture table.
  await page
    .getByRole("row", { name: new RegExp(storeName) })
    .getByRole("button", { name: "Discover", exact: true })
    .click();
  await page
    .getByRole("button", { name: "Discover layers", exact: true })
    .click();
  const candidate = page.getByRole("checkbox", {
    name: "Publish cite.BasicPolygons",
    exact: true,
  });
  await expect(candidate).toBeVisible({ timeout: 30_000 });
  await candidate.check();
  const published = page.waitForResponse(
    (response) =>
      response.request().method() === "POST" &&
      /\/services\/[^/]+\/layers$/.test(new URL(response.url()).pathname),
  );
  await page
    .getByRole("button", { name: "Publish 1 layer", exact: true })
    .click();
  const publication = await published;
  expect(publication.status(), await publication.text()).toBe(201);
  // Publishing hands off to the results step before the dialog closes.
  await expect(
    page.getByRole("dialog", { name: "Layers published" }),
  ).toBeVisible();
  await page.getByRole("button", { name: "Done", exact: true }).click();
  await expect(page.getByRole("dialog")).not.toBeVisible();

  await gotoConsole(page, `/workspaces/${workspace}/layers`);
  await expect(page.getByText("BasicPolygons").first()).toBeVisible();

  // The real proof: the workspace's OGC API serves the collection.
  const collections = await page.request.get(
    apiURL(`/workspaces/${workspace}/ogc/collections`),
  );
  expect(collections.status()).toBe(200);
  expect(await collections.text()).toContain("BasicPolygons");
});

test("a store rejecting the connection reports it on the form", async ({
  page,
}) => {
  const { bootstrapToken, workspace } = await readFixture();
  await signInWithToken(page, bootstrapToken);
  await gotoConsole(page, `/workspaces/${workspace}/stores`);

  await page.getByRole("button", { name: "Add store" }).click();
  await page.getByLabel("Store name").fill("broken-store");
  await page.getByLabel("Host").fill("db");
  await page.getByLabel("Database").fill("postgis");
  await page.getByLabel("User").fill("postgres");
  await page.getByLabel("Password").fill("definitely-wrong");

  await page.getByRole("button", { name: "Test connection" }).click();
  await expect(
    // The panel repeats the server's raw message under the explanation.
    page.getByText(/authentication failed|rejected the connection/i).first(),
  ).toBeVisible({ timeout: 30_000 });
});

test("client-side validation blocks an incomplete store before any request", async ({
  page,
}) => {
  const { bootstrapToken, workspace } = await readFixture();
  await signInWithToken(page, bootstrapToken);
  await gotoConsole(page, `/workspaces/${workspace}/stores`);

  await page.getByRole("button", { name: "Add store" }).click();
  await page.getByRole("button", { name: "Add store" }).last().click();

  // Zod schema rejects a blank name and database before the network is touched.
  await expect(page.getByText("Store name is required")).toBeVisible();
});
