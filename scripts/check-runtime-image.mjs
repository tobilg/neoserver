// Gate the uncompressed image size and, when supplied, scanner package coverage.
// node scripts/check-runtime-image.mjs IMAGE [SBOM] [evidence directory]
import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { readFileSync, writeFileSync, mkdirSync } from "node:fs";
import path from "node:path";

const image = process.argv[2] ?? "neoserver:candidate";
const sbomFile = process.argv[3];
const output = process.argv[4] ?? "test-results/runtime-container";
const docker = (...args) =>
  execFileSync("docker", args, {
    encoding: "utf8",
    maxBuffer: 32 * 1024 * 1024,
  }).trim();
const info = JSON.parse(docker("image", "inspect", image))[0];
const budget = JSON.parse(
  readFileSync("scripts/container/image-budget.json", "utf8"),
);
assert.equal(info.Os, "linux");
assert.equal(info.Architecture, "amd64");
assert.ok(
  Number.isSafeInteger(budget.max_uncompressed_bytes) &&
    budget.max_uncompressed_bytes > 0,
);
assert.ok(
  info.Size <= budget.max_uncompressed_bytes,
  `${info.Size} image bytes exceed ${budget.max_uncompressed_bytes} budget`,
);
const cat = (file) =>
  docker(
    "run",
    "--rm",
    "--network",
    "none",
    "--entrypoint",
    "cat",
    image,
    `/usr/share/neoserver/${file}`,
  );
const libraries = JSON.parse(cat("runtime-libraries.json"));
const explicit = cat("runtime-packages.txt").split("\n");
const packages = docker(
  "run",
  "--rm",
  "--network",
  "none",
  "--entrypoint",
  "dpkg-query",
  image,
  "-W",
  "-f=${Package}\t${Version}\t${db:Status-Status}\n",
)
  .split("\n")
  .map((line) => line.split("\t"))
  .filter(([, , status]) => status === "installed")
  .map(([name, version]) => ({ name, version }));
for (const name of explicit)
  assert.ok(
    packages.some((p) => p.name === name.replace(/:amd64$/, "")),
    `missing closure package ${name}`,
  );
if (sbomFile) {
  const sbom = JSON.parse(readFileSync(sbomFile, "utf8"));
  for (const pkg of packages)
    assert.ok(
      sbom.components?.some(
        (c) => c.name === pkg.name && c.version === pkg.version,
      ),
      `SBOM missed installed package ${pkg.name}@${pkg.version}`,
    );
}
const result = {
  image_id: info.Id,
  platform: "linux/amd64",
  uncompressed_bytes: info.Size,
  budget,
  packages,
  libraries,
  sbom_packages_verified: Boolean(sbomFile),
};
mkdirSync(output, { recursive: true });
writeFileSync(
  path.join(output, "inventory.json"),
  JSON.stringify(result, null, 2) + "\n",
);
console.log(
  `PASS: ${info.Size} bytes / ${budget.max_uncompressed_bytes} budget; ${packages.length} installed packages${sbomFile ? " verified in SBOM" : ""}`,
);
