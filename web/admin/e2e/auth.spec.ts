import { gotoConsole } from "./fixtures";
import { expect, test } from "@playwright/test";
import { chooseTokenSignIn, readFixture, signInWithToken } from "./fixtures";

test("a bootstrap token establishes a console session", async ({ page }) => {
  const { bootstrapToken } = await readFixture();
  await signInWithToken(page, bootstrapToken);
  await gotoConsole(page, "/workspaces");
  await expect(page.getByRole("heading", { name: "Workspaces" })).toBeVisible();
});

test("an invalid token is rejected without starting a session", async ({
  page,
}) => {
  await gotoConsole(page, "/login");
  await chooseTokenSignIn(page);
  await page.getByLabel("API key or JWT").fill("nsk_not-a-real-key");
  await page.getByRole("button", { name: "Start session" }).click();
  await expect(page).toHaveURL(/\/login/);
  await expect(page.getByRole("alert")).toBeVisible();
});

test("an authenticated session is restored on reload", async ({ page }) => {
  const { bootstrapToken } = await readFixture();
  await signInWithToken(page, bootstrapToken);
  await page.reload();
  await expect(page).not.toHaveURL(/\/login/);
});

test("signing out clears the session", async ({ page }) => {
  const { bootstrapToken } = await readFixture();
  await signInWithToken(page, bootstrapToken);
  await page.getByRole("button", { name: "Account menu" }).click();
  await page.getByRole("menuitem", { name: "End session" }).click();
  await expect(page).toHaveURL(/\/login/);

  // The cleared cookie must not still authorise the API.
  await gotoConsole(page, "/workspaces");
  await expect(page).toHaveURL(/\/login/);
});

test("a safe next route is honoured after sign-in", async ({ page }) => {
  const { bootstrapToken, workspace } = await readFixture();
  await signInWithToken(
    page,
    bootstrapToken,
    `/workspaces/${workspace}/stores`,
  );
  await expect(page).toHaveURL(new RegExp(`/workspaces/${workspace}/stores$`));
});

test("an off-origin next value cannot redirect away from the console", async ({
  page,
}) => {
  const { bootstrapToken } = await readFixture();
  await gotoConsole(page, "/login?next=https%3A%2F%2Fevil.example");
  await chooseTokenSignIn(page);
  await page.getByLabel("API key or JWT").fill(bootstrapToken);
  await page.getByRole("button", { name: "Start session" }).click();
  await expect(page).toHaveURL(/localhost:\d+\/admin/);
});
