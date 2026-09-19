import { test, expect } from "./fixture";
import AxeBuilder from "@axe-core/playwright";

test("audit searches retained history, follows cursors, and contains wide tables", async ({
  page,
  fixture,
}) => {
  expect(fixture.services.length).toBeGreaterThan(0);
  const requested: URL[] = [];
  await page.route("**/api/v1/audit?*", async (route) => {
    const url = new URL(route.request().url());
    requested.push(url);
    const older = url.searchParams.has("cursor");
    await route.fulfill({
      json: {
        events: Array.from({ length: 25 }, (_, index) => ({
          id: `${older ? "old" : "new"}-${index}`,
          timestamp: "2026-09-14T10:00:00Z",
          principal: older ? "historic-operator" : "current-operator",
          credential_id: "non-secret-credential-with-a-long-identifier",
          auth_method: "jwt",
          operation: "publish",
          workspace: "demo",
          action: "change",
          method: "POST",
          path: "/api/v1/workspaces/demo/services/source/layers",
          status: 201,
          duration_ms: 21,
        })),
        ...(older ? {} : { next_cursor: "older-page" }),
      },
    });
  });
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto("/admin/audit");
  await expect(page.getByText("current-operator").first()).toBeVisible();
  await page.getByRole("table", { name: "Scrollable data table" }).focus();
  await expect(
    page.getByRole("table", { name: "Scrollable data table" }),
  ).toBeFocused();
  for (const width of [1024, 1280, 1440]) {
    await page.setViewportSize({ width, height: 900 });
    await expect
      .poll(() => page.evaluate(() => document.documentElement.scrollWidth))
      .toBeLessThanOrEqual(width);
    const columns = await page
      .getByRole("button", { name: "Columns", exact: true })
      .boundingBox();
    expect(columns!.x + columns!.width).toBeLessThanOrEqual(width);
    expect(
      (
        await new AxeBuilder({ page })
          .withRules(["scrollable-region-focusable"])
          .analyze()
      ).violations,
    ).toEqual([]);
  }
  await page.getByRole("button", { name: "Older events" }).click();
  await expect(page.getByText("historic-operator").first()).toBeVisible();
  expect(requested.at(-1)?.searchParams.get("cursor")).toBe("older-page");
  await expect(
    page.getByRole("button", { name: "Older events" }),
  ).toBeDisabled();
  await page
    .getByLabel("Search all retained events", { exact: true })
    .fill("publish");
  await expect
    .poll(() => requested.at(-1)?.searchParams.get("q"))
    .toBe("publish");
  expect(requested.at(-1)?.searchParams.has("cursor")).toBe(false);
  await expect(page.getByText("Page 1 of 4")).toHaveCount(0);
});
