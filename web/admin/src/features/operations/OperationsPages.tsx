import { useState } from "react";
import { toast } from "sonner";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Wrench } from "lucide-react";
import { readinessQuery, fetchLiveness } from "@/lib/health";
import { useAuth } from "@/auth/auth-context";
import { Page } from "@/components/Page";
import { StatusChip } from "@/components/StatusChip";
import { Diagnostics } from "@/components/Diagnostics";
import {
  formatBytes,
  formatDateTime,
  formatDuration,
  formatMilliseconds,
} from "@/lib/format";
import { QueryError } from "@/components/QueryError";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/AppDialog";
import { useListDeletionOperations } from "@/api/generated/catalog-lifecycle/catalog-lifecycle";
import { Link } from "react-router";
import { plural } from "@/lib/format";
import {
  getGetCatalogIntegrityQueryKey,
  useGetCatalogIntegrity,
  repairCatalogIntegrity,
} from "@/api/generated/catalog-lifecycle/catalog-lifecycle";
import {
  clearCacheByType,
  getGetCacheStatsQueryKey,
  clearAllCaches,
  useGetCacheStats,
} from "@/api/generated/cache/cache";
import { POLL_READINESS_MS } from "@/hooks/use-active-polling";

/** The cache partitions the server exposes for targeted clearing. */
type CacheType = Parameters<typeof clearCacheByType>[0];

export function OperationsPage() {
  const { config } = useAuth();
  const readiness = useQuery(readinessQuery);
  const health = useQuery({
    queryKey: ["liveness"],
    queryFn: fetchLiveness,
    retry: false,
    refetchInterval: POLL_READINESS_MS,
  });
  const uptime =
    config?.server_started_at && readiness.dataUpdatedAt
      ? readiness.dataUpdatedAt - new Date(config.server_started_at).getTime()
      : 0;
  return (
    <Page
      title="Operations"
      description="Single-active process health, catalog ownership, resumable deletion work, and cache state."
    >
      <div className="grid gap-4 lg:grid-cols-12">
        <Card className="lg:col-span-7">
          <CardHeader>
            <CardTitle className="flex justify-between">
              Readiness{" "}
              <StatusChip
                value={
                  readiness.error
                    ? "unavailable"
                    : (readiness.data?.status ?? "pending")
                }
              />
            </CardTitle>
          </CardHeader>
          <CardContent className="divide-y">
            {readiness.error && (
              <div role="alert" className="py-2 text-sm text-destructive">
                Readiness unavailable. Previous checks are stale.{" "}
                <Button variant="link" onClick={() => void readiness.refetch()}>
                  Retry readiness
                </Button>
              </div>
            )}
            {readiness.dataUpdatedAt > 0 && (
              <p className="py-2 text-xs text-muted-foreground">
                Last response:{" "}
                {formatDateTime(readiness.dataUpdatedAt, { seconds: true })}
              </p>
            )}
            {Object.entries(readiness.data?.checks ?? {}).map(
              ([name, status]) => (
                <div
                  className="flex items-center justify-between py-2"
                  key={name}
                >
                  <span className="font-mono text-xs">{name}</span>
                  <div className="flex items-center gap-3">
                    <span className="font-mono text-xs text-muted-foreground">
                      {formatMilliseconds(
                        readiness.data?.durations_ms[name] ?? 0,
                      )}
                    </span>
                    <StatusChip
                      value={readiness.error ? "unavailable" : status}
                    />
                  </div>
                </div>
              ),
            )}
          </CardContent>
        </Card>
        <Card className="lg:col-span-5">
          <CardHeader>
            <CardTitle>Server</CardTitle>
          </CardHeader>
          <CardContent className="space-y-3">
            <Field label="Version" value={config?.version} />
            <Field label="Uptime" value={formatDuration(uptime / 1000)} />
            <Field
              label="Topology"
              value="single-active — one process owns catalog, cache, and jobs"
            />
            <Field
              label="Liveness"
              value={
                health.error
                  ? "unavailable"
                  : (health.data?.status ?? "checking")
              }
            />
          </CardContent>
        </Card>
      </div>
      <CatalogIntegrityPanel />
      <GlobalCachePanel />
      <DeletionSummary />
    </Page>
  );
}

function CatalogIntegrityPanel() {
  const client = useQueryClient();
  const [open, setOpen] = useState(false);
  const integrity = useGetCatalogIntegrity();
  const issues = integrity.data?.issues ?? [];

  const repair = useMutation({
    meta: { suppressErrorToast: true },
    mutationFn: () => repairCatalogIntegrity({ confirm: true }),
    onSuccess: async () => {
      setOpen(false);
      await client.invalidateQueries({
        queryKey: getGetCatalogIntegrityQueryKey(),
      });
    },
  });
  return (
    <Card className="mt-4">
      <CardHeader>
        <CardTitle className="flex items-center justify-between gap-3">
          Catalog integrity
          <Button size="sm" variant="outline" onClick={() => setOpen(true)}>
            <Wrench /> Repair catalog
          </Button>
        </CardTitle>
      </CardHeader>
      <CardContent>
        {integrity.isLoading && (
          <p role="status">Checking catalog integrity…</p>
        )}
        {integrity.data && (
          <>
            <p className="font-medium">
              {integrity.error
                ? "Last known result (stale)"
                : integrity.data.healthy
                  ? "Catalog healthy"
                  : "Catalog needs attention"}
              : {issues.length} issue
              {issues.length === 1 ? "" : "s"}
            </p>
            {issues.length === 0 ? (
              <p className="mt-1 text-sm text-muted-foreground">
                No catalog ownership inconsistencies were reported. No repair is
                needed.
              </p>
            ) : (
              <ul className="mt-3 space-y-3">
                {issues.map((issue, index) => (
                  <li
                    key={`${issue.id}-${index}`}
                    className="rounded border p-3 text-sm"
                  >
                    <strong>{issue.name || issue.id}</strong>
                    <p>{issue.reason}</p>
                    <p className="text-xs text-muted-foreground">
                      {issue.kind} in {issue.store}
                    </p>
                  </li>
                ))}
              </ul>
            )}
            <Diagnostics
              label="Catalog integrity details"
              value={integrity.data}
            />
          </>
        )}
        {integrity.error && (
          <p className="text-sm text-destructive">{integrity.error.message}</p>
        )}
      </CardContent>
      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Repair catalog integrity?</DialogTitle>
            <DialogDescription>
              neoserver will reconcile recoverable catalog inconsistencies.
              Review the current report before running the repair.
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setOpen(false)}>
              Cancel
            </Button>
            <Button disabled={repair.isPending} onClick={() => repair.mutate()}>
              Repair catalog
            </Button>
          </DialogFooter>
          {repair.error && (
            <p className="text-sm text-destructive">{repair.error.message}</p>
          )}
        </DialogContent>
      </Dialog>
    </Card>
  );
}
function GlobalCachePanel() {
  const client = useQueryClient();
  const stats = useGetCacheStats();

  const clear = useMutation({
    meta: { suppressErrorToast: true },
    mutationFn: (type: CacheType | "all") =>
      type === "all" ? clearAllCaches() : clearCacheByType(type),
    onSuccess: async (_result, type) => {
      toast.success(
        type === "all" ? "All caches cleared" : `Cleared ${type} cache`,
      );
      await client.invalidateQueries({
        queryKey: getGetCacheStatsQueryKey(),
      });
    },
  });
  return (
    <Card className="mt-4">
      <CardHeader>
        <CardTitle>Global response cache</CardTitle>
      </CardHeader>
      <CardContent>
        <QueryError
          error={stats.error}
          retry={() => stats.refetch()}
          context="Cache statistics could not be loaded"
        />
        {stats.isLoading && <p role="status">Loading cache statistics…</p>}
        {stats.data && (
          <>
            <p className="mb-3 text-sm">
              {stats.error ? "Last known cache statistics (stale). " : ""}
              {stats.data.enabled
                ? "Response cache enabled"
                : "Response cache disabled"}
              . Using {formatBytes(stats.data.total_size_bytes)} of{" "}
              {formatBytes(stats.data.total_max_size_bytes)}.
            </p>
            <dl className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
              {(
                [
                  "capabilities",
                  "collections",
                  "features",
                  "counts",
                  "tiles",
                ] as const
              ).map((name) => {
                const cache = stats.data?.[name];
                return cache ? (
                  <div className="rounded-lg border p-3" key={name}>
                    <dt className="text-sm font-medium capitalize">{name}</dt>
                    <dd className="mt-1 text-sm">
                      {cache.entry_count ?? 0} entries,{" "}
                      {formatBytes(cache.size_bytes)}
                      <p className="text-xs text-muted-foreground">
                        Hit rate {((cache.hit_rate ?? 0) * 100).toFixed(1)}%;{" "}
                        {cache.load_errors ?? 0} load errors
                      </p>
                    </dd>
                  </div>
                ) : null;
              })}
            </dl>
            <Diagnostics label="Raw cache statistics" value={stats.data} />
          </>
        )}
        <div className="flex flex-wrap gap-2">
          {(
            [
              "capabilities",
              "collections",
              "features",
              "counts",
              "tiles",
            ] as const
          ).map((type) => (
            <Button
              variant="outline"
              size="sm"
              disabled={clear.isPending}
              onClick={() => clear.mutate(type)}
              key={type}
            >
              Clear {type}
            </Button>
          ))}
          <Button
            variant="destructive"
            className="text-red-700 dark:text-destructive"
            size="sm"
            disabled={clear.isPending}
            onClick={() => clear.mutate("all")}
          >
            Clear all
          </Button>
        </div>
        {clear.error && (
          <p className="text-sm text-destructive">{clear.error.message}</p>
        )}
      </CardContent>
    </Card>
  );
}
function Field({ label, value }: { label: string; value?: string }) {
  return (
    <div>
      <span className="block text-xs text-muted-foreground">{label}</span>
      <code className="text-xs">{value ?? "—"}</code>
    </div>
  );
}

/** Deletions live on their own page; Operations only says whether to look. */
function DeletionSummary() {
  const actionable = useListDeletionOperations({
    status: "actionable",
    limit: 50,
  });
  const count = actionable.data?.deletions?.length ?? 0;
  const more = Boolean(actionable.data?.next_cursor);
  return (
    <Card className="mt-4">
      <CardHeader>
        <CardTitle>Deletion operations</CardTitle>
      </CardHeader>
      <CardContent className="flex flex-wrap items-center justify-between gap-3">
        <QueryError
          error={actionable.error}
          retry={() => actionable.refetch()}
          context="Deletion operations could not be loaded"
        />
        {!actionable.error && (
          <p className="text-sm" role="status">
            {actionable.isLoading
              ? "Checking deletion operations…"
              : count
                ? `${more ? "More than " : ""}${plural(count, "deletion")} pending, running or failed.`
                : "No deletions need attention."}
          </p>
        )}
        <Button variant="outline" size="sm" asChild>
          <Link
            to={count ? "/deletions?deletion_status=actionable" : "/deletions"}
          >
            Review deletions
          </Link>
        </Button>
      </CardContent>
    </Card>
  );
}
