import { readdir, readFile } from "node:fs/promises";
import path from "node:path";

const root = new URL("../", import.meta.url);
const manifest = JSON.parse(
  await readFile(new URL("components-manifest.json", root), "utf8"),
);
const directory = new URL("src/components/ui/", root);
const files = (await readdir(directory))
  .filter((file) => file.endsWith(".tsx"))
  .map((file) => path.posix.join("src/components/ui", file))
  .sort();
const declared = [...manifest.files].sort();
if (JSON.stringify(files) !== JSON.stringify(declared)) {
  console.error("shadcn provenance mismatch", { files, declared });
  process.exit(1);
}
console.log(`Verified ${files.length} CLI-managed shadcn files.`);
