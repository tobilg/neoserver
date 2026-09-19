import { describe, expect, it } from "vitest";
import { validateHarvest, validateTileJob } from "./operational-forms";
describe("operational forms", () => {
  it("requires compatible tile type, format and ordered zooms", () => {
    expect(
      validateTileJob({
        operation: "seed",
        resource: "roads",
        tile_type: "vector",
        tile_matrix_set: "WebMercatorQuad",
        format: "image/png",
        min_zoom: 8,
        max_zoom: 2,
      }),
    ).toMatch(/Vector tiles.*Zooms/);
    expect(
      validateTileJob({
        operation: "seed",
        resource: "roads",
        tile_type: "vector",
        tile_matrix_set: "WebMercatorQuad",
        format: "application/vnd.mapbox-vector-tile",
        min_zoom: 0,
        max_zoom: 2,
      }),
    ).toBe("");
  });
  it("rejects unsafe all-resource jobs and malformed bounds", () => {
    expect(
      validateTileJob({ operation: "seed", all_resources: true }),
    ).toContain("only available for truncate");
    expect(
      validateTileJob({
        operation: "truncate",
        all_resources: true,
        bounds: { bbox: [0, 0, 1, 1], crs: "EPSG:4326" },
      }),
    ).toContain("cannot use bounds");
    expect(
      validateTileJob({
        operation: "truncate",
        resource: "roads",
        bounds: { bbox: [1, 1, 0, 0], crs: "" },
      }),
    ).toContain("increasing extents");
  });
  it("requires an explicit harvest mode and valid granule source/time", () => {
    expect(validateHarvest({})).toContain("Choose append or synchronize");
    expect(
      validateHarvest({
        mode: "append",
        granules: [{ path: "", time: "2026-01-01T12:00" }],
      }),
    ).toMatch(/source path.*timezone/);
    expect(
      validateHarvest({
        mode: "append",
        directory: "data/rasters",
        pattern: "*.tif",
      }),
    ).toBe("");
  });
});
