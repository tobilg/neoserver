import type { SpatialExtent } from "@/api/generated/models";

/** Display bounds are padded independently of the authoritative data extent. */
export function previewBounds(extent?: SpatialExtent | null) {
  const world = { crs: "CRS:84", bbox: "-180,-90,180,90" };
  if (!extent || !Number.isInteger(extent.srid) || extent.srid <= 0)
    return world;
  const { min_x: x0, min_y: y0, max_x: x1, max_y: y1 } = extent;
  if (![x0, y0, x1, y1].every(Number.isFinite) || x1 < x0 || y1 < y0)
    return world;
  const geographic = extent.srid === 4326;
  const minimum = geographic ? 0.01 : 100;
  const width = Math.max(x1 - x0, minimum);
  const height = Math.max(y1 - y0, minimum);
  const cx = x0 / 2 + x1 / 2;
  const cy = y0 / 2 + y1 / 2;
  const bounds = [
    cx - width * 0.55,
    cy - height * 0.55,
    cx + width * 0.55,
    cy + height * 0.55,
  ];
  if (geographic) {
    bounds[0] = Math.max(-180, bounds[0]);
    bounds[1] = Math.max(-90, bounds[1]);
    bounds[2] = Math.min(180, bounds[2]);
    bounds[3] = Math.min(90, bounds[3]);
  }
  if (
    !bounds.every(Number.isFinite) ||
    bounds[0] >= bounds[2] ||
    bounds[1] >= bounds[3]
  )
    return world;
  return {
    crs: geographic ? "CRS:84" : `EPSG:${extent.srid}`,
    bbox: bounds.join(","),
  };
}
