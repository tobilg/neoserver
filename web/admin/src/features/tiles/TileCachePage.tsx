import { useEffect, useRef, useState } from "react";
import { toast } from "sonner";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useParams } from "react-router";
import { Plus } from "lucide-react";
import {
  getGetPersistentTileCacheStatsQueryKey,
  getListTileCacheJobsQueryKey,
  cancelTileCacheJob,
  useGetPersistentTileCacheStats,
  useGetTileCacheJobProgress,
  useListTileCacheJobs,
} from "@/api/generated/tile-cache/tile-cache";
import { useGetTileCacheJob } from "@/api/generated/tile-cache/tile-cache";
import type { TileCacheJob as TileJob } from "@/api/generated/models";
import {
  isActiveStatus,
  statusSignature,
  useActivePolling,
} from "@/hooks/use-active-polling";
import { Page } from "@/components/Page";
import { PyramidMeter, type ZoomProgress } from "@/components/PyramidMeter";
import { StatusChip } from "@/components/StatusChip";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Progress } from "@/components/ui/progress";
import { formatBytes, formatDateTime } from "@/lib/format";
import { ResponseCacheCard } from "@/features/cache/CachePage";
import { jobTitle } from "./job-title";
import { TileJobDialog } from "./TileJobDialog";
import { QueryError } from "@/components/QueryError";
import { ResourcePage } from "@/features/shared/ResourcePage";
import {
  getListTileMatrixSetsQueryKey,
  getTileMatrixSet,
  useDeleteTileMatrixSet,
  useListTileMatrixSets,
  usePutTileMatrixSet,
} from "@/api/generated/tile-matrix-sets/tile-matrix-sets";
import type { TileMatrixSetDefinition } from "@/api/generated/models";

interface Stats {
  enabled: boolean;
  workspace?: { size_bytes: number; quota_bytes: number; utilization: number };
  hits?: number;
  misses?: number;
  writes?: number;
  evictions?: number;
}
function JobPyramid({ workspace, job }: { workspace: string; job: TileJob }) {
  const client = useQueryClient();
  const detail = useGetTileCacheJob(workspace, job.id, {
    query: useActivePolling<TileJob>({
      isActive: (data) => isActiveStatus((data ?? job).status),
      signature: (data) => {
        const value = data ?? job;
        return `${value.status}:${value.processed_tiles}/${value.total_tiles}`;
      },
    }),
  });
  const current = detail.data ?? job;
  // Activity is owned by the job record; this query's own payload is the
  // per-zoom progress that tells us whether seeding is still advancing.
  const query = useGetTileCacheJobProgress(workspace, job.id, {
    query: useActivePolling<{ zooms?: ZoomProgress[] }>({
      isActive: () => isActiveStatus(current.status),
      signature: (data) =>
        (data?.zooms ?? [])
          .map((zoom) => `${zoom.zoom}:${zoom.processed_tiles}`)
          .join(","),
    }),
  });
  // Usage and write counters change when a job finishes; refresh them then.
  const previous = useRef(current.status);
  useEffect(() => {
    const wasActive = isActiveStatus(previous.current);
    previous.current = current.status;
    if (wasActive && !isActiveStatus(current.status))
      void client.invalidateQueries({
        queryKey: getGetPersistentTileCacheStatsQueryKey(workspace),
      });
  }, [client, workspace, current.status]);
  const cancel = useMutation({
    mutationFn: () => cancelTileCacheJob(workspace, job.id),
    onSuccess: () => {
      toast.success("Tile job cancelled");
      return client.invalidateQueries({
        queryKey: getListTileCacheJobsQueryKey(workspace),
      });
    },
  });
  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-start justify-between gap-2">
          <span className="min-w-0">
            <span className="block text-sm font-medium">
              {jobTitle(current.request)}
            </span>
            <span className="block font-mono text-xs font-normal text-muted-foreground">
              {job.id.slice(0, 8)} · {formatDateTime(current.created_at)}
            </span>
          </span>
          <StatusChip value={current.status} />
        </CardTitle>
      </CardHeader>
      <CardContent>
        <QueryError
          error={detail.error || query.error}
          retry={() => {
            void detail.refetch();
            void query.refetch();
          }}
          context="Job progress could not be loaded"
        />
        {query.isLoading && <p role="status">Loading job progress…</p>}
        <p className="mb-2 font-mono text-xs text-muted-foreground">
          {current.processed_tiles.toLocaleString()} /{" "}
          {current.total_tiles.toLocaleString()} tiles
          {current.failed_tiles ? ` · ${current.failed_tiles} failed` : ""}
          {current.bytes_written
            ? ` · ${formatBytes(current.bytes_written)} written`
            : ""}
          {current.bytes_deleted
            ? ` · ${formatBytes(current.bytes_deleted)} removed`
            : ""}
        </p>
        {current.error_message && (
          <p className="mb-2 text-sm text-destructive">
            {current.error_message}
          </p>
        )}
        {!query.error && query.data && (
          <PyramidMeter levels={query.data.zooms ?? []} />
        )}
        {["queued", "running"].includes(current.status) && (
          <Button
            className="mt-3"
            size="sm"
            variant="outline"
            disabled={cancel.isPending}
            onClick={() => cancel.mutate()}
          >
            Cancel job
          </Button>
        )}
      </CardContent>
    </Card>
  );
}
export function TileCachePage() {
  const { ws = "" } = useParams();
  const stats = useGetPersistentTileCacheStats(ws);
  const jobs = useListTileCacheJobs(ws, {
    query: useActivePolling<{ jobs?: TileJob[] }>({
      isActive: (data) =>
        Boolean(data?.jobs?.some((job) => isActiveStatus(job.status))),
      signature: (data) => statusSignature(data?.jobs),
    }),
  });
  const usage = stats.data?.workspace;
  const [open, setOpen] = useState(false);
  return (
    <Page
      title="Caching"
      description="Durable tile cache, seeding jobs and the in-memory response cache for this workspace."
      action={
        <Button
          disabled={Boolean(stats.error) || !stats.data?.enabled}
          onClick={() => setOpen(true)}
        >
          <Plus /> New job
        </Button>
      }
    >
      {open && <TileJobDialog workspace={ws} onClose={() => setOpen(false)} />}
      <QueryError
        error={stats.error}
        retry={() => stats.refetch()}
        context="Tile-cache statistics could not be loaded"
      />
      {stats.isLoading && <p role="status">Loading tile-cache statistics…</p>}
      {!stats.error && stats.data && !stats.data.enabled && (
        <p className="rounded-lg border p-4">
          Persistent tile caching is disabled. Enable it in the server
          configuration before creating jobs.
        </p>
      )}
      {!stats.error && stats.data?.enabled && (
        <div className="mb-4 grid gap-4 md:grid-cols-2 xl:grid-cols-3">
          <Card>
            <CardHeader>
              <CardTitle>Workspace usage</CardTitle>
            </CardHeader>
            <CardContent>
              <div className="mb-2 flex justify-between font-mono text-sm">
                <span>
                  {usage
                    ? `${formatBytes(usage.size_bytes ?? 0)}${usage.quota_bytes ? ` of ${formatBytes(usage.quota_bytes)}` : ""}`
                    : "Usage unavailable"}
                </span>
                <span>
                  {usage?.utilization !== undefined
                    ? `${Math.round(usage.utilization * 100)}%`
                    : "—"}
                </span>
              </div>
              {usage?.utilization !== undefined && (
                <Progress
                  aria-label="Workspace cache utilization"
                  value={usage.utilization * 100}
                />
              )}
            </CardContent>
          </Card>
          <Card>
            <CardHeader>
              <CardTitle>Cache instruments</CardTitle>
            </CardHeader>
            <CardContent className="grid grid-cols-4 gap-3">
              {["hits", "misses", "writes", "evictions"].map((name) => (
                <div key={name}>
                  <span className="block font-mono text-lg">
                    {String(stats.data?.[name as keyof Stats] ?? "—")}
                  </span>
                  <span className="text-xs text-muted-foreground">
                    {name.charAt(0).toUpperCase() + name.slice(1)}
                  </span>
                </div>
              ))}
            </CardContent>
          </Card>
          <ResponseCacheCard workspace={ws} />
        </div>
      )}
      {!stats.error && stats.data && !stats.data.enabled && (
        <div className="mb-4 max-w-xl">
          <ResponseCacheCard workspace={ws} />
        </div>
      )}
      <QueryError
        error={jobs.error}
        retry={() => jobs.refetch()}
        context="Tile-cache jobs could not be loaded"
      />
      {jobs.isLoading && <p role="status">Loading tile-cache jobs…</p>}
      <div className="grid gap-3 lg:grid-cols-2">
        {!jobs.error &&
          jobs.data?.jobs?.map((job) => (
            <JobPyramid workspace={ws} job={job} key={job.id} />
          ))}
      </div>
      {!jobs.error && jobs.data?.jobs?.length === 0 && (
        <p className="border p-8 text-center text-muted-foreground">
          No tile jobs yet. Seed or truncate a resource to see per-zoom
          progress.
        </p>
      )}
    </Page>
  );
}
export function TileMatrixSetsPage() {
  const client = useQueryClient();
  const sets = useListTileMatrixSets();
  const put = usePutTileMatrixSet();
  const remove = useDeleteTileMatrixSet();

  const invalidate = () =>
    client.invalidateQueries({ queryKey: getListTileMatrixSetsQueryKey() });

  // Custom grids are created and updated through the same idempotent PUT.
  const save = async (id: string, body: unknown) => {
    await put.mutateAsync({
      tileMatrixSet: id,
      data: body as TileMatrixSetDefinition,
    });
    await invalidate();
  };

  return (
    <ResourcePage
      title="Tile matrix sets"
      description="Built-in and custom bounded top-left grids for EPSG:3857 and CRS84."
      rows={sets.data?.tile_matrix_sets ?? []}
      isLoading={sets.isLoading}
      error={sets.error}
      urlKey="tile_matrix_sets"
      columns={["id", "title", "crs", "uri", "built_in"]}
      onRefresh={invalidate}
      createLabel="Add custom set"
      createTemplate={{
        id: "",
        title: "",
        crs: "http://www.opengis.net/def/crs/EPSG/0/3857",
        orderedAxes: ["X", "Y"],
        tileMatrices: [],
      }}
      onCreate={(body) => save(String(body.id), body)}
      onLoadItem={(row) => getTileMatrixSet(String(row.id))}
      editValue={(row) => row.definition ?? row}
      onUpdate={(row, body) => save(String(row.id), body)}
      canUpdate={(row) => !row.built_in}
      canDelete={(row) => !row.built_in}
      onDelete={async (row) => {
        await remove.mutateAsync({ tileMatrixSet: String(row.id) });
        await invalidate();
      }}
      deleteDescription="Built-in sets cannot be deleted. Removing a custom grid can invalidate cache identity and is rejected while it is in use."
    />
  );
}
