import { describe, expect, it } from "vitest";
import { jobTitle } from "./job-title";

describe("jobTitle", () => {
  it("describes what a job does", () => {
    expect(
      jobTitle({
        operation: "seed",
        resource: "ortho",
        min_zoom: 0,
        max_zoom: 6,
        format: "image/png",
      }),
    ).toBe("Seed · ortho · z0–6 · PNG");
    expect(jobTitle({ operation: "truncate", all_resources: true })).toBe(
      "Truncate · all resources",
    );
    expect(jobTitle(undefined)).toBe("Tile-cache job");
  });
});
