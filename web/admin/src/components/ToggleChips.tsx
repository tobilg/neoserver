import { Check } from "lucide-react";
import { cn } from "@/lib/utils";

export interface ChipOption {
  value: string;
  label: string;
}

/**
 * Multi-select as a row of toggle chips. Each chip is a real checkbox, so it
 * works with keyboard, forms and screen readers.
 */
export function ToggleChips({
  options,
  value,
  onChange,
  disabled,
  className,
}: {
  options: ChipOption[];
  value: string[];
  onChange: (value: string[]) => void;
  disabled?: boolean;
  className?: string;
}) {
  // Keep selected values the options no longer list, so saving doesn't drop them.
  const all = [
    ...options,
    ...value
      .filter((item) => !options.some((option) => option.value === item))
      .map((item) => ({ value: item, label: `${item} (current)` })),
  ];
  return (
    <div className={cn("flex flex-wrap gap-2", className)}>
      {all.map((option) => {
        const checked = value.includes(option.value);
        return (
          <label
            key={option.value}
            className={cn(
              "relative inline-flex min-h-8 cursor-pointer items-center gap-1.5 rounded-full border px-3 text-sm transition-colors select-none",
              "has-focus-visible:outline-2 has-focus-visible:outline-offset-2 has-focus-visible:outline-ring",
              checked
                ? "border-primary bg-primary text-primary-foreground"
                : "bg-background hover:bg-muted",
              disabled && "cursor-not-allowed opacity-60",
            )}
          >
            <input
              type="checkbox"
              // Covers the chip, so clicks and focus land on the real input.
              className="absolute inset-0 m-0 size-full cursor-[inherit] appearance-none rounded-full opacity-0"
              checked={checked}
              disabled={disabled}
              onChange={(event) =>
                onChange(
                  event.target.checked
                    ? [...value, option.value]
                    : value.filter((item) => item !== option.value),
                )
              }
            />
            {checked && <Check aria-hidden="true" className="size-3.5" />}
            {option.label}
          </label>
        );
      })}
    </div>
  );
}
