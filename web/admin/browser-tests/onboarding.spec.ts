import AxeBuilder from "@axe-core/playwright";
import { test, expect } from "./fixture";

test("guide supports keyboard dialog entry, dismissal and restoration", async ({
  page,
  fixture,
}) => {
  fixture.services = [];
  await page.goto("/admin/workspaces/demo");
  await expect(
    page.getByText("Publish your first dataset", { exact: true }),
  ).toBeVisible();
  await page.getByRole("button", { name: "Add store", exact: true }).focus();
  await page.keyboard.press("Enter");
  await expect(
    page.getByRole("dialog", { name: "Connect a store" }),
  ).toBeVisible();
  await page.keyboard.press("Escape");
  await expect(
    page.getByRole("button", { name: "Add store", exact: true }),
  ).toBeFocused();
  await page.getByRole("button", { name: "Hide guide" }).click();
  await page.reload();
  await expect(
    page.getByText("Publish your first dataset", { exact: true }),
  ).toHaveCount(0);
  await page
    .getByRole("button", { name: "Show getting-started guide" })
    .click();
  await page.getByRole("button", { name: "See all publishing steps" }).click();
  await expect(
    page.getByRole("link", { name: "Connect a client", exact: true }),
  ).toHaveAttribute("href", "/admin/workspaces/demo/endpoints");
  await expect(page.getByText(/Uploads are disabled/)).toBeVisible();
});

test("connection status distinguishes policy, real access checks and rendering", async ({
  page,
  fixture,
}) => {
  void fixture;
  let failed = true;
  await page.route(
    "**/workspaces/demo/ogc/collections/*/items?limit=1",
    (route) =>
      route.fulfill({
        status: failed ? 403 : 200,
        json: failed
          ? { message: "Forbidden" }
          : { type: "FeatureCollection", features: [] },
      }),
  );
  await page.goto("/admin/workspaces/demo/endpoints");
  await expect(page.getByLabel("Published layer")).toBeVisible();
  await page
    .getByText("Publication, access and rendering details", { exact: true })
    .click();
  await expect(page.getByText("Map rendering", { exact: true })).toBeVisible();
  await page
    .getByRole("button", { name: "Check access with my session" })
    .click();
  await expect(page.getByRole("alert")).toContainText(
    "Check the layer's allowed roles",
  );
  failed = false;
  await page.getByRole("button", { name: "Retry", exact: true }).click();
  await expect(
    page.getByRole("status").filter({ hasText: "Feature request succeeded" }),
  ).toContainText("0 sampled features");
  await page.getByLabel("Client example").selectOption("python");
  await expect(page.getByLabel("Connection example")).toContainText(
    'os.environ["NEOSRV_API_KEY"]',
  );
});

test("failed catalog is not presented as an empty or working connection", async ({
  page,
  fixture,
}) => {
  fixture.failStores = true;
  await page.goto("/admin/workspaces/demo/endpoints?layer=missing");
  await expect(page.getByRole("alert")).toContainText(
    "Could not load all published layers",
  );
  await expect(
    page.getByText("No feature layers published yet.", { exact: false }),
  ).toHaveCount(0);
  await expect(page.getByRole("button", { name: "Copy example" })).toHaveCount(
    0,
  );
  fixture.failStores = false;
  await page.getByRole("button", { name: "Retry", exact: true }).click();
  await expect(page.getByRole("alert")).toContainText(
    "The linked layer is not in the current catalog",
  );
});

for (const theme of ["light", "dark"] as const) {
  test(`onboarding, open form and client examples are accessible at narrow width (${theme})`, async ({
    page,
    fixture,
  }) => {
    void fixture;
    await page.setViewportSize({ width: 390, height: 844 });
    await page.emulateMedia({ colorScheme: theme, reducedMotion: "reduce" });
    async function scan() {
      await page.evaluate(
        (mode) =>
          document.documentElement.classList.toggle("dark", mode === "dark"),
        theme,
      );
      const results = await new AxeBuilder({ page })
        .withTags(["wcag2a", "wcag2aa", "wcag21a", "wcag21aa", "wcag22aa"])
        .analyze();
      expect(results.violations).toEqual([]);
      expect(
        await page.evaluate(
          () => document.documentElement.scrollWidth <= window.innerWidth,
        ),
      ).toBe(true);
    }
    await page.goto("/admin/workspaces/demo");
    await expect(
      page.getByText("Your workspace is publishing", { exact: true }),
    ).toBeVisible();
    await scan();
    await page.getByRole("button", { name: "Review publishing guide" }).click();
    await page.getByRole("button", { name: "Add store", exact: true }).click();
    await expect(page.getByRole("dialog")).toBeVisible();
    await scan();
    await page.keyboard.press("Escape");
    await page.goto("/admin/workspaces/demo/endpoints");
    await expect(page.getByLabel("Connection example")).toBeVisible();
    await scan();
  });
}
