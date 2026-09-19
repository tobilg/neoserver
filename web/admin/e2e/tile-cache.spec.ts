import { gotoConsole } from "./fixtures";
import { expect, test } from "@playwright/test";
import { readFixture, signInWithToken } from "./fixtures";

test("the caching page reports live stats", async ({ page }) => {
  const { bootstrapToken, workspace } = await readFixture();
  await signInWithToken(page, bootstrapToken);
  await gotoConsole(page, `/workspaces/${workspace}/caching`);
  await expect(
    page.getByRole("heading", { name: "Caching", level: 1 }),
  ).toBeVisible();
});

test("seed jobs list and poll without a persistent cache configured", async ({
  page,
}) => {
  const { bootstrapToken, workspace } = await readFixture();
  await signInWithToken(page, bootstrapToken);
  await gotoConsole(page, `/workspaces/${workspace}/caching`);

  // The fixture runs with NEOSRV_PERSISTENTCACHE_ENABLED=false, so the page
  // must explain that rather than appearing broken or spinning forever.
  await expect(
    page.getByText(/disabled|not enabled|no jobs/i).first(),
  ).toBeVisible();
});

test("the response cache can be cleared from the caching page", async ({
  page,
}) => {
  const { bootstrapToken, workspace } = await readFixture();
  await signInWithToken(page, bootstrapToken);
  // The former Cache page URL redirects here.
  await gotoConsole(page, `/workspaces/${workspace}/cache`);
  await expect(page).toHaveURL(/\/caching$/);
  await page.getByRole("button", { name: "Clear response cache" }).click();
  await expect(page.getByText("Response cache cleared.")).toBeVisible();
});
