import type { TileCacheJobRequestProperty } from "@/api/generated/models";

const formats: Record<string, string> = {
  "application/vnd.mapbox-vector-tile": "MVT",
  "image/png": "PNG",
  "image/jpeg": "JPEG",
  "image/webp": "WEBP",
};

/** "Seed · ortho · z0–6 · PNG" */
export function jobTitle(
  request: Partial<TileCacheJobRequestProperty> | undefined,
) {
  if (!request) return "Tile-cache job";
  const operation = String(request.operation ?? "job");
  const zoom =
    request.min_zoom !== undefined || request.max_zoom !== undefined
      ? `z${request.min_zoom ?? 0}–${request.max_zoom ?? "max"}`
      : "";
  return [
    operation.charAt(0).toUpperCase() + operation.slice(1),
    request.all_resources ? "all resources" : request.resource,
    zoom,
    request.format ? (formats[request.format] ?? request.format) : "",
  ]
    .filter(Boolean)
    .join(" · ");
}
