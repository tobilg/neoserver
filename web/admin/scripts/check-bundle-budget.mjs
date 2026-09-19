import { readFile } from "node:fs/promises";
import { gzipSync } from "node:zlib";
import { initialJavaScript } from "./bundle-entries.mjs";

const dist = new URL("../../../internal/admin/dist/", import.meta.url);
const manifest = JSON.parse(
  await readFile(new URL(".vite/manifest.json", dist), "utf8"),
);
let bytes = 0;
for (const file of initialJavaScript(manifest)) {
  bytes += gzipSync(await readFile(new URL(file, dist))).byteLength;
}
const budget = 250 * 1024;
console.log(
  `Initial JavaScript: ${Math.round(bytes / 1024)} KiB gzipped (budget 250 KiB)`,
);
if (bytes > budget) process.exit(1);
