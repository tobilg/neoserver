// Native, disposable acceptance/operating baseline. Never targets an existing
// server, catalog or source database. No credentials are included in evidence.
import assert from "node:assert/strict";
import { spawn, execFileSync } from "node:child_process";
import { createHash } from "node:crypto";
import { createServer } from "node:net";
import {
  mkdtemp,
  readFile,
  writeFile,
  mkdir,
  rm,
  open,
} from "node:fs/promises";
import { cpus, platform, arch, tmpdir, totalmem } from "node:os";
import { resolve, join } from "node:path";
import { setTimeout as delay } from "node:timers/promises";
import { once } from "node:events";
import { JSDOM } from "jsdom";
import { chromium, expect } from "@playwright/test";
import WMSCapabilities from "ol/format/WMSCapabilities.js";
import WMTSCapabilities from "ol/format/WMTSCapabilities.js";
import WFS from "ol/format/WFS.js";
import GeoJSON from "ol/format/GeoJSON.js";
import ImageWMS from "ol/source/ImageWMS.js";
import WMTS, { optionsFromCapabilities } from "ol/source/WMTS.js";
import { fromLonLat } from "ol/proj.js";
import {
  styleFormats,
  styleGeometries,
  styleTemplate,
} from "../src/features/styles/style-templates.ts";

const binary = resolve(process.argv[2] ?? "../../neoserver");
const output = resolve(
  process.argv[3] ?? "../../test-results/qualification-native",
);
const phaseSeconds = Number(process.env.QUAL_PHASE_SECONDS ?? 5);
const soakSeconds = Number(process.env.QUAL_SOAK_SECONDS ?? 120);
assert(
  phaseSeconds >= 1 &&
    phaseSeconds <= 60 &&
    soakSeconds >= 1 &&
    soakSeconds <= 3600,
  "invalid duration bounds",
);
await mkdir(output, { recursive: true });
const root = await mkdtemp(join(tmpdir(), "neoserver-qualification-"));
const env = Object.fromEntries(
  Object.entries(process.env).filter(([key]) => !key.startsWith("NEOSRV_")),
);
env.NEOSRV_STORE_KEY = "abc123";
const socket = createServer();
socket.listen(0, "127.0.0.1");
await once(socket, "listening");
const port = socket.address().port;
await new Promise((done) => socket.close(done));
const base = `http://127.0.0.1:${port}`;
const api = "/api/v1/workspaces/baseline";
const serviceRoot = "/workspaces/baseline";
const source = join(root, "points.geojson");
const points = {
  type: "FeatureCollection",
  features: Array.from({ length: 10000 }, (_, i) => ({
    type: "Feature",
    id: i + 1,
    properties: { id: i + 1, name: `point-${i + 1}`, radius: 5 },
    geometry: {
      type: "Point",
      coordinates: [7 + (i % 100) / 1000, 51 + Math.floor(i / 100) / 1000],
    },
  })),
};
await writeFile(source, JSON.stringify(points));
await writeFile(
  join(root, "config.toml"),
  `
[Server]
HttpHost = "127.0.0.1"
HttpPort = ${port}
UrlBase = "${base}"
AdminUI = true
[Store]
Path = ${JSON.stringify(join(root, "catalog.duckdb"))}
[Auth]
RequireHTTPS = false
[Datasource]
AllowedPaths = [${JSON.stringify(source)}]
[WMS]
Enabled = true
Extensions = ["dynamic-style"]
StyleAssetPath = ${JSON.stringify(join(root, "styles"))}
[WFS]
Enabled = true
[Tiles]
Enabled = true
[WMTS]
Enabled = true
[Cache]
Enabled = true
`,
);
const report = {
  kind: "native-local-baseline-not-release-qualification",
  date: new Date().toISOString(),
  binary_sha256: createHash("sha256")
    .update(await readFile(binary))
    .digest("hex"),
  version: execFileSync(binary, ["version"], { env, encoding: "utf8" }).trim(),
  host: {
    platform: platform(),
    arch: arch(),
    cpu: cpus()[0]?.model,
    logical_cpus: cpus().length,
    memory_bytes: totalmem(),
    node: process.version,
  },
  source: {
    type: "GeoJSON via GDAL/DuckDB",
    features: 10000,
    crs: "EPSG:4326",
    extent: [7, 51, 7.099, 51.099],
  },
  openlayers: JSON.parse(
    await readFile(new URL("../node_modules/ol/package.json", import.meta.url)),
  ).version,
  checks: [],
  phases: [],
  passed: false,
};
// Never leave a previous successful run looking current while this run proceeds.
await writeFile(
  join(output, "baseline.json"),
  JSON.stringify(report, null, 2) + "\n",
);
let server;
let log;
let token;
const headers = () => ({ Authorization: `Bearer ${token}` });
async function request(path, options = {}) {
  const url = new URL(path, base);
  assert.equal(
    url.origin,
    base,
    "client generated a URL outside the disposable fixture",
  );
  const response = await fetch(url, {
    ...options,
    headers: { ...headers(), ...options.headers },
    signal: options.signal ?? AbortSignal.timeout(30000),
  });
  if (!response.ok)
    throw new Error(
      `${options.method ?? "GET"} ${url.pathname}: ${response.status} ${(await response.text()).slice(0, 1000)}`,
    );
  return response;
}
async function json(path, method = "GET", data) {
  const response = await request(path, {
    method,
    headers: { "Content-Type": "application/json" },
    ...(data === undefined ? {} : { body: JSON.stringify(data) }),
  });
  return response.json();
}
function rssKiB() {
  return Number(
    execFileSync("ps", ["-o", "rss=", "-p", String(server.pid)], {
      encoding: "utf8",
    }).trim(),
  );
}
async function phase(name, concurrency, seconds, paths, fresh = false) {
  const latencies = [];
  const status = {};
  let bytes = 0;
  let maxRSS = rssKiB();
  const start = performance.now();
  const sampler = setInterval(() => {
    try {
      maxRSS = Math.max(maxRSS, rssKiB());
    } catch {
      // A process exit can race the RSS sampler; HTTP checks still fail the run.
    }
  }, 1000);
  try {
    await Promise.all(
      Array.from({ length: concurrency }, async (_, worker) => {
        let i = worker;
        while (performance.now() - start < seconds * 1000) {
          const began = performance.now();
          const response = await request(
            paths[i++ % paths.length],
            fresh ? { headers: { "Cache-Control": "no-cache" } } : {},
          );
          const body = await response.arrayBuffer();
          assert(body.byteLength > 0);
          bytes += body.byteLength;
          const cache = response.headers.get("x-cache") ?? "unspecified";
          status[cache] = (status[cache] ?? 0) + 1;
          latencies.push(performance.now() - began);
        }
      }),
    );
  } finally {
    clearInterval(sampler);
  }
  latencies.sort((a, b) => a - b);
  const elapsed = (performance.now() - start) / 1000;
  const percentile = (p) =>
    latencies[Math.min(latencies.length - 1, Math.floor(latencies.length * p))];
  report.phases.push({
    name,
    concurrency,
    seconds: elapsed,
    requests: latencies.length,
    requests_per_second: latencies.length / elapsed,
    p50_ms: percentile(0.5),
    p95_ms: percentile(0.95),
    p99_ms: percentile(0.99),
    bytes,
    cache: status,
    peak_process_rss_kib: maxRSS,
  });
}
try {
  const initialized = execFileSync(
    binary,
    ["init", "--store-path", join(root, "catalog.duckdb")],
    { env, cwd: root, encoding: "utf8" },
  );
  token = initialized
    .match(/Bootstrap Access Token[^\n]*\n([^\n]+)/)?.[1]
    ?.trim();
  assert(token, "bootstrap token missing");
  log = await open(join(output, "server.log"), "w");
  const startServer = async () => {
    server = spawn(binary, ["serve", "--config", join(root, "config.toml")], {
      env,
      cwd: root,
      stdio: ["ignore", log.fd, log.fd],
    });
    for (let attempt = 0; attempt < 120; attempt++) {
      if (server.exitCode !== null)
        throw new Error(`fixture exited ${server.exitCode}; see server.log`);
      try {
        if (
          (await fetch(`${base}/ready`, { signal: AbortSignal.timeout(2000) }))
            .ok
        )
          return;
      } catch {
        // The listener may not be accepting connections yet.
      }
      await delay(500);
    }
    throw new Error("fixture readiness timeout");
  };
  await startServer();
  await json("/api/v1/workspaces", "POST", { name: "baseline" });
  for (const name of ["ogcapi", "wms", "wfs", "ogc-tiles", "wmts"]) {
    const settings = await json(`${api}/settings/${name}`);
    await json(`${api}/settings/${name}`, "PUT", {
      ...settings,
      enabled: true,
      public: false,
      ...(name === "wms" ? { extensions: ["dynamic-style"] } : {}),
    });
  }
  await json(`${api}/services`, "POST", {
    name: "source",
    type: "vectorfile",
    enabled: true,
    connection_info: { path: source },
  });
  const discovered = await json(`${api}/services/source/discover`, "POST");
  assert(discovered.layers?.length > 0);
  const published = await json(`${api}/services/source/layers`, "POST", {
    source_layer: discovered.layers[0].name,
    public_id: "points",
    enabled: true,
    public: true,
    crs_default: 4326,
  });
  const dom = new JSDOM();
  for (const key of ["DOMParser", "XMLSerializer", "Node", "document", "Image"])
    globalThis[key] = dom.window[key];
  const featurePath = `${serviceRoot}/ogc/collections/points/items?limit=100`;
  const features = new GeoJSON().readFeatures(await json(featurePath));
  assert.equal(features.length, 100);
  assert.deepEqual(features[0].getGeometry().getCoordinates(), [7, 51]);
  report.checks.push(
    "OpenLayers GeoJSON: 100 features, source coordinate control",
  );
  const wfs = await request(
    `${serviceRoot}/wfs?service=WFS&version=2.0.0&request=GetFeature&typeNames=points&count=10&srsName=EPSG:4326`,
  );
  const decoded = new WFS({ version: "2.0.0" }).readFeatures(await wfs.text(), {
    featureProjection: "EPSG:4326",
  });
  assert.equal(decoded.length, 10);
  // OpenLayers expands a 2D GML position with a zero Z ordinate.
  assert.deepEqual(
    decoded[0].getGeometry().getCoordinates().slice(0, 2),
    [7, 51],
  );
  report.checks.push(
    "OpenLayers WFS 2.0 GML: 10 features and EPSG:4326 axis order",
  );
  const caps = new WMSCapabilities().read(
    await (
      await request(
        `${serviceRoot}/wms?service=WMS&request=GetCapabilities&version=1.3.0`,
      )
    ).text(),
  );
  assert.equal(caps.version, "1.3.0");
  assert(caps.Capability.Layer.Layer.some((layer) => layer.Name === "points"));
  const imageSource = new ImageWMS({
    url: `${base}${serviceRoot}/wms`,
    params: { LAYERS: "points", VERSION: "1.3.0" },
  });
  const identify = imageSource.getFeatureInfoUrl(
    fromLonLat([7, 51]),
    10,
    "EPSG:3857",
    { INFO_FORMAT: "application/json" },
  );
  assert((await json(identify)).features.length > 0);
  report.checks.push(
    "OpenLayers WMS 1.3 capabilities and generated EPSG:3857 identify request",
  );
  const wmtsCaps = new WMTSCapabilities().read(
    await (
      await request(
        `${serviceRoot}/wmts?service=WMTS&request=GetCapabilities&version=1.0.0`,
      )
    ).text(),
  );
  const layerID = wmtsCaps.Contents.Layer.find((layer) =>
    layer.Identifier.includes("points"),
  )?.Identifier;
  assert(layerID);
  const options = optionsFromCapabilities(wmtsCaps, {
    layer: layerID,
    matrixSet: "WebMercatorQuad",
  });
  assert(options);
  const tileURL = new WMTS(options).getTileUrlFunction()(
    [0, 0, 0],
    1,
    "EPSG:3857",
  );
  const tile = await request(tileURL);
  assert(tile.headers.get("content-type").startsWith("image/"));
  await tile.arrayBuffer();
  report.checks.push(
    "OpenLayers WMTS capabilities, matrix selection and generated tile request",
  );
  const unauthorized = await fetch(`${base}${featurePath}`);
  assert([401, 403].includes(unauthorized.status));
  report.checks.push("Anonymous denied; authenticated client requests succeed");
  for (const format of styleFormats)
    for (const geometry of styleGeometries) {
      const name = `${format.value.replaceAll(".", "-")}-${geometry}`;
      const created = await json(`${api}/styles`, "POST", {
        name,
        format: format.value,
        body: styleTemplate(format.value, geometry),
      });
      assert.equal(created.valid, true, JSON.stringify(created));
    }
  report.checks.push(
    "All 24 console format/starter combinations compile through the real API",
  );
  if (process.env.QUAL_BROWSER === "true") {
    const browser = await chromium.launch();
    try {
      const page = await browser.newPage();
      await page.goto(`${base}/admin/login`);
      await page.getByRole("tab", { name: "Token", exact: true }).click();
      await page.getByLabel("API key or JWT").fill(token);
      await page.getByRole("button", { name: "Start session" }).click();
      await page.waitForURL((url) => !url.pathname.endsWith("/login"));
      for (const format of styleFormats) {
        const name = `ui-${format.value.replaceAll(".", "-")}`;
        await page.goto(`${base}/admin/workspaces/baseline/styles`);
        await page
          .getByRole("button", { name: "New style", exact: true })
          .click();
        await page.getByLabel("Style name", { exact: true }).fill(name);
        await page.getByLabel("Style format", { exact: true }).click();
        await page
          .getByRole("option", { name: format.label, exact: true })
          .click();
        await page.getByLabel("Starter", { exact: true }).click();
        await page.getByRole("option", { name: "point", exact: true }).click();
        await page
          .getByRole("button", { name: "Create style", exact: true })
          .click();
        await expect(
          page.getByRole("combobox", { name: "Style", exact: true }),
        ).toContainText(name);
        const editor = page.getByRole("textbox", {
          name: /^(sld_|se_)/.test(format.value)
            ? "SLD XML"
            : `Style source (${format.value})`,
          exact: true,
        });
        await editor.fill(
          styleTemplate(format.value, "point").replaceAll("#5fa8cc", "#336699"),
        );
        const saved = page.waitForResponse(
          (response) =>
            response.request().method() === "PUT" &&
            response.url().endsWith(`/styles/${name}`),
        );
        await page.getByRole("button", { name: /Save style/ }).click();
        assert((await saved).ok());
        await page.goto(`${base}/admin/workspaces/baseline/layers`);
        await page
          .getByRole("button", { name: "Edit points", exact: true })
          .click();
        await page
          .getByLabel("Default style", { exact: true })
          .selectOption(name);
        await page
          .getByRole("button", { name: "Save publication", exact: true })
          .click();
        await expect(page.getByRole("dialog")).toHaveCount(0);
        await page.goto(`${base}/admin/workspaces/baseline/styles`);
        await page
          .getByRole("combobox", { name: "Style", exact: true })
          .click();
        await page.getByRole("option", { name, exact: true }).click();
        await page
          .getByLabel("Preview layer ID", { exact: true })
          .fill("points");
        await expect(
          page.getByRole("img", {
            name: `WMS preview of points using ${name}`,
            exact: true,
          }),
        ).toBeVisible();
        report.checks.push(
          `Chromium console: ${format.label} create/edit/save/bind/named preview`,
        );
      }
      const settings = await json(`${api}/settings/wms`);
      await json(`${api}/settings/wms`, "PUT", { ...settings, extensions: [] });
      await page.getByLabel("Preview layer ID", { exact: true }).fill("");
      await page.getByLabel("Preview layer ID", { exact: true }).fill("points");
      await expect(page.getByRole("alert")).toContainText("dynamic-style");
      await json(`${api}/settings/wms`, "PUT", settings);
      const editor = page.getByRole("textbox", {
        name: "Style source (mapbox)",
        exact: true,
      });
      const invalid =
        '{"layers":[{"type":"circle","paint":{"circle-radius":["step",["zoom"],1,2,3]}}]}';
      await editor.fill(invalid);
      await page.getByRole("button", { name: /Save style/ }).click();
      await expect(
        page
          .getByRole("alert")
          .filter({ hasText: "layers[0].paint.circle-radius" }),
      ).toBeVisible();
      await expect(editor).toContainText(invalid);
      report.checks.push(
        "Console extension-gate feedback and validation failure preserve the draft",
      );
      report.browser = await browser.version();
    } finally {
      await browser.close();
    }
  }
  const mapPath = `${serviceRoot}/wms?service=WMS&version=1.3.0&request=GetMap&layers=points&styles=mapbox-point&crs=CRS:84&bbox=6.99,50.99,7.11,51.11&width=256&height=256&format=image/png`;
  await json(`${api}/services/source/layers/${published.id}`, "PUT", {
    default_style: "mapbox-point",
    styles: [],
  });
  assert(
    (await request(mapPath)).headers
      .get("content-type")
      .startsWith("image/png"),
  );
  report.checks.push(
    "Alternate-format named style binding and WMS PNG portrayal",
  );
  if (process.env.QUAL_ONLY_CLIENTS !== "true") {
    await phase("features-origin", 1, phaseSeconds, [featurePath], true);
    for (const concurrency of [1, 4, 8])
      await phase("features-warm", concurrency, phaseSeconds, [featurePath]);
    for (const concurrency of [1, 4, 8])
      await phase("mixed-warm", concurrency, phaseSeconds, [
        featurePath,
        mapPath,
        tileURL,
      ]);
    await phase("mixed-soak", 4, soakSeconds, [featurePath, mapPath, tileURL]);
  }
  const exited = once(server, "exit");
  server.kill("SIGTERM");
  await exited;
  await startServer();
  assert.equal((await json(featurePath)).features.length, 100);
  assert(
    (await request(mapPath)).headers
      .get("content-type")
      .startsWith("image/png"),
  );
  report.checks.push("Clean restart retains publication and named style");
  report.passed = true;
} catch (error) {
  report.error = String(error);
  process.exitCode = 1;
} finally {
  if (server && server.exitCode === null) {
    const exited = once(server, "exit");
    server.kill("SIGTERM");
    await exited;
  }
  await log?.close();
  await writeFile(
    join(output, "baseline.json"),
    JSON.stringify(report, null, 2) + "\n",
  );
  // root is created by mkdtemp above and is never supplied by an operator.
  await rm(root, { recursive: true, force: true });
}
console.log(
  JSON.stringify({ passed: report.passed, output, error: report.error }),
);
