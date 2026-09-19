import { useState } from "react";
import { Link } from "react-router";
import { CheckCircle2 } from "lucide-react";
import type { WorkspaceSummary } from "@/api/generated/models";
import { useListServices } from "@/api/generated/services/services";
import { useAuth } from "@/auth/auth-context";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { StoreCreateDialog } from "@/features/catalog/StoreCreateDialog";
import { LayerDiscoveryDialog } from "@/features/catalog/LayerDiscoveryDialog";

/** A numbered step heading that shows a check once the catalog proves it done. */
function StepHeading({
  number,
  done,
  children,
}: {
  number: number;
  done?: boolean;
  children: string;
}) {
  return (
    <h3 className="flex items-center gap-2 font-medium">
      {done ? (
        <CheckCircle2 aria-hidden="true" className="size-5 text-success" />
      ) : (
        <span
          aria-hidden="true"
          className="grid size-5 place-items-center rounded-full border text-xs"
        >
          {number}
        </span>
      )}
      <span className={done ? "text-muted-foreground" : undefined}>
        {number}. {children}
      </span>
      {done && <span className="sr-only">(done)</span>}
    </h3>
  );
}

/** Catalog state is authoritative; dismissal stores no credentials or completion claims. */
export function GettingStarted({
  workspace,
  summary,
  onDirty,
}: {
  workspace: string;
  summary: WorkspaceSummary;
  onDirty: (dirty: boolean) => void;
}) {
  const { config } = useAuth();
  const services = useListServices(workspace);
  const [expanded, setExpanded] = useState(false);
  const storageKey = `neoserver:getting-started:${summary.workspace_id || workspace}`;
  const [dismissed, setDismissed] = useState(() => {
    try {
      return localStorage.getItem(storageKey) === "hidden";
    } catch {
      return false;
    }
  });
  function dismiss(value: boolean) {
    setDismissed(value);
    try {
      localStorage.setItem(storageKey, value ? "hidden" : "visible");
    } catch {
      /* Optional preference only. */
    }
  }
  const root = `/workspaces/${encodeURIComponent(workspace)}`;
  const publications =
    (summary.counts.layers ?? 0) + (summary.counts.coverages ?? 0);
  if (dismissed)
    return (
      <Button variant="outline" className="mb-4" onClick={() => dismiss(false)}>
        Show getting-started guide
      </Button>
    );
  return (
    <Card className="mb-4">
      <CardHeader className="flex flex-wrap items-center justify-between gap-2">
        <CardTitle>
          {publications
            ? "Your workspace is publishing"
            : summary.counts.services
              ? "Next: discover and publish"
              : "Publish your first dataset"}
        </CardTitle>
        <Button variant="ghost" size="sm" onClick={() => dismiss(true)}>
          Hide guide
        </Button>
      </CardHeader>
      <CardContent>
        {publications > 0 && (
          <div className="mb-3 space-y-3">
            <p className="text-sm">
              {publications} publication{publications === 1 ? "" : "s"}{" "}
              configured. Verify the preview and client access before sharing
              URLs.
            </p>
            <div className="flex flex-wrap gap-2">
              <Button asChild size="sm">
                <Link to={`${root}/endpoints`}>Connect a client</Link>
              </Button>
              <Button asChild size="sm" variant="outline">
                <Link to={`${root}/preview`}>Open preview</Link>
              </Button>
              <Button
                size="sm"
                variant="ghost"
                aria-expanded={expanded}
                onClick={() => setExpanded(!expanded)}
              >
                {expanded ? "Collapse guide" : "Review publishing guide"}
              </Button>
            </div>
          </div>
        )}
        <div hidden={publications > 0 && !expanded}>
          <p className="mb-4 text-sm text-muted-foreground">
            A store connects to data. A layer publishes it at a stable URL. New
            layers are private by default.
          </p>
          <ol className="grid gap-5 md:grid-cols-2">
            <li
              className={
                !expanded && summary.counts.services ? "hidden" : "space-y-2"
              }
            >
              <StepHeading number={1} done={Boolean(summary.counts.services)}>
                Add data
              </StepHeading>
              <p className="text-sm">
                {summary.counts.services
                  ? `${summary.counts.services} ${summary.counts.services === 1 ? "store" : "stores"} configured. Add another or continue to discovery.`
                  : "Connect PostGIS, DuckDB or an allowed file, or upload a vector dataset."}
              </p>
              <div className="flex flex-wrap gap-2">
                <StoreCreateDialog workspace={workspace} onDirty={onDirty} />
                {config?.features.imports ? (
                  <Button variant="outline" size="sm" asChild>
                    <Link to={`${root}/imports?new=upload`}>Upload a file</Link>
                  </Button>
                ) : (
                  <p className="text-xs text-muted-foreground">
                    Uploads are disabled. An operator can enable
                    Importer.Enabled and restart the server. Connecting a store
                    still works.
                  </p>
                )}
              </div>
            </li>
            <li
              className={
                !expanded && !summary.counts.services ? "hidden" : "space-y-2"
              }
            >
              <StepHeading number={2} done={publications > 0}>
                Discover and publish
              </StepHeading>
              <p className="text-sm">
                {publications
                  ? `${publications} ${publications === 1 ? "publication" : "publications"} in the catalog. Publication does not guarantee that a service or data source is available.`
                  : "Choose a store, discover its tables, then select the layers to publish. Uploads have their own inspect → plan → publish steps."}
              </p>
              <div className="flex flex-wrap items-center gap-3">
                <LayerDiscoveryDialog
                  workspace={workspace}
                  services={services.data?.services ?? []}
                  servicesLoading={services.isLoading}
                  servicesError={services.error as Error | null}
                  retryServices={() => services.refetch()}
                  manageStoresPath={`${root}/stores`}
                  onDirty={onDirty}
                  trigger={
                    <Button
                      size="sm"
                      variant={
                        summary.counts.services && !publications
                          ? "default"
                          : "outline"
                      }
                    >
                      Discover and publish layers
                    </Button>
                  }
                />
                <Button size="sm" variant="outline" asChild>
                  <Link to={`${root}/coverages`}>Raster coverages</Link>
                </Button>
              </div>
            </li>
            <li className={!expanded ? "hidden" : "space-y-2"}>
              <StepHeading number={3}>Preview and check services</StepHeading>
              <p className="text-sm">
                Check a published layer on the map. Features work without WMS;
                enable optional services in Settings. A working console preview
                uses your current session, not anonymous access.
              </p>
              <div className="flex flex-wrap gap-2">
                <Button size="sm" variant="outline" asChild>
                  <Link to={`${root}/preview`}>Open preview</Link>
                </Button>
                <Button size="sm" variant="outline" asChild>
                  <Link to={`${root}/settings`}>Configure services</Link>
                </Button>
              </div>
            </li>
            <li className={!expanded ? "hidden" : "space-y-2"}>
              <StepHeading number={4} done={Boolean(summary.counts.api_keys)}>
                Connect your application
              </StepHeading>
              <p className="text-sm">
                Create a workspace viewer API key for private data, then use a
                real layer URL in curl, Python, JavaScript or QGIS. Never share
                your administrator token.
              </p>
              <div className="flex flex-wrap gap-2">
                <Button size="sm" variant="outline" asChild>
                  <Link to={`${root}/api-keys`}>Create a client key</Link>
                </Button>
                <Button size="sm" variant="outline" asChild>
                  <Link to={`${root}/endpoints`}>Connect a client</Link>
                </Button>
              </div>
            </li>
          </ol>
          {!publications && (
            <Button
              className="mt-3"
              variant="ghost"
              size="sm"
              aria-expanded={expanded}
              onClick={() => setExpanded(!expanded)}
            >
              {expanded ? "Show current step" : "See all publishing steps"}
            </Button>
          )}
        </div>
      </CardContent>
    </Card>
  );
}
