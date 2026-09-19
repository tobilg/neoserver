import { test } from "node:test";
import assert from "node:assert/strict";
import { mkdtemp, mkdir, writeFile, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { pathToFileURL } from "node:url";
import { analyzeBundle } from "./analyze-bundle.mjs";

test("reports actual entry dependencies separately from lazy chunks", async () => {
  const directory = await mkdtemp(join(tmpdir(), "neoserver-bundle-test-"));
  try {
    await mkdir(join(directory, ".vite"));
    await writeFile(
      join(directory, ".vite/manifest.json"),
      JSON.stringify({
        main: {
          file: "main.js",
          isEntry: true,
          imports: ["shared"],
          dynamicImports: ["map"],
        },
        shared: { file: "shared.js" },
        map: { file: "map.js" },
      }),
    );
    for (const file of ["main.js", "shared.js", "map.js"])
      await writeFile(join(directory, file), "export default 1;");
    const report = await analyzeBundle(pathToFileURL(directory + "/"));
    assert.equal(
      report.chunks.find((chunk) => chunk.file === "map.js").loading,
      "lazy",
    );
    assert.equal(
      report.initialGzip,
      report.chunks
        .filter((chunk) => chunk.loading === "initial")
        .reduce((sum, chunk) => sum + chunk.gzip, 0),
    );
  } finally {
    await rm(directory, { recursive: true, force: true });
  }
});
