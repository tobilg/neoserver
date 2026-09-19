import { Link } from "react-router";
import { CheckCircle2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import { StatusChip } from "@/components/StatusChip";
import type { ImportJob } from "@/api/generated/models";
import { useListLayers } from "@/api/generated/layers/layers";
import { QueryError } from "@/components/QueryError";

/** Resolve current publications from this import's store, never the workspace's first layer. */
export function PublishedImportResults({
  workspace,
  job,
}: {
  workspace: string;
  job: ImportJob;
}) {
  const layers = useListLayers(workspace, job.service_id ?? "", {
    query: { enabled: Boolean(job.service_id) },
  });
  const root = `/workspaces/${encodeURIComponent(workspace)}`;
  const scope = new URLSearchParams({ store: job.service_id ?? "" });
  // Importer materializes each planned public ID as its source table name.
  // Joining on source_layer survives later public-ID/title edits.
  const sources = new Set(job.plan?.layers.map((layer) => layer.public_id));
  const publications = (layers.data?.layers ?? []).filter((layer) =>
    sources.has(layer.source_layer ?? ""),
  );
  if (!job.service_id)
    return (
      <p role="status">
        The import's created store is unavailable. Refresh the import to check
        its publication results.
      </p>
    );
  const privateCount = publications.filter((layer) => !layer.public).length;
  return (
    <div className="space-y-3 rounded-lg border border-success/40 bg-success/5 p-4">
      <div className="space-y-1">
        <p className="flex items-center gap-2 font-medium">
          <CheckCircle2 className="size-5 text-success" />
          {publications.length
            ? `Published ${publications.length} ${publications.length === 1 ? "layer" : "layers"}${job.processed_features ? ` · ${job.processed_features.toLocaleString()} features` : ""}`
            : "Import published"}
        </p>
        {publications.length > 0 && (
          <p className="text-sm text-muted-foreground">
            {privateCount
              ? "New layers are private: check them on the map, then give a client access with an API key."
              : "Check the result on the map, then connect a client."}
          </p>
        )}
      </div>
      <QueryError
        error={layers.error}
        retry={() => layers.refetch()}
        context="Published import results could not be loaded"
      />
      {layers.isLoading && (
        <p role="status">Loading published import results…</p>
      )}
      {!layers.isLoading && !layers.error && publications.length === 0 && (
        <p role="status">
          No matching layers remain in this import's store. They may have been
          unpublished; check the store's publications.
        </p>
      )}
      {!layers.error && publications.length > 0 && (
        <ul
          aria-label="Published import layers"
          className="divide-y rounded-md border bg-background"
        >
          {publications.map((layer, index) => (
            <li
              key={layer.id}
              className="flex flex-wrap items-center gap-2 p-3 text-sm"
            >
              <span className="min-w-0 flex-1 break-all font-mono">
                {layer.public_id}
              </span>
              <StatusChip value={layer.public ? "public" : "private"} />
              <Button
                size="sm"
                variant={index === 0 ? "default" : "outline"}
                asChild
              >
                <Link
                  to={`${root}/preview?${new URLSearchParams({ layers: layer.public_id })}`}
                >
                  Preview on map
                </Link>
              </Button>
              <Button size="sm" variant="outline" asChild>
                <Link
                  to={`${root}/endpoints?${new URLSearchParams({ layer: layer.public_id })}`}
                >
                  Connect a client
                </Link>
              </Button>
            </li>
          ))}
        </ul>
      )}
      <p className="flex flex-wrap gap-3 text-sm">
        <Link className="underline" to={`${root}/stores?${scope}`}>
          View created store
        </Link>
        <Link className="underline" to={`${root}/layers?${scope}`}>
          View published layers
        </Link>
      </p>
    </div>
  );
}
