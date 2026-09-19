import { expect, test } from "@playwright/test";
import { apiURL, readFixture } from "./fixtures";

test("managed import renders through WMS and tiles; graphic replacement updates both", async ({
  request,
}) => {
  test.setTimeout(120_000);
  const { bootstrapToken } = await readFixture();
  const headers = { Authorization: `Bearer ${bootstrapToken}` };
  const name = `render-${Date.now()}`;
  const api = `/api/v1/workspaces/${name}`;
  const root = `/workspaces/${name}`;
  expect(
    (
      await request.post(apiURL("/api/v1/workspaces"), {
        headers,
        data: { name },
      })
    ).status(),
  ).toBe(201);
  for (const service of ["wms", "ogc-tiles"]) {
    const settings = await (
      await request.get(apiURL(`${api}/settings/${service}`), { headers })
    ).json();
    const saved = await request.put(apiURL(`${api}/settings/${service}`), {
      headers,
      data: { ...settings, enabled: true, public: true },
    });
    expect(saved.ok(), await saved.text()).toBe(true);
  }
  const uploaded = await request.post(apiURL(`${api}/imports`), {
    headers,
    multipart: {
      name: "render-points",
      file: {
        name: "source-points.geojson",
        mimeType: "application/geo+json",
        buffer: Buffer.from(
          JSON.stringify({
            type: "FeatureCollection",
            features: [
              {
                type: "Feature",
                properties: { name: "Generated identifier control" },
                geometry: {
                  type: "Point",
                  coordinates: [0, 66.51326044311186],
                },
              },
            ],
          }),
        ),
      },
    },
  });
  expect(uploaded.status(), await uploaded.text()).toBe(202);
  const job = await uploaded.json();
  const importPath = `${api}/imports/${job.id}`;
  const readJob = async () =>
    (await request.get(apiURL(importPath), { headers })).json();
  await expect.poll(async () => (await readJob()).status).toBe("awaiting_plan");
  const discovered = (await readJob()).discovery.layers[0];
  const planned = await request.put(apiURL(`${importPath}/plan`), {
    headers,
    data: {
      service_name: "source",
      layers: [
        {
          source_layer: discovered.name,
          geometry_column: discovered.geometry_column,
          source_srid: 4326,
          target_srid: 3857,
          public_id: "published_points",
          enabled: true,
          public: true,
        },
      ],
    },
  });
  expect(planned.status(), await planned.text()).toBe(202);
  await expect
    .poll(async () => (await readJob()).status)
    .toBe("ready_to_publish");
  expect(
    (await request.post(apiURL(`${importPath}/publish`), { headers })).status(),
  ).toBe(202);
  await expect.poll(async () => (await readJob()).status).toBe("published");

  const items = await request.get(
    apiURL(`${root}/ogc/collections/published_points/items`),
    { headers },
  );
  expect(items.ok(), await items.text()).toBe(true);
  const feature = (await items.json()).features[0];
  expect(feature.id).not.toBeNull();
  expect(feature.geometry.coordinates[0]).toBeCloseTo(0, 7);
  expect(feature.geometry.coordinates[1]).toBeCloseTo(66.51326044311186, 7);
  const byID = await request.get(
    apiURL(`${root}/ogc/collections/published_points/items/${feature.id}`),
    { headers },
  );
  expect(byID.ok(), await byID.text()).toBe(true);
  expect((await byID.json()).id).toBe(feature.id);

  const asset = (color: string) =>
    request.put(apiURL(`${api}/style-assets/marker.svg`), {
      headers: { ...headers, "Content-Type": "image/svg+xml" },
      data: `<svg xmlns="http://www.w3.org/2000/svg" width="20" height="20"><rect width="20" height="20" fill="${color}"/></svg>`,
    });
  expect((await asset("#ff0000")).status()).toBe(201);
  const styled = await request.post(apiURL(`${api}/styles`), {
    headers,
    data: {
      name: "marker",
      body: `<StyledLayerDescriptor version="1.0.0" xmlns="http://www.opengis.net/sld" xmlns:xlink="http://www.w3.org/1999/xlink"><NamedLayer><Name>published_points</Name><UserStyle><FeatureTypeStyle><Rule><PointSymbolizer><Graphic><ExternalGraphic><OnlineResource xlink:type="simple" xlink:href="asset:marker.svg"/><Format>image/svg+xml</Format></ExternalGraphic><Size>20</Size></Graphic></PointSymbolizer></Rule></FeatureTypeStyle></UserStyle></NamedLayer></StyledLayerDescriptor>`,
    },
  });
  expect(styled.status(), await styled.text()).toBe(201);
  const paths = [
    `${root}/wms?SERVICE=WMS&VERSION=1.3.0&REQUEST=GetMap&LAYERS=published_points&STYLES=marker&CRS=EPSG:3857&BBOX=-1000,10017754,1000,10019754&WIDTH=128&HEIGHT=128&FORMAT=image/png&TRANSPARENT=TRUE`,
    `${root}/ogc-tiles/collections/published_points/map/tiles/WebMercatorQuad/0/0/0?f=png&style=marker`,
  ];
  const render = async (path: string) => {
    const response = await request.get(apiURL(path), { headers });
    expect(response.status(), await response.text()).toBe(200);
    expect(response.headers()["content-type"]).toContain("image/png");
    return response.body();
  };
  const before = await Promise.all(paths.map(render));
  const warm = await Promise.all(paths.map(render));
  expect(warm).toEqual(before);
  expect((await asset("#0000ff")).status()).toBe(201);
  const after = await Promise.all(paths.map(render));
  for (let index = 0; index < paths.length; index++)
    expect(after[index].equals(before[index])).toBe(false);
  const tileJSON = await (
    await request.get(
      apiURL(`${root}/ogc-tiles/collections/published_points/tilejson.json`),
      { headers },
    )
  ).json();
  expect(tileJSON.vector_layers[0].id).toBe("published_points");
  const tile = await request.get(
    apiURL(
      `${root}/ogc-tiles/collections/published_points/tiles/WebMercatorQuad/0/0/0`,
    ),
    { headers },
  );
  expect(tile.status()).toBe(200);
  expect((await tile.body()).includes(Buffer.from("published_points"))).toBe(
    true,
  );
});
