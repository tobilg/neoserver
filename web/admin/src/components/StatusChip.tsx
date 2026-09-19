import { cn } from "@/lib/utils";
import { statusLabel, statusTone } from "@/lib/display";

const toneClass = {
  running: "bg-brand/10 text-brand-strong",
  success: "bg-success/10 text-green-800 dark:text-success",
  failed: "bg-destructive/10 text-red-700 dark:text-destructive",
  warning: "bg-warning/15 text-amber-800 dark:text-warning",
  accent: "bg-primary/10 text-foreground",
  neutral: "bg-muted text-foreground/75",
} as const;

/**
 * A state as a soft pill. Raw values ("enabled", "not_ready") are shown in
 * the shared vocabulary ("On", "Not ready"); `label` overrides the text.
 */
export function StatusChip({
  value,
  label,
}: {
  value: string;
  label?: string;
}) {
  const tone = statusTone(value);
  return (
    <span
      data-status={value}
      className={cn(
        // Soft-filled pill on the stock radius, matching the badge treatment
        // the rest of the component set uses.
        "inline-flex items-center gap-1.5 rounded-md px-2 py-0.5 text-xs font-medium whitespace-nowrap",
        toneClass[tone],
      )}
    >
      <span
        className={cn(
          "size-1.5 rounded-full bg-current",
          tone === "running" && "motion-safe:animate-pulse",
        )}
      />
      {label ?? statusLabel(value)}
    </span>
  );
}
