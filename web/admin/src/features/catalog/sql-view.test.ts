import { describe, expect, it } from "vitest";
import { publishBlocker, suggestPublicID } from "./sql-view";

describe("suggestPublicID", () => {
  it("uses the first table name without its schema", () => {
    expect(suggestPublicID("SELECT id, geom FROM active_roads")).toBe(
      "active_roads",
    );
    expect(
      suggestPublicID('select * from "Public"."Road Segments" where x = 1'),
    ).toBe("road_segments");
  });

  it("returns nothing when no usable table is present", () => {
    expect(suggestPublicID("SELECT 1")).toBe("");
    expect(suggestPublicID("SELECT * FROM read_parquet('x')")).toBe("");
    expect(suggestPublicID('SELECT * FROM "123"')).toBe("");
  });
});

describe("publishBlocker", () => {
  it("names the first missing step", () => {
    const ready = { validated: true, idColumn: "id", publicID: "roads" };
    expect(publishBlocker({ ...ready, validated: false })).toMatch(/Validate/);
    expect(publishBlocker({ ...ready, idColumn: "" })).toMatch(/feature ID/);
    expect(publishBlocker({ ...ready, publicID: "" })).toBe(
      "Add a public layer ID to publish.",
    );
    expect(publishBlocker({ ...ready, publicID: "1x" })).toMatch(/Fix/);
    expect(publishBlocker(ready)).toBe("");
  });
});
