import assert from "node:assert/strict";
import { test } from "node:test";
import { readFileSync } from "node:fs";
import { qualification } from "./release-evidence.mjs";
const valid = () => ({
  sha: "a".repeat(40),
  checkout: "a".repeat(40),
  version: "v1.0.0-rc.1",
  needs: Object.fromEntries(
    ["ci", "security", "conformance"].map((name) => [
      name,
      { result: "success" },
    ]),
  ),
  image: { Id: `sha256:${"b".repeat(64)}`, Os: "linux", Architecture: "amd64" },
  runURL: "https://github.com/example/repo/actions/runs/1",
  artifacts: {},
});
test("qualified source and exact image produce a manifest", () =>
  assert.equal(qualification(valid()).source_sha, "a".repeat(40)));
test("every missing, failed, skipped or cancelled gate blocks export", () => {
  for (const gate of ["ci", "security", "conformance"])
    for (const status of [undefined, "failure", "cancelled", "skipped"]) {
      const input = valid();
      input.needs[gate] = { result: status };
      assert.throws(() => qualification(input));
    }
});
test("stale SHA and unqualified platforms are rejected", () => {
  const input = valid();
  input.checkout = "c".repeat(40);
  assert.throws(() => qualification(input));
  const other = valid();
  other.image.Architecture = "arm64";
  assert.throws(() => qualification(other));
});
test("release export is structurally gated and tests its stamped image", () => {
  const workflow = readFileSync(
    new URL("../.github/workflows/release.yml", import.meta.url),
    "utf8",
  );
  assert.match(workflow, /needs: \[ci, security, conformance\]/);
  // Failure evidence may upload unconditionally; candidate export may not.
  const evidenceStep = workflow.match(
    /      - name: Retain image-security evidence[^\n]*\n[\s\S]*?(?=      - name:)/,
  )?.[0];
  assert.ok(evidenceStep, "image scan evidence must survive a failed gate");
  assert.match(evidenceStep, /if: always\(\)/);
  assert.match(evidenceStep, /uses: actions\/upload-artifact@/);
  assert.match(evidenceStep, /path: image-security\.json/);
  assert.doesNotMatch(evidenceStep, /run:|docker save/);
  assert.doesNotMatch(
    workflow.replace(evidenceStep, ""),
    /continue-on-error: true|if: always\(\)/,
  );
  assert.match(workflow, /docker build --pull --no-cache/);
  assert.match(
    workflow,
    /image --exit-code 1 --severity HIGH,CRITICAL --format json neoserver:candidate > image-security\.json/,
  );
  assert.doesNotMatch(workflow, /--ignore-unfixed|--exit-code 0/);
  for (const gate of ["ci", "security", "conformance"])
    assert.ok(workflow.includes(`uses: ./.github/workflows/${gate}.yml`));
  assert.ok(
    workflow.indexOf("test-wfs-cache-container.mjs") <
      workflow.indexOf("docker save"),
  );
  assert.ok(
    workflow.indexOf("CONSOLE_TEST_IMAGE: neoserver:candidate") <
      workflow.indexOf("docker save"),
  );
});
