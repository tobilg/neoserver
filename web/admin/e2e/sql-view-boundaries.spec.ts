import { expect, test } from "@playwright/test";
import { apiURL, readFixture } from "./fixtures";

test("PostGIS SQL-view list, items and WFS reads stay within the publication", async ({
  request,
}) => {
  const { bootstrapToken, workspace } = await readFixture();
  const headers = { Authorization: `Bearer ${bootstrapToken}` };
  const name = `view-boundary-${Date.now()}`;
  const api = apiURL(`/api/v1/workspaces/${workspace}`);
  const service = await request.post(`${api}/services`, {
    headers,
    data: {
      name,
      type: "postgis",
      enabled: true,
      connection_info: {
        host: "db",
        port: 5432,
        database: "postgis",
        user: "postgres",
        password: "postgres",
        sslmode: "disable",
        schemas: ["public"],
      },
    },
  });
  expect(service.status(), await service.text()).toBe(201);
  const sql = `SELECT id AS record_key, name, ST_SetSRID(ST_MakePoint(x,y),4326) AS geom
    FROM (VALUES ('a','allowed','secret-a',7,51),('b','excluded','secret-b',8,52)) AS records(id,name,secret,x,y)
    WHERE id='a'`;
  const created = await request.post(`${api}/services/${name}/layers`, {
    headers,
    data: {
      public_id: name,
      source_layer: "cite.BasicPolygons",
      enabled: true,
      crs_default: 3857,
      sql_view: {
        sql,
        geometry_column: "geom",
        geometry_type: "Point",
        srid: 4326,
        id_column: "record_key",
      },
    },
  });
  expect(created.status(), await created.text()).toBe(201);
  expect((await created.json()).source_layer).toBe("_sql_view_");
  const collection = apiURL(`/workspaces/${workspace}/ogc/collections/${name}`);
  const list = await request.get(`${collection}/items`, { headers });
  expect(list.status(), await list.text()).toBe(200);
  expect(
    (await list.json()).features.map((feature: { id: string }) => feature.id),
  ).toEqual(["a"]);
  const item = await request.get(
    `${collection}/items/a?crs=http://www.opengis.net/def/crs/EPSG/0/3857`,
    { headers },
  );
  expect(item.status(), await item.text()).toBe(200);
  const feature = await item.json();
  expect(feature.properties).toEqual({ name: "allowed" });
  expect(feature.geometry.coordinates[0]).toBeGreaterThan(700000);
  expect(
    (await request.get(`${collection}/items/b`, { headers })).status(),
  ).toBe(404);
  const settings = await (
    await request.get(`${api}/settings/wfs`, { headers })
  ).json();
  expect(
    (
      await request.put(`${api}/settings/wfs`, {
        headers,
        data: { ...settings, enabled: true },
      })
    ).ok(),
  ).toBe(true);
  const wfs = apiURL(`/workspaces/${workspace}/wfs`);
  for (const id of ["a", "b"]) {
    const response = await request.get(wfs, {
      headers,
      params: {
        SERVICE: "WFS",
        REQUEST: "GetFeature",
        STOREDQUERY_ID: "urn:ogc:def:query:OGC-WFS::GetFeatureById",
        ID: `${name}.${id}`,
      },
    });
    expect(response.status(), await response.text()).toBe(
      id === "a" ? 200 : 404,
    );
    expect(await response.text()).not.toContain("secret-");
  }
  const values = await request.get(wfs, {
    headers,
    params: {
      SERVICE: "WFS",
      REQUEST: "GetPropertyValue",
      TYPENAMES: name,
      VALUEREFERENCE: "name",
      FILTER:
        "<Filter><PropertyIsEqualTo><ValueReference>name</ValueReference><Literal>allowed</Literal></PropertyIsEqualTo></Filter>",
    },
  });
  expect(values.status(), await values.text()).toBe(200);
  expect(await values.text()).toContain('numberMatched="1"');
  expect(await values.text()).toContain(">allowed<");
  expect(await values.text()).not.toContain("excluded");
});
