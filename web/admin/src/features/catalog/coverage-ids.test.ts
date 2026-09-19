import { describe, expect, it } from "vitest";
import { defaultCoverageID } from "./coverage-ids";

describe("defaultCoverageID", () => {
  it("names a store's only coverage after the store", () => {
    expect(defaultCoverageID("Ortho Mosaic", "mosaic", 1)).toBe("ortho-mosaic");
  });

  it("qualifies multiple sources with the store name", () => {
    expect(defaultCoverageID("elevation", "band 1", 2)).toBe(
      "elevation-band-1",
    );
  });

  it("falls back to the source and keeps IDs valid", () => {
    expect(defaultCoverageID("", "raster", 1)).toBe("raster");
    expect(defaultCoverageID("2026 scans", "x", 1)).toBe("coverage-2026-scans");
  });
});
