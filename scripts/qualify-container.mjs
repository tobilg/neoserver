// Disposable exact-image QGIS and constrained PostGIS/raster operating baseline.
// Usage: node scripts/qualify-container.mjs IMAGE [OUTPUT_DIRECTORY]
import assert from "node:assert/strict";
import { execFileSync, spawn } from "node:child_process";
import { mkdir, readFile, writeFile } from "node:fs/promises";
import { resolve } from "node:path";
import os from "node:os";
import { setTimeout as delay } from "node:timers/promises";

const image = process.argv[2];
assert(image, "an already built candidate image is required");
const soakSeconds = Number(process.env.QUAL_SOAK_SECONDS ?? 120);
assert(
  Number.isFinite(soakSeconds) && soakSeconds >= 1 && soakSeconds <= 3600,
  "QUAL_SOAK_SECONDS must be between 1 and 3600",
);
const output = resolve(
  process.argv[3] ?? "test-results/qualification-container",
);
await mkdir(output, { recursive: true });
const fixture = `neoserver-qualification-${process.pid}`;
const server = `${fixture}-server`,
  db = `${fixture}-db`,
  volume = `${fixture}-state`;
const qgisImage =
  "quay.io/bedata/jupyterlab/qgis/base:3.44.14@sha256:76be65483cc923aab99adb6ca7c1ca0ada3a29d8b4b608bd660d1ac063fa79c9";
const owned = [];
const docker = (...args) =>
  execFileSync("docker", args, {
    encoding: "utf8",
    timeout: 180000,
    stdio: ["ignore", "pipe", "pipe"],
  }).trim();
const report = {
  date: new Date().toISOString(),
  image: JSON.parse(docker("image", "inspect", image))[0].Id,
  host: {
    platform: os.platform(),
    architecture: os.arch(),
    cpu: os.cpus()[0]?.model,
    logical_cpus: os.cpus().length,
    memory_bytes: os.totalmem(),
  },
  limits: { server_cpus: 2, server_memory_bytes: 1073741824 },
  qgis_image: qgisImage,
  dataset: {
    vector: "10000 PostGIS points, EPSG:4326",
    raster: "10x10 PostGIS raster, three Float32 bands, nodata -9999",
  },
  checks: [],
  phases: [],
  passed: false,
};
await writeFile(`${output}/baseline.json`, JSON.stringify(report, null, 2));
let base, token, expiredToken;
const api = "/api/v1/workspaces/qualification";
const root = "/workspaces/qualification";
async function request(path, options = {}) {
  const { allowedStatuses = [], ...fetchOptions } = options;
  const response = await fetch(base + path, {
    ...fetchOptions,
    headers: { Authorization: `Bearer ${token}`, ...options.headers },
    signal: options.signal ?? AbortSignal.timeout(60000),
  });
  if (!response.ok && !allowedStatuses.includes(response.status))
    throw new Error(
      `${options.method ?? "GET"} ${path}: ${response.status} ${(await response.text()).slice(0, 1500)}`,
    );
  return response;
}
async function json(path, method = "GET", body) {
  return (
    await request(path, {
      method,
      headers: { "Content-Type": "application/json" },
      ...(body === undefined ? {} : { body: JSON.stringify(body) }),
    })
  ).json();
}
async function until(check, label) {
  for (let i = 0; i < 120; i++) {
    if (await check()) return;
    await delay(500);
  }
  throw new Error(`${label} timed out`);
}
async function ready() {
  base = `http://127.0.0.1:${docker("port", server, "9000/tcp").split(":").at(-1)}`;
  await until(async () => {
    try {
      return (
        await fetch(base + "/ready", { signal: AbortSignal.timeout(1000) })
      ).ok;
    } catch {
      return false;
    }
  }, "readiness");
}
function sql(command) {
  return docker(
    "exec",
    db,
    "psql",
    "-v",
    "ON_ERROR_STOP=1",
    "-U",
    "postgres",
    "-d",
    "postgis",
    "-tAc",
    command,
  );
}
function rss() {
  return Number(
    docker(
      "exec",
      server,
      "sh",
      "-c",
      "awk '/^VmRSS:/ {print $2}' /proc/1/status",
    ),
  );
}
async function phase(
  name,
  concurrency,
  seconds,
  paths,
  fresh = false,
  allowBusy = false,
) {
  const times = [],
    cache = {},
    statuses = {};
  let bytes = 0,
    maxRSS = rss(),
    sampleError,
    sequence = 0,
    busy = 0;
  const start = performance.now();
  const sampler = setInterval(() => {
    try {
      maxRSS = Math.max(maxRSS, rss());
    } catch (error) {
      sampleError = error;
    }
  }, 1000);
  try {
    const results = await Promise.allSettled(
      Array.from({ length: concurrency }, async (_, worker) => {
        let i = worker;
        while (performance.now() - start < seconds * 1000) {
          let path = paths[i++ % paths.length];
          if (fresh && path.includes("/wms?")) {
            // WMS does not promise request no-cache handling. Distinct, tiny
            // extent shifts force distinct render keys instead of relabelling
            // cache hits as cold renders.
            const url = new URL(path, base);
            const bbox = url.searchParams.get("BBOX").split(",").map(Number);
            bbox[0] += ++sequence / 1e7;
            url.searchParams.set("BBOX", bbox.join(","));
            path = url.pathname + url.search;
          }
          const before = performance.now();
          const response = await request(path, {
            ...(fresh ? { headers: { "Cache-Control": "no-cache" } } : {}),
            allowedStatuses: allowBusy ? [503] : [],
          });
          const body = await response.arrayBuffer();
          assert(body.byteLength > 0);
          statuses[response.status] = (statuses[response.status] ?? 0) + 1;
          if (response.status === 503) {
            assert.match(
              Buffer.from(body).toString(),
              /render queue is full|coverage processing queue timed out/,
            );
            busy++;
          }
          bytes += body.byteLength;
          const hit = response.headers.get("x-cache") ?? "unspecified";
          cache[hit] = (cache[hit] ?? 0) + 1;
          times.push(performance.now() - before);
        }
      }),
    );
    for (const result of results) {
      if (result.status === "rejected") throw result.reason;
    }
    if (sampleError) throw sampleError;
  } finally {
    clearInterval(sampler);
  }
  times.sort((a, b) => a - b);
  const secondsActual = (performance.now() - start) / 1000;
  report.phases.push({
    name,
    concurrency,
    seconds: secondsActual,
    requests: times.length,
    errors: busy,
    expected_queue_rejections: busy,
    statuses,
    latency_scope:
      "all completed HTTP requests, including recorded queue rejections",
    requests_per_second: times.length / secondsActual,
    p50_ms: times[Math.floor(times.length * 0.5)],
    p95_ms: times[Math.floor(times.length * 0.95)],
    p99_ms: times[Math.floor(times.length * 0.99)],
    peak_server_rss_kib: maxRSS,
    bytes,
    cache,
  });
  await writeFile(
    `${output}/baseline.json`,
    JSON.stringify(report, null, 2) + "\n",
  );
  console.log(
    JSON.stringify({
      phase: name,
      concurrency,
      requests: times.length,
      statuses,
    }),
  );
}
async function importPoints({
  name = "imported",
  count = 20000,
  interrupt = false,
} = {}) {
  console.log(JSON.stringify({ import: name, count, interrupt }));
  const features = Array.from({ length: count }, (_, i) => ({
    type: "Feature",
    properties: { id: i, name: `import-${i}` },
    geometry: {
      type: "Point",
      coordinates: [7 + (i % 100) / 1000, 51 + Math.floor(i / 100) / 1000],
    },
  }));
  const form = new FormData();
  form.set("name", name);
  form.set(
    "file",
    new Blob([JSON.stringify({ type: "FeatureCollection", features })], {
      type: "application/geo+json",
    }),
    "points.geojson",
  );
  let job = await (
    await request(api + "/imports", { method: "POST", body: form })
  ).json();
  const path = api + "/imports/" + job.id;
  await until(async () => {
    job = await json(path);
    assert(job.status !== "failed", JSON.stringify(job.error));
    return job.status === "awaiting_plan";
  }, "import discovery");
  const layer = job.discovery.layers[0];
  await json(path + "/plan", "PUT", {
    service_name: name,
    layers: [
      {
        source_layer: layer.name,
        public_id: name,
        geometry_column: layer.geometry_column,
        source_srid: 4326,
        target_srid: 3857,
        enabled: true,
        public: false,
      },
    ],
  });
  if (interrupt) {
    for (let i = 0; i < 500; i++) {
      job = await json(path);
      if (job.status === "running") break;
      assert.equal(
        job.status,
        "queued",
        "conversion completed before the interruption could be observed",
      );
      await delay(10);
    }
    assert.equal(job.status, "running");
    docker("kill", "--signal", "KILL", server);
    docker("start", server);
    await ready();
  }
  await until(async () => {
    job = await json(path);
    assert(job.status !== "failed", JSON.stringify(job.error));
    return job.status === "ready_to_publish";
  }, "import conversion");
  if (interrupt) {
    await json(path, "DELETE");
    await until(
      async () => (await json(path)).status === "cancelled",
      "import cancellation",
    );
    report.checks.push(
      `${count}-point running import resumes after forced process interruption; cancellation completes`,
    );
    return;
  }
  await json(path + "/publish", "POST");
  await until(async () => {
    job = await json(path);
    return job.status === "published";
  }, "import publication");
  assert.equal(
    (await json(root + `/ogc/collections/${name}/items?limit=1`)).numberMatched,
    count,
  );
  report.checks.push(
    `${count}-point upload, reprojection and publication while serving traffic`,
  );
}
try {
  docker("network", "create", fixture);
  owned.push(["network", "rm", fixture]);
  docker("volume", "create", volume);
  owned.push(["volume", "rm", volume]);
  docker(
    "run",
    "-d",
    "--name",
    db,
    "--network",
    fixture,
    "--network-alias",
    "db",
    "--tmpfs",
    "/var/lib/postgresql/data",
    "-e",
    "POSTGRES_PASSWORD=postgres",
    "-e",
    "POSTGRES_DB=postgis",
    "-e",
    "POSTGIS_GDAL_ENABLED_DRIVERS=GTiff",
    "postgis/postgis:16-3.4",
  );
  owned.push(["rm", "-f", db]);
  await until(async () => {
    try {
      return (
        sql("SELECT postgis_version()").length > 0 &&
        docker(
          "exec",
          db,
          "pg_isready",
          "-h",
          "127.0.0.1",
          "-U",
          "postgres",
        ).includes("accepting connections")
      );
    } catch {
      return false;
    }
  }, "PostGIS");
  report.postgis = sql("SELECT postgis_full_version()");
  execFileSync(
    "docker",
    [
      "exec",
      "-i",
      db,
      "psql",
      "-v",
      "ON_ERROR_STOP=1",
      "-U",
      "postgres",
      "-d",
      "postgis",
    ],
    { input: await readFile("testing/initdb/01-init.sql"), timeout: 30000 },
  );
  sql(
    "CREATE TABLE public.qualification_points(id serial PRIMARY KEY,name text,geom geometry(Point,4326)); INSERT INTO public.qualification_points(name,geom) SELECT 'point-'||i,ST_SetSRID(ST_Point(7+(i-1)%100/1000.0,51+floor((i-1)/100)/1000.0),4326) FROM generate_series(1,10000) i;",
  );
  const initialized = docker(
    "run",
    "--rm",
    "-v",
    `${volume}:/data`,
    "-e",
    "NEOSRV_STORE_KEY=abc123",
    image,
    "init",
  );
  token = initialized.match(/eyJ[\w-]+\.[\w-]+\.[\w-]+/)?.[0];
  assert(token);
  expiredToken = docker(
    "run",
    "--rm",
    "-v",
    `${volume}:/data`,
    "-e",
    "NEOSRV_STORE_KEY=abc123",
    image,
    "create-token",
    "--role",
    "super_admin",
    "--expires",
    "1s",
  ).match(/eyJ[\w-]+\.[\w-]+\.[\w-]+/)?.[0];
  assert(expiredToken);
  docker(
    "run",
    "-d",
    "--name",
    server,
    "--network",
    fixture,
    "--network-alias",
    "server",
    "--cpus",
    "2",
    "--memory",
    "1g",
    "--memory-swap",
    "1g",
    "-p",
    "127.0.0.1::9000",
    "-v",
    `${volume}:/data`,
    ...[
      "STORE_KEY=abc123",
      "SERVER_HTTPHOST=0.0.0.0",
      "SERVER_URLBASE=http://server:9000",
      "AUTH_REQUIREHTTPS=false",
      "WMS_ENABLED=true",
      "WFS_ENABLED=true",
      "WCS_ENABLED=true",
      "TILES_ENABLED=true",
      "WMTS_ENABLED=true",
      "IMPORTER_ENABLED=true",
    ].flatMap((v) => ["-e", "NEOSRV_" + v]),
    image,
  );
  owned.push(["rm", "-f", server]);
  await ready();
  await json("/api/v1/workspaces", "POST", { name: "qualification" });
  for (const name of ["ogcapi", "wms", "wfs", "wcs", "ogc-tiles", "wmts"]) {
    const settings = await json(api + "/settings/" + name);
    await json(api + "/settings/" + name, "PUT", {
      ...settings,
      enabled: true,
      public: false,
    });
  }
  await json(api + "/services", "POST", {
    name: "postgis",
    type: "postgis",
    enabled: true,
    connection_info: {
      host: "db",
      port: 5432,
      user: "postgres",
      password: "postgres",
      database: "postgis",
      sslmode: "disable",
      schemas: ["public"],
    },
  });
  await json(api + "/services/postgis/layers", "POST", {
    public_id: "points",
    source_layer: "public.qualification_points",
    enabled: true,
    public: true,
  });
  await json(api + "/services/postgis/coverages", "POST", {
    public_id: "coverage",
    source_coverage: "public.wcs_fixture:rast",
    enabled: true,
    public: true,
  });
  await json(api + "/layer-groups", "POST", {
    public_id: "group",
    enabled: true,
    public: true,
    members: [{ resource: "points" }],
  });
  assert.equal(
    (await fetch(base + root + "/ogc/collections/points/items")).status,
    401,
  );
  report.checks.push(
    "Anonymous denied; authenticated private-service access succeeds",
  );
  if (process.env.QUAL_SKIP_QGIS !== "true") {
    const child = spawn(
      "docker",
      [
        "run",
        "--rm",
        "--name",
        fixture + "-qgis",
        "--network",
        fixture,
        "-e",
        "QT_QPA_PLATFORM=offscreen",
        "-e",
        "QUAL_TOKEN",
        "-e",
        "QUAL_EXPIRED_TOKEN",
        "-v",
        `${resolve("testing/qualification")}:/qualification:ro`,
        "--entrypoint",
        "/usr/bin/python3",
        qgisImage,
        "/qualification/client_qgis.py",
      ],
      {
        env: {
          ...process.env,
          QUAL_TOKEN: token,
          QUAL_EXPIRED_TOKEN: expiredToken,
        },
        stdio: ["ignore", "pipe", "pipe"],
      },
    );
    owned.push(["rm", "-f", fixture + "-qgis"]);
    let stdout = "",
      stderr = "";
    child.stdout.on("data", (data) => {
      stdout += data;
    });
    child.stderr.on("data", (data) => {
      stderr += data;
    });
    const timer = setTimeout(() => {
      child.kill("SIGTERM");
    }, 600000);
    const code = await new Promise((done, reject) => {
      child.once("error", reject);
      child.once("exit", done);
    });
    clearTimeout(timer);
    await writeFile(
      `${output}/qgis.log`,
      (stdout + stderr)
        .replaceAll(token, "[redacted]")
        .replaceAll(expiredToken, "[redacted]"),
    );
    const result = stdout
      .split("\n")
      .find((line) => line.startsWith("NEOSERVER_QGIS_RESULT="));
    assert(result, "QGIS did not produce a result; see qgis.log");
    report.qgis = JSON.parse(result.slice("NEOSERVER_QGIS_RESULT=".length));
    assert.equal(code, 0, JSON.stringify(report.qgis));
    assert(report.qgis.passed);
  }
  const features = root + "/ogc/collections/points/items?limit=100";
  const map = (layer) =>
    root +
    `/wms?SERVICE=WMS&VERSION=1.3.0&REQUEST=GetMap&LAYERS=${layer}&STYLES=&CRS=CRS:84&BBOX=6.9,50.9,7.2,51.2&WIDTH=256&HEIGHT=256&FORMAT=image/png`;
  const raster =
    root +
    "/wcs?SERVICE=WCS&VERSION=2.0.1&REQUEST=GetCoverage&COVERAGEID=coverage&FORMAT=image/tiff&SUBSET=x(10.1,10.8)&SUBSET=y(54.1,54.8)";
  const paths = [features, map("points"), map("group"), raster];
  if (process.env.QUAL_ONLY_CLIENTS !== "true") {
    await phase("PostGIS-features-origin", 1, 5, [features], true);
    await phase("WMS-map-origin", 2, 5, [map("points"), map("group")], true);
    await phase("WCS-raster-origin", 2, 5, [raster]);
    for (const concurrency of [1, 2])
      await phase("mixed", concurrency, 5, paths);
    for (const concurrency of [4, 8])
      await phase("mixed-saturation", concurrency, 5, paths, false, true);
    const importing = await Promise.allSettled([
      importPoints(),
      phase("import-plus-serving", 1, 15, paths),
    ]);
    for (const result of importing) {
      if (result.status === "rejected") throw result.reason;
    }
    await phase("mixed-soak", 2, soakSeconds, paths);
    await importPoints({ name: "recovery", count: 100000, interrupt: true });
  }
  docker("restart", server);
  await ready();
  assert.equal((await json(features)).features.length, 100);
  const coverage = await request(raster);
  assert((await coverage.arrayBuffer()).byteLength > 0);
  report.checks.push(
    "Clean restart retains private vector/raster publications and access",
  );
  report.passed = true;
} catch (error) {
  report.error = String(error);
  process.exitCode = 1;
} finally {
  try {
    await writeFile(`${output}/server.log`, docker("logs", server));
  } catch {
    /* pre-start failure */
  }
  await writeFile(
    `${output}/baseline.json`,
    JSON.stringify(report, null, 2) + "\n",
  );
  for (const args of owned.reverse()) {
    try {
      docker(...args);
    } catch {
      /* already removed */
    }
  }
}
console.log(
  JSON.stringify({ passed: report.passed, output, error: report.error }),
);
