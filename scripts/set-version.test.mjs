import assert from "node:assert/strict";
import {
  mkdtempSync,
  mkdirSync,
  readFileSync,
  rmSync,
  writeFileSync,
} from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import { spawnSync } from "node:child_process";
import { fileURLToPath } from "node:url";
import test from "node:test";

const script = fileURLToPath(new URL("./set-version.mjs", import.meta.url));
const publishedNotes =
  "## 0.1.0 — 19 September 2026\n\ndocker pull tobilg/neoserver:0.1.0\n";

function fixture(t) {
  const root = mkdtempSync(join(tmpdir(), "neoserver-version-"));
  t.after(() => rmSync(root, { recursive: true, force: true }));
  const files = {
    "internal/conf/appconfig.go": 'var setVersion = "0.1.2"\n',
    Dockerfile: "ARG VERSION=0.1.2\nFROM example\nARG VERSION=0.1.2\n",
    "web/admin/package.json":
      '{"name": "neoserver-admin", "version": "0.1.2"}\n',
    "web/admin/package-lock.json":
      JSON.stringify(
        {
          name: "neoserver-admin",
          version: "0.1.2",
          packages: {
            "": { name: "neoserver-admin", version: "0.1.2" },
            dependency: { version: "9.8.7" },
          },
        },
        null,
        2,
      ) + "\n",
    "web/admin/openapi.json": '{"info":{"version":"0.1.2"}}\n',
    ".github/workflows/ci.yml":
      "run: ./scripts/test-release-container.sh neoserver:candidate 0.1.2\n",
    "scripts/test-release-container.sh": "version=${2:-0.1.2}\n",
    "README.md":
      "# neoserver\nCurrent version: **0.1.1**.\ndocker pull tobilg/neoserver:0.1.1\nBaseline measured at 0.1.0; WMS 1.3.0.\n",
    "docs/deployment.md":
      "FROM tobilg/neoserver:0.1.1\ndocker run tobilg/neoserver:0.1.1\nIn neoserver 0.1.1, catalogs from 0.1.0 upgrade.\n",
    "docs/releasing.md":
      "`make release-build VERSION=v0.1.0`\nmake set-version VERSION=v0.1.0\nBaseline: 0.1.0.\n",
    "docs/release-notes.md":
      "# Release notes\n\n## 0.1.1 — unreleased\n\nNew features.\n\n" +
      publishedNotes,
  };
  const write = (file, text) => {
    mkdirSync(dirname(join(root, file)), { recursive: true });
    writeFileSync(join(root, file), text);
  };
  for (const [file, text] of Object.entries(files)) write(file, text);
  return {
    read: (file) => readFileSync(join(root, file), "utf8"),
    write,
    run: (argument) =>
      spawnSync(process.execPath, [script, argument], {
        cwd: root,
        encoding: "utf8",
      }),
  };
}

function success(result) {
  assert.equal(result.status, 0, result.stdout + result.stderr);
}

test("rerunning an already applied version repairs stale docs and checks their drift", (t) => {
  const f = fixture(t);
  assert.equal(f.run("--check").status, 1);
  success(f.run("v0.1.2"));
  success(f.run("--check"));
  assert.match(f.read("README.md"), /Current version: \*\*0\.1\.2\*\*/);
  assert.match(f.read("README.md"), /tobilg\/neoserver:0\.1\.2/);
  assert.match(f.read("docs/deployment.md"), /In neoserver 0\.1\.2/);
  assert.equal(
    (f.read("docs/deployment.md").match(/tobilg\/neoserver:0\.1\.2/g) ?? [])
      .length,
    2,
  );
  assert.equal(
    (f.read("docs/releasing.md").match(/VERSION=v0\.1\.2/g) ?? []).length,
    2,
  );
  assert.match(f.read("docs/release-notes.md"), /^## 0\.1\.2 — unreleased$/m);
  assert.ok(f.read("docs/release-notes.md").endsWith(publishedNotes));
  assert.match(
    f.read("README.md"),
    /Baseline measured at 0\.1\.0; WMS 1\.3\.0/,
  );
  const repeated = f.run("0.1.2");
  success(repeated);
  assert.doesNotMatch(repeated.stdout, /  updated /);
  f.write(
    "docs/deployment.md",
    f
      .read("docs/deployment.md")
      .replace("In neoserver 0.1.2", "In neoserver 0.1.1"),
  );
  assert.equal(f.run("--check").status, 1);
});

test("prerelease bumps preserve dependencies and still require regenerated OpenAPI", (t) => {
  const f = fixture(t);
  success(f.run("v0.2.0-rc.1"));
  assert.match(f.read("README.md"), /tobilg\/neoserver:0\.2\.0-rc\.1/);
  assert.match(
    f.read("docs/release-notes.md"),
    /^## 0\.2\.0-rc\.1 — unreleased$/m,
  );
  assert.match(f.read("web/admin/package-lock.json"), /"version": "9\.8\.7"/);
  const check = f.run("--check");
  assert.equal(check.status, 1);
  assert.match(check.stderr, /make ui-openapi/);
  f.write("web/admin/openapi.json", '{"info":{"version":"0.2.0-rc.1"}}');
  success(f.run("--check"));
});

test("a new version adds notes ahead of published history and keeps dated current notes", (t) => {
  const f = fixture(t);
  f.write("docs/release-notes.md", "# Release notes\n\n" + publishedNotes);
  success(f.run("0.1.2"));
  assert.equal(
    f.read("docs/release-notes.md"),
    "# Release notes\n\n## 0.1.2 — unreleased\n\n" + publishedNotes,
  );
  const dated = f
    .read("docs/release-notes.md")
    .replace("0.1.2 — unreleased", "0.1.2 — 20 September 2026");
  f.write("docs/release-notes.md", dated);
  success(f.run("0.1.2"));
  success(f.run("--check"));
  assert.equal(f.read("docs/release-notes.md"), dated);
});

test("a missing documentation target fails before any version is changed", (t) => {
  const f = fixture(t);
  f.write("README.md", "# neoserver\n");
  const result = f.run("0.2.0");
  assert.equal(result.status, 1);
  assert.match(result.stderr, /README\.md.*cannot find current version/);
  assert.match(f.read("internal/conf/appconfig.go"), /"0\.1\.2"/);
  assert.equal(f.run("--check").status, 1);
});
