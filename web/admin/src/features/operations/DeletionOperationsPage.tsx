import { useSearchParams, useParams } from "react-router";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import {
  getGetDeletionOperationQueryKey,
  getListDeletionOperationsQueryKey,
  retryDeletionOperation,
  useGetDeletionOperation,
  useListDeletionOperations,
} from "@/api/generated/catalog-lifecycle/catalog-lifecycle";
import { useGetWorkspace } from "@/api/generated/workspaces/workspaces";
import type {
  DeletionList,
  DeletionOperation,
  ListDeletionOperationsParams,
} from "@/api/generated/models";
import { useAuth } from "@/auth/auth-context";
import { QueryError } from "@/components/QueryError";
import { StatusChip } from "@/components/StatusChip";
import { Button } from "@/components/ui/button";
import { ResourcePage } from "@/features/shared/ResourcePage";
import { isActiveStatus, useActivePolling } from "@/hooks/use-active-polling";
import { NativeSelect } from "@/components/NativeSelect";
import { CursorPager } from "@/components/CursorPager";
import { plural } from "@/lib/format";

const listPolling = {
  isActive: (data: DeletionList | undefined) =>
    data?.deletions.some((op) => isActiveStatus(op.status)) ?? false,
  signature: (data: DeletionList | undefined) =>
    data?.deletions
      .map((op) => `${op.id}:${op.status}:${op.phase}:${op.updated_at}`)
      .join("|") ?? "",
};
const detailPolling = {
  isActive: (data: DeletionOperation | undefined) =>
    isActiveStatus(data?.status),
  signature: (data: DeletionOperation | undefined) =>
    `${data?.id}:${data?.status}:${data?.phase}:${data?.updated_at}`,
};

export function DeletionOperationsPage() {
  const { ws = "" } = useParams();
  const workspace = useGetWorkspace(ws);
  return workspace.data ? (
    <DeletionHistory key={workspace.data.id} workspaceID={workspace.data.id} />
  ) : (
    <div className="p-6">
      {workspace.isLoading && <p>Loading workspace…</p>}
      <QueryError error={workspace.error} retry={() => workspace.refetch()} />
    </div>
  );
}

/** Server-wide deletion history, including deleted workspaces. */
export function ServerDeletionsPage() {
  return <DeletionHistory />;
}

export function DeletionHistory({
  workspaceID,
  embedded = false,
}: {
  workspaceID?: string;
  embedded?: boolean;
}) {
  const [search, setSearch] = useSearchParams();
  const { me } = useAuth();
  const client = useQueryClient();
  const selected = search.get("operation") ?? "";
  const cursor = search.get("deletion_cursor") ?? "";
  const statusValue = search.get("deletion_status") ?? "";
  const status = (
    ["pending", "running", "failed", "completed", "actionable"] as const
  ).find((value) => value === statusValue);
  const scope = workspaceID ?? search.get("deletion_workspace") ?? "";
  const params: ListDeletionOperationsParams = {
    workspace: scope || undefined,
    status,
    limit: 50,
    cursor: cursor || undefined,
  };
  const list = useListDeletionOperations(params, {
    query: useActivePolling(listPolling),
  });
  const detail = useGetDeletionOperation(selected, {
    query: { enabled: Boolean(selected), ...useActivePolling(detailPolling) },
  });
  const refresh = () =>
    client.invalidateQueries({ queryKey: getListDeletionOperationsQueryKey() });
  const change = (key: string, value: string, reset = false) => {
    setSearch((previous) => {
      const next = new URLSearchParams(previous);
      if (reset) next.delete("deletion_cursor");
      if (value) next.set(key, value);
      else next.delete(key);
      return next;
    });
  };
  const operation =
    !detail.error &&
    detail.data &&
    (!workspaceID || detail.data.workspace_id === workspaceID)
      ? detail.data
      : undefined;
  const retry = useMutation({
    meta: { suppressErrorToast: true },
    mutationFn: () => retryDeletionOperation(selected),
    onSuccess: async (result) => {
      client.setQueryData(getGetDeletionOperationQueryKey(selected), result);
      await refresh();
    },
  });
  return (
    <ResourcePage
      embedded={embedded}
      title="Deletion operations"
      description="Accepted deletions run asynchronously. Track progress or retry failed cleanup here, even after the target disappears."
      rows={list.data?.deletions ?? []}
      isLoading={list.isLoading}
      error={list.error}
      urlKey="deletions"
      serverMode
      onRefresh={refresh}
      columns={[
        "target_name",
        "scope",
        "status",
        "phase",
        "attempt_count",
        "last_error",
        "updated_at",
      ]}
      tableToolbar={
        <div className="flex flex-wrap items-center gap-2">
          <NativeSelect
            id="deletion-status"
            aria-label="Status"
            className="h-9 w-auto"
            value={status ?? ""}
            onChange={(event) =>
              change("deletion_status", event.target.value, true)
            }
          >
            <option value="">All statuses</option>
            <option value="actionable">
              Needs attention (pending, running, failed)
            </option>
            <option value="pending">Pending</option>
            <option value="running">Running</option>
            <option value="failed">Failed</option>
            <option value="completed">Completed</option>
          </NativeSelect>
          {!workspaceID && (
            <>
              <NativeSelect
                id="deletion-workspace"
                aria-label="Workspace"
                className="h-9 w-auto max-w-72"
                value={scope}
                onChange={(event) =>
                  change("deletion_workspace", event.target.value, true)
                }
              >
                <option value="">
                  All accessible workspaces (including deleted)
                </option>
                {me?.workspaces.map((workspace) => (
                  <option key={workspace.id} value={workspace.id}>
                    {workspace.name}
                  </option>
                ))}
                {scope &&
                  !me?.workspaces.some(
                    (workspace) => workspace.id === scope,
                  ) && <option value={scope}>{scope} (historical)</option>}
              </NativeSelect>
            </>
          )}
        </div>
      }
      tableFooter={
        <CursorPager
          busy={list.isFetching}
          summary={`${plural(list.data?.deletions?.length ?? 0, "operation")} on this page · newest first`}
          onNewer={cursor ? () => change("deletion_cursor", "") : undefined}
          onOlder={
            list.data?.next_cursor && !list.error
              ? () => change("deletion_cursor", list.data?.next_cursor ?? "")
              : undefined
          }
        />
      }
      renderActions={(row) => (
        <Button
          variant="outline"
          size="sm"
          onClick={() => {
            retry.reset();
            change("operation", String(row.id));
          }}
        >
          Inspect {String(row.target_name)}
        </Button>
      )}
      extraContent={
        selected && (
          <section
            aria-label="Deletion operation detail"
            className="space-y-3 rounded-lg border p-4"
          >
            <div className="flex items-center justify-between gap-3">
              <h2 className="font-semibold">Deletion operation</h2>
              <Button variant="ghost" onClick={() => change("operation", "")}>
                Close detail
              </Button>
            </div>
            <p className="break-all font-mono text-xs">
              Operation ID: {selected}
            </p>
            {detail.isLoading && <p role="status">Loading operation…</p>}
            <QueryError error={detail.error} retry={() => detail.refetch()} />
            {detail.data && !operation && (
              <p role="alert">
                This operation belongs to a different workspace.
              </p>
            )}
            {operation && (
              <>
                <div role="status" aria-live="polite" className="space-y-2">
                  <p>
                    <strong>{operation.target_name}</strong>{" "}
                    <StatusChip value={operation.status} />
                  </p>
                  <p>
                    {operation.status === "completed"
                      ? "Deletion completed."
                      : operation.status === "failed"
                        ? "Deletion failed. Review the error and retry cleanup."
                        : "Deletion accepted; cleanup is not complete yet. This page follows progress automatically."}
                  </p>
                  <p className="text-sm">
                    Phase: {operation.phase} · Attempts:{" "}
                    {operation.attempt_count}
                  </p>
                </div>
                {operation.last_error && (
                  <p
                    role="alert"
                    className="break-words text-sm text-destructive"
                  >
                    {operation.last_error}
                  </p>
                )}
                {operation.status === "failed" && (
                  <Button
                    disabled={retry.isPending}
                    onClick={() => retry.mutate()}
                  >
                    {retry.isPending ? "Retrying…" : "Retry deletion"}
                  </Button>
                )}
                <QueryError error={retry.error} retry={() => retry.mutate()} />
                <details>
                  <summary className="cursor-pointer text-sm">
                    Dependency plan
                  </summary>
                  <pre
                    tabIndex={0}
                    aria-label="Deletion dependency plan"
                    className="max-h-80 overflow-auto p-3 text-xs"
                  >
                    {JSON.stringify(operation.plan, null, 2)}
                  </pre>
                </details>
              </>
            )}
          </section>
        )
      }
    />
  );
}
