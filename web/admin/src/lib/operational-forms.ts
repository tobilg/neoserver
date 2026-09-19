import type {
  MosaicHarvestRequest,
  TileCacheJobRequest,
  TileMatrixSetDefinition,
} from "@/api/generated/models";
import { validateFields } from "./schema-fields";
import { formSchemas } from "./resource-schemas";

export function validateTileJob(
  value: TileCacheJobRequest,
  grid?: TileMatrixSetDefinition,
) {
  const errors = validateFields(
    formSchemas.TileCacheJobRequest,
    value as unknown as Record<string, unknown>,
  );
  if (value.all_resources) {
    if (value.operation !== "truncate")
      errors.push("All resources is only available for truncate.");
    if (value.resource)
      errors.push("Choose either one resource or all resources, not both.");
    if (value.bounds) errors.push("All-resource truncation cannot use bounds.");
  } else if (!value.resource?.trim()) errors.push("Choose a resource.");
  if (value.operation !== "truncate") {
    if (!value.tile_matrix_set) errors.push("Choose a tile matrix set.");
    if (
      value.tile_type === "vector" &&
      value.format !== "application/vnd.mapbox-vector-tile"
    )
      errors.push("Vector tiles require the Mapbox vector tile format.");
    if (
      value.tile_type === "map" &&
      !["image/png", "image/jpeg", "image/webp"].includes(value.format ?? "")
    )
      errors.push("Map tiles require PNG, JPEG or WebP.");
  }
  const min = value.min_zoom;
  const max = value.max_zoom;
  if (
    (min !== undefined && (!Number.isInteger(min) || min < 0)) ||
    (max !== undefined && (!Number.isInteger(max) || max < 0)) ||
    (min !== undefined && max !== undefined && min > max)
  )
    errors.push("Zooms must be non-negative integers with minimum ≤ maximum.");
  if (
    grid &&
    [min, max].some(
      (zoom) =>
        zoom !== undefined &&
        !grid.tileMatrices.some((matrix) => matrix.id === String(zoom)),
    )
  )
    errors.push("Zooms must exist in the selected tile matrix set.");
  if (value.bounds) {
    const bbox = value.bounds.bbox;
    if (
      !Array.isArray(bbox) ||
      bbox.length !== 4 ||
      !bbox.every(Number.isFinite) ||
      bbox[0] >= bbox[2] ||
      bbox[1] >= bbox[3]
    )
      errors.push(
        "Bounds need four coordinates: min X, min Y, max X, max Y, with increasing extents.",
      );
    if (!value.bounds.crs?.trim())
      errors.push("Specify the CRS for the bounds.");
  }
  return errors.join(" ");
}

export function validateHarvest(value: MosaicHarvestRequest) {
  const errors = validateFields(
    formSchemas.MosaicHarvestRequest,
    value as unknown as Record<string, unknown>,
  );
  // The server defaults an omitted mode to synchronize; require an explicit choice.
  if (!["append", "synchronize"].includes(value.mode ?? ""))
    errors.push("Choose append or synchronize.");
  for (const granule of value.granules ?? []) {
    if (!granule.path?.trim())
      errors.push("Every granule needs a source path.");
    if (
      granule.time &&
      !/^\d{4}-\d{2}-\d{2}T.*(?:Z|[+-]\d{2}:\d{2})$/.test(granule.time)
    )
      errors.push(
        "Granule time must include a timezone, for example 2026-01-01T00:00:00Z.",
      );
  }
  return errors.join(" ");
}
