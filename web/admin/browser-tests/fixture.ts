import { test as base, expect } from "@playwright/test";
import { assertContract } from "./contract";

const timestamps = {
  created_at: "2026-01-01T00:00:00Z",
  updated_at: "2026-01-01T00:00:00Z",
};

export const test = base.extend<{ fixture: ReturnType<typeof state> }>({
  fixture: async ({ page }, provideFixture) => {
    const data = state();
    // A signed-in browser holds the readable CSRF cookie of its session.
    await page
      .context()
      .addCookies([
        { name: "neosrv_csrf", value: "fixture", url: "http://127.0.0.1:5178" },
      ]);
    await page.route("**/api/v1/**", async (route) => {
      const request = route.request();
      const path = new URL(request.url()).pathname.replace("/api/v1", "");
      const method = request.method();
      assertContract(
        path,
        method,
        request.postData() ? request.postDataJSON() : undefined,
      );
      let body: unknown;
      let status = 200;
      if (path === "/console/config")
        body = {
          version: "test",
          base_path: "",
          auth: {
            enabled: true,
            method: "jwt",
            password_login: false,
            token_login: true,
            oidc: {
              enabled: false,
              issuer: "",
              client_id: "",
              scopes: [],
              redirect_uri: "",
            },
          },
          services: {
            ogcapi: true,
            wms: true,
            wfs: true,
            wcs: true,
            wmts: true,
            tiles: true,
          },
          features: {},
        };
      else if (path === "/catalog/integrity")
        body = { healthy: true, issues: [] };
      else if (path === "/cache/stats")
        body = {
          enabled: true,
          total_size_bytes: 0,
          total_max_size_bytes: 1048576,
        };
      else if (path === "/deletions") body = { deletions: [] };
      else if (path === "/auth/me") {
        data.authReads++;
        body = {
          principal: "review",
          subject: "review",
          authenticated: true,
          auth_method: "jwt",
          console_access: true,
          super_admin: true,
          capabilities: {},
          workspaces: data.workspaces.map((w) => ({
            ...w,
            role: "admin",
            console_access: true,
          })),
        };
        if (data.identity) body = data.identity;
      } else if (path === "/auth/login") {
        status = 201;
        body = data.identity;
      } else if (path === "/workspaces" && method === "POST") {
        const next = {
          id: "new",
          ...timestamps,
          ...request.postDataJSON(),
          counts: {},
        };
        data.workspaces.push(next);
        status = 201;
        body = next;
      } else if (path === "/workspaces") body = { workspaces: data.workspaces };
      else if (path.endsWith("/services") && method === "GET") {
        if (data.failStores) {
          status = 503;
          body = { code: 503, message: "Store connection unavailable" };
        } else
          body = {
            services: data.services,
          };
      } else if (path.endsWith("/roles"))
        body = {
          roles: [
            {
              id: "admin",
              name: "Administrator",
              is_system: true,
              ...timestamps,
            },
            { id: "viewer", name: "Viewer", is_system: true, ...timestamps },
          ],
        };
      else if (path.endsWith("/layer-groups")) body = { layer_groups: [] };
      else if (path.endsWith("/styles") && method === "GET")
        body = {
          styles: Object.keys(data.styles).map((name) => ({
            id: name,
            workspace_id: "w1",
            ...timestamps,
            name,
            title: name,
            format: "sld_1.0.0",
          })),
        };
      else if (path.includes("/styles/")) {
        const name = path.split("/").at(-1)!;
        if (method === "PUT") data.styles[name] = request.postDataJSON().body;
        body = {
          id: name,
          workspace_id: "w1",
          ...timestamps,
          name,
          sld_body: data.styles[name],
          body: data.styles[name],
          format: "sld_1.0.0",
        };
      } else if (path.endsWith("/style-assets")) body = { assets: [] };
      else if (path.includes("/settings/")) {
        const name = path.split("/").at(-1)!;
        if (method === "PUT") {
          if (data.failSettings) {
            status = 500;
            body = { code: 500, message: "Settings could not be saved" };
          } else {
            data.settings[name] = request.postDataJSON();
            body = data.settings[name];
          }
        } else
          body = data.settings[name] ?? {
            enabled: false,
            public: false,
            feature_info_enabled: false,
            settings: {
              vector_tiles: { enabled: false },
              map_tiles: { enabled: false },
              cache_enabled: false,
            },
          };
      } else if (path.endsWith("/summary"))
        body = {
          workspace_id: "w1",
          active_jobs: { imports: 0, tile_cache: 0 },
          counts: {
            services: data.services.length,
            layers: data.services.length ? 1 + data.published.length : 0,
            api_keys: 1,
          },
          protocols: {
            ogcapi: true,
            wms: true,
            wfs: true,
            wcs: false,
            wmts: false,
            ogc_tiles: true,
          },
        };
      else if (path.endsWith("/validate-sql"))
        body = {
          valid: true,
          discovered: {
            columns: [
              { name: "id", type: "INTEGER" },
              { name: "geom", type: "GEOMETRY" },
            ],
            geometry_column: "geom",
            geometry_type: "Point",
            srid: 4326,
            suggested_id_column: "id",
          },
        };
      else if (path.endsWith("/discover-coverages")) {
        if (data.failCoverageDiscovery) {
          status = 503;
          body = { code: 503, message: "Raster source unavailable" };
        } else
          body = {
            coverages: [
              {
                source_coverage: "elevation",
                title: "Elevation",
                info: {
                  crs: "EPSG:4326",
                  srid: 4326,
                  width: 32,
                  height: 32,
                  axis_labels: ["x", "y"],
                  origin_x: 0,
                  origin_y: 0,
                  resolution_x: 1,
                  resolution_y: -1,
                  envelope: [0, 0, 32, 32],
                },
              },
            ],
          };
      } else if (path.endsWith("/coverages")) {
        if (method === "POST") {
          status = 201;
          body = {
            id: "c1",
            service_id: path.split("/")[4],
            ...request.postDataJSON(),
          };
          data.coverages.push(body as Record<string, unknown>);
        } else
          body = {
            coverages: data.coverages.filter(
              (coverage) => coverage.service_id === path.split("/")[4],
            ),
          };
      } else if (path.endsWith("/discover"))
        body = {
          layers: Array.from({ length: data.discoveryCount }, (_, i) => ({
            name: "source_" + i,
            schema: "public",
            srid: 4326,
            title: "Source " + i,
          })),
        };
      else if (path.endsWith("/layers/l1")) {
        if (method === "PUT") Object.assign(data.layer, request.postDataJSON());
        body = data.layer;
      } else if (/\/layers\/published-\d+$/.test(path)) {
        const layer = data.published.find(
          (item) => item.id === path.split("/").at(-1),
        );
        if (!layer) status = 404;
        else if (method === "PUT") Object.assign(layer, request.postDataJSON());
        body = layer ?? { message: "layer not found" };
      } else if (path.endsWith("/layers")) {
        if (method === "POST") {
          body = {
            id: `published-${data.published.length}`,
            service_id: "s1",
            source_layer: "",
            ...timestamps,
            ...request.postDataJSON(),
          };
          data.published.push(body as Record<string, unknown>);
          status = 201;
        } else
          body = {
            layers: path.includes("/s1/")
              ? [data.layer, ...data.published]
              : [],
          };
      } else if (path.endsWith("/imports")) body = { imports: [data.job] };
      else if (path.endsWith("/imports/i1/plan")) {
        status = 202;
        data.plan = request.postDataJSON();
        data.job = {
          ...data.job,
          status: "ready_to_publish",
          phase: "preview",
          plan: data.plan,
        };
        body = data.job;
      } else if (path.endsWith("/imports/i1/preview")) body = sample;
      else if (path.endsWith("/imports/i1/history")) body = { events: [] };
      else if (path.endsWith("/imports/i1")) body = data.job;
      else if (
        data.workspaces.some(
          (workspace) =>
            path === `/workspaces/${workspace.name}` ||
            path === `/workspaces/${workspace.id}`,
        )
      ) {
        const workspace = data.workspaces.find(
          (workspace) =>
            path === `/workspaces/${workspace.name}` ||
            path === `/workspaces/${workspace.id}`,
        )!;
        if (method === "PUT") Object.assign(workspace, request.postDataJSON());
        body = workspace;
      } else if (path.endsWith("/apikeys") && method === "POST") {
        data.createdKey = request.postDataJSON();
        status = 201;
        body = {
          id: "k2",
          key_prefix: "nsk_test",
          revoked: false,
          ...timestamps,
          ...request.postDataJSON(),
          key: "nsk_once",
        };
      } else if (path.endsWith("/apikeys"))
        body = {
          api_keys: data.apiKeyDeleted
            ? []
            : [
                {
                  id: "k1",
                  name: "CI pipeline",
                  key_prefix: "nsk_ci",
                  role_id: "viewer",
                  revoked: data.revocations > 0,
                  ...timestamps,
                },
              ],
        };
      else if (path.endsWith("/apikeys/k1/permanent") && method === "DELETE") {
        if (data.revocations === 0) {
          status = 409;
          body = { message: "Conflict", detail: "revoke the API key first" };
        } else {
          data.apiKeyDeleted = true;
          status = 204;
        }
      } else if (path.endsWith("/apikeys/k1") && method === "DELETE") {
        data.revocations++;
        status = 204;
      } else if (path.endsWith("/tile-cache/stats")) {
        if (data.failStats) {
          status = 403;
          body = { code: 403, message: "Stats not authorized" };
        } else
          body = {
            enabled: true,
            workspace: { size_bytes: 1024, utilization: 0.25 },
            hits: 1,
            misses: 0,
          };
      } else if (path.endsWith("/tile-cache/jobs")) {
        if (method === "POST") {
          data.tileJobs.push(request.postDataJSON());
          status = 202;
          body = {
            id: "t1",
            workspace_id: "w1",
            request: request.postDataJSON(),
            status: "queued",
            total_tiles: 0,
            processed_tiles: 0,
            ...timestamps,
          };
        } else body = { jobs: [] };
      } else if (path === "/workspaces/demo/tile-matrix-sets")
        body = {
          tile_matrix_sets: [
            {
              id: "WebMercatorQuad",
              title: "Web Mercator",
              crs: "EPSG:3857",
              tileMatrices: Array.from({ length: 25 }, (_, i) => ({
                id: String(i),
                cellSize: 1,
                scaleDenominator: 1,
                pointOfOrigin: [0, 0],
                tileWidth: 256,
                tileHeight: 256,
                matrixWidth: 2 ** i,
                matrixHeight: 2 ** i,
              })),
            },
          ],
        };
      else if (path === "/tile-matrix-sets")
        body = {
          tile_matrix_sets: [
            { id: "WebMercatorQuad", built_in: true, title: "Web Mercator" },
          ],
        };
      else if (path === "/tile-matrix-sets/WebMercatorQuad")
        body = {
          id: "WebMercatorQuad",
          revision: 1,
          digest: "test",
          ...timestamps,
          definition: {
            id: "WebMercatorQuad",
            crs: "EPSG:3857",
            tileMatrices: Array.from({ length: 25 }, (_, i) => ({
              id: String(i),
              cellSize: 1,
              scaleDenominator: 1,
              pointOfOrigin: [0, 0],
              tileWidth: 256,
              tileHeight: 256,
              matrixWidth: 2 ** i,
              matrixHeight: 2 ** i,
            })),
          },
        };
      else if (path.endsWith("/mosaic/granules"))
        body = { granules: data.granules };
      else if (path.endsWith("/mosaic/granules/g1") && method === "DELETE") {
        data.granules = [];
        data.granuleDeletions++;
        status = 204;
      } else if (path.endsWith("/mosaic/harvest-jobs")) {
        if (method === "POST") {
          data.harvests.push(request.postDataJSON());
          status = 202;
          body = {
            id: "h1",
            workspace_id: "w1",
            service_id: "r1",
            request: request.postDataJSON(),
            status: "queued",
            total_granules: 0,
            processed_granules: 0,
            ...timestamps,
          };
        } else body = { jobs: [] };
      } else throw new Error(`Unexpected fixture request: ${method} ${path}`);
      assertContract(path, method, body, status);
      await route.fulfill(status === 204 ? { status } : { status, json: body });
    });
    await page.route("**/ready", (route) =>
      route.fulfill({
        json: {
          status: "ready",
          checks: { catalog: "ok" },
          durations_ms: { catalog: 1 },
        },
      }),
    );
    await page.route("**/workspaces/demo/ogc/collections", (route) =>
      route.fulfill({
        json: {
          collections: [
            {
              id: "roads",
              title: "Roads",
              extent: { spatial: { bbox: [[1, 1, 2, 2]] } },
            },
          ],
        },
      }),
    );
    await page.route("**/workspaces/demo/ogc/collections/*/items?**", (route) =>
      route.fulfill({ json: sample }),
    );
    await page.route("**/workspaces/demo/wms?**", (route) => {
      data.wmsRequests.push(route.request().url());
      return route.fulfill({
        contentType: "image/png",
        body: Buffer.from(
          "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVQIHWP4z8DwHwAFgAI/ScLbtAAAAABJRU5ErkJggg==",
          "base64",
        ),
      });
    });
    await provideFixture(data);
  },
});

const sample = {
  type: "FeatureCollection",
  features: [
    {
      type: "Feature",
      id: "feature-1",
      geometry: { type: "Point", coordinates: [1.5, 1.5] },
      properties: { name: "Sample road" },
    },
  ],
};
function state() {
  return {
    failStores: false,
    failSettings: false,
    failStats: false,
    failCoverageDiscovery: false,
    identity: undefined as Record<string, unknown> | undefined,
    createdKey: undefined as Record<string, unknown> | undefined,
    revocations: 0,
    apiKeyDeleted: false,
    granuleDeletions: 0,
    tileJobs: [] as Record<string, unknown>[],
    harvests: [] as Record<string, unknown>[],
    coverages: [] as Record<string, unknown>[],
    granules: [
      {
        id: "g1",
        workspace_id: "w1",
        service_id: "r1",
        generation: 1,
        source_uri: "data/elevation.tif",
        crs: "EPSG:4326",
        bbox: [0, 0, 1, 1],
        width: 32,
        height: 32,
        band_count: 1,
        data_type: "Float32",
      },
    ],
    services: [
      {
        id: "s1",
        name: "Primary data",
        type: "postgis",
        enabled: true,
        workspace_id: "w1",
        ...timestamps,
      },
    ],
    authReads: 0,
    discoveryCount: 3,
    workspaces: [
      {
        id: "w1",
        name: "demo",
        description: "Demo",
        counts: {},
        ...timestamps,
      },
    ],
    styles: {
      one: "<StyledLayerDescriptor>ORIGINAL_ONE</StyledLayerDescriptor>",
      two: "<StyledLayerDescriptor>ORIGINAL_TWO</StyledLayerDescriptor>",
    } as Record<string, string>,
    settings: {
      wms: { enabled: true, title: "Maps", unknown_option: "preserve me" },
      ogcapi: {
        enabled: true,
        title: "Features",
        limit_default: 10,
        limit_max: 100,
      },
    } as Record<string, Record<string, unknown>>,
    layer: {
      ...timestamps,
      id: "l1",
      service_id: "s1",
      public_id: "roads",
      source_layer: "public.roads",
      title: "Roads",
      enabled: true,
      public: false,
      crs_default: 4326,
      unknown_option: "preserve me",
    },
    published: [] as Record<string, unknown>[],
    wmsRequests: [] as string[],
    plan: undefined as Record<string, unknown> | undefined,
    job: {
      ...timestamps,
      id: "i1",
      workspace_id: "w1",
      name: "Import roads",
      status: "awaiting_plan",
      phase: "validate",
      source_kind: "upload",
      discovery: {
        layers: [
          {
            name: "roads",
            geometry_column: "geom",
            geometry_type: "Point",
            srid: 4326,
            feature_count: 1,
            properties: [{ name: "name", type: "VARCHAR" }],
          },
        ],
      },
    } as Record<string, any>,
  };
}
export { expect };

/** Answers the console's in-app "discard changes?" confirmation. */
export async function answerConfirm(
  page: import("@playwright/test").Page,
  choice: "keep" | "discard",
) {
  const dialog = page.getByRole("alertdialog");
  await expect(dialog).toBeVisible();
  await dialog
    .getByRole("button", {
      name: choice === "keep" ? "Keep editing" : "Discard changes",
      exact: true,
    })
    .click();
  await expect(dialog).toHaveCount(0);
}

/** Opens a store row's "More actions" menu and chooses an item. */
export async function storeAction(
  page: import("@playwright/test").Page,
  store: string,
  item: string,
) {
  await page
    .getByRole("button", { name: `More actions for ${store}`, exact: true })
    .click();
  await page.getByRole("menuitem", { name: item, exact: true }).click();
}
