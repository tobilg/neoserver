export type MapBounds = [[number, number], [number, number]];

/** OGC collection extents are CRS84 by default, optionally with a Z axis. */
export function collectionBounds(extent?: {
  spatial?: { bbox?: number[][]; crs?: string };
}): MapBounds | undefined {
  const spatial = extent?.spatial;
  if (spatial?.crs && !/\/CRS84h?$/.test(spatial.crs)) return undefined;
  const bounds = (spatial?.bbox ?? []).flatMap((box): MapBounds[] => {
    const b = box.length === 6 ? [box[0], box[1], box[3], box[4]] : box;
    if (b.length !== 4 || !b.every(Number.isFinite)) return [];
    const [west, south, east, north] = b;
    if (
      Math.abs(west) > 180 ||
      Math.abs(east) > 180 ||
      south < -90 ||
      north > 90 ||
      south > north
    )
      return [];
    // A crossing extent needs both sides of the dateline. A world-width fit
    // is conservative and composes safely with other selected publications.
    return [
      [
        [west > east ? -180 : west, south],
        [west > east ? 180 : east, north],
      ],
    ];
  });
  return unionBounds(bounds);
}

export function unionBounds(bounds: MapBounds[]): MapBounds | undefined {
  if (!bounds.length) return undefined;
  return [
    [
      Math.min(...bounds.map((b) => b[0][0])),
      Math.min(...bounds.map((b) => b[0][1])),
    ],
    [
      Math.max(...bounds.map((b) => b[1][0])),
      Math.max(...bounds.map((b) => b[1][1])),
    ],
  ];
}

/** Shared views may contain wrapped longitude values outside [-180, 180]. */
export function sharedBounds(value: string | null): MapBounds | undefined {
  if (!value || value.split(",").some((part) => !part.trim())) return undefined;
  const b = value.split(",").map(Number);
  if (
    b.length !== 4 ||
    !b.every(Number.isFinite) ||
    b[0] >= b[2] ||
    b[1] >= b[3] ||
    b[1] < -90 ||
    b[3] > 90
  )
    return undefined;
  return [
    [b[0], b[1]],
    [b[2], b[3]],
  ];
}

/** Base map colours for the current console theme. */
export function mapPalette() {
  const dark = document.documentElement.classList.contains("dark");
  return dark
    ? { background: "#1f2329", graticule: "#3d434d" }
    : { background: "#e5e7eb", graticule: "#9ca3af" };
}
