import type { ImportJob, ImportPlan } from "@/api/generated/models";

export function planFromDiscovery(job?: ImportJob): ImportPlan {
  return {
    service_name: job?.name || "imported-data",
    layers: (job?.discovery?.layers ?? []).map((layer) => ({
      source_layer: layer.name,
      public_id: String(layer.name ?? "")
        .toLowerCase()
        .replace(/[^a-z0-9]+/g, "-"),
      title: layer.title ?? layer.name,
      enabled: true,
      public: false,
      geometry_column: layer.geometry_column,
      source_srid: layer.srid,
      target_srid: layer.srid,
      fields: (layer.properties ?? []).map((field) => ({
        source: field.name,
        target: field.name,
        type: field.type,
        include: true,
      })),
    })),
  };
}

export function revisionPlan(job: ImportJob, saved: ImportPlan) {
  const available = planFromDiscovery(job).layers;
  const selected = new Map(
    saved.layers.map((layer) => [layer.source_layer, layer]),
  );
  const discovered = new Set(available.map((layer) => layer.source_layer));
  return {
    plan: {
      ...saved,
      layers: [
        ...available.map((layer) => selected.get(layer.source_layer) ?? layer),
        ...saved.layers.filter((layer) => !discovered.has(layer.source_layer)),
      ],
    },
    excluded: new Set(
      available
        .filter((layer) => !selected.has(layer.source_layer))
        .map((layer) => layer.source_layer),
    ),
  };
}
