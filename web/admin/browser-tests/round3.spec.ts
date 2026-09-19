import { test, expect } from "./fixture";

// Regressions for the round-3 console review (see
// plans/console-review-round3.md).

test("palette opens when typing immediately", async ({ page, fixture }) => {
  void fixture;
  await page.goto("/admin/workspaces/demo");
  await expect(page.getByRole("heading", { level: 1 })).toBeVisible();
  await page.keyboard.press("ControlOrMeta+k");
  await page.keyboard.type("sty");
  await expect(page.getByRole("dialog", { name: "Go to" })).toBeVisible();
  await expect(page.getByRole("combobox")).toHaveValue("sty");
});

test("phones reach the palette from the header", async ({ page, fixture }) => {
  void fixture;
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto("/admin/workspaces/demo");
  await page
    .getByRole("button", { name: "Search pages and publications" })
    .click();
  await expect(page.getByRole("dialog", { name: "Go to" })).toBeVisible();
});

test("key details describe state in plain words", async ({ page, fixture }) => {
  void fixture;
  await page.route("**/api/v1/workspaces/demo/apikeys/k1", (route) =>
    route.fulfill({
      json: {
        id: "k1",
        name: "CI pipeline",
        key_prefix: "nsk_ci",
        role_id: "viewer",
        revoked: false,
        created_at: "2026-01-01T00:00:00Z",
      },
    }),
  );
  await page.goto("/admin/workspaces/demo/api-keys");
  await page.getByRole("button", { name: "Inspect CI pipeline" }).click();
  const dialog = page.getByRole("dialog", { name: "CI pipeline" });
  await expect(dialog.locator('[data-status="active"]')).toHaveText("Active");
  await expect(dialog.getByText("Never", { exact: true })).toBeVisible();
  await expect(dialog.getByText("Disabled", { exact: true })).toHaveCount(0);
  await expect(dialog.getByText("Revoked", { exact: true })).toHaveCount(0);
});

test("identifier columns keep short IDs on one line", async ({
  page,
  fixture,
}) => {
  void fixture;
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto("/admin/tile-matrix-sets");
  const id = page
    .getByRole("cell")
    .getByText("WebMercatorQuad", { exact: true });
  await expect(id).toBeVisible();
  const box = await id.boundingBox();
  expect(box!.height).toBeLessThan(24);
});

test("narrow tables open one menu with every row action", async ({
  page,
  fixture,
}) => {
  void fixture;
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto("/admin/workspaces/demo/stores");
  await page.getByRole("button", { name: "Actions for Primary data" }).click();
  const menu = page.getByRole("group", { name: "Actions for Primary data" });
  for (const name of ["Discover", "Test connection", "Delete store…"])
    await expect(menu.getByRole("button", { name, exact: true })).toBeVisible();
  // The nested desktop menu is not part of the phone layout.
  await expect(
    menu.getByRole("button", { name: "More actions for Primary data" }),
  ).toBeHidden();
  await menu.getByRole("button", { name: "Add SQL view…" }).click();
  await expect(menu).toBeHidden();
  await expect(
    page.getByRole("dialog", { name: "Add SQL view layer" }),
  ).toBeVisible();
});

test("the audit log filters by outcome, collapses bursts and keeps time on phones", async ({
  page,
  fixture,
}) => {
  void fixture;
  const requested: URL[] = [];
  await page.route("**/api/v1/audit?*", async (route) => {
    const url = new URL(route.request().url());
    requested.push(url);
    const burst = Array.from({ length: 5 }, (_, index) => ({
      id: `burst-${index}`,
      timestamp: `2026-09-17T12:54:0${9 - index}Z`,
      action: "security",
      method: "GET",
      path: "/workspaces/demo/wms?request=GetMap",
      operation: "GETMAP",
      workspace: "demo",
      status: 401,
      duration_ms: 1,
      security_event: true,
    }));
    await route.fulfill({
      json: {
        events: [
          {
            id: "login",
            timestamp: "2026-09-17T13:17:48Z",
            action: "security",
            method: "POST",
            path: "/api/v1/auth/login",
            operation: "login",
            principal: "operator",
            auth_method: "password",
            status: 201,
            duration_ms: 4,
            security_event: true,
          },
          ...burst,
        ],
      },
    });
  });
  await page.goto("/admin/audit");
  await expect(page.getByText("operator").first()).toBeVisible();
  await expect(page.getByText("signed in (password)")).toBeVisible();
  await expect(page.getByLabel("5 identical events")).toHaveText("×5");
  await expect(page.getByText("Not set")).toHaveCount(0);
  await expect(
    page.getByText("6 events on this page in 2 lines", { exact: false }),
  ).toBeVisible();
  await page.getByRole("button", { name: "Failed", exact: true }).click();
  await expect
    .poll(() => requested.at(-1)?.searchParams.get("outcome"))
    .toBe("failed");
  await page.getByLabel("Time range").selectOption("24h");
  await expect
    .poll(() => requested.at(-1)?.searchParams.get("since"))
    .toBeTruthy();
  await page.setViewportSize({ width: 390, height: 844 });
  await expect(page.locator("time").first()).toBeVisible();
});

test("cache pages merge into Caching and deletions have one home", async ({
  page,
  fixture,
}) => {
  void fixture;
  await page.goto("/admin/workspaces/demo/cache");
  await expect(page).toHaveURL(/\/workspaces\/demo\/caching$/);
  await expect(
    page.getByRole("heading", { name: "Caching", level: 1 }),
  ).toBeVisible();
  await expect(
    page.getByRole("button", { name: "Clear response cache" }),
  ).toBeVisible();
  await page.goto("/admin/deletions");
  await expect(
    page.getByRole("heading", { name: "Deletion operations", level: 1 }),
  ).toBeVisible();
});

test("roles hide impossible deletes and identity explains missing OIDC", async ({
  page,
  fixture,
}) => {
  void fixture;
  await page.route("**/api/v1/roles/*/policies", (route) =>
    route.fulfill({ json: { policies: [] } }),
  );
  await page.route("**/api/v1/auth/sessions*", (route) =>
    route.fulfill({ json: { sessions: [] } }),
  );
  await page.goto("/admin/roles");
  await expect(
    page.getByRole("cell", { name: "viewer" }).first(),
  ).toBeVisible();
  await expect(page.getByRole("button", { name: /^Delete/ })).toHaveCount(0);
  await page.goto("/admin/identity");
  await expect(page.getByText("OIDC sign-in is not configured")).toBeVisible();
  await expect(
    page.getByText("Create mapping from a presented claim"),
  ).toHaveCount(0);
});

test("client examples wrap by default", async ({ page, fixture }) => {
  void fixture;
  await page.goto("/admin/workspaces/demo/endpoints");
  const wrap = page.getByRole("button", { name: /Wrap/ });
  await expect(wrap).toHaveAttribute("aria-pressed", "true");
  await expect(page.getByLabel("Connection example")).toHaveCSS(
    "white-space",
    "pre-wrap",
  );
  await wrap.click();
  await expect(page.getByLabel("Connection example")).toHaveCSS(
    "white-space",
    "pre",
  );
});
