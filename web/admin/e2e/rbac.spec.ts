import { gotoConsole } from "./fixtures";
import { expect, test } from "@playwright/test";
import { chooseTokenSignIn, readFixture, signInWithToken } from "./fixtures";

test("a workspace admin never enumerates the global workspace collection", async ({
  page,
}) => {
  const { workspaceAdminKey, workspace } = await readFixture();

  // Decision D6: GET /api/v1/workspaces is super-admin only, so the console
  // must discover workspaces from /auth/me instead. A regression here produces
  // a 403 that stays invisible until a real user hits it.
  let listRequests = 0;
  const failedRequests: string[] = [];
  page.on("response", (response) => {
    if (
      response.url().includes("/api/v1/workspaces/") &&
      response.status() >= 400
    ) {
      failedRequests.push(`${response.status()} ${response.url()}`);
    }
  });
  page.on("request", (request) => {
    if (new URL(request.url()).pathname.endsWith("/api/v1/workspaces")) {
      listRequests += 1;
    }
  });

  await signInWithToken(page, workspaceAdminKey);
  const storesLoaded = page.waitForResponse(
    (response) =>
      response.url().endsWith(`/api/v1/workspaces/${workspace}/services`) &&
      response.request().method() === "GET",
  );
  await gotoConsole(page, `/workspaces/${workspace}/layers`);
  expect((await storesLoaded).status()).toBe(200);
  await expect(page.getByRole("heading", { name: "Layers" })).toBeVisible();
  const stylesLoaded = page.waitForResponse(
    (response) =>
      response.url().endsWith(`/api/v1/workspaces/${workspace}/styles`) &&
      response.request().method() === "GET",
  );
  await gotoConsole(page, `/workspaces/${workspace}/styles`);
  expect((await stylesLoaded).status()).toBe(200);
  await expect(page.getByRole("heading", { name: "Styles" })).toBeVisible();

  expect(listRequests).toBe(0);
  expect(failedRequests).toEqual([]);
});

test("server-scope navigation is absent for a workspace admin", async ({
  page,
}) => {
  const { workspaceAdminKey, workspace } = await readFixture();
  await signInWithToken(page, workspaceAdminKey);
  await gotoConsole(page, `/workspaces/${workspace}`);

  for (const label of [
    "Workspaces",
    "Roles & policies",
    "Tile matrix sets",
    "Audit",
  ]) {
    await expect(page.getByRole("link", { name: label })).toHaveCount(0);
  }
});

test("a viewer credential lands on the no-access page", async ({ page }) => {
  const { viewerKey } = await readFixture();
  await gotoConsole(page, "/login");
  await chooseTokenSignIn(page);
  await page.getByLabel("API key or JWT").fill(viewerKey);
  await page.getByRole("button", { name: "Start session" }).click();

  // Expected outcome, not an error: the console requires workspace admin.
  await expect(page).toHaveURL(/\/no-access/);
  await expect(
    page.getByText("This account can't use the console"),
  ).toBeVisible();
});

test("a workspace admin reaching a server-scope route is redirected", async ({
  page,
}) => {
  const { workspaceAdminKey } = await readFixture();
  await signInWithToken(page, workspaceAdminKey);
  await gotoConsole(page, "/roles");
  await expect(page).not.toHaveURL(/\/roles$/);
});
