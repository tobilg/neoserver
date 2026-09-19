import { gotoConsole } from "./fixtures";
import { expect, test } from "@playwright/test";
import { readFixture, signInWithToken } from "./fixtures";

const SLD = `<?xml version="1.0" encoding="UTF-8"?>
<StyledLayerDescriptor xmlns="http://www.opengis.net/sld" version="1.0.0">
  <NamedLayer>
    <Name>e2e-style</Name>
    <UserStyle>
      <FeatureTypeStyle>
        <Rule>
          <PolygonSymbolizer>
            <Fill><CssParameter name="fill">#c81e78</CssParameter></Fill>
          </PolygonSymbolizer>
        </Rule>
      </FeatureTypeStyle>
    </UserStyle>
  </NamedLayer>
</StyledLayerDescriptor>`;

test("creates a style and lists it in the workspace", async ({ page }) => {
  const { workspaceAdminKey, workspace } = await readFixture();
  const name = `e2e-style-${Date.now()}`;

  await signInWithToken(page, workspaceAdminKey);
  await gotoConsole(page, `/workspaces/${workspace}/styles`);

  await page.getByRole("button", { name: "New style", exact: true }).click();
  await page.getByLabel("Style name", { exact: true }).fill(name);
  await page.getByRole("button", { name: "Create style", exact: true }).click();
  await expect(
    page.getByRole("combobox", { name: "Style", exact: true }),
  ).toContainText(name);
  await page.getByRole("textbox", { name: "SLD XML" }).fill(SLD);
  const saved = page.waitForResponse(
    (response) =>
      response.request().method() === "PUT" &&
      response.url().includes(`/styles/${name}`),
  );
  await page.getByRole("button", { name: /Save style/ }).click();
  expect((await saved).ok()).toBeTruthy();
  await page.reload();
  await page.getByRole("combobox", { name: "Style", exact: true }).click();
  await page.getByRole("option", { name, exact: true }).click();
  await expect(page.getByRole("textbox", { name: "SLD XML" })).toContainText(
    "#c81e78",
  );
});

test("an invalid SLD is rejected with the server's message", async ({
  page,
}) => {
  const { bootstrapToken, workspace } = await readFixture();
  await signInWithToken(page, bootstrapToken);
  await gotoConsole(page, `/workspaces/${workspace}/styles`);

  await page.getByRole("button", { name: "New style", exact: true }).click();
  await page
    .getByLabel("Style name", { exact: true })
    .fill(`bad-${Date.now()}`);
  await page.getByRole("button", { name: "Create style", exact: true }).click();
  const editor = page.getByRole("textbox", { name: "SLD XML" });
  await expect(editor).toContainText("StyledLayerDescriptor");
  await editor.fill("<StyledLayerDescriptor>unclosed");
  const rejected = page.waitForResponse(
    (response) =>
      response.request().method() === "PUT" &&
      response.url().includes("/styles/"),
  );
  await page.getByRole("button", { name: /Save style/ }).click();
  expect((await rejected).status()).toBe(400);
  await expect(page.getByRole("alert")).toContainText(/invalid|failed|error/i);
  await expect(editor).toContainText("<StyledLayerDescriptor>unclosed");
});

test("the preview page loads for the workspace", async ({ page }) => {
  const { bootstrapToken, workspace } = await readFixture();
  await signInWithToken(page, bootstrapToken);
  await gotoConsole(page, `/workspaces/${workspace}/preview`);
  // Cookie auth alone must be enough for the map and its tile requests.
  await expect(page.locator("canvas, .maplibregl-map").first()).toBeVisible({
    timeout: 30_000,
  });
});
