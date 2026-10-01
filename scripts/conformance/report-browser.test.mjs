import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { createServer } from "node:http";
import { mkdtemp, mkdir, readFile, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import { createRequire } from "node:module";
import { fileURLToPath, pathToFileURL } from "node:url";
import { after, before, test } from "node:test";

const root = fileURLToPath(new URL("../../", import.meta.url));
const require = createRequire(path.join(root, "web/admin/package.json"));
const { chromium } = require("@playwright/test");
const AxeBuilder = require("@axe-core/playwright").default;
let temporary, site, server, browser, baseURL;

before(async () => {
  temporary = await mkdtemp(path.join(tmpdir(), "etsreport-browser-"));
  const evidence = path.join(temporary, "evidence");
  const profile = path.join(evidence, "conformance/wms13/core");
  await mkdir(profile, { recursive: true });
  const counts = { total: 2, passed: 1, failed: 0, skipped: 1 };
  const manifest = {
    schema_version: 2,
    suites: { wms13: { image: "test-image@sha256:abc" } },
    profiles: { "wms13/core": { suite: "wms13", evidence_kind: "official" } },
    stac_validation: {
      selected_with: "ogcapi-features10",
      tools: {
        "stac-api-validator": "0.6.8",
        "stac-validator": "4.1.2",
        "pystac-client": "0.9.0",
      },
    },
  };
  manifest.suites["ogcapi-features10"] = { image: "features-image" };
  manifest.profiles["ogcapi-features10/stac"] = {
    suite: "ogcapi-features10",
    evidence_kind: "official",
  };
  await writeFile(
    path.join(temporary, "manifest.json"),
    JSON.stringify(manifest),
  );
  await writeFile(
    path.join(profile, "metadata.json"),
    JSON.stringify({
      suite: "wms13-core",
      evidence_kind: "official",
      image: manifest.suites.wms13.image,
      neoserver_commit: "test-commit",
      started_at: "2026-09-19T10:00:00Z",
      completed_at: "2026-09-19T10:00:05Z",
      result: {
        ...counts,
        format: "ctl",
        leaf: counts,
        skip_categories: { unclassified: 1 },
      },
    }),
  );
  await writeFile(
    path.join(profile, "junit.xml"),
    '<testsuite tests="2" failures="0" skipped="1"><testcase name="time-default" classname="assertion.wms"/><testcase name="optional-dimension" classname="assertion.wms"><skipped type="unclassified" message="&lt;script&gt;window.injected=true&lt;/script&gt;"/></testcase></testsuite>',
  );
  await writeFile(path.join(profile, "result.xml"), "<execution/>");
  const inherited = path.join(evidence, "conformance/ogcapi-features10/stac");
  await mkdir(inherited, { recursive: true });
  const metadata = JSON.parse(
    await readFile(path.join(profile, "metadata.json"), "utf8"),
  );
  Object.assign(metadata, {
    suite: "ogcapi-features10-stac",
    image: "features-image",
    neoserver_image_id: "sha256:server",
  });
  await writeFile(
    path.join(inherited, "metadata.json"),
    JSON.stringify(metadata),
  );
  for (const file of ["junit.xml", "result.xml"]) {
    await writeFile(
      path.join(inherited, file),
      await readFile(path.join(profile, file)),
    );
  }
  await writeFile(
    path.join(inherited, "coverage-check.json"),
    JSON.stringify({
      passed: true,
      assertions: 2,
      skipped_executions: 1,
      errors: [],
    }),
  );
  const stac = path.join(evidence, "conformance/stac");
  await mkdir(stac, { recursive: true });
  const checks = {};
  for (const [tool, version] of Object.entries(
    manifest.stac_validation.tools,
  )) {
    checks[tool] = {
      version,
      exit_code: 0,
      commands: [[tool]],
      log: `${tool}.log`,
    };
    const log =
      tool === "stac-api-validator"
        ? "Warnings:\n- [Collections] stac-check recommendations: <script>window.injected=true</script>\n- [Item Search] to validate ids does not override all other parameters returned 0 results\nErrors: none\n"
        : "Validation passed.\n";
    await writeFile(path.join(stac, `${tool}.log`), log);
  }
  await writeFile(
    path.join(stac, "results.json"),
    JSON.stringify({
      schema_version: 1,
      root: "http://server/workspaces/demo/stac",
      neoserver_commit: "test-commit",
      neoserver_image_id: "sha256:server",
      started_at: metadata.started_at,
      completed_at: metadata.completed_at,
      checks,
    }),
  );
  const output = path.join(temporary, "report");
  const result = spawnSync(
    "go",
    [
      "run",
      "./testing/officialets/cmd/etsreport",
      "--input",
      evidence,
      "--manifest",
      path.join(temporary, "manifest.json"),
      "--output",
      output,
    ],
    {
      cwd: root,
      encoding: "utf8",
      env: {
        ...process.env,
        GITHUB_SHA: "",
        ETS_REPORT_SELECTED: '["wms13","ogcapi-features10"]',
        ETS_REPORT_JOBS: "",
        GITHUB_EVENT_PATH: "",
      },
    },
  );
  assert.equal(result.status, 0, result.stderr);
  site = path.join(output, "site");
  server = createServer(async (req, res) => {
    try {
      let filename = decodeURIComponent(
        new URL(req.url, "http://localhost").pathname,
      );
      if (filename.endsWith("/")) filename += "index.html";
      const resolved = path.resolve(site, "." + filename);
      if (!resolved.startsWith(site + path.sep))
        throw new Error("Invalid path");
      const mime =
        {
          ".css": "text/css",
          ".js": "application/javascript",
          ".html": "text/html",
          ".zip": "application/zip",
        }[path.extname(resolved)] || "application/octet-stream";
      res.setHeader("Content-Type", mime);
      res.end(await readFile(resolved));
    } catch {
      res.statusCode = 404;
      res.end("Not found");
    }
  });
  await new Promise((resolve, reject) => {
    server.once("error", reject);
    server.listen(0, "127.0.0.1", resolve);
  });
  baseURL = `http://127.0.0.1:${server.address().port}`;
  browser = await chromium.launch({ headless: true });
});

after(async () => {
  await browser?.close();
  if (server) await new Promise((resolve) => server.close(resolve));
  if (temporary) await rm(temporary, { recursive: true, force: true });
});

test("overview, filtering, details, permalink and evidence download", async () => {
  const page = await browser.newPage();
  await page.goto(baseURL);
  assert.equal(await page.title(), "Conformance report — neoserver");
  await page
    .getByLabel("Find a protocol or profile")
    .fill("no matching protocol");
  assert.equal(await page.locator("[data-result]:visible").count(), 0);
  assert.equal(await page.getByRole("status").textContent(), "0 of 5 shown");
  await page.getByLabel("Find a protocol or profile").fill("WMS");
  await page.getByRole("link", { name: "WMS 1.3", exact: true }).click();
  await page.getByLabel("Status", { exact: true }).selectOption("Skipped");
  await page
    .getByLabel("Skip category", { exact: true })
    .selectOption("unclassified");
  assert.equal(await page.locator("[data-result]:visible").count(), 1);
  await page.locator("#case-2 summary").click();
  assert.ok(await page.locator("#case-2 pre").isVisible());
  assert.equal(await page.evaluate(() => window.injected), undefined);
  const download = page.waitForEvent("download");
  await page
    .getByRole("link", { name: /Download Stock official suite evidence/ })
    .click();
  assert.equal((await download).suggestedFilename(), "official-ets-wms13.zip");
  await page.goto(`${baseURL}/profiles/wms13/core/index.html#case-1`);
  assert.equal(await page.locator("#case-1").getAttribute("open"), "");
  await page.close();
});

test("keyboard access, mobile layout, light and dark accessibility", async () => {
  for (const colorScheme of ["light", "dark"]) {
    const context = await browser.newContext({
      viewport: { width: 390, height: 844 },
      colorScheme,
    });
    const page = await context.newPage();
    for (const url of [
      "/",
      "/profiles/wms13/core/index.html",
      "/profiles/stac/api/index.html",
    ]) {
      await page.goto(`${baseURL}${url}`);
      await page.keyboard.press("Tab");
      assert.equal(
        await page.evaluate(() => document.activeElement.textContent),
        "Skip to results",
      );
      const overflow = await page.evaluate(
        () => document.documentElement.scrollWidth > innerWidth,
      );
      assert.equal(
        overflow,
        false,
        "page must not overflow the mobile viewport",
      );
      const results = await new AxeBuilder({ page }).analyze();
      assert.deepEqual(
        results.violations.map(({ id, description }) => ({ id, description })),
        [],
      );
    }
    await context.close();
  }
});

test("STAC scope, warning details and separate evidence download", async () => {
  const page = await browser.newPage();
  await page.goto(baseURL);
  assert.ok(
    await page
      .getByRole("heading", { name: "STAC validation", exact: true })
      .isVisible(),
  );
  await page
    .getByLabel("Status", { exact: true })
    .selectOption("Passed with warnings");
  assert.equal(await page.locator("[data-result]:visible").count(), 1);
  await page.getByRole("link", { name: "STAC API 1.0", exact: true }).click();
  assert.ok(
    await page
      .getByText("Test coverage limitation", { exact: true })
      .isVisible(),
  );
  assert.ok(
    await page
      .getByText("Metadata recommendation", { exact: true })
      .isVisible(),
  );
  assert.equal(await page.evaluate(() => window.injected), undefined);
  const download = page.waitForEvent("download");
  await page
    .getByRole("link", { name: "Download STAC validation evidence (.zip)" })
    .click();
  assert.equal((await download).suggestedFilename(), "stac-validation.zip");
  await page.close();
});

test("downloaded report works with JavaScript disabled", async () => {
  const page = await browser.newPage({ javaScriptEnabled: false });
  await page.goto(pathToFileURL(path.join(site, "index.html")).href);
  await page.getByRole("link", { name: "WMS 1.3", exact: true }).click();
  assert.equal(await page.locator("[data-result]").count(), 2);
  assert.equal(await page.locator("[data-filters]").isVisible(), false);
  await page.locator("#case-2 summary").click();
  assert.ok(await page.locator("#case-2 pre").isVisible());
  await page.goto(
    pathToFileURL(path.join(site, "profiles/stac/api/index.html")).href,
  );
  assert.ok(
    await page
      .getByText("Test coverage limitation", { exact: true })
      .isVisible(),
  );
  assert.ok(await page.locator("#warning-0 pre").isVisible());
  await page.close();
});
