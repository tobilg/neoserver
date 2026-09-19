/**
 * Suggests a public coverage ID from the store name, so "raster"/"mosaic"
 * source names from different stores don't collide.
 */
export function defaultCoverageID(
  store: string,
  source: string,
  sourcesInStore: number,
) {
  const clean = (value: string) =>
    value
      .toLowerCase()
      .replace(/[^a-z0-9_.-]+/g, "-")
      .replace(/^-+|-+$/g, "");
  const base = clean(store);
  const id = !base
    ? clean(source)
    : sourcesInStore === 1
      ? base
      : `${base}-${clean(source)}`;
  return /^[A-Za-z_]/.test(id) ? id : `coverage-${id}`;
}
