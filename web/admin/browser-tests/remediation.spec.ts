import AxeBuilder from "@axe-core/playwright";
import { test, expect, answerConfirm, storeAction } from "./fixture";
import { assertContract } from "./contract";

test("contract rejects unknown operations, invalid expiry and incomplete responses", () => {
  expect(() => assertContract("/missing", "GET", undefined)).toThrow(
    "Unspecified",
  );
  expect(() =>
    assertContract("/workspaces/demo/apikeys", "POST", {
      name: "test",
      role_id: "viewer",
      expires_at: "2030-12-01T12:30",
    }),
  ).toThrow("date-time");
  expect(() => assertContract("/auth/me", "GET", {}, 200)).toThrow("principal");
});

test("session change cannot expose the old workspace catalog", async ({
  page,
  fixture,
}) => {
  await page.goto("/admin/workspaces/demo/stores");
  await expect(page.getByText("Primary data", { exact: true })).toBeVisible();
  fixture.identity = {
    authenticated: true,
    principal: "second",
    subject: "second",
    auth_method: "jwt",
    super_admin: false,
    console_access: true,
    capabilities: {},
    workspaces: [
      { id: "w1", name: "demo", role: "viewer", console_access: false },
      { id: "w2", name: "other", role: "admin", console_access: true },
    ],
  };
  await page.evaluate(() =>
    window.dispatchEvent(new CustomEvent("neoserver:unauthorized")),
  );
  await expect(page).toHaveURL(/\/login\?/);
  await page.getByLabel("API key or JWT").fill("test-issued-token");
  await page
    .getByRole("button", { name: "Start session", exact: true })
    .click();
  await expect(
    page.getByText("This account can't use the console"),
  ).toBeVisible();
  await expect(page.getByText("Primary data", { exact: true })).toHaveCount(0);
  await expect(page.getByRole("button", { name: /Edit/ })).toHaveCount(0);
});

test("expiry uses RFC3339 and revoke waits for confirmation", async ({
  page,
  fixture,
}) => {
  await page.goto("/admin/workspaces/demo/api-keys");
  await page.getByRole("button", { name: "Revoke CI pipeline" }).click();
  expect(fixture.revocations).toBe(0);
  await expect(
    page.getByRole("button", { name: "Cancel", exact: true }),
  ).toBeFocused();
  await page.getByRole("button", { name: "Cancel", exact: true }).click();
  expect(fixture.revocations).toBe(0);
  await page.getByRole("button", { name: "Revoke CI pipeline" }).click();
  await page.getByRole("button", { name: "Revoke key", exact: true }).click();
  await expect.poll(() => fixture.revocations).toBe(1);
  // A revoked key stays listed until it is deleted as a second step.
  const row = page.getByRole("row", { name: /CI pipeline/ });
  await expect(row.getByText("revoked")).toBeVisible();
  await expect(
    page.getByRole("button", { name: "Revoke CI pipeline" }),
  ).toHaveCount(0);
  await page.getByRole("button", { name: "Delete CI pipeline" }).click();
  await page.getByRole("button", { name: "Cancel", exact: true }).click();
  expect(fixture.apiKeyDeleted).toBe(false);
  await page.getByRole("button", { name: "Delete CI pipeline" }).click();
  await expect(page.getByRole("dialog")).toContainText("nsk_ci");
  await page.getByRole("button", { name: "Delete key", exact: true }).click();
  await expect(page.getByText("Deleted CI pipeline")).toBeVisible();
  await expect(row).toHaveCount(0);
  expect(fixture.apiKeyDeleted).toBe(true);
  await page.getByRole("button", { name: "Create key", exact: true }).click();
  await page.getByLabel("Key name").fill("Dated key");
  await page.getByLabel("Expires at").fill("2030-12-01T12:30");
  await page
    .getByRole("dialog")
    .getByRole("button", { name: "Create key" })
    .click();
  await expect(page.getByText("nsk_once")).toBeVisible();
  expect(fixture.createdKey?.expires_at).toMatch(
    /2030-12-01T\d{2}:\d{2}:00\.000Z$/,
  );
});

test("SQL drafts survive cancelled dismissal and publishing refreshes cached layers", async ({
  page,
  fixture,
}) => {
  await page.goto("/admin/workspaces/demo/layers");
  // The title is shown under the public ID.
  await expect(
    page.getByRole("cell", { name: /^roads\s*Roads/ }),
  ).toBeVisible();
  await page.getByRole("link", { name: "Stores", exact: true }).click();
  await storeAction(page, "Primary data", "Add SQL view…");
  await page.getByLabel("SQL query").fill("SELECT id, geom FROM active_roads");
  await page.keyboard.press("Escape");
  await answerConfirm(page, "keep");
  await expect(page.getByLabel("SQL query")).toHaveText(
    "SELECT id, geom FROM active_roads",
  );
  await page.getByLabel("Public layer ID").fill("active_roads");
  await page.getByRole("button", { name: "Validate SQL" }).click();
  await page.getByRole("button", { name: "Publish SQL view" }).click();
  await expect(page.getByRole("dialog")).toHaveCount(0);
  await page.getByRole("link", { name: "Layers", exact: true }).click();
  await expect(
    page.getByText("active_roads", { exact: true }).first(),
  ).toBeVisible();
  expect(fixture.published).toHaveLength(1);
});

function addRaster(fixture: {
  services: Array<{
    id: string;
    name: string;
    type: string;
    enabled: boolean;
    workspace_id: string;
    created_at: string;
    updated_at: string;
  }>;
}) {
  fixture.services.push({
    ...fixture.services[0],
    id: "r1",
    name: "Elevation store",
    type: "raster_mosaic",
  });
}

test("coverage discovery reports initial failure, retries and publishes editable IDs", async ({
  page,
  fixture,
}) => {
  addRaster(fixture);
  fixture.failCoverageDiscovery = true;
  await page.goto("/admin/workspaces/demo/coverages");
  await page.getByRole("button", { name: "Add coverage", exact: true }).click();
  await page.getByLabel("Raster store", { exact: true }).selectOption("r1");
  await page
    .getByRole("button", { name: "Discover coverages", exact: true })
    .click();
  await expect(page.getByRole("alert")).toContainText(
    "Raster source unavailable",
  );
  fixture.failCoverageDiscovery = false;
  await page.getByRole("button", { name: "Retry", exact: true }).click();
  await page.getByLabel("Public coverage ID").fill("terrain");
  await page
    .getByRole("button", { name: "Publish coverage", exact: true })
    .click();
  await expect(page.getByText("Published as terrain (private)")).toBeVisible();
  expect(fixture.coverages[0].public_id).toBe("terrain");
  await page.keyboard.press("Escape");
  await expect(
    page.getByText("terrain", { exact: true }).first(),
  ).toBeVisible();
});

test("stale pagination and overview shortcuts recover to real resources", async ({
  page,
  fixture,
}) => {
  void fixture;
  await page.goto("/admin/workspaces/demo/stores?services_page=2");
  await expect(page.getByText("Primary data", { exact: true })).toBeVisible();
  await expect(page).not.toHaveURL(/services_page/);
  await page.getByRole("link", { name: "Overview", exact: true }).click();
  await page.getByRole("link", { name: "1 API keys", exact: true }).click();
  await expect(page).toHaveURL(/\/api-keys$/);
});

test("stats denial is an actionable error, not zero usage", async ({
  page,
  fixture,
}) => {
  fixture.failStats = true;
  await page.goto("/admin/workspaces/demo/tile-cache");
  await expect(page.getByRole("alert")).toContainText("Stats not authorized");
  await expect(page.getByText("0 bytes", { exact: true })).toHaveCount(0);
  await expect(page.getByRole("button", { name: "New job" })).toBeDisabled();
  fixture.failStats = false;
  await page.getByRole("button", { name: "Retry", exact: true }).click();
  await expect(page.getByText(/^1 KiB/)).toBeVisible();
});

for (const theme of ["light", "dark"]) {
  test(`operational dialogs are labeled and destructive actions confirmed (${theme})`, async ({
    page,
    fixture,
  }) => {
    addRaster(fixture);
    await page.addInitScript(
      (theme) => localStorage.setItem("theme", theme),
      theme,
    );
    await page.goto("/admin/workspaces/demo/tile-cache");
    await page.getByRole("button", { name: "New job" }).click();
    await page.getByLabel("Resource", { exact: true }).selectOption("roads");
    await page.getByLabel(/^Operation/).selectOption("truncate");
    await expect(
      page.getByRole("button", { name: "Start job", exact: true }),
    ).toBeEnabled();
    await page.getByRole("dialog").evaluate(async (element) => {
      await Promise.all(
        element
          .getAnimations({ subtree: true })
          .map((animation) => animation.finished.catch(() => {})),
      );
    });
    expect(
      (
        await new AxeBuilder({ page })
          .withTags(["wcag2a", "wcag2aa", "wcag21aa"])
          .analyze()
      ).violations,
    ).toEqual([]);
    await page.getByRole("button", { name: "Start job", exact: true }).click();
    expect(fixture.tileJobs).toHaveLength(0);
    await page
      .getByRole("button", { name: "Truncate tiles", exact: true })
      .click();
    await expect.poll(() => fixture.tileJobs.length).toBe(1);
    await expect(page.getByRole("dialog")).toHaveCount(0);
    await page.getByRole("link", { name: "Stores", exact: true }).click();
    await storeAction(page, "Elevation store", "Granules & harvests…");
    await page.getByLabel("Directory", { exact: true }).fill("data/rasters");
    await page.getByRole("dialog").evaluate(async (element) => {
      await Promise.all(
        element
          .getAnimations({ subtree: true })
          .map((animation) => animation.finished.catch(() => {})),
      );
    });
    expect(
      (
        await new AxeBuilder({ page })
          .withTags(["wcag2a", "wcag2aa", "wcag21aa"])
          .analyze()
      ).violations,
    ).toEqual([]);
    await page
      .getByRole("button", { name: "Delete data/elevation.tif" })
      .click();
    expect(fixture.granuleDeletions).toBe(0);
    await page
      .getByRole("button", { name: "Delete granule", exact: true })
      .click();
    await expect.poll(() => fixture.granuleDeletions).toBe(1);
    await page.getByLabel("Mode", { exact: true }).selectOption("synchronize");
    await page
      .getByRole("button", { name: "Start harvest", exact: true })
      .click();
    expect(fixture.harvests).toHaveLength(0);
    await page
      .getByRole("button", { name: "Synchronize mosaic", exact: true })
      .click();
    await expect.poll(() => fixture.harvests.length).toBe(1);
  });
}
