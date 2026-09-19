import { copyFile, mkdir } from "node:fs/promises";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

/**
 * Copies Swagger UI's runtime assets into the console build output so the
 * management API docs at /api/v1/api.html can load them same-origin.
 *
 * The server sets a restrictive Content-Security-Policy (`script-src 'self'`),
 * so the docs page cannot pull these from a CDN. Serving them from the embedded
 * console keeps the page working without loosening that policy.
 *
 * Runs after `vite build`, because Vite empties the output directory.
 */

const here = dirname(fileURLToPath(import.meta.url));
const root = join(here, "..");
const source = join(root, "node_modules", "swagger-ui-dist");
const target = join(
  root,
  "..",
  "..",
  "internal",
  "admin",
  "dist",
  "vendor",
  "swagger",
);

const assets = ["swagger-ui-bundle.js", "swagger-ui.css"];

await mkdir(target, { recursive: true });
for (const asset of assets) {
  await copyFile(join(source, asset), join(target, asset));
}

console.log(`Copied ${assets.length} Swagger UI assets to ${target}`);
