import generated from "./form-schemas.generated.json";
import type { FieldSchema } from "@/lib/schema-fields";

export const formSchemas: Record<string, FieldSchema> = generated;
const serverFields = new Set([
  "id",
  "service_id",
  "workspace_id",
  "source_layer",
  "source_coverage",
  "created_at",
  "updated_at",
  "tile_cache_generation",
  "is_sql_view",
  "sql_view_config",
]);
export function editableSchema(schema: FieldSchema): FieldSchema {
  return {
    ...schema,
    properties: Object.fromEntries(
      Object.entries(schema.properties ?? {}).filter(
        ([key]) => !serverFields.has(key),
      ),
    ),
    required: schema.required?.filter((key) => !serverFields.has(key)),
  };
}
const string = { type: "string" };
const list = { type: "array", items: string };
const integer = { type: "integer", minimum: 0 };
const connectionFields: Record<string, FieldSchema> = {
  ...formSchemas.Store.properties?.connection_info.properties,
  geometry_column: string,
  id_column: string,
  srid: { type: "integer", minimum: 1 },
  schemas: list,
  extensions: list,
  read_only: { type: "boolean" },
  variables: list,
  name: string,
  directory: string,
  pattern: string,
};
const connectionKeys: Record<string, string[]> = {
  postgis: [
    "host",
    "port",
    "database",
    "user",
    "password",
    "sslmode",
    "schemas",
    "connection_string",
  ],
  duckdb: ["path", "srid", "layer_srids", "read_only", "extensions"],
  geoparquet: ["path", "geometry_column", "id_column", "srid"],
  vectorfile: ["path", "layer", "geometry_column", "id_column", "srid"],
  rasterfile: ["path", "variables"],
  raster_mosaic: ["name", "directory", "pattern"],
};

export function resourceSchema(
  key: string,
  row?: Record<string, unknown>,
): FieldSchema | undefined {
  if (key === "services")
    return {
      ...formSchemas.Store,
      properties: {
        ...formSchemas.Store.properties,
        connection_info: {
          type: "object",
          properties: Object.fromEntries(
            (
              connectionKeys[String(row?.type)] ?? Object.keys(connectionFields)
            ).map((key) => [key, connectionFields[key]]),
          ),
        },
      },
    };
  if (key === "layers") return editableSchema(formSchemas.Layer);
  if (key === "coverages") return editableSchema(formSchemas.CoverageUpdate);
  if (key === "layer_groups") return editableSchema(formSchemas.LayerGroup);
  if (key.includes("claim_mapping")) return formSchemas.ClaimMapping;
  if (key === "roles") return formSchemas.Role;
  if (key === "role_policies") return formSchemas.RolePolicy;
  return undefined;
}

export function protocolSchema(name: string): FieldSchema {
  const source = formSchemas[name];
  return {
    ...source,
    properties: Object.fromEntries(
      Object.entries(source.properties ?? {})
        .filter(([key]) => key !== "enabled")
        .map(([key, value]) => [
          key,
          value.type === "integer" || value.type === "number"
            ? { ...integer, ...value, minimum: 0 }
            : value,
        ]),
    ),
    required: source.required?.filter((key) => key !== "enabled"),
  };
}
