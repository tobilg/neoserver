import { isValidElement } from "react";
import { StatusChip } from "@/components/StatusChip";
import { cn } from "@/lib/utils";
import { fieldLabel } from "@/lib/schema-fields";
import { formatDateTime, formatList } from "@/lib/format";
import { booleanLabel } from "@/lib/display";

export function DisplayValue({
  value,
  field = "",
}: {
  value: unknown;
  field?: string;
}) {
  if (value === null || value === undefined || value === "")
    return (
      <span className="text-muted-foreground" aria-label="Not set">
        —
      </span>
    );
  if (typeof value === "boolean") {
    if (field === "enabled")
      return <StatusChip value={value ? "enabled" : "disabled"} />;
    if (field === "public")
      return <StatusChip value={value ? "public" : "restricted"} />;
    return <span>{booleanLabel(field, value)}</span>;
  }
  if (Array.isArray(value)) {
    const list = formatList(value);
    if (list === "") return <span className="text-muted-foreground">None</span>;
    if (list !== undefined)
      return (
        <span
          className={
            /(members|resources|layers|styles)$/.test(field)
              ? "break-words font-mono text-xs"
              : "break-words"
          }
        >
          {list}
        </span>
      );
  }
  if (isValidElement(value)) return value;
  if (typeof value === "object")
    return (
      <pre className="max-h-48 overflow-auto whitespace-pre-wrap break-all text-xs">
        {JSON.stringify(value, null, 2)}
      </pre>
    );
  if (
    typeof value === "string" &&
    /^(created_at|updated_at|expires_at|last_seen_at|timestamp)$/.test(field) &&
    !Number.isNaN(Date.parse(value))
  )
    return (
      <time dateTime={value} title={value}>
        {formatDateTime(value)}
      </time>
    );
  if (
    typeof value === "string" &&
    /^(queued|running|published|failed|cancelled|enabled|disabled|ready)$/.test(
      value,
    )
  )
    return <StatusChip value={value} />;
  const text = String(value);
  const code = /(^id$|_id$|prefix|^crs$|^uri$)/.test(field);
  return (
    <span
      className={cn(
        code && "font-mono text-xs",
        // Short identifiers keep their words intact so table columns size to
        // them; long unbroken values (UUIDs, URIs) may wrap anywhere.
        code && (text.length > 24 || field === "uri")
          ? "break-all"
          : "break-words",
      )}
    >
      {text}
    </span>
  );
}

export function ResourceDetails({
  value,
  raw = value,
}: {
  value: Record<string, unknown>;
  /** Shown under "Raw JSON"; defaults to the displayed value. */
  raw?: unknown;
}) {
  return (
    <>
      <dl className="grid gap-3 sm:grid-cols-2">
        {Object.entries(value).map(([field, item]) => (
          <div className="min-w-0" key={field}>
            <dt className="text-xs font-medium text-muted-foreground">
              {fieldLabel(field)}
            </dt>
            <dd className="mt-1 text-sm">
              <DisplayValue value={item} field={field} />
            </dd>
          </div>
        ))}
      </dl>
      <details className="mt-4">
        <summary className="cursor-pointer text-sm">Raw JSON</summary>
        <pre
          tabIndex={0}
          aria-label="Raw resource JSON"
          className="mt-2 max-h-64 overflow-auto whitespace-pre-wrap break-all text-xs"
        >
          {JSON.stringify(raw, null, 2)}
        </pre>
      </details>
    </>
  );
}
