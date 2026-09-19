import { Button } from "@/components/ui/button";
import { copyText } from "@/lib/clipboard";

export function Diagnostics({
  label,
  value,
}: {
  label: string;
  value: unknown;
}) {
  return (
    <details className="my-3 rounded-lg border p-3">
      <summary className="cursor-pointer text-sm">{label}</summary>
      <Button
        size="sm"
        variant="outline"
        className="my-2"
        onClick={() => void copyText(JSON.stringify(value, null, 2))}
      >
        Copy JSON
      </Button>
      <pre
        tabIndex={0}
        aria-label={label}
        className="max-h-64 overflow-auto whitespace-pre-wrap break-all text-xs"
      >
        {JSON.stringify(value, null, 2)}
      </pre>
    </details>
  );
}
