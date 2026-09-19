import { test, expect, answerConfirm } from "./fixture";

test("workspace description saves, clears, and guards dirty dismissal", async ({
  page,
  fixture,
}) => {
  fixture.workspaces[0].description = "Original description";
  await page.goto("/admin/workspaces/demo");
  await page
    .getByRole("button", { name: "Edit workspace", exact: true })
    .click();
  await page
    .getByLabel("Description", { exact: true })
    .fill("Saved description");
  await page.getByRole("button", { name: "Save workspace" }).click();
  await expect(page.getByRole("dialog")).toHaveCount(0);
  await expect(
    page.getByText("Saved description", { exact: true }),
  ).toBeVisible();
  await page
    .getByRole("button", { name: "Edit workspace", exact: true })
    .click();
  await expect(page.getByLabel("Description", { exact: true })).toHaveValue(
    "Saved description",
  );
  await page
    .getByLabel("Description", { exact: true })
    .fill("Unsaved description");
  await page.getByRole("button", { name: "Close", exact: true }).click();
  await answerConfirm(page, "keep");
  await expect(page.getByLabel("Description", { exact: true })).toHaveValue(
    "Unsaved description",
  );
  await page.keyboard.press("Escape");
  await answerConfirm(page, "keep");
  await page.getByLabel("Description", { exact: true }).fill("");
  await page.getByRole("button", { name: "Save workspace" }).click();
  await expect(page.getByRole("dialog")).toHaveCount(0);
  await expect(
    page.getByText("Saved description", { exact: true }),
  ).toHaveCount(0);
  await page
    .getByRole("button", { name: "Edit workspace", exact: true })
    .click();
  await expect(page.getByLabel("Description", { exact: true })).toHaveValue("");
});

test("workspace rename refreshes navigation without a spurious dirty prompt", async ({
  page,
  fixture,
}) => {
  await page.goto("/admin/workspaces/demo");
  await page
    .getByRole("button", { name: "Edit workspace", exact: true })
    .click();
  await page.getByLabel("Name", { exact: true }).fill("renamed");
  await page.getByRole("button", { name: "Save workspace" }).click();
  await expect(page).toHaveURL(/\/workspaces\/renamed$/);
  await expect(page.getByRole("alertdialog")).toHaveCount(0);
  expect(fixture.workspaces[0].name).toBe("renamed");
  await expect(
    page.getByRole("heading", { name: "renamed", exact: true }),
  ).toBeVisible();
});
