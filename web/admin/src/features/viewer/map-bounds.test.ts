import { describe, expect, it } from "vitest";
import { collectionBounds, sharedBounds, unionBounds } from "./map-bounds";

describe("map framing", () => {
  it("combines collections, including point and 3D extents", () => {
    expect(
      collectionBounds({
        spatial: {
          bbox: [
            [7, 51, 1, 8, 52, 10],
            [9, 50, 9, 50],
          ],
        },
      }),
    ).toEqual([
      [7, 50],
      [9, 52],
    ]);
    expect(
      unionBounds([
        [
          [7, 51],
          [7, 51],
        ],
        [
          [8, 50],
          [9, 53],
        ],
      ]),
    ).toEqual([
      [7, 50],
      [9, 53],
    ]);
  });
  it("rejects invalid or projected collection extents", () => {
    for (const bbox of [
      [0, NaN, 1, 1],
      [0, 91, 1, 92],
      [200, 0, 210, 1],
      [0, 2, 1, 1],
      [1, 2],
    ]) {
      expect(collectionBounds({ spatial: { bbox: [bbox] } })).toBeUndefined();
    }
    expect(
      collectionBounds({
        spatial: {
          crs: "http://www.opengis.net/def/crs/EPSG/0/3857",
          bbox: [[0, 0, 1, 1]],
        },
      }),
    ).toBeUndefined();
    expect(collectionBounds()).toBeUndefined();
  });
  it("includes both sides of antimeridian extents", () => {
    expect(
      collectionBounds({ spatial: { bbox: [[170, -10, -170, 10]] } }),
    ).toEqual([
      [-180, -10],
      [180, 10],
    ]);
  });
  it("preserves wrapped shared views but rejects incomplete and degenerate views", () => {
    expect(sharedBounds("-220,-40,140,60")).toEqual([
      [-220, -40],
      [140, 60],
    ]);
    for (const value of [
      null,
      "",
      "1,,2,3",
      "1,2,1,3",
      "NaN,2,3,4",
      "0,0,1,91",
      "1,2,3,4,5",
    ])
      expect(sharedBounds(value)).toBeUndefined();
  });
});
