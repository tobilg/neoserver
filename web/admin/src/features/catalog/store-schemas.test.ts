import { describe, expect, it } from "vitest";
import {
  connectionDefaults,
  STORE_TYPES,
  storeFormSchema,
  type StoreType,
} from "./store-schemas";

function form(type: StoreType, overrides: Record<string, unknown> = {}) {
  return {
    name: "production-data",
    type,
    enabled: true,
    connection_info: { ...connectionDefaults(type), ...overrides },
  };
}

describe("store form schema", () => {
  it("requires a store name whatever the type", () => {
    for (const type of STORE_TYPES) {
      const result = storeFormSchema.safeParse({ ...form(type), name: "" });
      expect(result.success, `${type} should reject a blank name`).toBe(false);
    }
  });

  it("accepts complete PostGIS connection details", () => {
    const result = storeFormSchema.safeParse(
      form("postgis", { database: "gis", user: "reader" }),
    );
    expect(result.success).toBe(true);
  });

  it("rejects PostGIS without a database or user", () => {
    expect(storeFormSchema.safeParse(form("postgis")).success).toBe(false);
  });

  it("rejects an out-of-range PostGIS port", () => {
    const result = storeFormSchema.safeParse(
      form("postgis", { database: "gis", user: "reader", port: 70000 }),
    );
    expect(result.success).toBe(false);
  });

  it("requires a path for file-backed stores", () => {
    for (const type of [
      "duckdb",
      "geoparquet",
      "vectorfile",
      "rasterfile",
    ] as const) {
      expect(
        storeFormSchema.safeParse(form(type)).success,
        `${type} should require a path`,
      ).toBe(false);
      expect(
        storeFormSchema.safeParse(form(type, { path: "/data/x" })).success,
        `${type} should accept a path`,
      ).toBe(true);
    }
  });

  it("rejects a non-positive SRID", () => {
    const result = storeFormSchema.safeParse(
      form("geoparquet", { path: "/data/x.parquet", srid: 0 }),
    );
    expect(result.success).toBe(false);
  });

  it("requires directory and pattern for a raster mosaic", () => {
    expect(storeFormSchema.safeParse(form("raster_mosaic")).success).toBe(
      false,
    );
    expect(
      storeFormSchema.safeParse(
        form("raster_mosaic", {
          name: "dem",
          directory: "/data/dem",
          pattern: "*.tif",
        }),
      ).success,
    ).toBe(true);
  });

  it("provides defaults for every declared store type", () => {
    for (const type of STORE_TYPES) {
      expect(connectionDefaults(type), type).toBeTypeOf("object");
    }
  });
});
