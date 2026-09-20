export interface FieldSchema {
  type?: string;
  properties?: Record<string, FieldSchema>;
  items?: FieldSchema;
  required?: string[];
  enum?: (string | number)[];
  minimum?: number;
  maximum?: number;
  format?: string;
  description?: string;
  default?: unknown;
  "x-service-operations"?: Record<string, Record<string, string>>;
}
export interface Choice {
  value: string;
  label: string;
}
export type FieldChoices = Record<string, Choice[]>;
const names: Record<string, string> = {
  dataset_map_layer_group_id: "Workspace map",
  services: "Stores",
  api_keys: "API keys",
  ogcapi: "OGC API – Features",
  ogc_tiles: "OGC API – Tiles",
  "ogc-tiles": "OGC API – Tiles",
  wms: "WMS",
  wfs: "WFS",
  wcs: "WCS",
  wmts: "WMTS",
  built_in: "Origin",
  is_system: "Origin",
  tile_seed: "Tile seeding",
  tile_truncate: "Tile truncation",
  public: "Public access",
  public_id: "Public ID",
  crs_default: "Default CRS (EPSG code)",
  allowed_roles: "Allowed roles",
  default_style: "Default style",
  styles: "Advertised styles",
  limit_default: "Default page size",
  limit_max: "Maximum page size",
  max_offset: "Maximum offset",
  max_width: "Maximum image width",
  max_height: "Maximum image height",
  max_pixels: "Maximum image pixels",
  count_timeout_ms: "Count timeout (ms)",
  processing_timeout_ms: "Processing timeout (ms)",
  source_srid: "Source CRS (EPSG code)",
  target_srid: "Target CRS (EPSG code)",
  sslmode: "SSL mode",
  connection_info: "Connection",
  cache_settings: "Cache settings",
  role_id: "Role",
  sld_body: "SLD XML",
  tile_cache_quota_bytes: "Tile cache quota",
};
const acronyms = new Set([
  "id",
  "ids",
  "crs",
  "uri",
  "url",
  "api",
  "sql",
  "sld",
  "srid",
  "epsg",
  "wms",
  "wfs",
  "wcs",
  "wmts",
  "oidc",
  "jwt",
  "csv",
  "json",
  "ogc",
]);

/** Sentence-case label for an API field name, keeping acronyms upper case. */
export function fieldLabel(key: string) {
  if (names[key]) return names[key];
  return key
    .split(/[_-]+/)
    .filter(Boolean)
    .map((word, index) => {
      const lower = word.toLowerCase();
      if (acronyms.has(lower))
        return lower.endsWith("s") && lower !== "crs"
          ? lower.slice(0, -1).toUpperCase() + "s"
          : lower.toUpperCase();
      return index === 0
        ? lower.charAt(0).toUpperCase() + lower.slice(1)
        : lower;
    })
    .join(" ");
}

const enumLabels: Record<string, string> = {
  vector: "Vector tiles",
  map: "Map tiles",
  wms: "WMS",
  wfs: "WFS",
  wcs: "WCS",
  wmts: "WMTS",
  png: "PNG",
  jpeg: "JPEG",
  crs84: "CRS84",
};

/** Human label for an enum value; codes with digits or dots stay verbatim. */
export function enumLabel(value: string) {
  const lower = value.toLowerCase();
  if (enumLabels[lower]) return enumLabels[lower];
  if (!/^[a-z][a-z_]*$/.test(value)) return value;
  const text = value.replaceAll("_", " ");
  return text.charAt(0).toUpperCase() + text.slice(1);
}

/** Wording of the empty option when a select has no value yet. */
export const emptyChoiceLabels: Record<string, string> = {
  dataset_map_layer_group_id: "None",
  resource: "Choose a publication…",
  tile_matrix_set: "Server default",
};

const columnNames: Record<string, string> = {
  enabled: "Status",
  public: "Access",
};

/** Table header for a field: states read as Status/Access, not as a verb. */
export function columnLabel(key: string) {
  return columnNames[key] ?? fieldLabel(key);
}

/** Validate before mutations; optional absent fields remain absent on the wire. */
export function validateFields(
  schema: FieldSchema,
  value: unknown,
  path = "",
): string[] {
  const errors: string[] = [];
  if (schema.type === "object") {
    if (!value || typeof value !== "object" || Array.isArray(value))
      return [`${path || "Form"} must be an object.`];
    const record = value as Record<string, unknown>;
    for (const key of schema.required ?? [])
      if (
        record[key] === undefined ||
        record[key] === null ||
        record[key] === ""
      )
        errors.push(`${path}${fieldLabel(key)} is required.`);
    for (const [key, child] of Object.entries(schema.properties ?? {}))
      if (record[key] !== undefined && record[key] !== null)
        errors.push(
          ...validateFields(child, record[key], `${path}${fieldLabel(key)}: `),
        );
  } else if (schema.type === "array") {
    if (!Array.isArray(value)) return [`${path}must be a list.`];
    value.forEach((item, i) =>
      errors.push(
        ...validateFields(schema.items ?? {}, item, `${path}${i + 1}: `),
      ),
    );
  } else if (schema.type === "integer" || schema.type === "number") {
    if (
      typeof value !== "number" ||
      !Number.isFinite(value) ||
      (schema.type === "integer" && !Number.isInteger(value))
    )
      errors.push(`${path}enter a valid ${schema.type}.`);
    else if (
      (schema.minimum !== undefined && value < schema.minimum) ||
      (schema.maximum !== undefined && value > schema.maximum)
    )
      errors.push(
        `${path}must be between ${schema.minimum ?? "−∞"} and ${schema.maximum ?? "∞"}.`,
      );
  } else if (schema.type === "boolean" && typeof value !== "boolean")
    errors.push(`${path}must be true or false.`);
  else if (schema.type === "string" && typeof value !== "string")
    errors.push(`${path}must be text.`);
  if (schema.enum && !schema.enum.includes(value as string))
    errors.push(`${path}choose a supported value.`);
  return errors;
}

export function fieldDefaults(schema: FieldSchema): unknown {
  if (schema.default !== undefined) return schema.default;
  if (schema.type === "object")
    return Object.fromEntries(
      Object.entries(schema.properties ?? {})
        .filter(([key]) => schema.required?.includes(key))
        .map(([key, child]) => [key, fieldDefaults(child)]),
    );
  if (schema.type === "array") return [];
  if (schema.type === "boolean") return false;
  if (schema.type === "number" || schema.type === "integer")
    return schema.minimum ?? 0;
  return schema.enum?.[0] ?? "";
}
