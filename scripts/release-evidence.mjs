import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { execFileSync } from "node:child_process";
import { createReadStream, writeFileSync } from "node:fs";
import { pathToFileURL } from "node:url";

export function qualification({
  sha,
  checkout,
  version,
  needs,
  image,
  runURL,
  artifacts,
}) {
  assert.match(sha, /^[0-9a-f]{40}$/);
  assert.equal(
    checkout,
    sha,
    "qualification must match the checked-out source SHA",
  );
  assert.match(version, /^v\d+\.\d+\.\d+(-[A-Za-z0-9.-]+)?$/);
  for (const name of ["ci", "security", "conformance"])
    assert.equal(
      needs[name]?.result,
      "success",
      `${name} qualification must succeed`,
    );
  assert.equal(image.Os, "linux");
  assert.equal(image.Architecture, "amd64");
  assert.match(image.Id, /^sha256:[0-9a-f]{64}$/);
  return {
    schema_version: 1,
    source_sha: sha,
    version,
    platform: "linux/amd64",
    image_id: image.Id,
    workflow_run: runURL,
    source_qualification: needs,
    exact_image_checks: [
      "non-root-release-smoke",
      "wfs-integrity-and-persistent-cache",
      "live-console-oidc",
      "trivy-high-critical",
      "cyclonedx-sbom",
      "released-schema-and-format-parity",
      "offline-init-and-spatial-queries",
      "datum-grids-mounted-network-derived",
      "image-size-budget",
      "sbom-package-coverage",
    ],
    artifacts,
  };
}

async function digest(path) {
  const hash = createHash("sha256");
  for await (const chunk of createReadStream(path)) hash.update(chunk);
  return hash.digest("hex");
}

if (
  process.argv[1] &&
  import.meta.url === pathToFileURL(process.argv[1]).href
) {
  const run = (...args) =>
    execFileSync(args[0], args.slice(1), { encoding: "utf8" }).trim();
  const artifacts = {};
  for (const path of [
    "neoserver-linux-amd64.tar.gz",
    "sbom.cdx.json",
    "image-copyright.txt",
    "runtime-evidence.tar.gz",
  ])
    artifacts[path] = await digest(path);
  const evidence = qualification({
    sha: process.env.GITHUB_SHA,
    checkout: run("git", "rev-parse", "HEAD"),
    version: process.env.VERSION,
    needs: JSON.parse(process.env.QUALIFICATIONS),
    image: JSON.parse(
      run("docker", "image", "inspect", "neoserver:candidate"),
    )[0],
    runURL: `${process.env.GITHUB_SERVER_URL}/${process.env.GITHUB_REPOSITORY}/actions/runs/${process.env.GITHUB_RUN_ID}`,
    artifacts,
  });
  writeFileSync("qualification.json", `${JSON.stringify(evidence, null, 2)}\n`);
}
