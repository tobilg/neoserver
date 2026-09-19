import { basePath } from "@/api/client";
import { POLL_READINESS_MS } from "@/hooks/use-active-polling";

export interface Readiness {
  status: "ready" | "not_ready";
  checks: Record<string, string>;
  durations_ms: Record<string, number>;
}

export async function fetchReadiness(): Promise<Readiness> {
  const response = await fetch(`${basePath}/ready`, { cache: "no-store" });
  const data = (await response.json()) as Readiness;
  // A valid 503 is a useful diagnostic, not a transport failure.
  if (
    (!response.ok && response.status !== 503) ||
    (data.status !== "ready" && data.status !== "not_ready") ||
    !data.checks ||
    typeof data.checks !== "object" ||
    !data.durations_ms ||
    typeof data.durations_ms !== "object" ||
    (response.status === 503 && data.status !== "not_ready")
  ) {
    throw new Error(`Readiness unavailable (${response.status})`);
  }
  return data;
}

export const readinessQuery = {
  queryKey: ["readiness"],
  queryFn: fetchReadiness,
  refetchInterval: POLL_READINESS_MS,
  retry: false,
};

export async function fetchLiveness(): Promise<{ status: "ok" }> {
  const response = await fetch(`${basePath}/health`, { cache: "no-store" });
  const data = (await response.json()) as { status: "ok" };
  if (!response.ok || data.status !== "ok")
    throw new Error(`Liveness unavailable (${response.status})`);
  return data;
}
