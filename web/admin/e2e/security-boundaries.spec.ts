import { expect, test } from "@playwright/test";
import { apiURL, readFixture } from "./fixtures";

test("a supported upload publishes for its owner and cannot be rebound by another workspace", async ({
  request,
}) => {
  const { bootstrapToken, workspaceAdminKey, workspace } = await readFixture();
  const headers = { "X-API-Key": workspaceAdminKey };
  const imports = apiURL(`/api/v1/workspaces/${workspace}/imports`);
  const uploaded = await request.post(imports, {
    headers,
    multipart: {
      name: "owned-import",
      file: {
        name: "owned.geojson",
        mimeType: "application/geo+json",
        buffer: Buffer.from(
          JSON.stringify({
            type: "FeatureCollection",
            features: [
              {
                type: "Feature",
                geometry: { type: "Point", coordinates: [1, 2] },
                properties: { name: "owner-marker" },
              },
            ],
          }),
        ),
      },
    },
  });
  expect(uploaded.status(), await uploaded.text()).toBe(202);
  const { id } = await uploaded.json();
  const jobURL = `${imports}/${id}`;
  async function waitForStatus(status: string) {
    await expect
      .poll(
        async () => {
          const result = await request.get(jobURL, { headers });
          expect(result.ok()).toBeTruthy();
          const job = await result.json();
          expect(job.status, job.error_message).not.toBe("failed");
          return job.status;
        },
        { timeout: 30000 },
      )
      .toBe(status);
  }
  await waitForStatus("awaiting_plan");
  const inspected = await (await request.get(jobURL, { headers })).json();
  expect(inspected.discovery.layers).toHaveLength(1);
  const planned = await request.put(`${jobURL}/plan`, {
    headers,
    data: {
      service_name: "owned-import",
      layers: [
        {
          source_layer: inspected.discovery.layers[0].name,
          public_id: "owned-import",
          enabled: true,
          public: false,
          source_srid: 4326,
          target_srid: 4326,
        },
      ],
    },
  });
  expect(planned.status(), await planned.text()).toBe(202);
  await waitForStatus("ready_to_publish");
  const published = await request.post(`${jobURL}/publish`, { headers });
  expect(published.status(), await published.text()).toBe(202);
  const items = await request.get(
    apiURL(`/workspaces/${workspace}/ogc/collections/owned-import/items`),
    { headers },
  );
  expect(items.status(), await items.text()).toBe(200);
  expect(await items.text()).toContain("owner-marker");

  // Managed encrypted imports use a one-connection pool. Metadata discovery
  // must release its rows before querying geometry metadata on that same pool.
  const sql = "SELECT 1 AS id, ST_Point(0,0) AS geom";
  const servicesURL = apiURL(`/api/v1/workspaces/${workspace}/services`);
  const managedServices = await (
    await request.get(servicesURL, { headers })
  ).json();
  const managed = managedServices.services.find(
    (service: { name: string }) => service.name === "owned-import",
  );
  expect(managed).toBeTruthy();
  for (const result of await Promise.all(
    Array.from({ length: 3 }, () =>
      request.post(`${servicesURL}/${managed.id}/validate-sql`, {
        headers,
        data: { sql },
        timeout: 15000,
      }),
    ),
  )) {
    expect(result.status(), await result.text()).toBe(200);
    expect((await result.json()).valid).toBe(true);
  }
  const view = await request.post(`${servicesURL}/${managed.id}/layers`, {
    headers,
    timeout: 15000,
    data: {
      public_id: "managed-sql-view",
      enabled: true,
      public: false,
      crs_default: 4326,
      sql_view: {
        sql,
        geometry_column: "geom",
        geometry_type: "Point",
        srid: 4326,
        id_column: "id",
      },
    },
  });
  expect(view.status(), await view.text()).toBe(201);
  const viewItems = await request.get(
    apiURL(`/workspaces/${workspace}/ogc/collections/managed-sql-view/items`),
    { headers },
  );
  expect(viewItems.status(), await viewItems.text()).toBe(200);
  expect((await viewItems.json()).features).toHaveLength(1);

  const admin = { Authorization: `Bearer ${bootstrapToken}` };
  const other = `foreign-${Date.now()}`;
  expect(
    (
      await request.post(apiURL("/api/v1/workspaces"), {
        headers: admin,
        data: { name: other },
      })
    ).status(),
  ).toBe(201);
  const key = await request.post(
    apiURL(`/api/v1/workspaces/${other}/apikeys`),
    {
      headers: admin,
      data: {
        name: "foreign-admin",
        owner_name: "foreign-admin",
        role_id: "admin",
      },
    },
  );
  expect(key.status()).toBe(201);
  const foreignHeaders = { "X-API-Key": (await key.json()).key };
  const services = apiURL(`/api/v1/workspaces/${other}/services`);
  const connection_info = { managed_import_id: id };
  for (const url of [services, `${services}/test-connection`]) {
    const denied = await request.post(url, {
      headers: foreignHeaders,
      data: { name: "stolen", type: "duckdb", enabled: true, connection_info },
    });
    expect(denied.status(), await denied.text()).toBe(400);
  }
  const hidden = await request.get(
    apiURL(`/api/v1/workspaces/${other}/imports/${id}`),
    { headers: foreignHeaders },
  );
  expect(hidden.status()).toBe(404);
  const sources = await (
    await request.get(services, { headers: foreignHeaders })
  ).json();
  expect(sources.services).toHaveLength(0);
});

test("the release image rejects a disguised VRT upload referencing a denied fixture", async ({
  request,
}) => {
  const { workspaceAdminKey, workspace } = await readFixture();
  const headers = { "X-API-Key": workspaceAdminKey };
  const response = await request.post(
    apiURL(`/api/v1/workspaces/${workspace}/imports`),
    {
      headers,
      multipart: {
        name: "indirect-boundary-regression",
        file: {
          name: "disguised.geojson",
          mimeType: "application/geo+json",
          buffer: Buffer.from(
            '<OGRVRTDataSource><OGRVRTLayer name="proxy"><SrcDataSource>/denied/source.geojson</SrcDataSource><SrcLayer>denied_fixture</SrcLayer></OGRVRTLayer></OGRVRTDataSource>',
          ),
        },
      },
    },
  );
  expect(response.status()).toBe(202);
  const job = await response.json();
  await expect
    .poll(
      async () => {
        const result = await request.get(
          apiURL(`/api/v1/workspaces/${workspace}/imports/${job.id}`),
          { headers },
        );
        expect(result.ok()).toBeTruthy();
        const current = await result.json();
        expect(current.discovery?.layers ?? []).toHaveLength(0);
        expect(current.status).not.toBe("awaiting_plan");
        expect(current.status).not.toBe("published");
        return current.status;
      },
      { timeout: 15000 },
    )
    .toBe("failed");
});
