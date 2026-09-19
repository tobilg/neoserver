import { test, expect, answerConfirm } from "./fixture";

test("point layer style preview requests a padded nonzero viewport", async ({
  page,
  fixture,
}) => {
  Object.assign(fixture.layer, {
    native_extent: { min_x: 7, min_y: 51, max_x: 7, max_y: 51, srid: 4326 },
  });
  await page.goto("/admin/workspaces/demo/styles");
  await page.getByLabel("Preview layer", { exact: true }).selectOption("roads");
  await expect
    .poll(() =>
      fixture.wmsRequests.some((request) => {
        const query = new URL(request).searchParams;
        const bounds = query.get("bbox")?.split(",").map(Number);
        return (
          query.get("layers") === "roads" &&
          query.get("crs") === "CRS:84" &&
          bounds &&
          bounds[0] < 7 &&
          bounds[1] < 51 &&
          bounds[2] > 7 &&
          bounds[3] > 51
        );
      }),
    )
    .toBe(true);
});

test("workspace edits survive declined dismissal and discard explicitly", async ({
  page,
  fixture,
}) => {
  void fixture;
  await page.goto("/admin/workspaces");
  await page.getByRole("button", { name: "Edit demo", exact: true }).click();
  await page
    .getByLabel("Description", { exact: true })
    .fill("Unsaved description");
  await page.keyboard.press("Escape");
  await answerConfirm(page, "keep");
  await expect(page.getByLabel("Description", { exact: true })).toHaveValue(
    "Unsaved description",
  );
  await page.getByRole("button", { name: "Cancel", exact: true }).click();
  await answerConfirm(page, "discard");
  await expect(page.getByRole("dialog")).toHaveCount(0);
  await page.getByRole("button", { name: "Edit demo", exact: true }).click();
  await expect(page.getByLabel("Description", { exact: true })).toHaveValue(
    "Demo",
  );
});

test("new workspace draft survives Escape and failed save", async ({
  page,
  fixture,
}) => {
  void fixture;
  await page.goto("/admin/workspaces");
  await page
    .getByRole("button", { name: "New workspace", exact: true })
    .click();
  await page.getByLabel("Name", { exact: true }).fill("draft");
  await page.keyboard.press("Escape");
  await answerConfirm(page, "keep");
  await expect(page.getByLabel("Name", { exact: true })).toHaveValue("draft");
  await page.route("**/api/v1/workspaces", async (route) => {
    if (route.request().method() !== "POST") return route.fallback();
    await route.fulfill({
      status: 503,
      json: { code: 503, message: "Storage unavailable" },
    });
  });
  await page
    .getByRole("button", { name: "Create workspace", exact: true })
    .click();
  await expect(page.getByRole("dialog").getByRole("alert")).toHaveText(
    "Storage unavailable",
  );
  await expect(page.getByLabel("Name", { exact: true })).toHaveValue("draft");
});

test("workspace dialog cannot dismiss during a pending save", async ({
  page,
  fixture,
}) => {
  void fixture;
  let finish!: () => void;
  const waiting = new Promise<void>((resolve) => {
    finish = resolve;
  });
  await page.route("**/api/v1/workspaces/demo", async (route) => {
    if (route.request().method() !== "PUT") return route.fallback();
    await waiting;
    return route.fallback();
  });
  await page.goto("/admin/workspaces");
  await page.getByRole("button", { name: "Edit demo", exact: true }).click();
  await page
    .getByLabel("Description", { exact: true })
    .fill("Saved description");
  await page
    .getByRole("button", { name: "Save workspace", exact: true })
    .click();
  await expect(
    page.getByRole("button", { name: "Save workspace", exact: true }),
  ).toBeDisabled();
  await page.keyboard.press("Escape");
  await expect(page.getByRole("dialog")).toBeVisible();
  finish();
  await expect(page.getByRole("dialog")).toHaveCount(0);
  expect(fixture.workspaces[0].description).toBe("Saved description");
});

test("failed sign-out keeps the session available for retry", async ({
  page,
  fixture,
}) => {
  void fixture;
  await page.route("**/api/v1/auth/logout", (route) =>
    route.fulfill({
      status: 503,
      json: {
        code: 503,
        message: "Sign-out failed",
        detail: "Retry signing out",
      },
    }),
  );
  await page.goto("/admin/workspaces");
  await page.getByRole("button", { name: "Account menu" }).click();
  await page.getByRole("menuitem", { name: "End session" }).click();
  await expect(
    page.getByText(
      "Your session could not be revoked. Please retry End session.",
    ),
  ).toBeVisible();
  await expect(page).toHaveURL(/\/admin\/workspaces$/);
  await expect(
    page.getByRole("button", { name: "Account menu" }),
  ).toBeVisible();
});
