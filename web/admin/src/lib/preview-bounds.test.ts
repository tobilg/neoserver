import { describe, expect, it } from "vitest";
import { previewBounds } from "./preview-bounds";

describe("style preview bounds", () => {
  it.each([
    [7, 51, 7, 51],
    [7, 51, 8, 51],
    [7, 51, 7, 52],
    [180, 90, 180, 90],
  ])("pads degenerate extent %j", (min_x, min_y, max_x, max_y) => {
    const input = { min_x, min_y, max_x, max_y, srid: 4326 };
    const result = previewBounds(input);
    const [x0, y0, x1, y1] = result.bbox.split(",").map(Number);
    expect(result.crs).toBe("CRS:84");
    expect(x0).toBeLessThan(x1);
    expect(y0).toBeLessThan(y1);
    expect(input).toEqual({ min_x, min_y, max_x, max_y, srid: 4326 });
  });
  it("uses native projected bounds instead of a world view", () => {
    const result = previewBounds({
      min_x: 779236,
      min_y: 6621293,
      max_x: 779236,
      max_y: 6621293,
      srid: 3857,
    });
    expect(result.crs).toBe("EPSG:3857");
    expect(result.bbox.split(",").map(Number)[0]).toBeCloseTo(779181);
  });
  it("falls back safely for unknown or invalid extents", () => {
    expect(previewBounds().bbox).toBe("-180,-90,180,90");
    expect(
      previewBounds({ min_x: NaN, min_y: 0, max_x: 0, max_y: 0, srid: 4326 })
        .bbox,
    ).toBe("-180,-90,180,90");
  });
});
