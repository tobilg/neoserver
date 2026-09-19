import { useState } from "react";
import type { ImportJob } from "@/api/generated/models";
import { isActiveStatus } from "@/hooks/use-active-polling";

export interface Counters {
  bytes: number;
  features: number;
  layers: number;
}

/**
 * Keeps the highest observed counters while a revision reprocesses the source,
 * so the numbers don't drop to zero and climb again.
 */
export function useStableCounters(job: ImportJob | undefined): Counters {
  const next: Counters = {
    bytes: job?.processed_bytes ?? 0,
    features: job?.processed_features ?? 0,
    layers: job?.processed_layers ?? 0,
  };
  const running = isActiveStatus(job?.status);
  const [peak, setPeak] = useState(next);
  const shown: Counters = running
    ? {
        bytes: Math.max(next.bytes, peak.bytes),
        features: Math.max(next.features, peak.features),
        layers: Math.max(next.layers, peak.layers),
      }
    : next;
  if (
    shown.bytes !== peak.bytes ||
    shown.features !== peak.features ||
    shown.layers !== peak.layers
  )
    setPeak(shown);
  return shown;
}
