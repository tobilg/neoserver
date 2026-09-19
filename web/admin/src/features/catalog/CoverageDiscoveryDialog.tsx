import { datasourceCapabilities } from "@/lib/datasource-capabilities";
import { useState, type ReactNode } from "react";
import { useMutation, useQueries, useQueryClient } from "@tanstack/react-query";
import { Link } from "react-router";
import {
  createCoverage,
  discoverCoverages,
  getListCoveragesQueryKey,
  getListCoveragesQueryOptions,
} from "@/api/generated/coverages/coverages";
import { getGetWorkspaceSummaryQueryKey } from "@/api/generated/workspaces/workspaces";
import { useListServices } from "@/api/generated/services/services";
import type { DiscoveredCoverage, Service } from "@/api/generated/models";
import { QueryError } from "@/components/QueryError";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@/components/AppDialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { confirmDiscard } from "@/lib/confirm";
import { defaultCoverageID } from "./coverage-ids";
import { NativeSelect } from "@/components/NativeSelect";

export function CoverageDiscoveryDialog({
  workspace,
  services,
  initialServiceID = "",
  trigger,
  servicesLoading,
  servicesError,
  retryServices,
  onDirty,
}: {
  workspace: string;
  services: Service[];
  initialServiceID?: string;
  trigger: ReactNode;
  servicesLoading?: boolean;
  servicesError?: unknown;
  retryServices?: () => unknown;
  onDirty?: (dirty: boolean) => void;
}) {
  const client = useQueryClient();
  const [open, setOpen] = useState(false);
  const [serviceID, setServiceID] = useState(initialServiceID);
  const [drafts, setDrafts] = useState<
    Record<string, { public_id: string; title: string }>
  >({});
  const [dirty, setDirty] = useState(false);
  const [published, setPublished] = useState<Set<string>>(new Set());
  const [search, setSearch] = useState("");
  const [page, setPage] = useState(0);
  const workspaceStores = useListServices(workspace, {
    query: { enabled: open },
  });
  const rasterStores = (workspaceStores.data?.services ?? services).filter(
    (store) => datasourceCapabilities(store.type).coverages,
  );
  const eligible = rasterStores.filter((store) => store.enabled);
  // Public coverage IDs are unique across the workspace, not just this store.
  const catalogs = useQueries({
    queries: rasterStores.map((store) =>
      getListCoveragesQueryOptions(workspace, store.id, {
        query: { enabled: open },
      }),
    ),
  });
  const catalogError =
    servicesError ||
    workspaceStores.error ||
    catalogs.find((query) => query.error)?.error;
  const catalogLoading =
    servicesLoading ||
    workspaceStores.isLoading ||
    catalogs.some((query) => query.isLoading);
  const existingIDs = new Set(
    catalogs.flatMap(
      (query) =>
        query.data?.coverages?.map((coverage) => coverage.public_id) ?? [],
    ),
  );
  const currentCatalog =
    catalogs[rasterStores.findIndex((store) => store.id === serviceID)];
  const existingSources = new Set(
    currentCatalog?.data?.coverages?.map(
      (coverage) => coverage.source_coverage,
    ) ?? [],
  );
  const storeName = (id: string) =>
    rasterStores.find((store) => store.id === id)?.name ?? "";
  const discover = useMutation({
    meta: { suppressErrorToast: true },
    mutationFn: (id: string) => discoverCoverages(workspace, id),
    onSuccess: (data, id) => {
      const found = data.coverages ?? [];
      setDrafts(
        Object.fromEntries(
          found.map((coverage) => [
            coverage.source_coverage,
            {
              public_id: defaultCoverageID(
                storeName(id),
                coverage.source_coverage,
                found.length,
              ),
              title: coverage.title || coverage.source_coverage,
            },
          ]),
        ),
      );
      setPage(0);
    },
  });
  const publish = useMutation({
    meta: { suppressErrorToast: true },
    mutationFn: (coverage: DiscoveredCoverage) =>
      createCoverage(workspace, serviceID, {
        source_coverage: coverage.source_coverage,
        ...drafts[coverage.source_coverage],
        description: coverage.description,
        enabled: true,
        public: false,
        // Let the server derive bands, dimensions and subtype from the source.
        resampling: "nearest",
      }),
    onSuccess: async (_, coverage) => {
      setPublished((value) => new Set([...value, coverage.source_coverage]));
      const remainingEdits = (discover.data?.coverages ?? []).some((source) => {
        if (
          source.source_coverage === coverage.source_coverage ||
          published.has(source.source_coverage) ||
          existingSources.has(source.source_coverage)
        )
          return false;
        const draft = drafts[source.source_coverage];
        const defaultID = defaultCoverageID(
          storeName(serviceID),
          source.source_coverage,
          discover.data?.coverages?.length ?? 0,
        );
        return (
          draft &&
          (draft.public_id !== defaultID ||
            draft.title !== (source.title || source.source_coverage))
        );
      });
      setDirty(remainingEdits);
      onDirty?.(remainingEdits);
      // Keep the dialog open with a per-source success state for further publications.
      await Promise.all([
        client.invalidateQueries({
          queryKey: getListCoveragesQueryKey(workspace, serviceID),
        }),
        client.invalidateQueries({
          queryKey: getGetWorkspaceSummaryQueryKey(workspace),
        }),
      ]);
    },
  });
  const busy = discover.isPending || publish.isPending;
  function reset() {
    discover.reset();
    publish.reset();
    setDrafts({});
    setPublished(new Set());
    setDirty(false);
    onDirty?.(false);
    setSearch("");
    setPage(0);
  }
  async function changeOpen(value: boolean) {
    if (
      !value &&
      (busy ||
        (dirty && !(await confirmDiscard("Discard remaining coverage edits?"))))
    )
      return;
    if (!value) reset();
    else {
      reset();
      const next =
        initialServiceID || (eligible.length === 1 ? eligible[0].id : "");
      setServiceID(next);
      // Opened from a store row: discover straight away.
      if (initialServiceID && eligible.some((store) => store.id === next))
        discover.mutate(next);
    }
    setOpen(value);
  }
  const sources = (discover.data?.coverages ?? []).filter((coverage) =>
    `${coverage.source_coverage} ${drafts[coverage.source_coverage]?.title ?? ""}`
      .toLowerCase()
      .includes(search.toLowerCase()),
  );
  const pageCount = Math.max(1, Math.ceil(sources.length / 50));
  const safePage = Math.min(page, pageCount - 1);
  return (
    <Dialog open={open} onOpenChange={changeOpen}>
      <DialogTrigger asChild>{trigger}</DialogTrigger>
      <DialogContent className="max-h-[88vh] overflow-y-auto sm:max-w-3xl">
        <DialogHeader>
          <DialogTitle>Add raster coverages</DialogTitle>
          <DialogDescription>
            Choose an enabled raster store, discover its sources, then publish
            with a unique public ID. New coverages are enabled and private.
          </DialogDescription>
        </DialogHeader>
        <QueryError
          error={catalogError}
          retry={() => {
            retryServices?.();
            void workspaceStores.refetch();
            catalogs.forEach((query) => void query.refetch());
          }}
        />
        {servicesLoading ? (
          <p role="status">Loading stores…</p>
        ) : eligible.length === 0 ? (
          <p>
            No enabled raster stores.{" "}
            <Link
              className="underline"
              to={`/workspaces/${encodeURIComponent(workspace)}/stores`}
            >
              Create or enable a raster store
            </Link>{" "}
            to add coverages.
          </p>
        ) : (
          <>
            <Label htmlFor="coverage-store">Raster store</Label>
            <NativeSelect
              id="coverage-store"
              className="w-full"
              value={serviceID}
              disabled={busy}
              onChange={async (event) => {
                const next = event.target.value;
                if (
                  dirty &&
                  !(await confirmDiscard(
                    "Discard coverage edits and switch store?",
                  ))
                )
                  return;
                reset();
                setServiceID(next);
              }}
            >
              <option value="">Select a store</option>
              {eligible.map((store) => (
                <option key={store.id} value={store.id}>
                  {store.name}
                </option>
              ))}
            </NativeSelect>
            <Button
              variant="outline"
              disabled={
                !eligible.some((store) => store.id === serviceID) || busy
              }
              onClick={async () => {
                if (
                  dirty &&
                  !(await confirmDiscard(
                    "Discard coverage edits and rediscover?",
                  ))
                )
                  return;
                reset();
                discover.mutate(serviceID);
              }}
            >
              {discover.isPending ? "Discovering…" : "Discover coverages"}
            </Button>
          </>
        )}
        <QueryError
          error={discover.error}
          retry={() => discover.mutate(serviceID)}
          context="Coverage discovery failed"
        />
        <QueryError
          error={publish.error}
          context="Coverage publication failed. Correct the fields and retry."
        />
        {discover.data && (
          <>
            <Input
              aria-label="Filter discovered coverages"
              placeholder="Filter sources…"
              value={search}
              onChange={(event) => {
                setSearch(event.target.value);
                setPage(0);
              }}
            />
            {sources.length === 0 && (
              <p>
                {discover.data.coverages?.length
                  ? "No sources match this filter."
                  : "No raster sources were discovered. Check the store connection and allowed paths."}
              </p>
            )}
            {sources
              .slice(safePage * 50, (safePage + 1) * 50)
              .map((coverage, index) => {
                const source = coverage.source_coverage;
                const draft = drafts[source];
                if (!draft) return null;
                const done =
                  published.has(source) || existingSources.has(source);
                const invalid = !/^[A-Za-z_][A-Za-z0-9_.-]*$/.test(
                  draft.public_id,
                )
                  ? "Start with a letter or underscore; use letters, digits, dots, underscores or hyphens."
                  : existingIDs.has(draft.public_id)
                    ? "This public ID already exists in the workspace."
                    : "";
                return (
                  <fieldset
                    key={source}
                    disabled={busy || done}
                    className="space-y-2 rounded-lg border p-3"
                  >
                    <legend className="break-all px-1 font-mono text-xs">
                      {source}
                    </legend>
                    <p className="text-xs text-muted-foreground">
                      {coverage.info.width}×{coverage.info.height} ·{" "}
                      {coverage.info.crs}
                    </p>
                    {done ? (
                      <div className="flex flex-wrap items-center gap-2">
                        <p role="status" className="text-sm text-success">
                          Published as {draft.public_id} (private)
                        </p>
                        <Button size="sm" variant="outline" asChild>
                          <Link
                            to={`/workspaces/${encodeURIComponent(workspace)}/preview?${new URLSearchParams({ layers: draft.public_id, sources: "wms" })}`}
                          >
                            Preview
                          </Link>
                        </Button>
                        <Button size="sm" variant="ghost" asChild>
                          <Link
                            to={`/workspaces/${encodeURIComponent(workspace)}/coverages`}
                          >
                            Manage access
                          </Link>
                        </Button>
                      </div>
                    ) : (
                      <>
                        <Label htmlFor={`coverage-id-${index}`}>
                          Public coverage ID
                        </Label>
                        <Input
                          id={`coverage-id-${index}`}
                          value={draft.public_id}
                          aria-invalid={Boolean(invalid)}
                          onChange={(event) => {
                            setDrafts({
                              ...drafts,
                              [source]: {
                                ...draft,
                                public_id: event.target.value,
                              },
                            });
                            setDirty(true);
                            onDirty?.(true);
                          }}
                        />
                        <Label htmlFor={`coverage-title-${index}`}>Title</Label>
                        <Input
                          id={`coverage-title-${index}`}
                          value={draft.title}
                          onChange={(event) => {
                            setDrafts({
                              ...drafts,
                              [source]: { ...draft, title: event.target.value },
                            });
                            setDirty(true);
                            onDirty?.(true);
                          }}
                        />
                        {invalid && (
                          <p className="text-sm text-destructive">{invalid}</p>
                        )}
                        <Button
                          size="sm"
                          disabled={Boolean(
                            invalid || catalogError || catalogLoading,
                          )}
                          onClick={() => publish.mutate(coverage)}
                        >
                          Publish coverage
                        </Button>
                      </>
                    )}
                  </fieldset>
                );
              })}
            {pageCount > 1 && (
              <div className="flex items-center justify-between">
                <Button
                  variant="outline"
                  disabled={safePage === 0}
                  onClick={() => setPage(safePage - 1)}
                >
                  Previous
                </Button>
                <span>
                  Page {safePage + 1} of {pageCount}
                </span>
                <Button
                  variant="outline"
                  disabled={safePage + 1 === pageCount}
                  onClick={() => setPage(safePage + 1)}
                >
                  Next
                </Button>
              </div>
            )}
          </>
        )}
      </DialogContent>
    </Dialog>
  );
}
