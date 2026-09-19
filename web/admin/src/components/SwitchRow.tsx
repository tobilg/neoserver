import { useId, type ReactNode } from "react";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import { cn } from "@/lib/utils";

/**
 * A labelled on/off setting: label and optional help on the left, the switch
 * on the right. The help text is announced as the switch's description.
 */
export function SwitchRow({
  id,
  label,
  description,
  checked,
  onCheckedChange,
  disabled,
  stateLabel,
  className,
  labelClassName,
  "aria-label": ariaLabel,
}: {
  id?: string;
  label: ReactNode;
  description?: ReactNode;
  checked: boolean;
  onCheckedChange: (checked: boolean) => void;
  disabled?: boolean;
  /** Shows the current state under the switch, e.g. "Enabled" / "Disabled". */
  stateLabel?: [on: string, off: string];
  className?: string;
  labelClassName?: string;
  "aria-label"?: string;
}) {
  const generated = useId();
  const switchID = id ?? generated;
  const descriptionID = `${switchID}-description`;
  return (
    <div className={cn("flex items-center justify-between gap-4", className)}>
      <div className="min-w-0">
        <Label htmlFor={switchID} className={labelClassName}>
          {label}
        </Label>
        {description && (
          <div
            id={descriptionID}
            className="mt-1 text-sm text-muted-foreground"
          >
            {description}
          </div>
        )}
      </div>
      <div className="flex shrink-0 flex-col items-center gap-1">
        <Switch
          id={switchID}
          aria-label={ariaLabel}
          aria-describedby={description ? descriptionID : undefined}
          checked={checked}
          disabled={disabled}
          onCheckedChange={onCheckedChange}
        />
        {stateLabel && (
          <span className="text-xs text-muted-foreground">
            {checked ? stateLabel[0] : stateLabel[1]}
          </span>
        )}
      </div>
    </div>
  );
}
