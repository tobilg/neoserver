import { readFile } from "node:fs/promises";
import { gzipSync } from "node:zlib";
import { pathToFileURL } from "node:url";
import { initialJavaScript } from "./bundle-entries.mjs";

export async function analyzeBundle(dist) {
  const manifest = JSON.parse(
    await readFile(new URL(".vite/manifest.json", dist), "utf8"),
  );
  const initial = new Set(initialJavaScript(manifest));
  const files = new Set(
    Object.values(manifest)
      .map((entry) => entry.file)
      .filter((file) => file.endsWith(".js")),
  );
  const chunks = await Promise.all(
    [...files].map(async (file) => {
      const bytes = await readFile(new URL(file, dist));
      return {
        file,
        loading: initial.has(file) ? "initial" : "lazy",
        bytes: bytes.length,
        gzip: gzipSync(bytes).length,
      };
    }),
  );
  chunks.sort((a, b) => b.gzip - a.gzip);
  return {
    initialGzip: chunks
      .filter((chunk) => chunk.loading === "initial")
      .reduce((sum, chunk) => sum + chunk.gzip, 0),
    chunks,
  };
}

if (
  process.argv[1] &&
  import.meta.url === pathToFileURL(process.argv[1]).href
) {
  const report = await analyzeBundle(
    new URL("../../../internal/admin/dist/", import.meta.url),
  );
  if (process.argv.includes("--json"))
    console.log(JSON.stringify(report, null, 2));
  else {
    console.log(
      `Initial JavaScript: ${(report.initialGzip / 1024).toFixed(1)} KiB gzip`,
    );
    console.table(
      report.chunks.map((chunk) => ({
        chunk: chunk.file,
        loading: chunk.loading,
        "raw KiB": (chunk.bytes / 1024).toFixed(1),
        "gzip KiB": (chunk.gzip / 1024).toFixed(1),
      })),
    );
    console.log(
      "Initial/lazy classification follows the Vite manifest dependency graph. Workers and static assets are not part of the initial JavaScript total.",
    );
  }
}
