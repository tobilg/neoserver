// Live WFS/cache acceptance check. Creates only uniquely named disposable Docker
// resources; never connects to the operator's catalog or PostGIS database.
// Usage: node scripts/test-wfs-cache-container.mjs neoserver:candidate
import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { setTimeout as delay } from "node:timers/promises";

const image = process.argv[2] ?? "neoserver:candidate";
const fixture = `neoserver-wfs-cache-${process.pid}`;
const server = `${fixture}-server`;
const database = `${fixture}-db`;
const volume = `${fixture}-data`;
const owned = [];
const docker = (...args) =>
  execFileSync("docker", args, {
    encoding: "utf8",
    stdio: ["ignore", "pipe", "pipe"],
    timeout: 120_000,
  }).trim();
let baseURL;
let token;

async function untilReady(check, label) {
  for (let i = 0; i < 60; i++) {
    try {
      if (await check()) return;
    } catch {
      /* startup */
    }
    await delay(1000);
  }
  throw new Error(`${label} did not become ready`);
}

async function request(path, options = {}) {
  const response = await fetch(`${baseURL}${path}`, {
    ...options,
    headers: {
      Authorization: `Bearer ${token}`,
      "Content-Type": "application/json",
      ...options.headers,
    },
    signal: AbortSignal.timeout(30_000),
  });
  assert.ok(
    response.ok,
    `${options.method ?? "GET"} ${path}: ${response.status} ${response.ok ? "" : await response.text()}`,
  );
  return response;
}

async function json(path, method, body) {
  const response = await request(path, {
    method,
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  return response.json();
}

async function ready() {
  baseURL = `http://127.0.0.1:${docker("port", server, "9000/tcp").split(":").at(-1)}`;
  await untilReady(
    async () =>
      (await fetch(`${baseURL}/ready`, { signal: AbortSignal.timeout(2000) }))
        .ok,
    "server",
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
    database,
    "--network",
    fixture,
    "--tmpfs",
    "/var/lib/postgresql/data",
    "-e",
    "POSTGRES_PASSWORD=postgres",
    "-e",
    "POSTGRES_DB=postgis",
    "postgis/postgis:16-3.4",
  );
  owned.push(["rm", "-f", database]);
  // The image uses a temporary socket-only server during initialization.
  await untilReady(
    () =>
      docker(
        "exec",
        database,
        "pg_isready",
        "-h",
        "127.0.0.1",
        "-U",
        "postgres",
      ).includes("accepting connections"),
    "PostGIS",
  );
  await untilReady(
    () =>
      docker(
        "exec",
        database,
        "psql",
        "-U",
        "postgres",
        "-d",
        "postgis",
        "-tAc",
        "SELECT postgis_version()",
      ).length > 0,
    "PostGIS extension",
  );
  docker(
    "exec",
    database,
    "psql",
    "-v",
    "ON_ERROR_STOP=1",
    "-U",
    "postgres",
    "-d",
    "postgis",
    "-c",
    "CREATE TABLE public.cache_probe (id serial PRIMARY KEY, name text, geom geometry(Point,4326)); INSERT INTO public.cache_probe(name,geom) VALUES ('base',ST_SetSRID(ST_MakePoint(-80,40),4326));",
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
  assert.ok(token, "init did not return a bootstrap token");
  docker(
    "run",
    "-d",
    "--name",
    server,
    "--network",
    fixture,
    "-p",
    "127.0.0.1::9000",
    "-v",
    `${volume}:/data`,
    ...[
      "STORE_KEY=abc123",
      "STORE_PATH=/data/neoserver.db",
      "SERVER_HTTPHOST=0.0.0.0",
      "SERVER_URLBASE=http://localhost:9000",
      "AUTH_REQUIREHTTPS=false",
      "WFS_ENABLED=true",
      "TILES_ENABLED=true",
      "WMTS_ENABLED=true",
      "PERSISTENTCACHE_ENABLED=true",
      "PERSISTENTCACHE_MAXBYTES=67108864",
    ].flatMap((value) => ["-e", `NEOSRV_${value}`]),
    image,
  );
  owned.push(["rm", "-f", server]);
  await ready();
  await json("/api/v1/workspaces", "POST", { name: "writes" });
  const api = "/api/v1/workspaces/writes";
  await json(`${api}/services`, "POST", {
    name: "postgis",
    type: "postgis",
    enabled: true,
    connection_info: {
      host: database,
      port: 5432,
      database: "postgis",
      user: "postgres",
      password: "postgres",
      sslmode: "disable",
      schemas: ["public"],
      max_open_conns: 1,
      max_idle_conns: 1,
    },
  });
  await json(`${api}/services/postgis/layers`, "POST", {
    public_id: "roads",
    source_layer: "public.cache_probe",
    enabled: true,
    public: true,
  });
  await json(`${api}/layer-groups`, "POST", {
    public_id: "base",
    enabled: true,
    public: true,
    members: [{ resource: "roads" }],
  });
  for (const service of ["wfs", "ogc-tiles", "wmts"]) {
    const settings = await json(`${api}/settings/${service}`);
    settings.enabled = true;
    if (service === "ogc-tiles") settings.settings.cache_enabled = true;
    await json(`${api}/settings/${service}`, "PUT", settings);
  }
  const paths = [
    "/workspaces/writes/ogc-tiles/collections/roads/tiles/WebMercatorQuad/0/0/0",
    "/workspaces/writes/ogc-tiles/collections/roads/map/tiles/WebMercatorQuad/0/0/0?f=png",
    "/workspaces/writes/ogc-tiles/collections/base/map/tiles/WebMercatorQuad/0/0/0?f=png",
  ];
  const warm = [];
  for (const path of paths)
    warm.push(Buffer.from(await (await request(path)).arrayBuffer()));
  // A restart empties memory; the next hits must come from durable storage.
  docker("restart", server);
  await ready();
  for (let i = 0; i < paths.length; i++) {
    const response = await request(paths[i]);
    assert.equal(response.headers.get("x-cache-tier"), "persistent");
    assert.deepEqual(Buffer.from(await response.arrayBuffer()), warm[i]);
  }
  const filter = '<fes:Filter><fes:ResourceId rid="roads.2"/></fes:Filter>';
  const point = (pos) =>
    `<geom><gml:Point srsName="EPSG:4326"><gml:pos>${pos}</gml:pos></gml:Point></geom>`;
  const edits = [
    [
      "insert",
      `<wfs:Insert><roads><name>inserted</name>${point("20 20")}</roads></wfs:Insert>`,
      2,
    ],
    [
      "update",
      `<wfs:Update typeName="roads"><wfs:Property><wfs:ValueReference>name</wfs:ValueReference><wfs:Value>updated</wfs:Value></wfs:Property>${filter}</wfs:Update>`,
      2,
    ],
    ["delete", `<wfs:Delete typeName="roads">${filter}</wfs:Delete>`, 1],
  ];
  let previous = warm;
  for (const [operation, body, count] of edits) {
    const response = await request("/workspaces/writes/wfs", {
      method: "POST",
      headers: { "Content-Type": "application/xml" },
      body: `<wfs:Transaction service="WFS" version="2.0.0" xmlns:wfs="http://www.opengis.net/wfs/2.0" xmlns:fes="http://www.opengis.net/fes/2.0" xmlns:gml="http://www.opengis.net/gml/3.2">${body}</wfs:Transaction>`,
    });
    const result = await response.text();
    assert.ok(!result.includes("ExceptionReport"), result);
    assert.match(
      result,
      new RegExp(
        `total${operation[0].toUpperCase()}${operation.slice(1)}(?:ed|d)>1<`,
      ),
    );
    assert.equal(
      Number(
        docker(
          "exec",
          database,
          "psql",
          "-U",
          "postgres",
          "-d",
          "postgis",
          "-tAc",
          "SELECT count(*) FROM public.cache_probe",
        ),
      ),
      count,
    );
    if (operation === "update")
      assert.equal(
        docker(
          "exec",
          database,
          "psql",
          "-U",
          "postgres",
          "-d",
          "postgis",
          "-tAc",
          "SELECT name FROM public.cache_probe WHERE id=2",
        ),
        "updated",
      );
    const current = [];
    for (let i = 0; i < paths.length; i++) {
      const tile = await request(paths[i]);
      assert.equal(
        tile.headers.get("x-cache"),
        "MISS",
        `${operation} ${paths[i]}`,
      );
      current.push(Buffer.from(await tile.arrayBuffer()));
      // Attribute edits change the vector payload; unchanged geometry with the
      // default style correctly produces identical pixels, but must still MISS.
      if (operation !== "update" || i === 0)
        assert.notDeepEqual(
          current[i],
          previous[i],
          `${operation} left rendered tile unchanged: ${paths[i]}`,
        );
    }
    // WMTS must use the same refreshed rendering identity as OGC API Tiles.
    const wmts = await request(
      "/workspaces/writes/wmts?SERVICE=WMTS&VERSION=1.0.0&REQUEST=GetTile&LAYER=roads&STYLE=default&FORMAT=image/png&TILEMATRIXSET=WebMercatorQuad&TILEMATRIX=0&TILEROW=0&TILECOL=0",
    );
    assert.deepEqual(Buffer.from(await wmts.arrayBuffer()), current[1]);
    docker("restart", server);
    await ready();
    for (let i = 0; i < paths.length; i++) {
      const tile = await request(paths[i]);
      assert.equal(tile.headers.get("x-cache-tier"), "persistent");
      assert.deepEqual(Buffer.from(await tile.arrayBuffer()), current[i]);
    }
    previous = current;
    console.log(
      `PASS: live PostGIS WFS ${operation}, vector/map/group tiles, WMTS, and persistent restart`,
    );
  }
  const sql = (query) =>
    docker(
      "exec",
      database,
      "psql",
      "-v",
      "ON_ERROR_STOP=1",
      "-U",
      "postgres",
      "-d",
      "postgis",
      "-tAc",
      query,
    );
  const wfs = "/workspaces/writes/wfs";
  const tx = async (body, attrs = "", expected = 200) => {
    const response = await fetch(`${baseURL}${wfs}`, {
      method: "POST",
      headers: {
        Authorization: `Bearer ${token}`,
        "Content-Type": "application/xml",
      },
      body: `<wfs:Transaction service="WFS" version="2.0.0" xmlns:wfs="http://www.opengis.net/wfs/2.0" xmlns:fes="http://www.opengis.net/fes/2.0" xmlns:gml="http://www.opengis.net/gml/3.2" xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance" ${attrs}>${body}</wfs:Transaction>`,
      signal: AbortSignal.timeout(30_000),
    });
    const text = await response.text();
    assert.equal(response.status, expected, text);
    return text;
  };
  const featureFilter = (id) =>
    `<fes:Filter><fes:ResourceId rid="roads.${id}"/></fes:Filter>`;
  const update = (name, value, filter = "") =>
    `<wfs:Update typeName="roads"><wfs:Property><wfs:ValueReference>${name}</wfs:ValueReference>${value}</wfs:Property>${filter}</wfs:Update>`;
  await tx(
    update(
      "name",
      "<wfs:Value>  A &amp; B &lt;測試&gt;  </wfs:Value>",
      featureFilter(1),
    ),
  );
  assert.equal(
    sql("SELECT name = '  A & B <測試>  ' FROM public.cache_probe WHERE id=1"),
    "t",
  );
  await tx(update("name", "<wfs:Value/>", featureFilter(1)));
  assert.equal(sql("SELECT name = '' FROM public.cache_probe WHERE id=1"), "t");
  await tx(update("name", '<wfs:Value xsi:nil="true"/>', featureFilter(1)));
  assert.equal(
    sql("SELECT name IS NULL FROM public.cache_probe WHERE id=1"),
    "t",
  );
  await tx(
    update(
      "geom",
      '<wfs:Value><gml:Point srsName="urn:ogc:def:crs:OGC::CRS84"><gml:pos>10 20</gml:pos></gml:Point></wfs:Value>',
      featureFilter(1),
    ),
  );
  assert.equal(
    sql(
      "SELECT ST_AsText(geom)||':'||ST_SRID(geom) FROM public.cache_probe WHERE id=1",
    ),
    "POINT(10 20):4326",
  );
  await tx(
    update(
      "geom",
      '<wfs:Value><gml:Point srsName="http://www.opengis.net/def/crs/EPSG/0/4326"><gml:pos>20 10</gml:pos></gml:Point></wfs:Value>',
      featureFilter(1),
    ),
  );
  assert.equal(
    sql("SELECT ST_AsText(geom) FROM public.cache_probe WHERE id=1"),
    "POINT(10 20)",
  );
  await tx(update("geom", '<wfs:Value xsi:nil="true"/>', featureFilter(1)));
  assert.equal(
    sql("SELECT geom IS NULL FROM public.cache_probe WHERE id=1"),
    "t",
  );
  const projectedPoint =
    "<geom><gml:Point><gml:pos>1113194.9079327357 1118889.9748579594</gml:pos></gml:Point></geom>";
  await tx(
    `<wfs:Insert><roads><name>projected</name>${projectedPoint}</roads></wfs:Insert>`,
    'srsName="EPSG:3857"',
  );
  assert.equal(
    sql(
      "SELECT abs(ST_X(geom)-10)<0.000001 AND abs(ST_Y(geom)-10)<0.000001 AND ST_SRID(geom)=4326 FROM public.cache_probe WHERE name='projected'",
    ),
    "t",
  );
  await tx(
    `<wfs:Replace srsName="EPSG:3857"><roads><name>replaced</name>${projectedPoint}</roads>${featureFilter(1)}</wfs:Replace>`,
  );
  assert.equal(
    sql(
      "SELECT abs(ST_X(geom)-10)<0.000001 AND abs(ST_Y(geom)-10)<0.000001 FROM public.cache_probe WHERE id=1",
    ),
    "t",
  );
  await tx(
    `${update("name", "<wfs:Value>old</wfs:Value>")}<wfs:Insert><roads><name>new</name>${point("30 40")}</roads></wfs:Insert>`,
  );
  assert.equal(
    sql("SELECT count(*) FROM public.cache_probe WHERE name='new'"),
    "1",
    "XML order must not update a later insertion",
  );
  await tx(
    `${update("name", "<wfs:Value>must-rollback</wfs:Value>", featureFilter(1))}${update("geom", '<wfs:Value><gml:Point srsName="EPSG:99999"><gml:pos>1 2</gml:pos></gml:Point></wfs:Value>', featureFilter(1))}`,
    "",
    400,
  );
  assert.equal(
    sql("SELECT name FROM public.cache_probe WHERE id=1"),
    "old",
    "failed transaction partially committed",
  );
  sql(
    "INSERT INTO public.cache_probe(name,geom) SELECT 'bulk',ST_SetSRID(ST_Point(0,0),4326) FROM generate_series(1,10001)",
  );
  const last = sql("SELECT max(id) FROM public.cache_probe");
  const lock = await (
    await request(wfs, {
      method: "POST",
      headers: { "Content-Type": "application/xml" },
      body: `<wfs:LockFeature service="WFS" version="2.0.0" expiry="5" lockAction="ALL" xmlns:wfs="http://www.opengis.net/wfs/2.0" xmlns:fes="http://www.opengis.net/fes/2.0"><wfs:Query typeNames="roads">${featureFilter(last)}</wfs:Query></wfs:LockFeature>`,
    })
  ).text();
  const lockID = lock.match(/lockId="([^"]+)"/)?.[1];
  assert.ok(lockID, lock);
  const countBefore = sql("SELECT count(*) FROM public.cache_probe");
  await tx(update("name", "<wfs:Value>lock-bypass</wfs:Value>"), "", 400);
  await tx('<wfs:Delete typeName="roads"/>', "", 400);
  await tx(
    `<wfs:Replace><roads><name>lock-bypass</name>${point("1 2")}</roads>${featureFilter(last)}</wfs:Replace>`,
    "",
    400,
  );
  assert.equal(sql("SELECT count(*) FROM public.cache_probe"), countBefore);
  assert.equal(
    sql("SELECT count(*) FROM public.cache_probe WHERE name='lock-bypass'"),
    "0",
  );
  await tx(
    update("name", "<wfs:Value>authorized</wfs:Value>"),
    `lockId="${lockID}"`,
  );
  assert.equal(
    sql("SELECT count(*) FROM public.cache_probe WHERE name='authorized'"),
    countBefore,
  );
  console.log(
    "PASS: live WFS XML/scalar/geometry/CRS/order/rollback and >10,000-row lock integrity",
  );
  sql(
    "CREATE TABLE public.geometry_probe (id serial PRIMARY KEY, label text, amount integer, precise numeric(38,20), active boolean, shape geometry(Geometry,4326)); INSERT INTO public.geometry_probe(label,amount,active,shape) VALUES ('initial',1,false,ST_SetSRID(ST_Point(1,2),4326))",
  );
  await json(`${api}/services/postgis/layers`, "POST", {
    public_id: "shapes",
    source_layer: "public.geometry_probe",
    enabled: true,
    public: true,
  });
  const shapeFilter =
    '<fes:Filter><fes:ResourceId rid="shapes.1"/></fes:Filter>';
  const shapeUpdate = (properties) =>
    `<wfs:Update typeName="shapes">${properties}${shapeFilter}</wfs:Update>`;
  const property = (name, value) =>
    `<wfs:Property><wfs:ValueReference>${name}</wfs:ValueReference><wfs:Value>${value}</wfs:Value></wfs:Property>`;
  const geometryCases = [
    ["Point", "<gml:pos>10 20</gml:pos>", "POINT(10 20)"],
    [
      "LineString",
      "<gml:posList>0 0 10 20</gml:posList>",
      "LINESTRING(0 0,10 20)",
    ],
    [
      "Polygon",
      "<gml:exterior><gml:LinearRing><gml:posList>0 0 10 0 10 10 0 0</gml:posList></gml:LinearRing></gml:exterior>",
      "POLYGON((0 0,10 0,10 10,0 0))",
    ],
    [
      "MultiPoint",
      "<gml:pointMember><gml:Point><gml:pos>10 20</gml:pos></gml:Point></gml:pointMember>",
      "MULTIPOINT((10 20))",
    ],
    [
      "MultiCurve",
      "<gml:curveMember><gml:LineString><gml:posList>0 0 10 20</gml:posList></gml:LineString></gml:curveMember>",
      "MULTILINESTRING((0 0,10 20))",
    ],
    [
      "MultiSurface",
      "<gml:surfaceMember><gml:Polygon><gml:exterior><gml:LinearRing><gml:posList>0 0 10 0 10 10 0 0</gml:posList></gml:LinearRing></gml:exterior></gml:Polygon></gml:surfaceMember>",
      "MULTIPOLYGON(((0 0,10 0,10 10,0 0)))",
    ],
  ];
  for (const [type, coordinates, expected] of geometryCases) {
    await tx(
      shapeUpdate(
        property(
          "shape",
          `<gml:${type} srsName="EPSG:4326">${coordinates}</gml:${type}>`,
        ),
      ),
    );
    assert.equal(
      sql("SELECT ST_AsText(shape) FROM public.geometry_probe WHERE id=1"),
      expected,
      type,
    );
  }
  const exactDecimal = "12345678901234567.12345678901234567890";
  await tx(shapeUpdate(property("precise", exactDecimal)));
  assert.equal(
    sql("SELECT precise::text FROM public.geometry_probe WHERE id=1"),
    exactDecimal,
  );
  await tx(
    shapeUpdate(
      property("label", "  A &amp; B &lt;世界&gt;  ") +
        property("amount", "42") +
        property("active", "true"),
    ),
  );
  assert.equal(
    sql(
      "SELECT label='  A & B <世界>  ' AND amount=42 AND active FROM public.geometry_probe WHERE id=1",
    ),
    "t",
  );
  await tx(
    shapeUpdate(
      property("label", "rollback") + property("amount", "not-a-number"),
    ),
    "",
    400,
  );
  assert.equal(
    sql(
      "SELECT label='  A & B <世界>  ' FROM public.geometry_probe WHERE id=1",
    ),
    "t",
  );
  await tx(
    shapeUpdate(property("amount", "42")) +
      update(
        "geom",
        '<wfs:Value><gml:LineString srsName="EPSG:4326"><gml:posList>0 0 1 1</gml:posList></gml:LineString></wfs:Value>',
        featureFilter(1),
      ),
    "",
    400,
  );
  console.log(
    "PASS: live custom geometry columns, point/line/polygon/multi updates, typed XML values, and invalid-type rollback (one-connection pool)",
  );
  // Boundary regressions: namespace-equivalent filters, read-only publications,
  // stable IDs/source locks, and management contracts use a separate source.
  sql(
    "CREATE TABLE public.boundary_probe(id serial PRIMARY KEY,name text,geom geometry(Point,4326)); INSERT INTO public.boundary_probe(name,geom) VALUES('one',ST_SetSRID(ST_Point(1,2),4326)),('two',ST_SetSRID(ST_Point(3,4),4326));",
  );
  const layers = `${api}/services/postgis/layers`;
  const published = await json(layers, "POST", {
    public_id: "roads_alias",
    source_layer: "public.boundary_probe",
    description: "Original",
    dimensions: [{ name: "time", units: "ISO8601", source_property: "name" }],
  });
  const ridFilter = (name, id) =>
    `<fes:Filter><fes:ResourceId rid="${name}.${id}"/></fes:Filter>`;
  const edit = (name, value, id) =>
    `<wfs:Update typeName="${name}">${property("name", value)}${ridFilter(name, id)}</wfs:Update>`;
  const prefixResult = await tx(
    edit("roads_alias", "only-two", 2).replaceAll("fes:", "f:"),
    'xmlns:f="http://www.opengis.net/fes/2.0"',
  );
  assert.match(prefixResult, /<wfs:totalUpdated>1<\/wfs:totalUpdated>/);
  assert.equal(sql("SELECT name FROM public.boundary_probe WHERE id=1"), "one");
  for (const bad of [
    "<fes:Filter/>",
    '<x:Filter xmlns:x="urn:wrong"><x:ResourceId rid="roads_alias.2"/></x:Filter>',
    ridFilter("roads_alias", 1) + ridFilter("roads_alias", 2),
  ]) {
    await tx(
      `<wfs:Update typeName="roads_alias">${property("name", "must-not-write")}${bad}</wfs:Update>`,
      "",
      400,
    );
  }
  await json(layers, "POST", {
    public_id: "readonlyview",
    source_layer: "public.boundary_probe",
    sql_view: {
      sql: "SELECT id,name,geom FROM public.boundary_probe WHERE id=1",
      geometry_column: "geom",
      id_column: "id",
      srid: 4326,
    },
  });
  await tx(edit("readonlyview", "must-not-write", 2), "", 501);
  assert.equal(
    sql("SELECT name FROM public.boundary_probe WHERE id=2"),
    "only-two",
  );
  const lockFeature = async (name, id) => {
    const response = await request(wfs, {
      method: "POST",
      headers: { "Content-Type": "application/xml" },
      body: `<wfs:LockFeature service="WFS" version="2.0.0" expiry="60" lockAction="ALL" xmlns:wfs="http://www.opengis.net/wfs/2.0" xmlns:fes="http://www.opengis.net/fes/2.0"><wfs:Query typeNames="${name}">${ridFilter(name, id)}</wfs:Query></wfs:LockFeature>`,
    });
    const body = await response.text();
    const idMatch = body.match(/lockId="([^"]+)"/);
    assert.ok(idMatch, body);
    return idMatch[1];
  };
  const sourceLock = await lockFeature("roads_alias", 1);
  await json(layers, "POST", {
    public_id: "roadscopy",
    source_layer: "public.boundary_probe",
  });
  await tx(edit("roadscopy", "alias-bypass", 1), "", 400);
  await json(`${layers}/${published.id}`, "PUT", { public_id: "renamedroads" });
  await tx(edit("renamedroads", "rename-bypass", 1), "", 400);
  docker("restart", server);
  await ready();
  await tx(edit("renamedroads", "restart-bypass", 1), "", 400);
  await tx(edit("roadscopy", "authorized", 1), `lockId="${sourceLock}"`);
  for (const [kind, type, id] of [
    ["uuid", "uuid", "53357982-5ff0-4dfd-a351-931606645881"],
    ["bigint", "bigint", "9007199254740993"],
    ["text", "text", "text_id.part"],
  ]) {
    sql(
      `CREATE TABLE public.id_${kind}(id ${type} PRIMARY KEY,name text,geom geometry(Point,4326)); INSERT INTO public.id_${kind} VALUES('${id}','original',ST_SetSRID(ST_Point(1,2),4326));`,
    );
    const name = `id_${kind}`;
    await json(layers, "POST", {
      public_id: name,
      source_layer: `public.id_${kind}`,
    });
    const locked = await lockFeature(name, id);
    await tx(edit(name, "bypass", id), "", 400);
    assert.equal(sql(`SELECT name FROM public.id_${kind}`), "original");
    const response = await tx(
      edit(name, "authorized", id),
      `lockId="${locked}"`,
    );
    assert.ok(response.includes(`rid="${name}.${id}"`), response);
  }
  const cleared = await json(`${layers}/${published.id}`, "PUT", {
    description: "",
    dimensions: [],
  });
  assert.equal(cleared.description ?? "", "");
  assert.equal(cleared.dimensions?.length ?? 0, 0);
  for (const [body, status] of [
    [{ public_id: "roadscopy", source_layer: "public.boundary_probe" }, 409],
    [{ public_id: "missing", source_layer: "public.not_here" }, 422],
  ]) {
    const response = await fetch(baseURL + layers, {
      method: "POST",
      headers: {
        Authorization: `Bearer ${token}`,
        "Content-Type": "application/json",
      },
      body: JSON.stringify(body),
    });
    assert.equal(response.status, status, await response.text());
  }
  await json(`${api}/layer-groups`, "POST", {
    public_id: "boundary_group",
    members: [{ resource: "roadscopy" }],
  });
  await json(`${api}/tile-cache/stats?resource=boundary_group`);
  console.log(
    "PASS: alternative XML prefixes, read-only views, UUID/bigint/text IDs, alias/rename/restart locks, layer metadata/conflicts/source validation, group cache stats, and container default init",
  );
} catch (error) {
  try {
    console.error(docker("logs", "--tail", "20", server));
  } catch {
    /* startup may not have created it */
  }
  throw error;
} finally {
  for (const args of owned.reverse()) {
    try {
      docker(...args);
    } catch {
      console.error(`Could not clean disposable resource: ${args.at(-1)}`);
    }
  }
}
