/**
 * The console's shared display vocabulary. Every page names the same state
 * the same way (see the round-3 review, concept C2).
 */
export { formatBytes } from "./format";

const statusLabels: Record<string, string> = {
  enabled: "On",
  disabled: "Off",
  "store disabled": "Store off",
  private: "Restricted",
  restricted: "Restricted",
  public: "Public",
  ok: "OK",
  not_ready: "Not ready",
};

/** Sentence-case label for a raw status value: "not_ready" → "Not ready". */
export function statusLabel(value: string) {
  const normalized = value.toLowerCase();
  if (statusLabels[normalized]) return statusLabels[normalized];
  const text = value.replaceAll("_", " ");
  return text.charAt(0).toUpperCase() + text.slice(1);
}

export type StatusTone =
  "running" | "success" | "failed" | "warning" | "accent" | "neutral";

const tones: Record<string, StatusTone> = {
  running: "running",
  publishing: "running",
  rolling_back: "running",
  cancelling: "running",
  queued: "running",
  published: "success",
  complete: "success",
  completed: "success",
  succeeded: "success",
  healthy: "success",
  enabled: "success",
  on: "success",
  ready: "success",
  ok: "success",
  active: "success",
  current: "success",
  failed: "failed",
  unhealthy: "failed",
  not_ready: "failed",
  unavailable: "failed",
  expired: "warning",
  "store disabled": "warning",
  public: "accent",
};

export function statusTone(value: string): StatusTone {
  const normalized = value.toLowerCase();
  if (/^\d+ running$/.test(normalized)) return "running";
  return tones[normalized] ?? "neutral";
}

/** Field-aware wording for booleans; generic fields read Yes/No. */
export function booleanLabel(field: string, value: boolean) {
  switch (field) {
    case "enabled":
      return value ? "On" : "Off";
    case "public":
      return value ? "Public" : "Restricted";
    case "revoked":
      return value ? "Revoked" : "Active";
    case "built_in":
    case "is_system":
      return value ? "Built-in" : "Custom";
    default:
      return value ? "Yes" : "No";
  }
}
