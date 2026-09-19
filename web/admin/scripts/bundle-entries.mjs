/** Count actual initial dependencies; never infer lazy loading from filenames. */
export function initialJavaScript(manifest) {
  const files = new Set();
  const visited = new Set();
  function visit(key) {
    if (visited.has(key)) return;
    visited.add(key);
    const chunk = manifest[key];
    if (!chunk) throw new Error("Missing manifest dependency: " + key);
    if (chunk.file.endsWith(".js")) files.add(chunk.file);
    for (const dependency of chunk.imports ?? []) visit(dependency);
    // dynamicImports are downloaded only when their routes are opened.
  }
  const entries = Object.keys(manifest).filter((key) => manifest[key].isEntry);
  if (!entries.length) throw new Error("Build manifest has no entry point");
  entries.forEach(visit);
  return [...files];
}
