import { describe, expect, it } from "vitest";
import {
  fieldDefaults,
  validateFields,
  type FieldSchema,
} from "./schema-fields";
import {
  editableSchema,
  formSchemas,
  protocolSchema,
  resourceSchema,
} from "./resource-schemas";

describe("curated schema forms", () => {
  it("validates required fields, nested ranges, arrays and booleans", () => {
    const schema: FieldSchema = {
      type: "object",
      required: ["name"],
      properties: {
        name: { type: "string" },
        enabled: { type: "boolean" },
        values: {
          type: "array",
          items: { type: "integer", minimum: 1, maximum: 10 },
        },
      },
    };
    expect(
      validateFields(schema, { name: "", enabled: "true", values: [1.5, 11] }),
    ).toHaveLength(4);
    expect(
      validateFields(schema, {
        name: "roads",
        enabled: false,
        values: [1, 10],
      }),
    ).toEqual([]);
  });
  it("permits unknown settings so an edit does not require deleting them", () => {
    expect(
      validateFields(
        { type: "object", properties: { title: { type: "string" } } },
        { title: "Maps", future: { option: true } },
      ),
    ).toEqual([]);
  });
  it("does not manufacture optional fields when adding an array item", () => {
    expect(
      fieldDefaults({
        type: "object",
        required: ["resource"],
        properties: {
          resource: { type: "string" },
          opacity: { type: "number" },
        },
      }),
    ).toEqual({ resource: "" });
  });
  it("retains native extent, role and style controls while hiding server metadata", () => {
    const schema = editableSchema(formSchemas.Layer);
    expect(schema.properties).toHaveProperty("native_extent");
    expect(schema.properties).toHaveProperty("allowed_roles");
    expect(schema.properties).toHaveProperty("default_style");
    expect(schema.properties).not.toHaveProperty("created_at");
    expect(schema.properties).not.toHaveProperty("id");
  });
  it("provides defined connection fields for all six store types", () => {
    for (const type of [
      "postgis",
      "duckdb",
      "geoparquet",
      "vectorfile",
      "rasterfile",
      "raster_mosaic",
    ]) {
      const fields = resourceSchema("services", { type })?.properties
        ?.connection_info.properties;
      expect(fields).toBeDefined();
      expect(Object.values(fields!).every(Boolean)).toBe(true);
    }
  });
  it("keeps activation separate and rejects negative protocol limits", () => {
    const schema = protocolSchema("WMSSettings");
    expect(schema.properties).not.toHaveProperty("enabled");
    expect(validateFields(schema, { max_width: -1 }).length).toBeGreaterThan(0);
  });
});
