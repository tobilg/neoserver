import { Button } from "@/components/ui/button";

function errorText(error: unknown): string {
  if (error instanceof Error) {
    const detail = "detail" in error ? error.detail : undefined;
    return [error.message, detail].filter(Boolean).join(": ");
  }
  return "The request could not be completed.";
}

export function QueryError({
  error,
  retry,
  context = "Data could not be loaded",
}: {
  error: unknown;
  retry?: () => unknown;
  context?: string;
}) {
  if (!error) return null;
  const forbidden =
    typeof error === "object" && "status" in error && error.status === 403;
  return (
    <div
      role="alert"
      className="my-3 rounded-lg border border-destructive/40 bg-destructive/5 p-4 text-sm"
    >
      <p className="font-medium">
        {forbidden
          ? "You do not have permission to view this resource."
          : context}
      </p>
      <p className="mt-1 whitespace-pre-wrap">{errorText(error)}</p>
      {retry && (
        <Button
          className="mt-2"
          size="sm"
          variant="outline"
          onClick={() => void retry()}
        >
          Retry
        </Button>
      )}
    </div>
  );
}
