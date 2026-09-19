import { Link } from "react-router";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { StatusChip } from "@/components/StatusChip";
import { useCatalogChoices } from "@/features/catalog/use-catalog-choices";

const kindLabels = {
  feature: "layer",
  coverage: "coverage",
  group: "layer group",
} as const;

/** The publications a developer is most likely coming back to. */
export function RecentlyPublished({
  workspace,
  className,
}: {
  workspace: string;
  className?: string;
}) {
  const catalog = useCatalogChoices(workspace);
  const root = `/workspaces/${encodeURIComponent(workspace)}`;
  const recent = [...(catalog.allResources ?? [])]
    .sort(
      (a, b) =>
        (Date.parse(String((b as { updated_at?: string }).updated_at ?? "")) ||
          0) -
        (Date.parse(String((a as { updated_at?: string }).updated_at ?? "")) ||
          0),
    )
    .slice(0, 5);
  return (
    <Card className={className}>
      <CardHeader>
        <CardTitle>Recently published</CardTitle>
      </CardHeader>
      <CardContent>
        {catalog.isLoading && <p role="status">Loading publications…</p>}
        {!catalog.isLoading && recent.length === 0 && (
          <p className="text-sm text-muted-foreground">
            Nothing published yet. Add a store or upload a file to start.
          </p>
        )}
        <ul className="divide-y">
          {recent.map((resource) => (
            <li
              key={`${resource.kind}-${resource.public_id}`}
              className="grid grid-cols-[minmax(0,1fr)_auto] items-center gap-x-3 gap-y-1 py-2 text-sm sm:grid-cols-[minmax(0,1fr)_auto_auto]"
            >
              <span className="min-w-0">
                <span className="flex items-center gap-2">
                  <span className="truncate font-medium">
                    {resource.title || resource.public_id}
                  </span>
                  {resource.enabled === false && (
                    <StatusChip value="disabled" />
                  )}
                </span>
                <span className="block truncate font-mono text-xs text-muted-foreground">
                  {resource.public_id} · {kindLabels[resource.kind]}
                </span>
              </span>
              <Button size="sm" variant="outline" asChild>
                <Link
                  to={`${root}/preview?${new URLSearchParams({
                    layers: resource.public_id,
                    ...(resource.kind === "feature" ? {} : { sources: "wms" }),
                  })}`}
                  aria-label={`Preview ${resource.public_id}`}
                >
                  Preview
                </Link>
              </Button>
              {/* A fixed slot keeps the action column aligned for groups. */}
              <span className="col-start-2 sm:col-start-auto">
                {resource.kind === "feature" ? (
                  <Button size="sm" variant="ghost" asChild>
                    <Link
                      to={`${root}/endpoints?${new URLSearchParams({ layer: resource.public_id })}`}
                      aria-label={`Connect to ${resource.public_id}`}
                    >
                      Connect
                    </Link>
                  </Button>
                ) : (
                  <span
                    className="inline-block w-[4.75rem]"
                    aria-hidden="true"
                  />
                )}
              </span>
            </li>
          ))}
        </ul>
      </CardContent>
    </Card>
  );
}
