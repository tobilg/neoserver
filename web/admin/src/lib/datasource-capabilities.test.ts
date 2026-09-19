import { describe, expect, it } from "vitest";
import { datasourceCapabilities } from "./datasource-capabilities";
describe("source workflows", () => {
  it.each(["postgis", "duckdb"])("allows SQL views for %s", (type) =>
    expect(datasourceCapabilities(type).sql).toBe(true),
  );
  it.each([
    "geoparquet",
    "vectorfile",
    "rasterfile",
    "raster_mosaic",
    "unknown",
  ])("does not offer unsupported SQL for %s", (type) =>
    expect(datasourceCapabilities(type).sql).toBe(false),
  );
  it.each(["postgis", "rasterfile", "raster_mosaic"])(
    "includes %s in coverage discovery and workspace collision checks",
    (type) => expect(datasourceCapabilities(type).coverages).toBe(true),
  );
  it("fails closed for unknown sources", () =>
    expect(datasourceCapabilities("unknown")).toEqual({
      features: false,
      sql: false,
      coverages: false,
    }));
});
