import { Link } from "react-router";
import type { ServiceStateValue } from "./services";
import { cn } from "@/lib/utils";

const chip =
  "inline-flex items-center gap-1.5 whitespace-nowrap rounded-md px-2 py-0.5 text-xs font-medium";

/** Status chip that links to whatever turns the service on. */
export function ServiceState({
  state,
  env,
  base,
}: {
  state: ServiceStateValue;
  env?: string;
  /** Workspace path; links resolve against the current page without it. */
  base?: string;
}) {
  const to = (path: string) => (base ? `${base}/${path}` : path);
  switch (state) {
    case "on":
      return (
        <span
          className={cn(chip, "bg-success/10 text-green-800 dark:text-success")}
        >
          <span className="size-1.5 rounded-full bg-current" />
          On
        </span>
      );
    case "on_no_publications":
      return (
        <Link
          to={to("layers")}
          className={cn(
            chip,
            "bg-brand/10 text-brand-strong underline-offset-4 hover:underline",
          )}
        >
          On · no publications
        </Link>
      );
    case "off_workspace":
      return (
        <Link
          to={to("settings")}
          className={cn(
            chip,
            "bg-muted text-foreground/75 underline-offset-4 hover:underline",
          )}
        >
          Off · turn on
        </Link>
      );
    case "off_server":
      return (
        <span
          className={cn(chip, "bg-warning/10 text-warning")}
          title={
            env
              ? `Set ${env}=true and restart the server to enable it.`
              : undefined
          }
        >
          Not enabled on server
          {env && <span className="sr-only"> (set {env}=true)</span>}
        </span>
      );
  }
}
