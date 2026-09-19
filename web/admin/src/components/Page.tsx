import { useEffect, type ReactNode } from "react";

export function Page({
  title,
  description,
  action,
  children,
  embedded = false,
}: {
  title: string;
  description?: string;
  action?: ReactNode;
  children: ReactNode;
  embedded?: boolean;
}) {
  const Heading = embedded ? "h2" : "h1";
  useEffect(() => {
    if (!embedded) document.title = `${title} · neoserver`;
  }, [embedded, title]);
  return (
    <div
      className={
        embedded
          ? "min-w-0"
          : "mx-auto min-w-0 w-full max-w-[1600px] p-4 sm:p-6 lg:p-8"
      }
    >
      <header className="mb-6 flex flex-wrap items-start justify-between gap-4">
        <div className="space-y-1">
          <Heading
            tabIndex={-1}
            className="m-0 text-2xl font-semibold tracking-tight"
          >
            {title}
          </Heading>
          {description && (
            <p className="max-w-3xl text-sm text-muted-foreground">
              {description}
            </p>
          )}
        </div>
        {action}
      </header>
      {children}
    </div>
  );
}
