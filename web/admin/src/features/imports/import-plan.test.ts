import { describe, expect, it } from "vitest";
import { planFromDiscovery, revisionPlan } from "./import-plan";
import type { ImportJob } from "@/api/generated/models";

describe("import revision selection", () => {
  it("restores excluded source choices while preserving selected transformations", () => {
    const job = {
      name: "dataset",
      discovery: { layers: [{ name: "first" }, { name: "second" }] },
    } as ImportJob;
    const initial = planFromDiscovery(job);
    const saved = {
      service_name: "custom",
      layers: [
        { ...initial.layers[0], public_id: "edited", target_srid: 3857 },
      ],
    };
    const { plan, excluded } = revisionPlan(job, saved);
    expect(plan.layers).toHaveLength(2);
    expect(plan.layers[0]).toEqual(saved.layers[0]);
    expect(excluded).toEqual(new Set(["second"]));
    excluded.delete("second");
    expect(
      plan.layers.filter((layer) => !excluded.has(layer.source_layer)),
    ).toHaveLength(2);
    expect(revisionPlan(job, initial).excluded.size).toBe(0);
  });
});
