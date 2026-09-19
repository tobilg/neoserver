// Runtime format parity and offline acceptance. All Docker resources are disposable.
// Usage: node scripts/test-runtime-container.mjs <candidate> [evidence directory]
import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { mkdirSync, writeFileSync, rmSync } from "node:fs";
import path from "node:path";
import { setTimeout as delay } from "node:timers/promises";

const candidate = process.argv[2] ?? "neoserver:candidate";
const evidence = path.resolve(
  process.argv[3] ?? "test-results/runtime-container",
);
const reference =
  "tobilg/neoserver@sha256:339a91d38f996216c123e1ab01cf24eeed56187d4d15ed54e34834e05a991b23";
const fixture = `neoserver-runtime-${process.pid}`;
const owned = [];
const fixtures = path.resolve("testing/fixtures");
mkdirSync(evidence, { recursive: true });
for (const file of ["result.json", "server.log"])
  rmSync(path.join(evidence, file), { force: true });
const docker = (...args) =>
  execFileSync("docker", args, {
    encoding: "utf8",
    stdio: ["ignore", "pipe", "pipe"],
    timeout: 180_000,
    maxBuffer: 32 * 1024 * 1024,
  }).trim();
const volume = (name) => {
  const v = `${fixture}-${name}`;
  docker("volume", "create", v);
  owned.push(["volume", "rm", v]);
  return v;
};
let current;
let token;
let client;
let base;

async function request(url, method = "GET", body) {
  const headers = {
    Authorization: `Bearer ${token}`,
    "Content-Type": "application/json",
  };
  let status, contentType, bytes;
  if (client) {
    const result = JSON.parse(
      docker(
        "exec",
        client,
        "node",
        "-e",
        `
      const [url,method,headers,body]=JSON.parse(process.argv[1]);
      fetch('http://127.0.0.1:9000'+url,{method,headers,body:body===null?undefined:body,signal:AbortSignal.timeout(60000)})
      .then(async r=>console.log(JSON.stringify({status:r.status,type:r.headers.get('content-type'),body:Buffer.from(await r.arrayBuffer()).toString('base64')})))
      .catch(e=>{console.error(e);process.exit(1)});
    `,
        JSON.stringify([
          url,
          method,
          headers,
          body === undefined ? null : JSON.stringify(body),
        ]),
      ),
    );
    status = result.status;
    contentType = result.type;
    bytes = Buffer.from(result.body, "base64");
  } else {
    const r = await fetch(base + url, {
      method,
      headers,
      body: body === undefined ? undefined : JSON.stringify(body),
      signal: AbortSignal.timeout(60_000),
    });
    status = r.status;
    contentType = r.headers.get("content-type");
    bytes = Buffer.from(await r.arrayBuffer());
  }
  assert.ok(
    status >= 200 && status < 300,
    `${method} ${url}: ${status} ${bytes.toString().slice(0, 1800)}`,
  );
  return { bytes, contentType };
}
const json = async (...args) => JSON.parse((await request(...args)).bytes);

async function start(image, data, name, offline = false, extra = []) {
  current = `${fixture}-${name}`;
  const args = [
    "run",
    "-d",
    "--platform",
    "linux/amd64",
    "--name",
    current,
    "-v",
    `${data}:/data`,
    "-v",
    `${fixtures}:/fixtures:ro`,
    "-e",
    "NEOSRV_STORE_KEY=abc123",
    "-e",
    "NEOSRV_SERVER_HTTPHOST=0.0.0.0",
    "-e",
    "NEOSRV_SERVER_URLBASE=http://localhost:9000",
    "-e",
    "NEOSRV_AUTH_REQUIREHTTPS=false",
    "-e",
    "NEOSRV_DATASOURCE_ALLOWEDPATHS=/fixtures/**",
    "-e",
    "NEOSRV_PERSISTENTCACHE_MAXBYTES=67108864",
  ];
  for (const feature of [
    "WMS",
    "WFS",
    "WCS",
    "WMTS",
    "TILES",
    "AUDIT",
    "MOSAICCATALOG",
    "PERSISTENTCACHE",
  ])
    args.push("-e", `NEOSRV_${feature}_ENABLED=true`);
  args.push(
    ...(offline ? ["--network", "none"] : ["-p", "127.0.0.1::9000"]),
    ...extra,
    image,
  );
  docker(...args);
  owned.push(["rm", "-f", current]);
  client = undefined;
  if (offline) {
    client = `${current}-client`;
    docker(
      "run",
      "-d",
      "--name",
      client,
      "--network",
      `container:${current}`,
      "node:24-alpine",
      "node",
      "-e",
      "setInterval(()=>{},100000)",
    );
    owned.push(["rm", "-f", client]);
  } else
    base = `http://127.0.0.1:${docker("port", current, "9000/tcp").split(":").at(-1)}`;
  for (let i = 0; i < 90; i++) {
    if (docker("inspect", "--format", "{{.State.Running}}", current) !== "true")
      throw new Error(`server exited: ${docker("logs", current)}`);
    try {
      await request("/ready");
      return;
    } catch {
      await delay(1000);
    }
  }
  throw new Error(`server never ready: ${docker("logs", current)}`);
}

async function publish(name, type, file, coverage = false) {
  const api = `/api/v1/workspaces/formats/services/${name}`;
  await json("/api/v1/workspaces/formats/services", "POST", {
    name,
    type,
    connection_info: {
      path: `/fixtures/${file}`,
      ...(name === "nadvector" ? { srid: 4267 } : {}),
      ...(type === "geoparquet"
        ? { geometry_column: "geom", id_column: "id", srid: 4326 }
        : {}),
      ...(type === "duckdb" ? { srid: 4326, read_only: true } : {}),
    },
  });
  const found = await json(
    api + (coverage ? "/discover-coverages" : "/discover"),
    "POST",
    {},
  );
  const source = (coverage ? found.coverages : found.layers)[0];
  assert.ok(source, `no source discovered for ${file}`);
  if (name === "nadvector") assert.equal(source.srid, 4267);
  await json(api + (coverage ? "/coverages" : "/layers"), "POST", {
    public_id: name,
    public: true,
    ...(coverage
      ? { source_coverage: source.source_coverage }
      : {
          source_layer: source.schema
            ? `${source.schema}.${source.name}`
            : source.name,
        }),
  });
}

function formats(xml) {
  const values = [
    ...xml.matchAll(
      /<(?:[\w-]+:)?(?:Format|formatSupported|DefaultFormat|OtherFormat)\b[^>]*>([^<]+)<\//gi,
    ),
  ].map((m) => m[1]);
  for (const section of xml.matchAll(
    /<(?:[\w-]+:)?Parameter\s+name="outputFormat"[^>]*>([\s\S]*?)<\/(?:[\w-]+:)?Parameter>/g,
  )) {
    values.push(
      ...[...section[1].matchAll(/<(?:[\w-]+:)?Value>([^<]+)<\//g)].map(
        (m) => m[1],
      ),
    );
  }
  return [
    ...new Set(values.map((v) => v.trim().replaceAll("&amp;", "&"))),
  ].sort();
}

async function capabilities() {
  const snapshot = {};
  for (const [service, version] of [
    ["wms", "1.3.0"],
    ["wfs", "2.0.0"],
    ["wcs", "2.1.0"],
    ["wmts", "1.0.0"],
  ]) {
    snapshot[service] = formats(
      (
        await request(
          `/workspaces/formats/${service}?SERVICE=${service.toUpperCase()}&REQUEST=GetCapabilities&VERSION=${version}`,
        )
      ).bytes.toString(),
    );
    assert.ok(snapshot[service].length, `no formats extracted for ${service}`);
  }
  for (const endpoint of [
    "ogc/",
    "ogc/collections",
    "ogc-tiles/",
    "ogc-tiles/collections",
    "ogc-tiles/collections/points/tiles/WebMercatorQuad",
    "ogc-tiles/collections/points/map/tiles/WebMercatorQuad",
  ]) {
    const types = new Set();
    const visit = (v) => {
      if (!v || typeof v !== "object") return;
      if (typeof v.type === "string" && v.type.includes("/")) types.add(v.type);
      Object.values(v).forEach(visit);
    };
    visit(await json(`/workspaces/formats/${endpoint}`));
    snapshot[endpoint] = [...types].sort();
  }
  return snapshot;
}

async function rasterResponse(url, filename, driver, expectedSize) {
  const { bytes } = await request(url);
  const file = path.join(evidence, filename);
  writeFileSync(file, bytes);
  docker("cp", file, `${current}:/tmp/${filename}`);
  const result = JSON.parse(
    docker("exec", current, "gdalinfo", "-json", "-stats", `/tmp/${filename}`),
  );
  assert.equal(result.driverShortName, driver, filename);
  if (expectedSize) assert.deepEqual(result.size, expectedSize, filename);
  return result;
}

try {
  // A fresh initialization must work before any online process has warmed a cache.
  const empty = volume("offline-init");
  docker(
    "run",
    "--rm",
    "--network",
    "none",
    "--platform",
    "linux/amd64",
    "-v",
    `${empty}:/data`,
    "-e",
    "NEOSRV_STORE_KEY=abc123",
    candidate,
    "init",
  );
  const installed = JSON.parse(
    docker(
      "run",
      "--rm",
      "--network",
      "none",
      "--platform",
      "linux/amd64",
      candidate,
      "install-extensions",
    ),
  );
  assert.deepEqual(
    installed.extensions.map((e) => e.name),
    ["spatial", "httpfs"],
  );
  writeFileSync(
    path.join(evidence, "extensions.json"),
    JSON.stringify(installed, null, 2),
  );
  const original = volume("reference");
  const init = docker(
    "run",
    "--rm",
    "--platform",
    "linux/amd64",
    "-v",
    `${original}:/data`,
    "-e",
    "NEOSRV_STORE_KEY=abc123",
    reference,
    "init",
  );
  token = init.split(/\r?\n/).find((line) => line.startsWith("eyJ"));
  assert.ok(token);
  await start(reference, original, "reference");
  console.log("Reference 0.1.0 server ready");
  await json("/api/v1/workspaces", "POST", { name: "formats" });
  for (const service of ["wms", "wfs", "wcs", "wmts", "ogcapi", "ogc-tiles"]) {
    const url = `/api/v1/workspaces/formats/settings/${service}`;
    const settings = await json(url);
    await json(url, "PUT", {
      ...settings,
      enabled: true,
      public: true,
      ...(service === "wcs"
        ? {
            extensions: ["crs"],
            allowed_output_crs: ["EPSG:4326", "EPSG:4269"],
            output_formats: [
              "image/tiff",
              "application/gml+xml",
              "multipart/related",
              "application/netcdf",
              "image/jp2",
            ],
          }
        : {}),
    });
  }
  await publish("points", "vectorfile", "vector/runtime-points.geojson");
  await publish("grid", "rasterfile", "raster/runtime-grid.tif", true);
  await publish("nadvector", "vectorfile", "vector/runtime-nad27.shp");
  const referenceVector = (
    await json("/workspaces/formats/ogc/collections/nadvector/items")
  ).features[0].geometry.coordinates;
  const before = await capabilities();
  const oldServer = current;
  docker("stop", oldServer);
  const data = volume("candidate");
  docker(
    "run",
    "--rm",
    "--user",
    "0:0",
    "--platform",
    "linux/amd64",
    "-v",
    `${original}:/from:ro`,
    "-v",
    `${data}:/to`,
    "--entrypoint",
    "sh",
    candidate,
    "-ec",
    "cp -a /from/. /to/; chown -R 65532:65532 /to",
  );
  await start(candidate, data, "offline", true);
  assert.equal(
    docker("inspect", "--format", "{{.HostConfig.NetworkMode}}", current),
    "none",
  );
  const after = await capabilities();
  writeFileSync(
    path.join(evidence, "capabilities.json"),
    JSON.stringify({ reference, before, after }, null, 2),
  );
  assert.deepEqual(after, before, "advertised formats changed from 0.1.0");
  console.log("Candidate ready offline; capabilities match 0.1.0");
  const candidateVector = (
    await json("/workspaces/formats/ogc/collections/nadvector/items")
  ).features[0].geometry.coordinates;
  assert.deepEqual(
    candidateVector,
    referenceVector,
    "default DuckDB vector reprojection changed with system grid removal",
  );
  writeFileSync(
    path.join(evidence, "vector-reprojection.json"),
    JSON.stringify(
      { reference: referenceVector, candidate: candidateVector },
      null,
      2,
    ),
  );
  assert.ok(
    after.wcs.includes("application/netcdf") && after.wcs.includes("image/jp2"),
  );
  assert.ok(after.wms.includes("application/pdf"));
  assert.ok(after.wfs.includes("SHAPE-ZIP") || after.wfs.includes("shape-zip"));
  for (const [name, type, file, coverage] of [
    ["netcdf", "rasterfile", "raster/runtime-grid.nc", true],
    ["grib", "rasterfile", "raster/runtime-grid.grib2", true],
    ["duckdb", "duckdb", "vector/runtime-points.duckdb", false],
    ["parquet", "geoparquet", "vector/runtime-points.parquet", false],
  ])
    await publish(name, type, file, coverage);
  for (const name of ["points", "duckdb", "parquet"]) {
    const features = await json(
      `/workspaces/formats/ogc/collections/${name}/items?limit=10`,
    );
    assert.equal(features.features.length, 1, name);
    assert.equal(features.features[0].properties.name, "München", name);
    assert.deepEqual(features.features[0].geometry.coordinates, [8, 51], name);
  }
  // The image's gconv modules belong to system GDAL. DuckDB spatial has its
  // own GDAL build, which also cannot decode CP1252 in released 0.1.0.
  assert.match(
    docker(
      "exec",
      current,
      "ogrinfo",
      "-al",
      "/fixtures/vector/runtime-cp1252.shp",
    ),
    /München/,
  );
  const wcs = (name, format) =>
    `/workspaces/formats/wcs?SERVICE=WCS&VERSION=2.1.0&REQUEST=GetCoverage&COVERAGEID=${name}&FORMAT=${encodeURIComponent(format)}`;
  for (const [format, suffix, driver] of [
    ["application/netcdf", "nc", "netCDF"],
    ["image/jp2", "jp2", "JP2OpenJPEG"],
  ]) {
    const result = await rasterResponse(
      wcs("grid", format),
      `coverage.${suffix}`,
      driver,
      [3, 2],
    );
    assert.equal(result.bands[0].minimum, 1);
    assert.equal(result.bands[0].maximum, 6);
  }
  for (const name of ["netcdf", "grib"]) {
    const result = await rasterResponse(
      wcs(name, "image/tiff"),
      `${name}.tif`,
      "GTiff",
      [3, 2],
    );
    assert.equal(result.bands[0].minimum, 1);
    assert.equal(result.bands[0].maximum, 6);
  }
  await publish("nad27", "rasterfile", "raster/runtime-nad27.tif", true);
  const fallback = await rasterResponse(
    wcs("nad27", "image/tiff") +
      "&OUTPUTCRS=http://www.opengis.net/def/crs/EPSG/0/4269",
    "nad83-fallback.tif",
    "GTiff",
  );
  assert.ok(fallback.coordinateSystem.wkt.includes("NAD83"));
  assert.equal(fallback.bands[0].minimum, 1);
  assert.equal(fallback.bands[0].maximum, 6);
  const wms =
    "/workspaces/formats/wms?SERVICE=WMS&VERSION=1.3.0&REQUEST=GetMap&LAYERS=points&STYLES=&CRS=EPSG:4326&BBOX=50,7,52,10&WIDTH=64&HEIGHT=64&FORMAT=";
  await rasterResponse(wms + "application/pdf", "map.pdf", "PDF", [64, 64]);
  for (const format of ["image/png", "image/jpeg"]) {
    const { bytes, contentType } = await request(wms + format);
    assert.equal(contentType, format);
    if (format === "image/png")
      assert.equal(bytes.subarray(1, 4).toString(), "PNG");
    else {
      assert.equal(bytes.subarray(0, 3).toString("hex"), "ffd8ff");
    }
  }
  for (const [format, suffix, expected] of [
    ["application/geopackage+sqlite3", "gpkg", "GPKG"],
    ["SHAPE-ZIP", "zip", "ESRI Shapefile"],
  ]) {
    const { bytes } = await request(
      `/workspaces/formats/wfs?SERVICE=WFS&VERSION=2.0.0&REQUEST=GetFeature&TYPENAMES=points&OUTPUTFORMAT=${encodeURIComponent(format)}`,
    );
    const file = path.join(evidence, `features.${suffix}`);
    writeFileSync(file, bytes);
    docker("cp", file, `${current}:/tmp/features.${suffix}`);
    const source =
      suffix === "zip" ? "/vsizip//tmp/features.zip" : "/tmp/features.gpkg";
    const info = JSON.parse(
      docker("exec", current, "ogrinfo", "-json", "-al", "-so", source),
    );
    assert.equal(info.driverShortName, expected);
    assert.equal(info.layers[0].featureCount, 1);
  }
  for (const endpoint of [
    "/workspaces/formats/ogc-tiles/collections/points/map/tiles/WebMercatorQuad/0/0/0?f=webp",
    "/workspaces/formats/wmts?SERVICE=WMTS&VERSION=1.0.0&REQUEST=GetTile&LAYER=points&STYLE=default&FORMAT=image/webp&TILEMATRIXSET=WebMercatorQuad&TILEMATRIX=0&TILEROW=0&TILECOL=0",
  ]) {
    const { bytes, contentType } = await request(endpoint);
    assert.equal(contentType, "image/webp");
    assert.equal(bytes.subarray(0, 4).toString(), "RIFF");
    assert.equal(bytes.subarray(8, 12).toString(), "WEBP");
  }
  writeFileSync(
    path.join(evidence, "result.json"),
    JSON.stringify(
      {
        candidate,
        image_id: docker("image", "inspect", "--format", "{{.Id}}", candidate),
        reference,
        checks: [
          "released-schema-reopen",
          "capabilities-parity",
          "offline-init-and-queries",
          "format-decode",
          "netcdf-grib-input",
          "cp1252",
        ],
      },
      null,
      2,
    ),
  );
  console.log(
    "PASS: 0.1.0 capability parity, offline initialization/queries, raster/vector formats and code-page conversion",
  );
} catch (error) {
  if (current) {
    try {
      writeFileSync(path.join(evidence, "server.log"), docker("logs", current));
    } catch {}
  }
  throw error;
} finally {
  for (const args of owned.reverse()) {
    try {
      docker(...args);
    } catch (error) {
      console.error(`Cleanup failed: ${args.join(" ")}: ${error.message}`);
    }
  }
}
