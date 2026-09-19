/** Presentation metadata only: generated schemas remain the validation contract. */
const order = [
  "name",
  "public_id",
  "title",
  // A group is defined by its members; ask for them before the prose.
  "members",
  "description",
  "abstract",
  "enabled",
  "type",
  "source_layer",
  "source_coverage",
  "connection_info",
  "public",
  "allowed_roles",
  "default_style",
  "styles",
];
export function fieldOrder(key: string) {
  const index = order.indexOf(key);
  return index < 0 ? order.length : index;
}
export const advancedFields = new Set([
  "native_extent",
  "wgs84_extent",
  "cache_settings",
  "tile_cache_settings",
  "geometry_column",
  "id_column",
  "source_srid",
  "target_srid",
  "crs_default",
  "crs_available",
  "geometry_type",
  "geometry_precision",
  "attributes",
  "dimensions",
  "transform",
  "reproject",
  "promote_to_multi",
  "skip_failures",
]);
