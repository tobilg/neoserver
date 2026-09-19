import { test, expect, storeAction } from "./fixture";
import { assertContract } from "./contract";

function deletion() {
  const operation = {
    id: "delete-1",
    workspace_id: "w1",
    target_id: "s1",
    target_name: "Primary data",
    scope: "service",
    status: "pending",
    phase: "tombstoned",
    attempt_count: 0,
    last_error: "",
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
    plan: {
      scope: "service",
      workspace_id: "w1",
      target: { id: "s1", name: "Primary data", kind: "service" },
    },
  };
  assertContract("/deletions/delete-1", "GET", operation, 200);
  return operation;
}

for (const superAdmin of [false, true]) {
  test(`${superAdmin ? "super" : "workspace"} admin follows accepted deletion, reloads failure and retries`, async ({
    page,
    fixture,
  }) => {
    fixture.identity = {
      principal: "operator",
      authenticated: true,
      auth_method: "jwt",
      console_access: true,
      super_admin: superAdmin,
      capabilities: {},
      workspaces: fixture.workspaces.map((ws) => ({
        ...ws,
        role: "admin",
        console_access: true,
      })),
    };
    let operation = deletion();
    let deleted = false;
    let finish!: () => void;
    const waiting = new Promise<void>((resolve) => {
      finish = resolve;
    });
    await page.route(
      "**/api/v1/workspaces/demo/services/s1/deletion-plan",
      (route) => route.fulfill({ json: operation.plan }),
    );
    await page.route(
      "**/api/v1/workspaces/demo/services/s1?recurse=true",
      async (route) => {
        await waiting;
        deleted = true;
        fixture.services = [];
        await route.fulfill({
          status: 202,
          headers: { Location: "/api/v1/deletions/delete-1" },
          json: operation,
        });
      },
    );
    await page.route("**/api/v1/deletions?**", (route) => {
      expect(new URL(route.request().url()).searchParams.get("workspace")).toBe(
        "w1",
      );
      return route.fulfill({ json: { deletions: deleted ? [operation] : [] } });
    });
    await page.route("**/api/v1/deletions/delete-1", (route) =>
      route.fulfill({ json: operation }),
    );
    let retries = 0;
    await page.route("**/api/v1/deletions/delete-1/retry", (route) => {
      retries++;
      if (retries === 1)
        return route.fulfill({
          status: 503,
          json: { code: 503, message: "Retry unavailable" },
        });
      operation = {
        ...operation,
        status: "running",
        phase: "catalog_committed",
        last_error: "",
        attempt_count: 2,
      };
      return route.fulfill({ status: 202, json: operation });
    });
    await page.goto("/admin/workspaces/demo/stores");
    await storeAction(page, "Primary data", "Delete store…");
    await page.getByLabel("Type Primary data to confirm").fill("Primary data");
    await page
      .getByRole("button", { name: "Delete store", exact: true })
      .click();
    try {
      await expect(
        page.getByRole("button", { name: "Cancel", exact: true }),
      ).toBeDisabled();
      await expect(
        page.getByLabel("Type Primary data to confirm"),
      ).toBeDisabled();
      await page.keyboard.press("Escape");
      await expect(page.getByRole("dialog")).toBeVisible();
    } finally {
      finish();
    }
    await expect(page).toHaveURL(/\/deletions\?operation=delete-1/);
    const detail = page.getByRole("region", {
      name: "Deletion operation detail",
    });
    await expect(detail).toContainText("cleanup is not complete yet");
    operation = {
      ...operation,
      status: "failed",
      last_error: "Cleanup storage unavailable",
    };
    await expect(detail).toContainText("Cleanup storage unavailable", {
      timeout: 15000,
    });
    await page.reload();
    await expect(detail).toContainText("Cleanup storage unavailable");
    await page
      .getByRole("button", { name: "Retry deletion", exact: true })
      .click();
    await expect(detail).toContainText("Retry unavailable");
    await page
      .getByRole("button", { name: "Retry deletion", exact: true })
      .click();
    await expect(detail).toContainText("catalog_committed");
    operation = { ...operation, status: "completed", phase: "completed" };
    await expect(detail).toContainText("Deletion completed.", {
      timeout: 15000,
    });
    await page
      .getByRole("button", { name: "Close detail", exact: true })
      .click();
    await expect(
      page.getByRole("button", { name: "Inspect Primary data", exact: true }),
    ).toBeVisible();
    if (!superAdmin)
      await expect(
        page.getByRole("link", { name: "Operations", exact: true }),
      ).toHaveCount(0);
  });
}

test("deletion history fetches next pages and resets the cursor on filtering", async ({
  page,
  fixture,
}) => {
  void fixture;
  const queries: URLSearchParams[] = [];
  await page.route("**/api/v1/deletions?**", (route) => {
    const query = new URL(route.request().url()).searchParams;
    queries.push(query);
    const later = query.has("cursor");
    return route.fulfill({
      json: {
        deletions: [
          {
            ...deletion(),
            id: later ? "old" : "new",
            target_name: later ? "Older failed store" : "Recent store",
            status: "failed",
          },
        ],
        next_cursor: later ? undefined : "page-two",
      },
    });
  });
  await page.goto("/admin/workspaces/demo/deletions");
  await page.getByRole("button", { name: "Older", exact: true }).click();
  await expect(
    page.getByRole("button", { name: "Inspect Older failed store" }),
  ).toBeVisible();
  await page.getByLabel("Status", { exact: true }).selectOption("failed");
  await expect.poll(() => queries.at(-1)?.get("status")).toBe("failed");
  expect(queries.at(-1)?.has("cursor")).toBe(false);
  await expect(
    page.getByRole("button", { name: "Inspect Recent store" }),
  ).toBeVisible();
});

test("a denied direct deletion link shows an error, not stale operation data", async ({
  page,
  fixture,
}) => {
  fixture.identity = {
    principal: "operator",
    authenticated: true,
    auth_method: "jwt",
    console_access: true,
    super_admin: false,
    capabilities: {},
    workspaces: fixture.workspaces.map((ws) => ({
      ...ws,
      role: "admin",
      console_access: true,
    })),
  };
  await page.route("**/api/v1/deletions?**", (route) =>
    route.fulfill({ json: { deletions: [] } }),
  );
  await page.route("**/api/v1/deletions/denied", (route) =>
    route.fulfill({
      status: 403,
      json: {
        code: 403,
        message: "Workspace administrator access is required",
      },
    }),
  );
  await page.goto("/admin/workspaces/demo/deletions?operation=denied");
  await expect(
    page.getByRole("region", { name: "Deletion operation detail" }),
  ).toContainText("Workspace administrator access is required");
  await expect(
    page.getByRole("button", { name: "Retry deletion", exact: true }),
  ).toHaveCount(0);
});

test("workspace deletion hands off to global history after its catalog entry disappears", async ({
  page,
  fixture,
}) => {
  const operation = {
    ...deletion(),
    scope: "workspace",
    target_id: "w1",
    target_name: "demo",
    status: "completed",
    phase: "completed",
  };
  await page.route("**/api/v1/workspaces/demo/deletion-plan", (route) =>
    route.fulfill({
      json: {
        ...operation.plan,
        scope: "workspace",
        target: { id: "w1", name: "demo", kind: "workspace" },
      },
    }),
  );
  await page.route("**/api/v1/workspaces/demo?recurse=true", (route) => {
    fixture.workspaces = [];
    return route.fulfill({ status: 202, json: operation });
  });
  await page.route("**/api/v1/deletions?**", (route) =>
    route.fulfill({ json: { deletions: [operation] } }),
  );
  await page.route("**/api/v1/deletions/delete-1", (route) =>
    route.fulfill({ json: operation }),
  );
  await page.route("**/api/v1/catalog/integrity", (route) =>
    route.fulfill({ json: { issues: [], healthy: true } }),
  );
  await page.route("**/api/v1/cache/stats", (route) =>
    route.fulfill({ json: {} }),
  );
  await page.route("**/health", (route) =>
    route.fulfill({ json: { status: "ok" } }),
  );
  await page.goto("/admin/workspaces");
  await page.getByRole("button", { name: "Delete demo", exact: true }).click();
  await page.getByLabel("Type demo to confirm").fill("demo");
  await page
    .getByRole("button", { name: "Delete workspace", exact: true })
    .click();
  await expect(page).toHaveURL(/\/deletions\?operation=delete-1/);
  await expect(
    page.getByRole("region", { name: "Deletion operation detail" }),
  ).toContainText("Deletion completed.");
  await page.reload();
  await expect(
    page.getByRole("region", { name: "Deletion operation detail" }),
  ).toContainText("demo");
});
