import { datasourceCapabilities } from "@/lib/datasource-capabilities";
import { useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Link } from "react-router";
import { Database, Plus, RefreshCw } from "lucide-react";
import {
  createLayer,
  getListLayersQueryKey,
  useListLayers,
} from "@/api/generated/layers/layers";
import { discoverLayers } from "@/api/generated/services/services";
import type { CreateLayerBody, Service } from "@/api/generated/models";
import { StatusChip } from "@/components/StatusChip";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@/components/AppDialog";
import { Label } from "@/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { Input } from "@/components/ui/input";
import { getGetWorkspaceSummaryQueryKey } from "@/api/generated/workspaces/workspaces";
import { assertCurrentSession, sessionGeneration } from "@/api/session";
import { confirmDiscard } from "@/lib/confirm";
import {
  PublishedLayersPanel,
  type PublishedLayer,
} from "./PublishedLayersPanel";

const storeTypeLabels: Record<string, string> = {
  postgis: "PostGIS",
  duckdb: "DuckDB Spatial",
  geoparquet: "GeoParquet",
  vectorfile: "Vector file",
};

interface DiscoveredLayer {
  name: string;
  schema?: string;
  title?: string;
  description?: string;
  geometry_column?: string;
  geometry_type?: string;
  srid?: number;
}

interface PublishFailure {
  source: string;
  message: string;
}

function sourceName(layer: DiscoveredLayer) {
  return layer.schema && !layer.name.startsWith(`${layer.schema}.`)
    ? `${layer.schema}.${layer.name}`
    : layer.name;
}

/** The table name without its schema: "cite.Autos" → "Autos". */
function baseName(layer: DiscoveredLayer) {
  return layer.schema && layer.name.startsWith(`${layer.schema}.`)
    ? layer.name.slice(layer.schema.length + 1)
    : layer.name;
}

/** Lowercase, URL-friendly public ID suggestion. */
function publicID(name: string) {
  const cleaned = name
    .toLowerCase()
    .replace(/[^a-z0-9_-]+/g, "-")
    .replace(/^-+|-+$/g, "");
  return /^[a-z_]/.test(cleaned) ? cleaned : `layer-${cleaned}`;
}

function errorMessage(reason: unknown) {
  if (!(reason instanceof Error)) return "Publication failed";
  const detail =
    "detail" in reason && typeof reason.detail === "string"
      ? reason.detail
      : "";
  return [reason.message, detail].filter(Boolean).join(": ");
}

export function LayerDiscoveryDialog({
  workspace,
  services,
  trigger,
  initialServiceID,
  lockService = false,
  servicesLoading = false,
  servicesError,
  retryServices,
  manageStoresPath,
  onPublished,
  onDirty,
}: {
  workspace: string;
  services: Service[];
  trigger: ReactNode;
  initialServiceID?: string;
  lockService?: boolean;
  servicesLoading?: boolean;
  servicesError?: Error | null;
  retryServices?: () => Promise<unknown> | unknown;
  manageStoresPath?: string;
  onPublished?: () => Promise<unknown> | unknown;
  onDirty?: (dirty: boolean) => void;
}) {
  const client = useQueryClient();
  const eligibleServices = useMemo(
    () =>
      services.filter(
        (service) =>
          service.enabled &&
          datasourceCapabilities(String(service.type)).features,
      ),
    [services],
  );
  const initialSelection =
    eligibleServices.find((service) => service.id === initialServiceID)?.id ??
    (eligibleServices.length === 1 ? eligibleServices[0].id : "");
  const [open, setOpen] = useState(false);
  const [serviceID, setServiceID] = useState(initialSelection);
  const [selected, setSelected] = useState<string[]>([]);
  const [succeeded, setSucceeded] = useState<Set<string>>(new Set());
  const [failures, setFailures] = useState<Record<string, string>>({});
  const [drafts, setDrafts] = useState<
    Record<string, { public_id: string; title: string }>
  >({});
  const [search, setSearch] = useState("");
  const [showPublished, setShowPublished] = useState(false);
  const [page, setPage] = useState(0);
  const [completedCount, setCompletedCount] = useState(0);
  const [completed, setCompleted] = useState<{
    serviceID: string;
    layers: PublishedLayer[];
  }>({ serviceID: "", layers: [] });
  const completedIDs = completed.layers;
  const [edited, setEdited] = useState(false);
  const [discoveredLayers, setDiscoveredLayers] = useState<
    DiscoveredLayer[] | null
  >(null);
  const [refreshNotice, setRefreshNotice] = useState("");
  const operation = useRef<object | null>(null);
  useEffect(
    () => () => {
      operation.current = null;
    },
    [],
  );
  function assertOperation(token: object, generation: number) {
    assertCurrentSession(generation);
    if (operation.current !== token)
      throw new DOMException(
        "The discovery dialog has closed. Remaining publications were cancelled.",
        "AbortError",
      );
  }
  const selectedService = eligibleServices.find(
    (service) => service.id === serviceID,
  );
  const published = useListLayers(workspace, selectedService?.id ?? "", {
    query: { enabled: open && Boolean(selectedService) },
  });
  const publishedSources = useMemo(
    () =>
      new Set(
        (published.data?.layers ?? []).map((layer) => layer.source_layer),
      ),
    [published.data],
  );
  const discover = useMutation({
    meta: { suppressErrorToast: true },
    mutationFn: async (input: {
      token: object;
      generation: number;
      workspace: string;
      serviceID: string;
    }) => {
      assertOperation(input.token, input.generation);
      const result = (await discoverLayers(
        input.workspace,
        input.serviceID,
      )) as { layers: DiscoveredLayer[] };
      assertOperation(input.token, input.generation);
      return result;
    },
    onSuccess: (result) => {
      const counts = new Map<string, number>();
      result.layers.forEach((layer) =>
        counts.set(baseName(layer), (counts.get(baseName(layer)) ?? 0) + 1),
      );
      const previousSources = new Set((discoveredLayers ?? []).map(sourceName));
      const nextSources = new Set(result.layers.map(sourceName));
      const removed = [...previousSources].filter(
        (source) => !nextSources.has(source),
      );
      setRefreshNotice(
        removed.length
          ? `${removed.length} source(s) are no longer available and were deselected. Their draft names are retained if they return on a later refresh.`
          : "",
      );
      setDrafts((current) => ({
        ...current,
        ...Object.fromEntries(
          result.layers.map((layer) => {
            const source = sourceName(layer);
            return [
              source,
              current[source] ?? {
                // Keep the schema only when two schemas share a table name.
                public_id: publicID(
                  (counts.get(baseName(layer)) ?? 0) > 1
                    ? source
                    : baseName(layer),
                ),
                title: layer.title || layer.name,
              },
            ];
          }),
        ),
      }));
      // Nothing is preselected: publishing is an explicit choice. A refresh
      // keeps the sources the user already picked.
      setSelected((current) =>
        result.layers
          .map(sourceName)
          .filter(
            (source) =>
              !publishedSources.has(source) &&
              !succeeded.has(source) &&
              current.includes(source),
          ),
      );
      setDiscoveredLayers(result.layers);
    },
    onSettled: (_result, _error, input) => {
      if (operation.current === input.token) operation.current = null;
    },
  });
  const publish = useMutation({
    meta: { suppressErrorToast: true },
    mutationFn: async (input: {
      token: object;
      generation: number;
      workspace: string;
      serviceID: string;
      layers: { source: string; body: CreateLayerBody }[];
    }) => {
      const completed: string[] = [];
      const created: PublishedLayer[] = [];
      const failed: PublishFailure[] = [];
      for (const { source, body } of input.layers) {
        assertOperation(input.token, input.generation);
        try {
          const layer = await createLayer(
            input.workspace,
            input.serviceID,
            body,
          );
          completed.push(source);
          created.push({
            id: layer.id,
            public_id: layer.public_id ?? body.public_id ?? "",
          });
        } catch (reason) {
          if (reason instanceof Error && reason.name === "AbortError")
            throw reason;
          failed.push({ source, message: errorMessage(reason) });
        }
        assertOperation(input.token, input.generation);
        setCompletedCount(completed.length + failed.length);
      }
      return { completed, created, failed };
    },
    onSuccess: async (result, input) => {
      assertOperation(input.token, input.generation);
      setSucceeded((current) => new Set([...current, ...result.completed]));
      setFailures(
        Object.fromEntries(
          result.failed.map((failure) => [failure.source, failure.message]),
        ),
      );
      setSelected(result.failed.map((failure) => failure.source));
      await client.invalidateQueries({
        queryKey: getListLayersQueryKey(input.workspace, input.serviceID),
      });
      assertOperation(input.token, input.generation);
      await onPublished?.();
      await client.invalidateQueries({
        queryKey: getGetWorkspaceSummaryQueryKey(input.workspace),
      });
      assertOperation(input.token, input.generation);
      if (result.failed.length === 0) {
        setEdited(false);
        setCompleted({
          serviceID: input.serviceID,
          layers: result.created,
        });
      }
    },
    onSettled: (_result, _error, input) => {
      if (operation.current === input.token) operation.current = null;
    },
  });
  const busy = discover.isPending || publish.isPending;
  useEffect(() => {
    onDirty?.(open && (edited || busy));
  }, [onDirty, open, edited, busy]);

  function startDiscovery() {
    if (operation.current || !selectedService) return;
    const token = {};
    operation.current = token;
    discover.mutate({
      token,
      generation: sessionGeneration(),
      workspace,
      serviceID: selectedService.id,
    });
  }

  function startPublish() {
    if (
      operation.current ||
      !selectedService ||
      !selected.length ||
      Object.keys(validationErrors).length
    )
      return;
    const bySource = new Map(
      (discoveredLayers ?? []).map((layer) => [sourceName(layer), layer]),
    );
    const layers = selected.map((source) => {
      const layer = bySource.get(source)!;
      return {
        source,
        body: {
          source_layer: source,
          public_id: drafts[source].public_id,
          title: drafts[source].title,
          description: layer.description,
          enabled: true,
          public: false,
          crs_default: layer.srid || undefined,
        } as CreateLayerBody,
      };
    });
    const token = {};
    operation.current = token;
    setCompletedCount(0);
    publish.mutate({
      token,
      generation: sessionGeneration(),
      workspace,
      serviceID: selectedService.id,
      layers,
    });
  }

  function resetDiscovery() {
    if (operation.current) return;
    setDiscoveredLayers(null);
    setCompleted({ serviceID: "", layers: [] });
    setRefreshNotice("");
    discover.reset();
    publish.reset();
    setSelected([]);
    setSucceeded(new Set());
    setFailures({});
    setDrafts({});
    setEdited(false);
    setSearch("");
    setShowPublished(false);
    setPage(0);
  }

  async function changeService(nextServiceID: string) {
    if (operation.current || busy || nextServiceID === serviceID) return;
    if (
      edited &&
      !(await confirmDiscard("Discard edited layer names and switch store?"))
    )
      return;
    setServiceID(nextServiceID);
    resetDiscovery();
  }

  async function changeOpen(nextOpen: boolean) {
    if (operation.current || busy) return;
    if (
      !nextOpen &&
      (publish.isPending ||
        discover.isPending ||
        (edited && !(await confirmDiscard("Discard edited layer names?"))))
    )
      return;
    setOpen(nextOpen);
    if (nextOpen) {
      const nextService =
        eligibleServices.find((service) => service.id === initialServiceID)
          ?.id ?? (eligibleServices.length === 1 ? eligibleServices[0].id : "");
      setServiceID(nextService);
      resetDiscovery();
    }
  }

  const isPublished = (source: string) =>
    publishedSources.has(source) || succeeded.has(source);
  const publishedCount = (discoveredLayers ?? []).filter((layer) =>
    isPublished(sourceName(layer)),
  ).length;
  const newCount = (discoveredLayers?.length ?? 0) - publishedCount;
  // New sources come first; published ones only appear on request.
  const filteredLayers = (discoveredLayers ?? [])
    .filter((layer) => showPublished || !isPublished(sourceName(layer)))
    .filter((layer) =>
      `${sourceName(layer)} ${drafts[sourceName(layer)]?.title ?? ""}`
        .toLowerCase()
        .includes(search.toLowerCase()),
    )
    .sort(
      (a, b) =>
        Number(isPublished(sourceName(a))) - Number(isPublished(sourceName(b))),
    );
  const pageCount = Math.max(1, Math.ceil(filteredLayers.length / 50));
  const visibleLayers = filteredLayers.slice(
    Math.min(page, pageCount - 1) * 50,
    (Math.min(page, pageCount - 1) + 1) * 50,
  );
  const validationErrors: Record<string, string> = {};
  const existingIDs = new Set(
    (published.data?.layers ?? []).map((layer) => layer.public_id),
  );
  const seenIDs = new Map<string, string>();
  for (const source of selected) {
    const id = drafts[source]?.public_id ?? "";
    if (!/^[A-Za-z_][A-Za-z0-9_.-]*$/.test(id))
      validationErrors[source] =
        "Start with a letter or underscore; use letters, digits, dots, underscores or hyphens.";
    else if (existingIDs.has(id))
      validationErrors[source] =
        "This public ID already exists in the store. Choose another ID.";
    else if (seenIDs.has(id)) {
      validationErrors[source] = "Public IDs must be unique.";
      validationErrors[seenIDs.get(id)!] = "Public IDs must be unique.";
    }
    seenIDs.set(id, source);
  }
  const selectableSources = (discoveredLayers ?? [])
    .map(sourceName)
    .filter(
      (source) => !publishedSources.has(source) && !succeeded.has(source),
    );
  const blockingError = servicesError ?? published.error;

  return (
    <Dialog open={open} onOpenChange={changeOpen}>
      <DialogTrigger asChild>{trigger}</DialogTrigger>
      <DialogContent className="sm:max-w-3xl">
        <DialogHeader>
          <DialogTitle>
            {completedIDs.length ? "Layers published" : "Add layers"}
          </DialogTitle>
          <DialogDescription>
            Choose an enabled feature store, discover its spatial sources, and
            publish selected layers as enabled and private.
          </DialogDescription>
          {publish.isPending && (
            <p className="text-sm text-muted-foreground" role="status">
              Keep this dialog open until publication finishes. Leaving this
              page stops queued publications; a request already sent may still
              finish.
            </p>
          )}
        </DialogHeader>

        {completedIDs.length ? (
          <PublishedLayersPanel
            workspace={workspace}
            serviceID={completed.serviceID}
            layers={completed.layers}
          />
        ) : servicesLoading ? (
          <div className="space-y-3 py-2">
            <Skeleton className="h-4 w-20" />
            <Skeleton className="h-9 w-full" />
          </div>
        ) : blockingError ? (
          <div className="border border-destructive/40 bg-destructive/5 p-4">
            <p className="text-sm text-destructive" role="alert">
              {blockingError.message}
            </p>
            <Button
              size="sm"
              variant="outline"
              className="mt-3"
              disabled={published.isFetching}
              onClick={() =>
                void (servicesError ? retryServices?.() : published.refetch())
              }
            >
              <RefreshCw /> Retry
            </Button>
          </div>
        ) : eligibleServices.length === 0 ? (
          <div className="border p-6 text-center">
            <Database className="mx-auto mb-3 text-muted-foreground" />
            <p className="font-medium">No enabled feature store is available</p>
            <p className="mx-auto mt-1 max-w-md text-sm text-muted-foreground">
              Create or enable a PostGIS, DuckDB, GeoParquet, or vector-file
              store before publishing a feature layer.
            </p>
            {manageStoresPath && (
              <Button asChild size="sm" className="mt-4">
                <Link to={manageStoresPath}>
                  <Plus /> Manage stores
                </Link>
              </Button>
            )}
          </div>
        ) : (
          <>
            <div>
              <Label htmlFor="layer-store">Store</Label>
              {lockService && selectedService ? (
                <div className="mt-1 flex h-9 items-center justify-between border px-3">
                  <span>{selectedService.name}</span>
                  <span className="font-mono text-xs text-muted-foreground">
                    {storeTypeLabels[selectedService.type] ||
                      selectedService.type}
                  </span>
                </div>
              ) : (
                <Select
                  value={serviceID}
                  onValueChange={changeService}
                  disabled={busy}
                >
                  <SelectTrigger id="layer-store" className="mt-1 w-full">
                    <SelectValue placeholder="Select a feature store" />
                  </SelectTrigger>
                  <SelectContent>
                    {eligibleServices.map((service) => (
                      <SelectItem value={service.id} key={service.id}>
                        {service.name} ·{" "}
                        {storeTypeLabels[service.type] || service.type}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              )}
            </div>

            {selectedService && published.isLoading && (
              <Skeleton className="h-20 w-full" />
            )}

            {discoveredLayers && (
              <div className="border">
                <div className="flex flex-wrap items-center justify-between gap-2 border-b bg-muted/40 px-3 py-2">
                  <p className="text-sm">
                    {newCount} new · {publishedCount} already published
                  </p>
                  <div className="flex flex-wrap gap-1">
                    {publishedCount > 0 && (
                      <Button
                        type="button"
                        size="sm"
                        variant="ghost"
                        aria-pressed={showPublished}
                        disabled={busy}
                        onClick={() => {
                          setShowPublished((value) => !value);
                          setPage(0);
                        }}
                      >
                        {showPublished
                          ? "Hide published"
                          : `Show ${publishedCount} published`}
                      </Button>
                    )}
                    <Button
                      type="button"
                      size="sm"
                      variant="ghost"
                      disabled={busy || selectableSources.length === 0}
                      onClick={() => setSelected(selectableSources)}
                    >
                      Select all
                    </Button>
                    <Button
                      type="button"
                      size="sm"
                      variant="ghost"
                      disabled={busy || selected.length === 0}
                      onClick={() => setSelected([])}
                    >
                      Clear
                    </Button>
                  </div>
                </div>
                <div className="p-3">
                  <Input
                    aria-label="Filter discovered layers"
                    placeholder="Find a source or title…"
                    value={search}
                    disabled={busy}
                    onChange={(e) => {
                      setSearch(e.target.value);
                      setPage(0);
                    }}
                  />
                </div>
                {newCount === 0 && !showPublished && (
                  <div role="status" className="space-y-2 px-3 pb-4 text-sm">
                    <p className="font-medium">
                      {`All ${discoveredLayers.length} sources in ${selectedService?.name ?? "this store"} are published.`}
                    </p>
                    <p className="text-muted-foreground">
                      Add tables to the store and refresh discovery, or manage
                      the existing publications.
                    </p>
                    <p className="flex flex-wrap gap-3">
                      <Link
                        className="underline"
                        to={`/workspaces/${encodeURIComponent(workspace)}/layers`}
                      >
                        View layers
                      </Link>
                      <Link
                        className="underline"
                        to={`/workspaces/${encodeURIComponent(workspace)}/preview`}
                      >
                        Open preview
                      </Link>
                    </p>
                  </div>
                )}
                <div className="max-h-[45vh] divide-y overflow-auto">
                  {visibleLayers.map((layer) => {
                    const source = sourceName(layer);
                    const alreadyPublished =
                      publishedSources.has(source) || succeeded.has(source);
                    const failure =
                      validationErrors[source] || failures[source];
                    return (
                      <div
                        className="flex flex-wrap items-center gap-3 px-3 py-2.5 text-left"
                        key={source}
                      >
                        <Checkbox
                          checked={selected.includes(source)}
                          disabled={alreadyPublished || busy}
                          aria-label={`Publish ${source}`}
                          onCheckedChange={(checked) =>
                            setSelected((current) =>
                              checked
                                ? [...new Set([...current, source])]
                                : current.filter((item) => item !== source),
                            )
                          }
                        />
                        <span className="min-w-0 flex-1">
                          <span className="block font-mono text-xs">
                            {source}
                          </span>
                          <span className="text-xs text-muted-foreground">
                            {layer.geometry_type || "geometry"} · EPSG:
                            {layer.srid || "unknown"}
                          </span>
                          {failure && (
                            <span className="mt-1 block text-xs text-destructive">
                              {failure}
                            </span>
                          )}
                        </span>
                        {!alreadyPublished && (
                          <div className="w-full space-y-2 sm:w-1/2">
                            <Label htmlFor={`public-id-${source}`}>
                              Public ID
                            </Label>
                            <Input
                              id={`public-id-${source}`}
                              aria-label={`Public ID for ${source}`}
                              aria-invalid={Boolean(validationErrors[source])}
                              disabled={busy}
                              value={drafts[source]?.public_id ?? ""}
                              onChange={(e) => {
                                setEdited(true);
                                setDrafts((current) => ({
                                  ...current,
                                  [source]: {
                                    ...current[source],
                                    public_id: e.target.value,
                                  },
                                }));
                              }}
                            />
                            <Label htmlFor={`title-${source}`}>Title</Label>
                            <Input
                              id={`title-${source}`}
                              aria-label={`Title for ${source}`}
                              disabled={busy}
                              value={drafts[source]?.title ?? ""}
                              onChange={(e) => {
                                setEdited(true);
                                setDrafts((current) => ({
                                  ...current,
                                  [source]: {
                                    ...current[source],
                                    title: e.target.value,
                                  },
                                }));
                              }}
                            />
                          </div>
                        )}
                        {alreadyPublished && <StatusChip value="published" />}
                      </div>
                    );
                  })}
                  {discoveredLayers.length === 0 && (
                    <p className="p-8 text-center text-sm text-muted-foreground">
                      No feature layers were discovered in this store.
                    </p>
                  )}
                </div>
                <div className="flex items-center justify-between border-t p-3 text-sm">
                  <span>
                    {filteredLayers.length} sources · page{" "}
                    {Math.min(page, pageCount - 1) + 1} of {pageCount} ·{" "}
                    {selected.length} selected across pages
                  </span>
                  <div className="flex gap-2">
                    <Button
                      variant="outline"
                      size="sm"
                      disabled={page === 0 || busy}
                      onClick={() => setPage((v) => v - 1)}
                    >
                      Previous
                    </Button>
                    <Button
                      variant="outline"
                      size="sm"
                      disabled={page >= pageCount - 1 || busy}
                      onClick={() => setPage((v) => v + 1)}
                    >
                      Next
                    </Button>
                  </div>
                </div>
              </div>
            )}

            {refreshNotice && (
              <p className="text-sm text-muted-foreground" role="status">
                {refreshNotice}
              </p>
            )}
            {discover.error && (
              <p className="text-sm text-destructive" role="alert">
                {discover.error.message}
              </p>
            )}
            {publish.error && (
              <p className="text-sm text-destructive" role="alert">
                {publish.error.message}
              </p>
            )}
          </>
        )}

        <DialogFooter>
          <Button
            variant="outline"
            disabled={publish.isPending || discover.isPending}
            onClick={() => changeOpen(false)}
          >
            {completedIDs.length ? "Done" : "Cancel"}
          </Button>
          {eligibleServices.length > 0 &&
            selectedService &&
            !blockingError &&
            !discoveredLayers && (
              <Button
                disabled={published.isLoading || discover.isPending}
                onClick={startDiscovery}
              >
                <RefreshCw />
                {discover.isPending ? "Discovering…" : "Discover layers"}
              </Button>
            )}
          {!completedIDs.length && !blockingError && discoveredLayers && (
            <>
              <Button
                variant="outline"
                disabled={discover.isPending || busy}
                onClick={startDiscovery}
              >
                <RefreshCw /> Refresh discovery
              </Button>
              <Button
                disabled={
                  selected.length === 0 ||
                  busy ||
                  Object.keys(validationErrors).length > 0
                }
                onClick={startPublish}
              >
                {publish.isPending
                  ? `Publishing ${completedCount} / ${publish.variables?.layers.length ?? 0}…`
                  : selected.length === 0
                    ? newCount === 0
                      ? "Nothing new to publish"
                      : "Select layers to publish"
                    : `Publish ${selected.length} layer${selected.length === 1 ? "" : "s"}`}
              </Button>
            </>
          )}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
