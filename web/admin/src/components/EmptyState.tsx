import type { ComponentType, ReactNode } from "react";

/** Shared first-run and empty-list panel: what this is and what to do next. */
export function EmptyState({
  icon: Icon,
  title,
  children,
  action,
}: {
  icon?: ComponentType<{ className?: string }>;
  title: string;
  children?: ReactNode;
  action?: ReactNode;
}) {
  return (
    <section
      aria-label={title}
      className="flex flex-col items-start gap-3 rounded-lg border border-dashed p-6 sm:p-8"
    >
      {Icon && (
        <span className="grid size-10 place-items-center rounded-md border bg-muted text-muted-foreground">
          <Icon className="size-5" />
        </span>
      )}
      <h2 className="text-lg font-semibold">{title}</h2>
      {children && (
        <div className="max-w-2xl space-y-2 text-sm text-muted-foreground">
          {children}
        </div>
      )}
      {action && <div className="flex flex-wrap gap-2 pt-1">{action}</div>}
    </section>
  );
}
