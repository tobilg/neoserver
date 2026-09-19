import { test, expect, answerConfirm } from "./fixture";
import AxeBuilder from "@axe-core/playwright";
const populatedStyle = `<?xml version="1.0"?>
<StyledLayerDescriptor version="1.0.0" xmlns="http://www.opengis.net/sld">
<!-- A readable populated editor, including comments and attribute values. -->
<NamedLayer><Name>points</Name><UserStyle><FeatureTypeStyle><Rule>
<PointSymbolizer><Graphic><Mark><WellKnownName>circle</WellKnownName>
<Fill><CssParameter name="fill">#5fa8cc</CssParameter></Fill>
</Mark><Size>10</Size></Graphic></PointSymbolizer>
</Rule></FeatureTypeStyle></UserStyle></NamedLayer>
${"<!-- Additional lines exercise keyboard scrolling. -->\n".repeat(40)}
</StyledLayerDescriptor>`;

test("connection edits invalidate success and late responses, including raw JSON", async ({
  page,
  fixture,
}) => {
  void fixture;
  let release: (() => void) | undefined;
  let requests = 0;
  await page.route(
    "**/api/v1/workspaces/demo/services/test-connection",
    async (route) => {
      requests++;
      if (requests === 2)
        await new Promise<void>((resolve) => {
          release = resolve;
        });
      await route.fulfill({ json: { ok: true, duration_ms: 12 } });
    },
  );
  await page.goto("/admin/workspaces/demo/stores");
  await page.getByRole("button", { name: "Add store", exact: true }).click();
  await page.getByLabel("Database", { exact: true }).fill("good");
  await page.getByRole("button", { name: "Test connection" }).click();
  await expect(page.getByRole("status")).toContainText("Connected in");
  await page.getByLabel("Database", { exact: true }).fill("changed");
  await expect(page.getByRole("status")).toContainText("not tested");
  await page.getByRole("button", { name: "Test connection" }).click();
  await expect.poll(() => Boolean(release)).toBe(true);
  await page.getByLabel("Database", { exact: true }).fill("newer-draft");
  release!();
  await expect(
    page.getByRole("button", { name: "Test connection" }),
  ).toBeEnabled();
  await expect(page.getByRole("status")).toContainText("not tested");
  await expect(page.getByText(/Connected in/)).toHaveCount(0);
  await page.getByRole("button", { name: "Test connection" }).click();
  await expect(page.getByRole("status")).toContainText("Connected in");
  await page
    .locator("summary", { hasText: "Advanced connection JSON" })
    .click();
  const json = page.getByLabel("Advanced connection JSON", { exact: true });
  const next = JSON.parse(await json.inputValue());
  next.database = "raw-change";
  await json.fill(JSON.stringify(next));
  await expect(page.getByText(/Connected in/)).toHaveCount(0);
  await expect(
    page.getByRole("button", { name: "Test connection" }),
  ).toBeDisabled();
  await page.getByRole("button", { name: "Apply JSON" }).click();
  await expect(page.getByLabel("Database", { exact: true })).toHaveValue(
    "raw-change",
  );
  await expect(
    page.getByRole("button", { name: "Test connection" }),
  ).toBeEnabled();
});

for (const disabled of ["layer", "store"] as const) {
  test(`disabled ${disabled} is explained in catalog, preview and connections`, async ({
    page,
    fixture,
  }) => {
    if (disabled === "layer") fixture.layer.enabled = false;
    else fixture.services[0].enabled = false;
    let reads = 0;
    await page.route(
      "**/workspaces/demo/ogc/collections/roads/items?*",
      (route) => {
        reads++;
        return route.fulfill({
          json: { type: "FeatureCollection", features: [] },
        });
      },
    );
    await page.goto("/admin/workspaces/demo/layers");
    const row = page.getByRole("row").filter({ hasText: "roads" });
    await expect(
      row
        .getByText(disabled === "layer" ? "Off" : "Store off", {
          exact: true,
        })
        .filter({ visible: true }),
    ).toBeVisible();
    await row.getByRole("link", { name: "Preview", exact: true }).click();
    await expect(page.getByRole("alert")).toContainText(
      disabled === "layer" ? "publication is disabled" : "store is disabled",
    );
    await expect(page.getByText(/Coverages and groups require/)).toHaveCount(0);
    expect(reads).toBe(0);
    expect(fixture.wmsRequests).toEqual([]);
    await page.getByRole("link", { name: "Connect a client" }).click();
    await expect(
      page.getByText(
        disabled === "layer"
          ? /Published, but layer is disabled/
          : /Store disabled/,
      ),
    ).toBeVisible();
    await expect(
      page.getByRole("button", { name: "Check access with my session" }),
    ).toBeDisabled();
    await page.goBack();
    fixture.layer.enabled = true;
    fixture.services[0].enabled = true;
    await page.getByRole("button", { name: "Refresh preview" }).click();
    await expect(page.getByRole("alert")).toHaveCount(0);
    await expect.poll(() => reads).toBeGreaterThan(0);
  });
}

test("stale preview links explain missing publications", async ({
  page,
  fixture,
}) => {
  void fixture;
  await page.goto("/admin/workspaces/demo/preview?layers=deleted-layer");
  await expect(page.getByRole("alert")).toContainText(
    "no longer exists or is not accessible",
  );
  await expect(page.getByText(/Coverages and groups require/)).toHaveCount(0);
  await page.getByRole("button", { name: "Remove from preview" }).click();
  await expect(page).not.toHaveURL(/layers=/);
});

test("key expiry updates badges and filtering without a reload", async ({
  page,
  fixture,
}) => {
  void fixture;
  const now = new Date("2030-01-01T00:00:00Z");
  await page.clock.install({ time: now });
  await page.route("**/api/v1/workspaces/demo/apikeys", (route) =>
    route.fulfill({
      json: {
        api_keys: [
          {
            id: "future",
            name: "Soon",
            expires_at: new Date(+now + 60_000).toISOString(),
            revoked: false,
          },
          {
            id: "past",
            name: "Past",
            expires_at: new Date(+now - 1).toISOString(),
            revoked: false,
          },
          {
            id: "revoked",
            name: "Revoked",
            expires_at: new Date(+now - 1).toISOString(),
            revoked: true,
          },
          { id: "never", name: "Never", revoked: false },
        ].map((key) => ({
          ...key,
          role_id: "viewer",
          key_prefix: "mock",
          created_at: now.toISOString(),
          updated_at: now.toISOString(),
        })),
      },
    }),
  );
  await page.goto("/admin/workspaces/demo/api-keys");
  const soon = page.getByRole("row").filter({ hasText: "Soon" });
  await expect(soon.locator('[data-status="active"]')).toHaveText("Active");
  await expect(
    page
      .getByRole("row")
      .filter({ hasText: "Past" })
      .locator('[data-status="expired"]'),
  ).toBeVisible();
  await expect(
    page
      .getByRole("row")
      .filter({ hasText: "Revoked" })
      .locator('[data-status="revoked"]'),
  ).toBeVisible();
  await page.clock.fastForward(60_001);
  await expect(soon.locator('[data-status="expired"]')).toHaveText("Expired");
  await page.getByPlaceholder("Filter keys…").fill("expired");
  await expect(page.getByRole("row").filter({ hasText: "Soon" })).toBeVisible();
  await expect(page.getByRole("row").filter({ hasText: "Never" })).toHaveCount(
    0,
  );
});

// Only the fake nsk_once fixture is exposed; never run these with real credentials.
for (const clipboard of ["allowed", "denied", "unavailable"] as const) {
  test(`one-time key copy handles ${clipboard} clipboard`, async ({
    page,
    fixture,
  }) => {
    void fixture;
    const errors: string[] = [];
    page.on("pageerror", (error) => errors.push(error.message));
    await page.addInitScript(
      (mode) =>
        Object.defineProperty(navigator, "clipboard", {
          configurable: true,
          value:
            mode === "unavailable"
              ? undefined
              : {
                  writeText: async () => {
                    if (mode === "denied")
                      throw new DOMException("Denied", "NotAllowedError");
                  },
                },
        }),
      clipboard,
    );
    await page.goto("/admin/workspaces/demo/api-keys");
    await page.getByRole("button", { name: "Create key", exact: true }).click();
    await page.getByLabel("Key name").fill("Mock reader");
    await page
      .getByRole("dialog")
      .getByRole("button", { name: "Create key", exact: true })
      .click();
    const secret = page.getByRole("dialog", { name: "Copy this key now" });
    await secret.getByRole("button", { name: "Copy key", exact: true }).click();
    if (clipboard === "allowed")
      await expect(
        secret.getByRole("button", { name: "Copied", exact: true }),
      ).toBeVisible();
    else {
      await expect(secret.getByRole("alert")).toContainText(
        "Save it before closing",
      );
      await expect(
        secret.getByRole("button", { name: "Copied", exact: true }),
      ).toHaveCount(0);
      await secret.getByRole("button", { name: "Select key" }).click();
      expect(await page.evaluate(() => window.getSelection()?.toString())).toBe(
        "nsk_once",
      );
    }
    expect(errors).toEqual([]);
  });
}

for (const width of [320, 390, 768]) {
  test(`import planning stays contained at ${width}px`, async ({
    page,
    fixture,
  }) => {
    void fixture;
    await page.setViewportSize({ width, height: 900 });
    // Keep the narrowest content column: an expanded sidebar where one exists.
    await page.addInitScript(() => localStorage.setItem("sidebar", "open"));
    await page.goto("/admin/workspaces/demo/imports");
    await page.getByRole("link", { name: "Import roads", exact: true }).click();
    const fits = () =>
      page.evaluate(() => document.documentElement.scrollWidth <= innerWidth);
    await expect.poll(fits).toBe(true);
    await page
      .getByRole("button", { name: "Validate plan", exact: true })
      .click();
    await expect(page.getByRole("alert")).toContainText("already published");
    await expect.poll(fits).toBe(true);
    const mapping = page.getByRole("region", {
      name: /Field mapping for roads/,
    });
    await mapping.focus();
    await expect(mapping).toBeFocused();
    expect(
      await mapping.evaluate((node) => node.clientWidth < node.scrollWidth),
    ).toBe(true);
    await page.keyboard.press("ArrowRight");
    await expect
      .poll(() => mapping.evaluate((node) => node.scrollLeft))
      .toBeGreaterThan(0);
    const target = page.getByLabel("Target name for roads.name");
    expect((await target.boundingBox())!.width).toBeGreaterThanOrEqual(128);
    await target.fill("road_name");
    await page
      .getByLabel("Public ID *", { exact: true })
      .fill("imported_roads");
    await page
      .getByRole("button", { name: "Validate plan", exact: true })
      .click();
    await expect(
      page.getByRole("region", { name: "Feature preview map" }),
    ).toBeVisible();
    await expect.poll(fits).toBe(true);
  });
}

test("mobile navigation closes after success, retains blocked drafts and desktop preference", async ({
  page,
  fixture,
}) => {
  void fixture;
  await page.addInitScript(() => localStorage.setItem("sidebar", "closed"));
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto("/admin/workspaces/demo/styles");
  const editor = page.getByRole("textbox", { name: "SLD XML" });
  await editor.fill("<draft>keep me</draft>");
  await page.getByRole("button", { name: "Toggle Sidebar" }).click();
  const sidebar = page.getByRole("dialog", { name: "Sidebar", exact: true });
  await sidebar
    .getByRole("link", { name: "Service settings", exact: true })
    .click();
  await answerConfirm(page, "keep");
  await expect(sidebar).toBeVisible();
  await expect(page).toHaveURL(/\/styles$/);
  await sidebar
    .getByRole("link", { name: "Service settings", exact: true })
    .focus();
  await page.keyboard.press("Enter");
  await answerConfirm(page, "discard");
  await expect(page).toHaveURL(/\/settings$/);
  await expect(sidebar).not.toBeVisible();
  await expect(page.getByRole("heading", { level: 1 })).toBeFocused();
  await page.getByRole("button", { name: "Toggle Sidebar" }).click();
  await sidebar.getByRole("link", { name: "Layers", exact: true }).click();
  await expect(sidebar).not.toBeVisible();
  await expect(
    page.getByRole("heading", { level: 1, name: "Layers" }),
  ).toBeFocused();
  await page.setViewportSize({ width: 1280, height: 900 });
  await expect(
    page.locator('[data-slot="sidebar"][data-state="collapsed"]'),
  ).toBeVisible();
});

for (const entryPoint of ["Stores", "Overview"] as const) {
  test(`new-store drafts guard Back and reload on ${entryPoint}, and reset after creation`, async ({
    page,
    fixture,
  }) => {
    void fixture;
    await page.route("**/api/v1/workspaces/demo/services", async (route) => {
      if (route.request().method() !== "POST") return route.fallback();
      return route.fulfill({
        status: 201,
        json: { ...route.request().postDataJSON(), id: "created-store" },
      });
    });
    await page.goto("/admin/workspaces/demo/layers");
    await page.getByRole("link", { name: entryPoint, exact: true }).click();
    const editorURL = page.url();
    if (entryPoint === "Overview")
      await page
        .getByRole("button", { name: "Review publishing guide" })
        .click();
    await page.getByRole("button", { name: "Add store", exact: true }).click();
    await page.getByLabel("Store name").fill("new-postgis");
    await page.getByLabel("Host", { exact: true }).fill("localhost");
    await page.getByLabel("Database", { exact: true }).fill("postgis");
    await page.getByLabel("User", { exact: true }).fill("postgres");
    await page.goBack();
    await answerConfirm(page, "keep");
    await expect(page).toHaveURL(editorURL);
    await expect(page.getByLabel("Database", { exact: true })).toHaveValue(
      "postgis",
    );
    const reloadPrompt = page.waitForEvent("dialog");
    const dismissUnload = (dialog: import("@playwright/test").Dialog) => {
      void dialog.dismiss();
    };
    page.on("dialog", dismissUnload);
    const reload = page.reload({ timeout: 2000 }).catch(() => undefined);
    expect((await reloadPrompt).type()).toBe("beforeunload");
    await reload;
    page.off("dialog", dismissUnload);
    await expect(page.getByLabel("Store name")).toHaveValue("new-postgis");
    await page
      .getByRole("dialog")
      .getByRole("button", { name: "Add store", exact: true })
      .click();
    await expect(page.getByRole("dialog")).not.toBeVisible();
    await page.getByRole("button", { name: "Add store", exact: true }).click();
    await expect(page.getByLabel("Store name")).toHaveValue("");
    await expect(page.getByLabel("Database", { exact: true })).toHaveValue("");
    await page.keyboard.press("Escape");
    await page.goBack();
    await expect(page).toHaveURL(/\/layers$/);
    await page.getByRole("link", { name: entryPoint, exact: true }).click();
    if (entryPoint === "Overview")
      await page
        .getByRole("button", { name: "Review publishing guide" })
        .click();
    await page.getByRole("button", { name: "Add store", exact: true }).click();
    await page.getByLabel("Store name").fill("discard-this-draft");
    await page.goBack();
    await answerConfirm(page, "discard");
    await expect(page).toHaveURL(/\/layers$/);
  });
}

test("mobile workspace selection closes the sheet and focuses the destination", async ({
  page,
  fixture,
}) => {
  fixture.workspaces.push({
    ...fixture.workspaces[0],
    id: "other",
    name: "other",
  });
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto("/admin/workspaces/demo/layers");
  await page.getByRole("button", { name: "Toggle Sidebar" }).click();
  const sidebar = page.getByRole("dialog", { name: "Sidebar", exact: true });
  await sidebar
    .getByRole("combobox", { name: "Workspace", exact: true })
    .click();
  await page
    .getByRole("option", { name: "other · admin", exact: true })
    .click();
  await expect(page).toHaveURL(/\/workspaces\/other$/);
  await expect(sidebar).not.toBeVisible();
  await expect(page.getByRole("heading", { level: 1 })).toBeFocused();
});

for (const theme of ["light", "dark"] as const) {
  test(`populated editor and read-only fields are accessible (${theme})`, async ({
    page,
    fixture,
  }) => {
    fixture.styles.one = populatedStyle;
    await page.setViewportSize({ width: 390, height: 844 });
    await page.emulateMedia({ colorScheme: theme, reducedMotion: "reduce" });
    await page.goto("/admin/workspaces/demo/styles");
    await expect(page.getByRole("textbox", { name: "SLD XML" })).toBeVisible();
    await page.addStyleTag({
      content:
        "*,*::before,*::after { transition:none!important; animation:none!important }",
    });
    await page.evaluate(
      (mode) =>
        document.documentElement.classList.toggle("dark", mode === "dark"),
      theme,
    );
    const editor = page.getByRole("textbox", { name: "SLD XML" });
    await page.getByLabel("Layout", { exact: true }).focus();
    await page.keyboard.press("Tab");
    await expect(
      page.getByRole("tab", { name: "Editor", exact: true }),
    ).toBeFocused();
    await page.keyboard.press("Tab");
    await expect(editor).toBeFocused();
    await page.keyboard.press("ControlOrMeta+End");
    const results = await new AxeBuilder({ page })
      .withTags(["wcag2a", "wcag2aa", "wcag21a", "wcag21aa", "wcag22aa"])
      .analyze();
    expect(results.violations).toEqual([]);
    // Axe briefly opens an auxiliary page; restore the tested page before
    // exercising real keyboard input (not just DOM focus).
    await page.bringToFront();
    const assets = page.getByRole("table", {
      name: "Style assets, scroll to see all columns",
    });
    await assets.focus();
    await expect(assets).toBeFocused();
    const scroller = assets.locator("..");
    await page.keyboard.press("ArrowRight");
    await expect
      .poll(() => scroller.evaluate((node) => node.scrollLeft))
      .toBeGreaterThan(0);
    await editor.focus();
    await page.keyboard.press("ControlOrMeta+A");
    await page.keyboard.insertText(
      "<draft>keep this while changing theme</draft>",
    );
    await expect(editor).toContainText("keep this while changing theme");
    await page.getByRole("button", { name: /Color theme/ }).click();
    await page
      .getByRole("menuitemradio", {
        name: theme === "light" ? "Dark" : "Light",
        exact: true,
      })
      .click();
    await expect(editor).toContainText("keep this while changing theme");
  });
}

for (const width of [1440, 390]) {
  test(`one-time API key renders as one readable block at ${width}px`, async ({
    page,
    fixture,
  }) => {
    void fixture;
    const key = `nsk_${"5345854888c3588329f2898243eecd81755fca4d0033512fbb5f7fa77d438894"}`;
    await page.setViewportSize({ width, height: 900 });
    await page.route("**/api/v1/workspaces/demo/apikeys", async (route) => {
      if (route.request().method() !== "POST") return route.fallback();
      await route.fulfill({
        status: 201,
        json: {
          id: "k-long",
          name: "Long key",
          key_prefix: "nsk_53458548",
          role_id: "viewer",
          revoked: false,
          created_at: "2026-09-16T10:00:00Z",
          updated_at: "2026-09-16T10:00:00Z",
          key,
        },
      });
    });
    await page.goto("/admin/workspaces/demo/api-keys");
    await page.getByRole("button", { name: "Create key", exact: true }).click();
    await page.getByLabel("Key name").fill("Long key");
    await page
      .getByRole("dialog")
      .getByRole("button", { name: "Create key", exact: true })
      .click();
    const secret = page
      .getByRole("dialog", { name: "Copy this key now" })
      .getByLabel("One-time API key");
    await expect(secret).toHaveText(key);
    const layout = await secret.evaluate((element) => {
      const box = element.getBoundingClientRect();
      const text = document.createRange();
      text.selectNodeContents(element);
      const first = text.getClientRects()[0];
      return {
        display: getComputedStyle(element).display,
        firstLineInside:
          first.left >= box.left &&
          first.top >= box.top &&
          first.right <= box.right,
        rects: element.getClientRects().length,
      };
    });
    expect(layout).toEqual({
      display: "block",
      firstLineInside: true,
      rects: 1,
    });
    await expect(page.getByText("Listed as nsk_53458548")).toBeVisible();
  });
}

for (const viewport of [
  { width: 820, height: 1180 },
  { width: 1024, height: 768 },
]) {
  test(`tables keep row actions reachable at ${viewport.width}px`, async ({
    page,
    fixture,
  }) => {
    void fixture;
    await page.setViewportSize(viewport);
    for (const route of ["layers", "stores", "api-keys"]) {
      await page.goto(`/admin/workspaces/demo/${route}`);
      const table = page.getByRole("table", { name: "Scrollable data table" });
      await expect(table.locator("tbody tr").first()).toBeVisible();
      const report = await page.evaluate(() => {
        const width = document.documentElement.clientWidth;
        const rows = [
          ...document.querySelectorAll(
            '[aria-label="Scrollable data table"] tbody tr',
          ),
        ];
        const clipped = rows.flatMap((row) => {
          const cells = [...row.querySelectorAll("td")];
          const actions = cells[cells.length - 1];
          const box = actions?.getBoundingClientRect();
          const container = actions
            ?.closest('[data-slot="table-container"]')
            ?.getBoundingClientRect();
          return !box ||
            !container ||
            box.right > container.right + 1 ||
            box.right > width
            ? [row.textContent?.slice(0, 40)]
            : [];
        });
        return {
          overflow: document.documentElement.scrollWidth > width + 1,
          clipped,
        };
      });
      expect(report, route).toEqual({ overflow: false, clipped: [] });
    }
  });
}

test("the sidebar starts as a rail on narrow desktops without a saved choice", async ({
  page,
  fixture,
}) => {
  void fixture;
  await page.setViewportSize({ width: 1024, height: 768 });
  await page.goto("/admin/workspaces/demo/layers");
  await expect(
    page.locator('[data-slot="sidebar"][data-state="collapsed"]'),
  ).toHaveCount(1);
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.evaluate(() => window.localStorage.clear());
  await page.reload();
  await expect(
    page.locator('[data-slot="sidebar"][data-state="expanded"]'),
  ).toHaveCount(1);
});

for (const width of [1280, 1440]) {
  test(`import details use the full page width at ${width}px`, async ({
    page,
    fixture,
  }) => {
    void fixture;
    await page.setViewportSize({ width, height: 900 });
    await page.goto("/admin/workspaces/demo/imports");
    await page.getByRole("link", { name: "Import roads", exact: true }).click();
    await expect(
      page.getByRole("heading", { name: "Import roads", level: 1 }),
    ).toBeVisible();
    const mapping = page.getByRole("region", {
      name: /Field mapping for roads/,
    });
    await expect(mapping).toBeVisible();
    const layout = await mapping.evaluate((node) => {
      const main = node.closest("main")!.getBoundingClientRect();
      return {
        scrolls: node.scrollWidth > node.clientWidth + 1,
        share: node.getBoundingClientRect().width / main.width,
      };
    });
    expect(layout.scrolls).toBe(false);
    expect(layout.share).toBeGreaterThan(0.8);
  });
}
