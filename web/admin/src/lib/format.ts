/** Human-readable binary size: 20973 → "20 KiB", 451 → "451 B". */
export function formatBytes(bytes = 0) {
  const units = ["B", "KiB", "MiB", "GiB", "TiB"];
  let value = bytes;
  let unit = 0;
  while (value >= 1024 && unit < units.length - 1) {
    value /= 1024;
    unit++;
  }
  const digits = unit === 0 ? 0 : value < 10 ? 1 : 0;
  return `${value.toLocaleString("en-US", { maximumFractionDigits: digits })} ${units[unit]}`;
}

/**
 * The console's date style: "Sep 16, 2026, 6:34 PM" in the viewer's locale.
 * Logs pass `seconds` so events within one minute stay distinguishable.
 */
export function formatDateTime(
  value: string | number | Date,
  { seconds = false, timeOnly = false } = {},
) {
  const date = value instanceof Date ? value : new Date(value);
  if (Number.isNaN(date.getTime())) return String(value);
  return date.toLocaleString(
    undefined,
    timeOnly
      ? { timeStyle: seconds ? "medium" : "short" }
      : { dateStyle: "medium", timeStyle: seconds ? "medium" : "short" },
  );
}

/** "just now", "5 min ago", "18 h ago", "3 d ago"; future values read "in …". */
export function formatRelative(
  value: string | number | Date,
  now = Date.now(),
) {
  const time = new Date(value).getTime();
  if (Number.isNaN(time)) return String(value);
  const delta = Math.round((time - now) / 1000);
  const abs = Math.abs(delta);
  const [amount, unit] =
    abs < 45
      ? [0, ""]
      : abs < 3600
        ? [Math.round(abs / 60), "min"]
        : abs < 86_400
          ? [Math.round(abs / 3600), "h"]
          : [Math.round(abs / 86_400), "d"];
  if (!unit) return "just now";
  return delta < 0 ? `${amount} ${unit} ago` : `in ${amount} ${unit}`;
}

/** Durations from seconds: 42 → "42 s", 3610 → "1 h 0 min", 90061 → "1 d 1 h". */
export function formatDuration(totalSeconds: number) {
  const s = Math.max(0, Math.round(totalSeconds));
  if (s < 60) return `${s} s`;
  const minutes = Math.floor(s / 60);
  if (minutes < 60) return `${minutes} min ${s % 60} s`;
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return `${hours} h ${minutes % 60} min`;
  return `${Math.floor(hours / 24)} d ${hours % 24} h`;
}

/** Short latency: 0.04 → "<0.1 ms", 12.34 → "12 ms". */
export function formatMilliseconds(ms: number) {
  if (ms < 0.1) return "<0.1 ms";
  return ms < 10 ? `${ms.toFixed(1)} ms` : `${Math.round(ms)} ms`;
}

/** "1 import", "2 imports". */
export function plural(
  count: number,
  singular: string,
  pluralForm = `${singular}s`,
) {
  return `${count.toLocaleString("en-US")} ${count === 1 ? singular : pluralForm}`;
}

const itemKeys = ["resource", "public_id", "name", "id"] as const;

/**
 * Short list form for array values: ["a", "b"] and member objects such as
 * [{ resource: "a" }] both become "a, b". Returns undefined when an item has
 * no readable name, so callers can fall back to JSON.
 */
export function formatList(items: unknown[]): string | undefined {
  const labels: string[] = [];
  for (const item of items) {
    if (typeof item === "string" || typeof item === "number")
      labels.push(String(item));
    else if (item && typeof item === "object") {
      const record = item as Record<string, unknown>;
      const key = itemKeys.find((name) => typeof record[name] === "string");
      if (!key) return undefined;
      labels.push(String(record[key]));
    } else return undefined;
  }
  return labels.join(", ");
}
