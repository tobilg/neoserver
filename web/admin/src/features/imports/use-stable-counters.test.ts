import { renderHook } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { ImportJob } from "@/api/generated/models";
import { useStableCounters } from "./use-stable-counters";

const base = {
  id: "imp-1",
  name: "roads",
  workspace_id: "demo",
  source_kind: "upload",
  created_at: "2026-01-01T00:00:00Z",
  updated_at: "2026-01-01T00:00:00Z",
} as ImportJob;

function job(status: string, bytes: number, features: number) {
  return {
    ...base,
    status,
    phase: "transform",
    processed_bytes: bytes,
    processed_features: features,
    processed_layers: 1,
  } as ImportJob;
}

describe("useStableCounters", () => {
  it("keeps the highest counters while a job reprocesses", () => {
    const { result, rerender } = renderHook(
      ({ current }) => useStableCounters(current),
      { initialProps: { current: job("ready_to_publish", 451, 3) } },
    );
    expect(result.current).toEqual({ bytes: 451, features: 3, layers: 1 });
    rerender({ current: job("running", 451, 0) });
    expect(result.current).toEqual({ bytes: 451, features: 3, layers: 1 });
  });

  it("shows the reported counters once the job settles", () => {
    const { result, rerender } = renderHook(
      ({ current }) => useStableCounters(current),
      { initialProps: { current: job("running", 900, 8) } },
    );
    rerender({ current: job("ready_to_publish", 451, 3) });
    expect(result.current).toEqual({ bytes: 451, features: 3, layers: 1 });
  });
});
