import { isRouteErrorResponse, useNavigate, useRouteError } from "react-router";
import { AlertTriangle, RotateCcw } from "lucide-react";
import { ApiError } from "@/api/client";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";

interface Described {
  title: string;
  detail?: string;
  /** Shown in mono so it can be matched against server logs and the audit log. */
  reference?: string;
  stack?: string;
}

function describe(error: unknown): Described {
  if (error instanceof ApiError) {
    return {
      title: `${error.status} · ${error.message}`,
      detail: error.detail,
      reference: error.code === undefined ? undefined : String(error.code),
    };
  }
  if (isRouteErrorResponse(error)) {
    return {
      title: `${error.status} · ${error.statusText}`,
      detail: typeof error.data === "string" ? error.data : undefined,
    };
  }
  if (error instanceof Error) {
    return {
      title: "This screen stopped unexpectedly",
      detail: error.message,
      // Only in dev: a stack trace is noise for an operator and can leak
      // internals in a deployed console.
      stack: import.meta.env.DEV ? error.stack : undefined,
    };
  }
  return { title: "This screen stopped unexpectedly" };
}

/**
 * Route-level error boundary.
 *
 * Mounted on the workspace and server layouts so a screen that throws leaves
 * the shell and its navigation usable, and once at the root for failures that
 * take the shell with them.
 */
export function RouteError({ scope = "screen" }: { scope?: "screen" | "app" }) {
  const error = useRouteError();
  const navigate = useNavigate();
  const { title, detail, reference, stack } = describe(error);

  return (
    <div className={scope === "app" ? "p-8" : "p-6 lg:p-8"}>
      <Card className="mx-auto max-w-2xl">
        <CardHeader>
          <CardTitle className="flex items-center gap-2">
            <AlertTriangle className="size-5 text-destructive" />
            {title}
          </CardTitle>
        </CardHeader>
        <CardContent className="space-y-4">
          {detail && <p className="text-sm text-muted-foreground">{detail}</p>}
          {reference && (
            <p className="text-xs text-muted-foreground">
              Reference <span className="font-mono">{reference}</span>
            </p>
          )}
          {stack && (
            <pre className="max-h-64 overflow-auto rounded-lg border bg-muted p-3 text-xs">
              {stack}
            </pre>
          )}
          <div className="flex gap-2">
            <Button onClick={() => navigate(0)}>
              <RotateCcw /> Reload this screen
            </Button>
            {scope === "screen" && (
              <Button variant="outline" onClick={() => navigate("/")}>
                Back to the console
              </Button>
            )}
          </div>
        </CardContent>
      </Card>
    </div>
  );
}
