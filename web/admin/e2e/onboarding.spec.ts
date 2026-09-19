import { execFileSync } from "node:child_process";
import { expect, test, request } from "@playwright/test";
import {
  apiURL,
  gotoConsole,
  readFixture,
  signInWithToken,
  sessionHeaders,
} from "./fixtures";

// These tests handle a one-time key. Do not record secrets in videos/traces.
test.use({ trace: "off", video: "off", screenshot: "off" });

test("API-first tutorial: PostGIS publications, protocols and scoped client access", async () => {
  const { bootstrapToken } = await readFixture();
  const workspace = `tutorial-api-${Date.now()}`;
  const admin = await request.newContext({
    extraHTTPHeaders: { Authorization: `Bearer ${bootstrapToken}` },
  });
  const client = await request.newContext();
  const root = `/api/v1/workspaces/${workspace}`;
  try {
    const created = await admin.post(apiURL("/api/v1/workspaces"), {
      data: { name: workspace, description: "API-first tutorial fixture" },
    });
    expect(created.status()).toBe(201);
    const store = await admin.post(apiURL(`${root}/services`), {
      data: {
        name: "sample-postgis",
        type: "postgis",
        connection_info: {
          host: process.env.CONSOLE_POSTGIS_HOST ?? "db",
          port: 5432,
          database: "postgis",
          user: "postgres",
          password: "postgres",
          sslmode: "disable",
          schemas: ["public"],
        },
      },
    });
    expect(store.status()).toBe(201);
    expect(
      (
        await admin.post(apiURL(`${root}/services/sample-postgis/discover`))
      ).ok(),
    ).toBe(true);
    for (const id of ["places", "areas"]) {
      expect(
        (
          await admin.post(apiURL(`${root}/services/sample-postgis/layers`), {
            data: { source_layer: `public.${id}`, public_id: id, title: id },
          })
        ).status(),
      ).toBe(201);
    }
    for (const protocol of ["wms", "wfs"]) {
      expect(
        (
          await admin.put(apiURL(`${root}/settings/${protocol}`), {
            data: { enabled: true, title: `Tutorial ${protocol}` },
          })
        ).ok(),
      ).toBe(true);
      const capabilities = await admin.get(
        apiURL(
          `/workspaces/${workspace}/${protocol}?service=${protocol.toUpperCase()}&request=GetCapabilities`,
        ),
      );
      expect(capabilities.ok()).toBe(true);
      expect(await capabilities.text()).toContain("places");
    }
    const keyResponse = await admin.post(apiURL(`${root}/apikeys`), {
      data: {
        name: "tutorial-reader",
        owner_name: "tutorial",
        role_id: "viewer",
      },
    });
    expect(keyResponse.status()).toBe(201);
    const { key } = await keyResponse.json();
    const url = apiURL(
      `/workspaces/${workspace}/ogc/collections/places/items?limit=2`,
    );
    expect((await client.get(url)).ok()).toBe(false);
    const response = await client.get(url, { headers: { "X-API-Key": key } });
    expect(response.ok()).toBe(true);
    const data = await response.json();
    expect(data.type).toBe("FeatureCollection");
    expect(data.features).toHaveLength(2);
  } finally {
    await admin.dispose();
    await client.dispose();
  }
});

test("UI tutorial: new workspace to private PostGIS layer, preview and external viewer client", async ({
  page,
}) => {
  test.setTimeout(120_000);
  const { bootstrapToken } = await readFixture();
  const workspace = `tutorial-ui-${Date.now()}`;
  await signInWithToken(page, bootstrapToken, "/workspaces");
  await page
    .getByRole("button", { name: "New workspace", exact: true })
    .click();
  await page.getByLabel("Name", { exact: true }).fill(workspace);
  await page
    .getByRole("button", { name: "Create workspace", exact: true })
    .click();
  // Creating a workspace opens it.
  await expect(
    page.getByRole("heading", { name: workspace, level: 1 }),
  ).toBeVisible();
  await expect(
    page.getByText("Publish your first dataset", { exact: true }),
  ).toBeVisible();
  await page.getByRole("button", { name: "Add store", exact: true }).click();
  await page.getByLabel("Store name").fill("sample-postgis");
  await page
    .getByLabel("Host", { exact: true })
    .fill(process.env.CONSOLE_POSTGIS_HOST ?? "db");
  await page.getByLabel("Database", { exact: true }).fill("postgis");
  await page.getByLabel("User", { exact: true }).fill("postgres");
  await page.getByLabel("Password", { exact: true }).fill("postgres");
  await page.getByLabel("Schemas (comma separated)").fill("public");
  await page
    .getByRole("button", { name: "Test connection", exact: true })
    .click();
  await expect(page.getByText(/Connected in/)).toBeVisible({ timeout: 30_000 });
  await page
    .getByRole("dialog")
    .getByRole("button", { name: "Add store", exact: true })
    .click();
  await expect(page.getByRole("dialog")).not.toBeVisible();
  await page
    .getByRole("button", { name: "Discover and publish layers" })
    .click();
  await page
    .getByRole("button", { name: "Discover layers", exact: true })
    .click();
  const candidate = page.getByRole("checkbox", {
    name: "Publish public.places",
    exact: true,
  });
  await expect(candidate).toBeVisible({ timeout: 30_000 });
  await candidate.check();
  await page.getByLabel("Public ID for public.places").fill("places");
  await page
    .getByRole("button", { name: "Publish 1 layer", exact: true })
    .click();
  await expect(
    page.getByRole("dialog", { name: "Layers published" }),
  ).toBeVisible();
  await page.getByRole("button", { name: "Done", exact: true }).click();
  await expect(page.getByRole("dialog")).not.toBeVisible();
  await gotoConsole(page, `/workspaces/${workspace}/layers`);
  const row = page
    .getByRole("row")
    .filter({ has: page.getByText("places", { exact: true }) });
  // The Access column spells the layer's visibility in the shared vocabulary.
  await expect(row).toContainText("Restricted");
  await row.getByRole("link", { name: "Preview", exact: true }).click();
  await expect(
    page.getByRole("switch", { name: "Show places", exact: true }),
  ).toBeChecked();
  await page.getByRole("button", { name: "Fit places" }).click();
  // The real preview must fetch features, not just display a checked switch.
  await expect(page.getByText(/sampled features/)).toBeVisible();
  await page.getByRole("link", { name: "Connect a client" }).click();
  await expect(page.getByLabel("Published layer")).toHaveValue("places");
  await expect(
    page.getByRole("button", { name: "Copy WMS GetCapabilities", exact: true }),
  ).toBeDisabled();
  await gotoConsole(page, `/workspaces/${workspace}/settings`);
  for (const name of [
    "Enable Web Map Service (WMS)",
    "Enable Web Feature Service (WFS)",
  ]) {
    const toggle = page.getByRole("switch", { name, exact: true });
    await toggle.click();
    await expect(toggle).toBeChecked();
    await expect(toggle).toBeEnabled();
  }
  await gotoConsole(page, `/workspaces/${workspace}/endpoints?layer=places`);
  await expect(
    page.getByRole("button", { name: "Copy WMS GetCapabilities", exact: true }),
  ).toBeEnabled();
  await page
    .getByRole("button", { name: "Check access with my session" })
    .click();
  await expect(
    page.getByText(/Feature request succeeded with your console session/),
  ).toBeVisible();
  await page
    .getByRole("link", { name: "Manage API keys", exact: true })
    .click();
  await page.getByRole("button", { name: "Create key", exact: true }).click();
  await page.getByLabel("Key name", { exact: true }).fill("tutorial-reader");
  await page
    .getByRole("dialog")
    .getByRole("button", { name: "Create key", exact: true })
    .click();
  const secretDialog = page.getByRole("dialog", { name: "Copy this key now" });
  await expect(secretDialog).toBeVisible();
  const key = (await secretDialog.locator("code").textContent())!.trim();
  expect(key.length > 16).toBe(true);
  // The secret cannot be shown again, so closing it takes a confirmation.
  await secretDialog.getByLabel("I have stored this key").check();
  await secretDialog.getByRole("button", { name: "Done", exact: true }).click();
  await expect(secretDialog).toBeHidden();
  await gotoConsole(page, `/workspaces/${workspace}/endpoints?layer=places`);
  const external = await request.newContext();
  const url = apiURL(
    `/workspaces/${workspace}/ogc/collections/places/items?limit=10`,
  );
  try {
    expect((await external.get(url)).ok()).toBe(false);
    const response = await external.get(url, { headers: { "X-API-Key": key } });
    expect(response.ok()).toBe(true);
    expect((await response.json()).features.length).toBeGreaterThan(0);
    // Execute the exact source displayed by the UI, not a hand-written proxy.
    const env = { ...process.env, NEOSRV_API_KEY: key };
    for (const language of ["javascript", "python", "curl"] as const) {
      await page.getByLabel("Client example").selectOption(language);
      const source = await page.getByLabel("Connection example").textContent();
      const result =
        language === "javascript"
          ? execFileSync(
              process.execPath,
              ["--input-type=module", "-e", source!],
              { env, encoding: "utf8", timeout: 30_000 },
            )
          : language === "python"
            ? execFileSync("python3", ["-c", source!], {
                env,
                encoding: "utf8",
                timeout: 30_000,
              })
            : execFileSync("bash", ["-c", source!], {
                env,
                input: `${key}\n`,
                encoding: "utf8",
                timeout: 30_000,
              });
      expect(JSON.parse(result).features.length).toBeGreaterThan(0);
    }
    // A viewer credential must not be accepted for catalog writes.
    const write = await external.post(
      apiURL(`/api/v1/workspaces/${workspace}/services`),
      {
        headers: { "X-API-Key": key },
        data: { name: "forbidden", type: "postgis", connection_info: {} },
      },
    );
    expect(write.status()).toBe(403);
    await gotoConsole(page, `/workspaces/${workspace}/api-keys`);
    await page
      .getByRole("button", { name: "Revoke tutorial-reader", exact: true })
      .click();
    await page
      .getByRole("dialog")
      .getByRole("button", { name: "Revoke key", exact: true })
      .click();
    await expect(page.getByRole("dialog")).not.toBeVisible();
    expect(
      (await external.get(url, { headers: { "X-API-Key": key } })).ok(),
    ).toBe(false);
    await gotoConsole(page, `/workspaces/${workspace}/endpoints?layer=places`);
    await page
      .getByRole("button", { name: "Check access with my session" })
      .click();
    await expect(
      page.getByText(/Feature request succeeded with your console session/),
    ).toBeVisible();
  } finally {
    await external.dispose();
  }
});

for (const populated of [false, true]) {
  test(`UI tutorial: upload, publish and connect in a ${populated ? "populated" : "new"} workspace`, async ({
    page,
  }) => {
    test.setTimeout(120_000);
    const { bootstrapToken } = await readFixture();
    const workspace = `tutorial-upload-${Date.now()}`;
    const apiRoot = `/api/v1/workspaces/${workspace}`;
    await signInWithToken(page, bootstrapToken, "/workspaces");
    await page
      .getByRole("button", { name: "New workspace", exact: true })
      .click();
    await page.getByLabel("Name", { exact: true }).fill(workspace);
    await page
      .getByRole("button", { name: "Create workspace", exact: true })
      .click();
    await expect(
      page.getByRole("heading", { name: workspace, level: 1 }),
    ).toBeVisible();
    if (populated) {
      const store = await page.request.post(apiURL(`${apiRoot}/services`), {
        headers: await sessionHeaders(page),
        data: {
          name: "existing-data",
          type: "postgis",
          enabled: true,
          connection_info: {
            host: process.env.CONSOLE_POSTGIS_HOST ?? "db",
            port: 5432,
            database: "postgis",
            user: "postgres",
            password: "postgres",
            sslmode: "disable",
          },
        },
      });
      expect(store.status()).toBe(201);
      const layer = await page.request.post(
        apiURL(`${apiRoot}/services/existing-data/layers`),
        {
          headers: await sessionHeaders(page),
          data: {
            source_layer: "public.areas",
            public_id: "aaa-existing",
            enabled: true,
          },
        },
      );
      expect(layer.status()).toBe(201);
      await page.reload();
      await page
        .getByRole("button", { name: "Review publishing guide" })
        .click();
    }
    await page
      .getByRole("link", { name: "Upload a file", exact: true })
      .click();
    await page.getByLabel("Import name", { exact: true }).fill("sample-places");
    await page
      .getByLabel("Dataset or archive")
      .setInputFiles("../../testing/tutorial/places.geojson");
    await page
      .getByRole("button", { name: "Upload and inspect", exact: true })
      .click();
    await expect(
      page.getByRole("button", { name: "Validate plan", exact: true }),
    ).toBeVisible({ timeout: 60_000 });
    await page
      .getByLabel("Public ID *", { exact: true })
      .fill("imported_places");
    await page
      .getByRole("button", { name: "Validate plan", exact: true })
      .click();
    await expect(
      page.getByRole("button", { name: "Publish dataset", exact: true }),
    ).toBeVisible({ timeout: 30_000 });
    await page
      .getByRole("button", { name: "Publish dataset", exact: true })
      .click();
    await expect(
      page.getByRole("link", { name: "Connect a client", exact: true }),
    ).toBeVisible({ timeout: 30_000 });
    const importURL = page.url();
    const jobs = await (
      await page.request.get(apiURL(`${apiRoot}/imports`))
    ).json();
    const job = jobs.imports.find(
      (item: { name: string }) => item.name === "sample-places",
    );
    expect(Boolean(job.service_id)).toBe(true);
    await page
      .getByRole("link", { name: "View created store", exact: true })
      .click();
    await expect(page).toHaveURL(new RegExp(`store=${job.service_id}`));
    await expect(page.getByRole("row")).toHaveCount(2);
    await page.goBack();
    await page
      .getByRole("link", { name: "View published layers", exact: true })
      .click();
    await expect(page).toHaveURL(new RegExp(`store=${job.service_id}`));
    await expect(
      page.getByRole("row").filter({ hasText: "imported_places" }),
    ).toBeVisible();
    await expect(
      page.getByRole("row").filter({ hasText: "aaa-existing" }),
    ).toHaveCount(0);
    await page.goBack();
    await page
      .getByRole("link", { name: "Preview on map", exact: true })
      .click();
    await expect(
      page.getByRole("switch", { name: "Show imported_places", exact: true }),
    ).toBeChecked();
    await expect(page.getByText(/sampled features/)).toBeVisible();
    await page.goBack();
    await page
      .getByRole("link", { name: "Connect a client", exact: true })
      .click();
    await expect(page.getByLabel("Published layer")).toHaveValue(
      "imported_places",
    );
    await page
      .getByRole("button", { name: "Check access with my session" })
      .click();
    await expect(
      page.getByText(/Feature request succeeded with your console session/),
    ).toBeVisible();
    if (populated) {
      const layers = await (
        await page.request.get(
          apiURL(`${apiRoot}/services/${job.service_id}/layers`),
        )
      ).json();
      const layer = layers.layers.find(
        (item: { public_id: string }) => item.public_id === "imported_places",
      );
      const renamed = await page.request.put(
        apiURL(`${apiRoot}/services/${job.service_id}/layers/${layer.id}`),
        {
          headers: await sessionHeaders(page),
          data: { public_id: "renamed_places" },
        },
      );
      expect(renamed.ok()).toBe(true);
      await page.goto(importURL);
      await page
        .getByRole("link", { name: "Connect a client", exact: true })
        .click();
      await expect(page.getByLabel("Published layer")).toHaveValue(
        "renamed_places",
      );
    }
  });
}
