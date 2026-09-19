import { z } from "zod";

/**
 * Connection schemas per store type, mirroring the `*ConnectionInfo` structs in
 * internal/store/types.go. These validate before a request is sent so an
 * operator sees a field-level message instead of a round-trip 422.
 */

export const STORE_TYPES = [
  "postgis",
  "duckdb",
  "geoparquet",
  "vectorfile",
  "rasterfile",
  "raster_mosaic",
] as const;

export type StoreType = (typeof STORE_TYPES)[number];

export const STORE_LABELS: Record<StoreType, string> = {
  postgis: "PostGIS",
  duckdb: "DuckDB Spatial",
  geoparquet: "GeoParquet",
  vectorfile: "Vector file",
  rasterfile: "Raster file",
  raster_mosaic: "Raster mosaic",
};

const srid = z
  .number()
  .int("SRID must be a whole number")
  .positive("SRID must be positive");

const requiredPath = z.string().min(1, "A path or URL is required");

export const postgisConnection = z.object({
  host: z.string().min(1, "Host is required"),
  port: z
    .number()
    .int("Port must be a whole number")
    .min(1, "Port must be between 1 and 65535")
    .max(65535, "Port must be between 1 and 65535"),
  database: z.string().min(1, "Database is required"),
  user: z.string().min(1, "User is required"),
  password: z.string(),
  sslmode: z.string().optional(),
  schemas: z.array(z.string()).optional(),
});

export const duckdbConnection = z.object({
  path: requiredPath,
  srid: srid.optional(),
  layer_srids: z.record(z.string(), srid).optional(),
  read_only: z.boolean().optional(),
  extensions: z.array(z.string()).optional(),
});

export const geoparquetConnection = z.object({
  path: requiredPath,
  geometry_column: z.string().min(1, "Geometry column is required"),
  id_column: z.string().optional(),
  srid,
});

export const vectorfileConnection = z.object({
  path: requiredPath,
  layer: z.string().optional(),
  geometry_column: z.string().optional(),
  id_column: z.string().optional(),
  srid,
  open_options: z.record(z.string(), z.unknown()).optional(),
});

export const rasterfileConnection = z.object({
  path: requiredPath,
  variables: z.array(z.string()).optional(),
  open_options: z.record(z.string(), z.unknown()).optional(),
});

export const rasterMosaicConnection = z.object({
  name: z.string().min(1, "Mosaic name is required"),
  directory: z.string().min(1, "A directory is required"),
  pattern: z.string().min(1, "A filename pattern is required"),
  granules: z.array(z.unknown()).optional(),
});

export const CONNECTION_SCHEMAS = {
  postgis: postgisConnection,
  duckdb: duckdbConnection,
  geoparquet: geoparquetConnection,
  vectorfile: vectorfileConnection,
  rasterfile: rasterfileConnection,
  raster_mosaic: rasterMosaicConnection,
} satisfies Record<StoreType, z.ZodType>;

/**
 * Whole-form schema. A discriminated union keyed on `type` means switching the
 * source type swaps in the right connection validation automatically.
 */
export const storeFormSchema = z.discriminatedUnion("type", [
  z.object({
    name: z.string().min(1, "Store name is required"),
    enabled: z.boolean(),
    type: z.literal("postgis"),
    connection_info: postgisConnection,
  }),
  z.object({
    name: z.string().min(1, "Store name is required"),
    enabled: z.boolean(),
    type: z.literal("duckdb"),
    connection_info: duckdbConnection,
  }),
  z.object({
    name: z.string().min(1, "Store name is required"),
    enabled: z.boolean(),
    type: z.literal("geoparquet"),
    connection_info: geoparquetConnection,
  }),
  z.object({
    name: z.string().min(1, "Store name is required"),
    enabled: z.boolean(),
    type: z.literal("vectorfile"),
    connection_info: vectorfileConnection,
  }),
  z.object({
    name: z.string().min(1, "Store name is required"),
    enabled: z.boolean(),
    type: z.literal("rasterfile"),
    connection_info: rasterfileConnection,
  }),
  z.object({
    name: z.string().min(1, "Store name is required"),
    enabled: z.boolean(),
    type: z.literal("raster_mosaic"),
    connection_info: rasterMosaicConnection,
  }),
]);

export type StoreFormValues = z.infer<typeof storeFormSchema>;

export function connectionDefaults(type: StoreType): Record<string, unknown> {
  switch (type) {
    case "postgis":
      return {
        host: "localhost",
        port: 5432,
        database: "",
        user: "",
        password: "",
        sslmode: "prefer",
        schemas: ["public"],
      };
    case "duckdb":
      return { path: "", read_only: true, extensions: ["spatial"] };
    case "geoparquet":
      return {
        path: "",
        geometry_column: "geometry",
        id_column: "",
        srid: 4326,
      };
    case "vectorfile":
      return {
        path: "",
        layer: "",
        geometry_column: "wkb_geometry",
        id_column: "",
        srid: 4326,
        open_options: {},
      };
    case "rasterfile":
      return { path: "", variables: [], open_options: {} };
    case "raster_mosaic":
      return { name: "", directory: "", pattern: "*.tif", granules: [] };
  }
}
