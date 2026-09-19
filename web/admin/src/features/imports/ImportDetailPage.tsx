import { lazy, Suspense, useEffect, useMemo, useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { ArrowLeft, Globe2, RotateCcw, Square, Undo2 } from "lucide-react";
import { Link, useLocation, useParams } from "react-router";
import { PublishedImportResults } from "./PublishedImportResults";
import { useStableCounters, type Counters } from "./use-stable-counters";
import { planFromDiscovery } from "./import-plan";
import { ImportPlanEditor } from "./ImportPlanEditor";
import { useUnsavedChangesGuard } from "@/lib/forms";
import { basePath } from "@/api/client";
import { getListServicesQueryKey } from "@/api/generated/services/services";
import { getListLayersQueryKey } from "@/api/generated/layers/layers";
import {
  getGetWorkspaceSummaryQueryKey,
  getListWorkspacesQueryKey,
} from "@/api/generated/workspaces/workspaces";
import type {
  ImportEvent,
  ImportJob,
  ImportPlan,
} from "@/api/generated/models";
import {
  getGetImportHistoryQueryKey,
  getGetImportQueryKey,
  getListImportsQueryKey,
  cancelImport,
  publishImport,
  retryImport,
  rollbackImport,
  setImportPlan,
  useGetImport,
  useGetImportHistory,
  usePreviewImport,
} from "@/api/generated/imports/imports";
import { isActiveStatus, useActivePolling } from "@/hooks/use-active-polling";
import { Page } from "@/components/Page";
import { PhaseStrip } from "@/components/PhaseStrip";
import { QueryError } from "@/components/QueryError";
import { StatusChip } from "@/components/StatusChip";
import { Button } from "@/components/ui/button";
import { confirmAction } from "@/lib/confirm";
import { formatBytes, formatDateTime } from "@/lib/format";
const FeaturePreview = lazy(() =>
  import("@/features/viewer/FeaturePreview").then((module) => ({
    default: module.FeaturePreview,
  })),
);

/** The lifecycle actions the detail page can trigger on a job. */
type ImportAction = "plan" | "publish" | "retry" | "rollback" | "cancel";

const detailPolling = {
  isActive: (data: ImportJob | undefined) => isActiveStatus(data?.status),
  signature: (data: ImportJob | undefined) =>
    `${data?.status ?? ""}:${data?.phase ?? ""}`,
};

/** Where "All imports" returns to: the list page the user came from. */
export interface ImportListLocationState {
  listSearch?: string;
}

const HISTORY_PREVIEW = 8;

export function ImportDetailPage() {
  const { ws = "", importId = "" } = useParams();
  return <ImportDetail key={`${ws}/${importId}`} ws={ws} id={importId} />;
}

function ImportDetail({ ws, id }: { ws: string; id: string }) {
  const client = useQueryClient();
  const location = useLocation();
  const listSearch =
    (location.state as ImportListLocationState | null)?.listSearch ?? "";
  const [dirty, setDirty] = useState(false);
  useUnsavedChangesGuard(dirty);
  const detail = useGetImport(ws, id, {
    query: { enabled: Boolean(id), ...useActivePolling(detailPolling) },
  });
  const history = useGetImportHistory(ws, id, {
    query: { enabled: Boolean(id) },
  });
  const refreshJob = async (jobID: string) => {
    await Promise.all([
      client.invalidateQueries({ queryKey: getListImportsQueryKey(ws) }),
      client.invalidateQueries({ queryKey: getGetImportQueryKey(ws, jobID) }),
      client.invalidateQueries({
        queryKey: getGetImportHistoryQueryKey(ws, jobID),
      }),
      client.invalidateQueries({
        predicate: (query) =>
          String(query.queryKey[0]).includes(
            `/imports/${encodeURIComponent(jobID)}/preview`,
          ),
      }),
    ]);
  };
  const action = useMutation({
    meta: { suppressErrorToast: true },
    mutationFn: ({
      verb,
      body,
    }: {
      verb: ImportAction;
      body?: unknown;
    }): Promise<ImportJob> => {
      switch (verb) {
        case "plan":
          return setImportPlan(ws, id, body as ImportPlan);
        case "publish":
          return publishImport(ws, id);
        case "retry":
          return retryImport(ws, id);
        case "rollback":
          return rollbackImport(ws, id);
        case "cancel":
          return cancelImport(ws, id);
      }
    },
    onSuccess: async (job) => {
      setDirty(false);
      client.setQueryData(getGetImportQueryKey(ws, job.id), job);
      await refreshJob(job.id);
    },
  });
  const current = detail.data;
  const counters = useStableCounters(current);
  const catalogStatus = current?.status;
  const catalogServiceID = current?.service_id;
  useEffect(() => {
    // Publish/rollback return before the worker commits the catalog. Refresh
    // when polling observes completion, not when the action is merely queued.
    if (catalogStatus !== "published" && catalogStatus !== "rolled_back")
      return;
    void client.invalidateQueries({ queryKey: getListServicesQueryKey(ws) });
    void client.invalidateQueries({
      queryKey: getGetWorkspaceSummaryQueryKey(ws),
    });
    void client.invalidateQueries({ queryKey: getListWorkspacesQueryKey() });
    if (catalogServiceID)
      void client.invalidateQueries({
        queryKey: getListLayersQueryKey(ws, catalogServiceID),
      });
    void client.invalidateQueries({
      queryKey: [
        `${basePath}/workspaces/${encodeURIComponent(ws)}/ogc/collections`,
      ],
    });
  }, [client, ws, catalogStatus, catalogServiceID]);
  const initialPlan = useMemo(
    () => current?.plan ?? planFromDiscovery(current),
    [current],
  );
  const listPath = `/workspaces/${encodeURIComponent(ws)}/imports${listSearch}`;
  return (
    <Page
      title={current?.name ?? "Import"}
      description="Inspect, plan, validate, preview, and publish this managed dataset."
      action={
        <Button variant="outline" size="sm" asChild>
          <Link to={listPath}>
            <ArrowLeft /> All imports
          </Link>
        </Button>
      }
    >
      <QueryError
        error={detail.error || history.error}
        retry={() => {
          void detail.refetch();
          void history.refetch();
        }}
        context="Import data could not be fully loaded"
      />
      <QueryError error={action.error} context="The import action failed" />
      {detail.isLoading && <p role="status">Loading import…</p>}
      {current && (
        <ImportInspector
          key={`${current.id}-${current.status}`}
          workspace={ws}
          onDirty={setDirty}
          job={current}
          history={history.data?.events ?? []}
          initialPlan={initialPlan}
          busy={action.isPending}
          run={(verb, body) => action.mutate({ verb, body })}
          counters={counters}
        />
      )}
    </Page>
  );
}

function ImportInspector({
  workspace,
  onDirty,
  job,
  history,
  initialPlan,
  busy,
  run,
  counters,
}: {
  workspace: string;
  onDirty: (dirty: boolean) => void;
  job: ImportJob;
  counters: Counters;
  history: ImportEvent[];
  initialPlan: ImportPlan;
  busy: boolean;
  run: (verb: ImportAction, body?: unknown) => void;
}) {
  const [revising, setRevising] = useState(false);
  const [planDirty, setPlanDirty] = useState(false);
  const [showAllHistory, setShowAllHistory] = useState(false);
  const preview = usePreviewImport(workspace, job.id, {
    query: { enabled: job.status === "ready_to_publish" },
  });
  const running = isActiveStatus(job.status);
  const canCancel =
    running ||
    job.status === "awaiting_plan" ||
    job.status === "ready_to_publish" ||
    job.status === "failed";
  const canEditPlan =
    Boolean(job.discovery) &&
    ["awaiting_plan", "ready_to_publish", "failed"].includes(job.status) &&
    job.phase !== "rollback" &&
    !job.service_id;
  return (
    <div className="min-w-0 space-y-6">
      <section
        aria-label="Import status"
        className="space-y-4 rounded-lg border p-4"
      >
        <div className="flex flex-wrap items-start justify-between gap-3">
          <div className="min-w-0 space-y-1 break-all">
            <p className="font-mono text-xs text-muted-foreground">{job.id}</p>
            <p className="text-sm text-muted-foreground">
              Source:{" "}
              <span className="font-mono">
                {job.source_locator || job.source_kind}
              </span>
            </p>
          </div>
          <div className="flex items-center gap-2">
            {running && (
              <span role="status" className="text-xs text-muted-foreground">
                Working…
              </span>
            )}
            <StatusChip value={job.status} />
          </div>
        </div>
        <PhaseStrip
          phase={job.phase}
          failed={job.status === "failed"}
          complete={job.status === "published"}
        />
        {job.error_message && (
          <pre className="whitespace-pre-wrap border-l-2 border-destructive bg-destructive/5 p-3 text-xs text-destructive">
            {job.phase}: {job.error_message}
          </pre>
        )}
        {job.discovery?.warnings?.map((warning) => (
          <p
            className="border-l-2 border-warning bg-warning/10 p-2 text-sm"
            key={warning}
          >
            {warning}
          </p>
        ))}
        <div className="grid grid-cols-3 gap-2 text-center sm:max-w-md">
          <Metric label="Processed" value={formatBytes(counters.bytes)} />
          <Metric label="Features" value={counters.features} />
          <Metric
            label="Layers"
            value={`${counters.layers}/${job.total_layers ?? 0}`}
          />
        </div>
        <div className="flex flex-wrap gap-2">
          {job.status === "ready_to_publish" && (
            <Button
              disabled={busy || revising || planDirty}
              onClick={() => run("publish")}
            >
              <Globe2 /> Publish dataset
            </Button>
          )}
          {job.status === "ready_to_publish" && !revising && (
            <Button
              variant="outline"
              disabled={busy}
              onClick={() => setRevising(true)}
            >
              Revise plan
            </Button>
          )}
          {job.status === "failed" && (
            <Button disabled={busy} onClick={() => run("retry")}>
              <RotateCcw /> Retry import
            </Button>
          )}
          {canCancel && (
            <Button
              variant="outline"
              disabled={busy}
              onClick={async () => {
                if (
                  await confirmAction({
                    title: "Cancel this import?",
                    description: "Incomplete staged data will be discarded.",
                    confirmLabel: "Cancel import",
                    cancelLabel: "Keep import",
                    destructive: true,
                  })
                )
                  run("cancel");
              }}
            >
              <Square /> Cancel import
            </Button>
          )}
        </div>
      </section>
      {job.status === "published" && (
        <section aria-label="Published results">
          <PublishedImportResults workspace={workspace} job={job} />
        </section>
      )}
      {canEditPlan && (job.status !== "ready_to_publish" || revising) && (
        <section aria-labelledby="import-plan-heading" className="space-y-3">
          <h2 id="import-plan-heading" className="text-lg font-semibold">
            Plan
          </h2>
          <ImportPlanEditor
            key={`${job.id}-${job.status}`}
            workspace={workspace}
            job={job}
            initialPlan={initialPlan}
            busy={busy}
            submit={(plan) => run("plan", plan)}
            onDirty={(dirty) => {
              setPlanDirty(dirty);
              onDirty(dirty);
            }}
          />
          <p className="text-xs text-muted-foreground">
            The displayed plan is the latest submitted revision. Revalidate
            changes before publishing. Sources are retained until publication or
            cancellation.
          </p>
        </section>
      )}
      {job.status === "ready_to_publish" && !revising && (
        <section aria-labelledby="import-preview-heading" className="space-y-3">
          <h2 id="import-preview-heading" className="text-lg font-semibold">
            Preview
          </h2>
          <QueryError
            error={preview.error}
            retry={() => preview.refetch()}
            context="The import sample could not be loaded"
          />
          {preview.isLoading && <p role="status">Loading preview sample…</p>}
          {preview.data && (
            <Suspense fallback={<p>Loading preview map…</p>}>
              <FeaturePreview data={preview.data} urlKey="import_preview" />
            </Suspense>
          )}
        </section>
      )}
      <section aria-labelledby="import-history-heading" className="space-y-2">
        <h2 id="import-history-heading" className="text-lg font-semibold">
          History & diagnostics
        </h2>
        <div
          role="region"
          aria-label="Import event history"
          className="space-y-2 rounded-lg border p-3"
        >
          {(showAllHistory ? history : history.slice(-HISTORY_PREVIEW)).map(
            (event) => (
              <div
                key={event.id}
                className="grid grid-cols-[7rem_1fr] gap-3 border-b pb-2 text-xs"
              >
                <time className="font-mono text-muted-foreground">
                  {formatDateTime(event.created_at, {
                    timeOnly: true,
                    seconds: true,
                  })}
                </time>
                <div>
                  <span className="font-mono">{event.phase}</span> ·{" "}
                  {event.status}
                  {event.error_message && (
                    <pre className="whitespace-pre-wrap text-destructive">
                      {event.error_message}
                    </pre>
                  )}
                </div>
              </div>
            ),
          )}
          {history.length === 0 && (
            <p className="text-xs text-muted-foreground">
              No history events recorded yet.
            </p>
          )}
          {history.length > HISTORY_PREVIEW && (
            <Button
              variant="ghost"
              size="sm"
              aria-expanded={showAllHistory}
              onClick={() => setShowAllHistory((value) => !value)}
            >
              {showAllHistory
                ? `Show the latest ${HISTORY_PREVIEW} events`
                : `Show all ${history.length} events`}
            </Button>
          )}
        </div>
        {job.status === "published" && (
          <div className="flex flex-wrap items-center gap-3 rounded-lg border border-dashed p-3 text-sm">
            <p className="min-w-0 flex-1 text-muted-foreground">
              Rolling back removes the managed dataset and every layer published
              from it.
            </p>
            {job.status === "published" && (
              <Button
                variant="outline"
                className="text-red-700 dark:text-destructive"
                disabled={busy}
                onClick={async () => {
                  if (
                    await confirmAction({
                      title: "Roll back this publication?",
                      description:
                        "Rollback deletes the managed dataset and all layers published from it.",
                      confirmLabel: "Roll back",
                      destructive: true,
                    })
                  )
                    run("rollback");
                }}
              >
                <Undo2 /> Roll back publication
              </Button>
            )}
          </div>
        )}
      </section>
    </div>
  );
}

function Metric({ label, value }: { label: string; value: string | number }) {
  return (
    <div className="rounded-md border p-2">
      <span className="block font-mono text-lg">{value}</span>
      <span className="text-xs text-muted-foreground">{label}</span>
    </div>
  );
}
