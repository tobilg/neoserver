import { test, expect, storeAction } from "./fixture";

test("SQL views require a selected feature ID and retain a rejected draft", async ({
  page,
  fixture,
}) => {
  await page.route("**/validate-sql", async (route) => {
    await route.fulfill({
      json: {
        valid: true,
        discovered: {
          columns: [
            { name: "record_key", type: "VARCHAR" },
            { name: "geom", type: "GEOMETRY" },
          ],
          geometry_column: "geom",
          geometry_type: "Point",
          srid: 4326,
          suggested_id_column: "",
        },
      },
    });
  });
  let reject = true;
  await page.route("**/services/s1/layers", async (route) => {
    if (route.request().method() === "POST" && reject)
      return route.fulfill({
        status: 400,
        json: { message: "Feature ID must be unique and non-null" },
      });
    await route.fallback();
  });
  await page.goto("/admin/workspaces/demo/stores");
  await storeAction(page, "Primary data", "Add SQL view…");
  await page
    .getByLabel("SQL query")
    .fill("SELECT record_key, geom FROM points");
  await page.getByLabel("Public layer ID").fill("points");
  await page.getByRole("button", { name: "Validate SQL" }).click();
  await expect(
    page.getByRole("button", { name: "Publish SQL view" }),
  ).toBeDisabled();
  await page.getByLabel("Feature ID column").click();
  await page.getByRole("option", { name: "record_key", exact: true }).click();
  await page.getByRole("button", { name: "Publish SQL view" }).click();
  await expect(page.getByRole("alert")).toContainText("unique and non-null");
  await expect(page.getByLabel("Feature ID column")).toContainText(
    "record_key",
  );
  reject = false;
  await page.getByRole("button", { name: "Publish SQL view" }).click();
  await expect(page.getByRole("dialog")).not.toBeVisible();
  expect(fixture.published[0]).toMatchObject({
    sql_view: { id_column: "record_key" },
  });
});

test("discovery and publication retain their store and pending controls", async ({
  page,
  fixture,
}) => {
  fixture.discoveryCount = 2;
  fixture.services.push({
    ...fixture.services[0],
    id: "s2",
    name: "Second store",
  });
  let finishDiscovery!: () => void;
  let finishPublication!: () => void;
  const discoveryGate = new Promise<void>((resolve) => {
    finishDiscovery = resolve;
  });
  const publicationGate = new Promise<void>((resolve) => {
    finishPublication = resolve;
  });
  const writes: { url: string; body: Record<string, unknown> }[] = [];
  await page.route("**/services/s1/discover", async (route) => {
    await discoveryGate;
    await route.fallback();
  });
  await page.route("**/services/*/layers", async (route) => {
    if (route.request().method() !== "POST") return route.fallback();
    writes.push({
      url: route.request().url(),
      body: route.request().postDataJSON(),
    });
    if (writes.length === 1) await publicationGate;
    await route.fulfill({
      status: 201,
      json: { id: `new-${writes.length}`, ...route.request().postDataJSON() },
    });
  });
  await page.goto("/admin/workspaces/demo/layers");
  await page.getByRole("button", { name: "Add layer", exact: true }).click();
  const dialog = page.getByRole("dialog");
  const store = dialog.getByRole("combobox");
  await store.click();
  await page.getByRole("option", { name: /Primary data/ }).click();
  await dialog.getByRole("button", { name: "Discover layers" }).click();
  await expect(store).toBeDisabled();
  await expect(dialog.getByRole("button", { name: "Cancel" })).toBeDisabled();
  await page.keyboard.press("Escape");
  await expect(dialog).toBeVisible();
  finishDiscovery();
  await dialog.getByRole("button", { name: "Select all" }).click();
  await dialog.getByLabel("Public ID for public.source_0").fill("custom-name");
  await dialog.getByRole("button", { name: "Publish 2 layers" }).click();
  await expect.poll(() => writes.length).toBe(1);
  await expect(store).toBeDisabled();
  await expect(store).toContainText("Primary data");
  await expect(dialog.getByRole("button", { name: "Cancel" })).toBeDisabled();
  await expect(
    dialog.getByRole("button", { name: "Clear", exact: true }),
  ).toBeDisabled();
  await page.keyboard.press("Escape");
  await expect(dialog).toBeVisible();
  finishPublication();
  await expect(
    page.getByRole("dialog", { name: "Layers published" }),
  ).toBeVisible();
  await page.getByRole("button", { name: "Done", exact: true }).click();
  await expect(dialog).not.toBeVisible();
  expect(writes).toHaveLength(2);
  expect(
    writes.every((write) => write.url.includes("/services/s1/layers")),
  ).toBe(true);
  expect(writes[0].body.public_id).toBe("custom-name");
});

test("refresh preserves edited names and selections, even after a failed refresh", async ({
  page,
  fixture,
}) => {
  fixture.discoveryCount = 2;
  let fail = false;
  await page.route("**/discover", async (route) => {
    if (fail)
      return route.fulfill({
        status: 503,
        json: { message: "Discovery unavailable" },
      });
    await route.fallback();
  });
  await page.goto("/admin/workspaces/demo/layers");
  await page.getByRole("button", { name: "Add layer", exact: true }).click();
  const dialog = page.getByRole("dialog");
  await dialog.getByRole("button", { name: "Discover layers" }).click();
  const id = dialog.getByLabel("Public ID for public.source_0");
  const title = dialog.getByLabel("Title for public.source_0");
  await id.fill("important-name");
  await title.fill("Edited title");
  await dialog
    .getByRole("checkbox", { name: "Publish public.source_0" })
    .check();
  fail = true;
  await dialog.getByRole("button", { name: "Refresh discovery" }).click();
  await expect(dialog.getByRole("alert")).toContainText(
    "Discovery unavailable",
  );
  await expect(id).toHaveValue("important-name");
  await expect(title).toHaveValue("Edited title");
  fail = false;
  fixture.discoveryCount = 3;
  await dialog.getByRole("button", { name: "Refresh discovery" }).click();
  await expect(
    dialog.getByRole("checkbox", { name: "Publish public.source_2" }),
  ).not.toBeChecked();
  await expect(
    dialog.getByRole("checkbox", { name: "Publish public.source_0" }),
  ).toBeChecked();
  await expect(id).toHaveValue("important-name");
  await expect(title).toHaveValue("Edited title");
  await expect(
    dialog.getByRole("button", { name: "Publish 1 layer" }),
  ).toBeEnabled();
});

test("a conflicting publication name retains edits and can be corrected", async ({
  page,
  fixture,
}) => {
  let conflict = true;
  await page.route("**/layers/l1", async (route) => {
    if (route.request().method() === "PUT" && conflict) {
      conflict = false;
      await route.fulfill({
        status: 409,
        json: {
          message: "Conflict",
          detail: "public_id is already published in this workspace: occupied",
        },
      });
    } else {
      await route.fallback();
    }
  });
  await page.goto("/admin/workspaces/demo/layers");
  await page.getByRole("button", { name: "Edit roads", exact: true }).click();
  await page.getByLabel("Public ID", { exact: false }).fill("occupied");
  await page.getByLabel("Title", { exact: true }).fill("Preserved edit");
  await page.getByRole("button", { name: "Save publication" }).click();
  await expect(page.getByRole("alert")).toContainText(
    "public_id is already published in this workspace: occupied",
  );
  await expect(page.getByRole("alert")).toHaveCount(1);
  await expect(page.getByLabel("Title", { exact: true })).toHaveValue(
    "Preserved edit",
  );
  await page.getByLabel("Public ID", { exact: false }).fill("available");
  await page.getByRole("button", { name: "Save publication" }).click();
  await expect(page.getByRole("dialog")).not.toBeVisible();
  expect(fixture.layer.public_id).toBe("available");
  expect(fixture.layer.title).toBe("Preserved edit");
});

test("bulk actions apply to every layer just published", async ({
  page,
  fixture,
}) => {
  fixture.discoveryCount = 2;
  await page.goto("/admin/workspaces/demo/layers");
  await page.getByRole("button", { name: "Add layer", exact: true }).click();
  const dialog = page.getByRole("dialog");
  await dialog.getByRole("button", { name: "Discover layers" }).click();
  await dialog.getByRole("button", { name: "Select all" }).click();
  await dialog.getByRole("button", { name: "Publish 2 layers" }).click();
  const done = page.getByRole("dialog", { name: "Layers published" });
  await expect(done).toBeVisible();
  await expect(
    done.getByRole("link", { name: "Preview all on map" }),
  ).toHaveAttribute("href", /layers=source_0%2Csource_1$/);

  await done.getByLabel("Default style").selectOption("two");
  await done.getByRole("button", { name: "Set style" }).click();
  await expect
    .poll(() => fixture.published.map((layer) => layer.default_style))
    .toEqual(["two", "two"]);

  const limit = done.getByRole("button", { name: /Limit access|Allow any/ });
  await expect(limit).toBeDisabled();
  await done.getByRole("checkbox", { name: "Viewer" }).check();
  await done.getByRole("button", { name: "Limit access" }).click();
  await expect
    .poll(() => fixture.published.map((layer) => layer.allowed_roles))
    .toEqual([["viewer"], ["viewer"]]);
  await expect(limit).toBeDisabled();

  await done.getByRole("button", { name: "Make all public…" }).click();
  const confirm = page.getByRole("alertdialog");
  await confirm.getByRole("button", { name: "Cancel" }).click();
  expect(fixture.published.every((layer) => !layer.public)).toBe(true);
  await done.getByRole("button", { name: "Make all public…" }).click();
  await confirm.getByRole("button", { name: "Make public" }).click();
  await expect
    .poll(() => fixture.published.map((layer) => layer.public))
    .toEqual([true, true]);
  await expect(done.getByText("Published 2 public layers.")).toBeVisible();
});
