import { useState } from "react";
import { Input } from "@/components/ui/input";
import { NativeSelect } from "@/components/NativeSelect";

const units = [
  ["MiB", 1024 ** 2],
  ["GiB", 1024 ** 3],
] as const;

/** A size field entered in MiB/GiB and stored in bytes. */
export function ByteSizeInput({
  id,
  value,
  onChange,
  disabled,
  required,
}: {
  id: string;
  value: number | undefined;
  onChange: (bytes: number | undefined) => void;
  disabled?: boolean;
  required?: boolean;
}) {
  const [unit, setUnit] = useState<number>(() =>
    value !== undefined && value >= 1024 ** 3 && value % 1024 ** 3 === 0
      ? 1024 ** 3
      : 1024 ** 2,
  );
  const shown =
    value === undefined ? "" : String(Math.round((value / unit) * 1000) / 1000);
  return (
    <div className="flex gap-2">
      <Input
        id={id}
        type="number"
        min={0}
        step="any"
        className="min-w-0 flex-1"
        disabled={disabled}
        required={required}
        value={shown}
        onChange={(event) =>
          onChange(
            event.target.value === ""
              ? undefined
              : Math.round(Number(event.target.value) * unit),
          )
        }
      />
      <NativeSelect
        aria-label="Unit"
        className="w-24"
        disabled={disabled}
        value={unit}
        onChange={(event) => setUnit(Number(event.target.value))}
      >
        {units.map(([label, factor]) => (
          <option key={label} value={factor}>
            {label}
          </option>
        ))}
      </NativeSelect>
    </div>
  );
}
