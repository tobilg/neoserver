import type { FeatureCollection, Geometry, Position } from "geojson";
import type { MapBounds } from "./map-bounds";

export type PreviewSource = "geojson" | "wms" | "tiles";
export type PreviewKind = "feature" | "coverage" | "group";
export interface PreviewAvailability {
  geojson: boolean;
  wms: boolean;
  tiles: boolean;
}

/** Sources a publication kind can render with. Only features have raw geometry. */
export function sourcesFor(kind: PreviewKind): PreviewSource[] {
  return kind === "feature" ? ["geojson", "wms", "tiles"] : ["wms"];
}

/**
 * The source used when the URL names a layer without one, and when a layer is
 * switched on. Features prefer the server style when WMS is active.
 */
export function defaultSource(
  kind: PreviewKind,
  active: PreviewAvailability,
): PreviewSource {
  if (kind !== "feature") return "wms";
  if (active.wms) return "wms";
  if (active.geojson) return "geojson";
  return active.tiles ? "tiles" : "geojson";
}

/**
 * Parses a URL-provided source. Without a valid one, features keep the GeoJSON
 * sample (independent of service state, so the source doesn't flip while the
 * workspace summary loads) and other kinds use WMS, their only renderer.
 */
export function resolveSource(
  requested: string | undefined,
  kind: PreviewKind,
): PreviewSource {
  return sourcesFor(kind).includes(requested as PreviewSource)
    ? (requested as PreviewSource)
    : kind === "feature"
      ? "geojson"
      : "wms";
}

export interface SelectableResource {
  id: string;
  kind?: PreviewKind;
  unavailable?: string;
  updatedAt?: string;
}

/**
 * Picks what Preview shows when the URL selects nothing: the most recently
 * updated publication that can render with an active service.
 */
export function defaultPublication<T extends SelectableResource>(
  resources: T[],
  active: PreviewAvailability,
): T | undefined {
  const renderable = resources.filter((resource) => {
    if (resource.unavailable) return false;
    const kind = resource.kind ?? "feature";
    return kind === "feature"
      ? active.wms || active.geojson || active.tiles
      : active.wms;
  });
  return renderable
    .map((resource, index) => ({ resource, index }))
    .sort((a, b) => {
      const byTime =
        (Date.parse(b.resource.updatedAt ?? "") || 0) -
        (Date.parse(a.resource.updatedAt ?? "") || 0);
      return byTime || a.index - b.index;
    })[0]?.resource;
}

function visit(geometry: Geometry | null, add: (position: Position) => void) {
  if (!geometry) return;
  switch (geometry.type) {
    case "Point":
      add(geometry.coordinates);
      break;
    case "MultiPoint":
    case "LineString":
      geometry.coordinates.forEach(add);
      break;
    case "MultiLineString":
    case "Polygon":
      geometry.coordinates.forEach((part) => part.forEach(add));
      break;
    case "MultiPolygon":
      geometry.coordinates.forEach((polygon) =>
        polygon.forEach((ring) => ring.forEach(add)),
      );
      break;
    case "GeometryCollection":
      geometry.geometries.forEach((child) => visit(child, add));
      break;
  }
}

/** Bounds of a loaded GeoJSON sample, used when no stored extent exists. */
export function sampleBounds(data: FeatureCollection): MapBounds | undefined {
  let west = Infinity,
    south = Infinity,
    east = -Infinity,
    north = -Infinity;
  for (const feature of data.features)
    visit(feature.geometry, ([x, y]) => {
      if (!Number.isFinite(x) || !Number.isFinite(y)) return;
      if (Math.abs(x) > 180 || Math.abs(y) > 90) return;
      west = Math.min(west, x);
      east = Math.max(east, x);
      south = Math.min(south, y);
      north = Math.max(north, y);
    });
  if (!Number.isFinite(west)) return undefined;
  return [
    [west, south],
    [east, north],
  ];
}

const GRATICULE_STEPS = [30, 10, 5, 1, 0.5, 0.1];

/** Graticule spacing in degrees for a MapLibre zoom level. */
export function graticuleStep(zoom: number): number {
  const index = Math.max(
    0,
    Math.min(GRATICULE_STEPS.length - 1, Math.floor((zoom - 1) / 2)),
  );
  return GRATICULE_STEPS[index];
}

/**
 * Graticule lines covering the visible bounds. Generated on the client, so the
 * preview has geographic context without external basemap requests.
 */
export function graticule(bounds: MapBounds, zoom: number): FeatureCollection {
  const step = graticuleStep(zoom);
  const west = Math.max(-180, Math.floor(bounds[0][0] / step) * step);
  const east = Math.min(180, Math.ceil(bounds[1][0] / step) * step);
  const south = Math.max(-85, Math.floor(bounds[0][1] / step) * step);
  const north = Math.min(85, Math.ceil(bounds[1][1] / step) * step);
  const round = (value: number) => Math.round(value * 1e6) / 1e6;
  const features: FeatureCollection["features"] = [];
  for (let x = west; x <= east + 1e-9 && features.length < 400; x += step)
    features.push({
      type: "Feature",
      properties: { axis: "meridian", value: round(x) },
      geometry: {
        type: "LineString",
        coordinates: [
          [round(x), south],
          [round(x), north],
        ],
      },
    });
  for (let y = south; y <= north + 1e-9 && features.length < 800; y += step)
    features.push({
      type: "Feature",
      properties: { axis: "parallel", value: round(y) },
      geometry: {
        type: "LineString",
        coordinates: [
          [west, round(y)],
          [east, round(y)],
        ],
      },
    });
  return { type: "FeatureCollection", features };
}

const kindGroups: Array<[PreviewKind, string]> = [
  ["feature", "Layers"],
  ["coverage", "Coverages"],
  ["group", "Layer groups"],
];

export type PreviewGrouping = "store" | "kind";

/**
 * The catalog part of the layer controls: publications not yet on the map,
 * filtered by text and grouped by store (layer groups and store-less
 * publications get their own sections) or by publication kind. Shown
 * publications are listed separately, above the catalog.
 */
export function groupPreviewResources<
  T extends { id: string; title?: string; kind?: PreviewKind; store?: string },
>(
  resources: T[],
  {
    query,
    shown,
    groupBy = "kind",
  }: {
    query: string;
    shown: Set<string>;
    groupBy?: PreviewGrouping;
  },
) {
  const needle = query.trim().toLowerCase();
  const visible = resources.filter((item) => {
    if (shown.has(item.id)) return false;
    return (
      !needle ||
      item.id.toLowerCase().includes(needle) ||
      (item.title ?? "").toLowerCase().includes(needle) ||
      (item.store ?? "").toLowerCase().includes(needle)
    );
  });
  if (groupBy === "kind")
    return kindGroups
      .map(([kind, label]) => ({
        key: kind,
        label,
        items: visible.filter((item) => (item.kind ?? "feature") === kind),
      }))
      .filter((group) => group.items.length > 0);
  const stores = [
    ...new Set(visible.flatMap((item) => (item.store ? [item.store] : []))),
  ].sort((a, b) => a.localeCompare(b));
  return [
    ...stores.map((store) => ({
      key: `store:${store}`,
      label: store,
      items: visible.filter((item) => item.store === store),
    })),
    {
      key: "groups",
      label: "Layer groups",
      items: visible.filter((item) => !item.store && item.kind === "group"),
    },
    {
      key: "other",
      label: "Other publications",
      items: visible.filter((item) => !item.store && item.kind !== "group"),
    },
  ].filter((group) => group.items.length > 0);
}

export interface LegendTarget {
  layer: string;
  style: string;
}

/**
 * The legends to show for a preview entry. A layer group has no legend of its
 * own, so it expands to its members (nested groups included, each member with
 * its own style). Cycles end the expansion.
 */
export function legendTargets(
  id: string,
  style: string,
  groups: ReadonlyArray<{
    public_id: string;
    members: ReadonlyArray<{ resource: string; style?: string }>;
  }>,
): LegendTarget[] {
  const byID = new Map(groups.map((group) => [group.public_id, group]));
  if (!byID.has(id)) return [{ layer: id, style }];
  const targets: LegendTarget[] = [];
  const seen = new Set<string>();
  const expand = (groupID: string, visiting: Set<string>) => {
    const group = byID.get(groupID);
    if (!group || visiting.has(groupID)) return;
    visiting.add(groupID);
    for (const member of group.members) {
      if (byID.has(member.resource)) expand(member.resource, visiting);
      else {
        const key = `${member.resource}\u0000${member.style ?? ""}`;
        if (seen.has(key)) continue;
        seen.add(key);
        targets.push({ layer: member.resource, style: member.style ?? "" });
      }
    }
    visiting.delete(groupID);
  };
  expand(id, new Set());
  return targets;
}
