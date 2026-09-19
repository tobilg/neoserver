import type { Geometry } from "geojson";
import { describe, expect, it } from "vitest";
import {
  defaultPublication,
  defaultSource,
  graticule,
  graticuleStep,
  groupPreviewResources,
  legendTargets,
  resolveSource,
  sampleBounds,
  sourcesFor,
} from "./preview-selection";

const all = { geojson: true, wms: true, tiles: true };
const featuresOnly = { geojson: true, wms: false, tiles: false };

describe("preview sources", () => {
  it("offers only WMS for coverages and groups", () => {
    expect(sourcesFor("coverage")).toEqual(["wms"]);
    expect(sourcesFor("group")).toEqual(["wms"]);
    expect(sourcesFor("feature")).toEqual(["geojson", "wms", "tiles"]);
  });

  it("falls back to a source the kind can render", () => {
    expect(resolveSource(undefined, "coverage")).toBe("wms");
    expect(resolveSource("geojson", "group")).toBe("wms");
    expect(resolveSource(undefined, "feature")).toBe("geojson");
    expect(resolveSource("tiles", "feature")).toBe("tiles");
  });

  it("switches layers on with an active service", () => {
    expect(defaultSource("feature", all)).toBe("wms");
    expect(defaultSource("feature", featuresOnly)).toBe("geojson");
    expect(defaultSource("coverage", featuresOnly)).toBe("wms");
  });
});

describe("defaultPublication", () => {
  const resources = [
    { id: "old", kind: "feature" as const, updatedAt: "2026-09-01T00:00:00Z" },
    { id: "new", kind: "feature" as const, updatedAt: "2026-09-16T00:00:00Z" },
    { id: "cov", kind: "coverage" as const, updatedAt: "2026-09-20T00:00:00Z" },
    {
      id: "off",
      kind: "feature" as const,
      updatedAt: "2026-09-30T00:00:00Z",
      unavailable: "disabled",
    },
  ];

  it("picks the most recently updated renderable publication", () => {
    expect(defaultPublication(resources, all)?.id).toBe("cov");
    expect(defaultPublication(resources, featuresOnly)?.id).toBe("new");
  });

  it("keeps catalog order without timestamps and handles empty input", () => {
    expect(defaultPublication([{ id: "a" }, { id: "b" }], all)?.id).toBe("a");
    expect(defaultPublication([], all)).toBeUndefined();
  });
});

describe("sampleBounds", () => {
  it("covers every sampled geometry", () => {
    expect(
      sampleBounds({
        type: "FeatureCollection",
        features: [
          {
            type: "Feature",
            properties: {},
            geometry: { type: "Point", coordinates: [13.405, 52.52] },
          },
          {
            type: "Feature",
            properties: {},
            geometry: { type: "Point", coordinates: [2.3522, 48.8566] },
          },
          {
            type: "Feature",
            properties: {},
            geometry: {
              type: "Polygon",
              coordinates: [
                [
                  [-0.2, 51.4],
                  [0, 51.6],
                  [-0.2, 51.4],
                ],
              ],
            },
          },
          // Servers may emit features without geometry.
          {
            type: "Feature",
            properties: {},
            geometry: null as unknown as Geometry,
          },
        ],
      }),
    ).toEqual([
      [-0.2, 48.8566],
      [13.405, 52.52],
    ]);
  });

  it("ignores empty or projected samples", () => {
    expect(
      sampleBounds({ type: "FeatureCollection", features: [] }),
    ).toBeUndefined();
    expect(
      sampleBounds({
        type: "FeatureCollection",
        features: [
          {
            type: "Feature",
            properties: {},
            geometry: { type: "Point", coordinates: [1492237, 6894699] },
          },
        ],
      }),
    ).toBeUndefined();
  });
});

describe("graticule", () => {
  it("gets finer as the map zooms in", () => {
    expect(graticuleStep(0)).toBe(30);
    expect(graticuleStep(4)).toBe(10);
    expect(graticuleStep(6)).toBe(5);
    expect(graticuleStep(14)).toBe(0.1);
  });

  it("covers the view with meridians and parallels at the step", () => {
    const lines = graticule(
      [
        [-12, 44],
        [18, 56],
      ],
      6,
    );
    const meridians = lines.features.filter(
      (f) => f.properties?.axis === "meridian",
    );
    const parallels = lines.features.filter(
      (f) => f.properties?.axis === "parallel",
    );
    expect(meridians.map((f) => f.properties?.value)).toEqual([
      -15, -10, -5, 0, 5, 10, 15, 20,
    ]);
    expect(parallels.map((f) => f.properties?.value)).toEqual([
      40, 45, 50, 55, 60,
    ]);
  });

  it("stays bounded for world views", () => {
    const lines = graticule(
      [
        [-540, -85],
        [540, 85],
      ],
      0,
    );
    expect(lines.features.length).toBeLessThanOrEqual(800);
    expect(lines.features.every((f) => f.geometry.type === "LineString")).toBe(
      true,
    );
  });
});

describe("groupPreviewResources", () => {
  const resources: Array<{
    id: string;
    title?: string;
    kind: "feature" | "coverage" | "group";
    store?: string;
  }> = [
    { id: "roads", title: "Road network", kind: "feature", store: "postgis" },
    { id: "rivers", kind: "feature", store: "files" },
    { id: "terrain", kind: "coverage", store: "postgis" },
    { id: "basemap", kind: "group" },
    { id: "legacy", kind: "feature" },
  ];

  it("groups by kind in a stable order", () => {
    const groups = groupPreviewResources(resources, {
      query: "",
      shown: new Set(),
    });
    expect(groups.map((group) => [group.label, group.items.length])).toEqual([
      ["Layers", 3],
      ["Coverages", 1],
      ["Layer groups", 1],
    ]);
  });

  it("searches IDs and titles and leaves shown publications out", () => {
    const groups = groupPreviewResources(resources, {
      query: "network",
      shown: new Set(["terrain"]),
    });
    expect(
      groups.flatMap((group) => group.items.map((item) => item.id)),
    ).toEqual(["roads"]);
    const all = groupPreviewResources(resources, {
      query: "",
      shown: new Set(["roads", "basemap"]),
    });
    expect(all.map((group) => group.label)).toEqual(["Layers", "Coverages"]);
  });
});

describe("groupPreviewResources by store", () => {
  it("sorts stores and keeps store-less publications in their own sections", () => {
    const groups = groupPreviewResources(
      [
        { id: "roads", kind: "feature" as const, store: "postgis" },
        { id: "terrain", kind: "coverage" as const, store: "postgis" },
        { id: "rivers", kind: "feature" as const, store: "files" },
        { id: "basemap", kind: "group" as const },
        { id: "legacy", kind: "feature" as const },
      ],
      { query: "", shown: new Set(), groupBy: "store" },
    );
    expect(
      groups.map((group) => [group.label, group.items.map((item) => item.id)]),
    ).toEqual([
      ["files", ["rivers"]],
      ["postgis", ["roads", "terrain"]],
      ["Layer groups", ["basemap"]],
      ["Other publications", ["legacy"]],
    ]);
  });

  it("matches the store name in searches", () => {
    const groups = groupPreviewResources(
      [
        { id: "roads", kind: "feature" as const, store: "postgis" },
        { id: "rivers", kind: "feature" as const, store: "files" },
      ],
      {
        query: "files",
        shown: new Set(),
        groupBy: "store",
      },
    );
    expect(groups.map((group) => group.label)).toEqual(["files"]);
  });
});

describe("legendTargets", () => {
  const groups = [
    {
      public_id: "basemap",
      members: [
        { resource: "lakes" },
        { resource: "roads", style: "thin" },
        { resource: "overlay" },
      ],
    },
    {
      public_id: "overlay",
      members: [{ resource: "places" }, { resource: "basemap" }],
    },
  ];

  it("keeps a plain layer and its selected style", () => {
    expect(legendTargets("lakes", "blue", groups)).toEqual([
      { layer: "lakes", style: "blue" },
    ]);
  });

  it("expands nested groups with member styles and stops at cycles", () => {
    expect(legendTargets("basemap", "ignored", groups)).toEqual([
      { layer: "lakes", style: "" },
      { layer: "roads", style: "thin" },
      { layer: "places", style: "" },
    ]);
  });
});
