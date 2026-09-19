// Verify all documented grid installation methods using disposable resources.
import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { mkdirSync, writeFileSync, mkdtempSync, rmSync } from "node:fs";
import os from "node:os";
import path from "node:path";

const image = process.argv[2] ?? "neoserver:candidate";
const output = path.resolve(
  process.argv[3] ?? "test-results/runtime-container",
);
mkdirSync(output, { recursive: true });
const docker = (...args) =>
  execFileSync("docker", args, {
    encoding: "utf8",
    timeout: 180_000,
    maxBuffer: 8 * 1024 * 1024,
  }).trim();
const volume = `neoserver-grids-${process.pid}`;
const derived = `neoserver:grids-${process.pid}`;
const scratch = mkdtempSync(path.join(os.tmpdir(), "neoserver-grids-"));
const options = [
  "-s",
  "EPSG:4267",
  "-t",
  "EPSG:4269",
  "--bbox",
  "-100,30,-99,31",
  "--spatial-test",
  "intersects",
  "--hide-ballpark",
  "-o",
  "PROJ",
];
const projinfo = (extra = [], target = image, gridCheck = "discard_missing") =>
  docker(
    "run",
    "--rm",
    "--platform",
    "linux/amd64",
    ...extra,
    "--entrypoint",
    "projinfo",
    target,
    ...options,
    ...(gridCheck ? ["--grid-check", gridCheck] : []),
  );
try {
  const absent = projinfo(["--network", "none"]);
  assert.doesNotMatch(
    absent,
    /\+grids=/,
    "base image unexpectedly contains a datum grid",
  );
  docker("volume", "create", volume);
  // Match a directory prepared by an operator, owned by the runtime user.
  docker(
    "run",
    "--rm",
    "--user",
    "0:0",
    "-v",
    `${volume}:/proj-grids`,
    "--entrypoint",
    "chown",
    image,
    "65532:65532",
    "/proj-grids",
  );
  docker(
    "run",
    "--rm",
    "-v",
    `${volume}:/proj-grids`,
    "--entrypoint",
    "projsync",
    image,
    "--file",
    "us_noaa_conus.tif",
    "--target-dir",
    "/proj-grids",
  );
  const mounted = projinfo([
    "--network",
    "none",
    "-v",
    `${volume}:/proj-grids:ro`,
    "-e",
    "PROJ_DATA=/usr/local/gdal-internal/share/proj:/proj-grids",
  ]);
  assert.match(mounted, /\+grids=us_noaa_conus\.tif/);
  // No local grids or warm cache: PROJ must recognize remotely available grids.
  const network = projinfo(
    [
      "-e",
      "PROJ_NETWORK=ON",
      "-e",
      "PROJ_USER_WRITABLE_DIRECTORY=/data/proj-cache",
    ],
    image,
    null,
  );
  assert.match(network, /\+grids=us_noaa_/);
  const holder = docker("create", "-v", `${volume}:/proj-grids:ro`, image);
  try {
    docker(
      "cp",
      `${holder}:/proj-grids/us_noaa_conus.tif`,
      path.join(scratch, "us_noaa_conus.tif"),
    );
  } finally {
    docker("rm", holder);
  }
  writeFileSync(
    path.join(scratch, "Dockerfile"),
    `FROM ${image}\nCOPY us_noaa_conus.tif /usr/local/gdal-internal/share/proj/\n`,
  );
  docker("build", "--platform", "linux/amd64", "-t", derived, scratch);
  const baked = projinfo(["--network", "none"], derived);
  assert.match(baked, /\+grids=us_noaa_conus\.tif/);
  writeFileSync(
    path.join(output, "proj-grids.json"),
    JSON.stringify(
      {
        image,
        image_id: docker("image", "inspect", "--format", "{{.Id}}", image),
        absent,
        mounted,
        network,
        derived: baked,
      },
      null,
      2,
    ) + "\n",
  );
  console.log(
    "PASS: no default grids; mounted, network, and derived-image grid operations",
  );
} finally {
  try {
    docker("image", "rm", derived);
  } catch {}
  try {
    docker("volume", "rm", volume);
  } catch {}
  rmSync(scratch, { recursive: true, force: true });
}
