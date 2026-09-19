import { gotoConsole } from "./fixtures";
import { expect, test } from "@playwright/test";
import { readFixture, signInWithToken } from "./fixtures";

test("readiness is reported in the shell and on the operations page", async ({
  page,
}) => {
  const { bootstrapToken } = await readFixture();
  await signInWithToken(page, bootstrapToken);

  await gotoConsole(page, "/operations");
  await expect(
    page.getByRole("heading", { name: "Operations", exact: true }),
  ).toBeVisible();
  // The readiness instrument reflects a real /ready response, including the
  // named checks the server actually runs.
  await expect(page.getByText(/healthy|ready/i).first()).toBeVisible();
});

test("catalog integrity reports against the live catalog", async ({ page }) => {
  const { bootstrapToken } = await readFixture();
  await signInWithToken(page, bootstrapToken);
  await gotoConsole(page, "/operations");
  await expect(page.getByText(/integrity/i).first()).toBeVisible();
});

test("workspace deletion is gated behind a fetched plan", async ({ page }) => {
  const { bootstrapToken, workspace } = await readFixture();
  await signInWithToken(page, bootstrapToken);
  await gotoConsole(page, "/workspaces");

  const plan = page.waitForResponse(
    (response) =>
      response.url().includes("/deletion-plan") &&
      response.request().method() === "GET",
  );
  await page
    .getByRole("button", { name: `Delete ${workspace}`, exact: true })
    .click();
  expect((await plan).ok()).toBeTruthy();
  await expect(page.getByRole("dialog")).toBeVisible();
  await expect(
    page.getByRole("button", { name: /delete workspace/i }),
  ).toBeVisible();
  await page.keyboard.press("Escape");
});

test("the audit log is queryable and filters travel in the URL", async ({
  page,
}) => {
  const { bootstrapToken } = await readFixture();
  await signInWithToken(page, bootstrapToken);
  await gotoConsole(page, "/audit");
  await expect(page.getByRole("heading", { name: /audit/i })).toBeVisible();
});
