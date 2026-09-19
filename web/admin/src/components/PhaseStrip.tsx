import { cn } from "@/lib/utils";

const phases = [
  "acquire",
  "discover",
  "validate",
  "transform",
  "preview",
  "publish",
] as const;

export function PhaseStrip({
  phase,
  failed = false,
  complete = false,
}: {
  phase: string;
  failed?: boolean;
  /** Every phase finished; the last one is shown as done, not in progress. */
  complete?: boolean;
}) {
  const active = complete
    ? phases.length
    : Math.max(0, phases.indexOf(phase as (typeof phases)[number]));
  return (
    <div
      className="grid grid-cols-6 gap-1"
      aria-label={
        complete ? "Import phases complete" : `Import phase: ${phase}`
      }
    >
      {phases.map((value, index) => (
        <div key={value} className="min-w-0">
          <div
            className={cn(
              "mb-1 h-1.5 rounded-full",
              index < active && "bg-success",
              index === active && (failed ? "bg-destructive" : "bg-brand"),
              index > active && "bg-muted",
            )}
          />
          <span
            className={cn(
              "block truncate text-xs text-muted-foreground",
              index === active && "font-medium text-foreground",
            )}
          >
            {value}
          </span>
        </div>
      ))}
    </div>
  );
}
