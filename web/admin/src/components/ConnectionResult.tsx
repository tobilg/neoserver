import { connectionMessage } from "@/lib/connection-errors";

export function ConnectionResult({
  ok,
  detail,
  allowedPaths,
}: {
  ok: boolean;
  detail: string;
  allowedPaths?: string[];
}) {
  const message = ok
    ? `${detail}. The store is ready to add.`
    : connectionMessage(detail, allowedPaths);
  return (
    <div className="space-y-2">
      <p
        role="status"
        className={
          ok
            ? "text-sm text-success"
            : "text-sm text-red-700 dark:text-destructive"
        }
      >
        {message}
      </p>
      {!ok && (
        <details>
          <summary className="cursor-pointer text-sm">
            Technical details
          </summary>
          <pre className="mt-2 max-h-48 overflow-auto whitespace-pre-wrap break-all text-xs">
            {detail}
          </pre>
        </details>
      )}
    </div>
  );
}
