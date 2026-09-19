import { useMemo, useState } from "react";
import { useSearchParams } from "react-router";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import type { ColumnDef } from "@tanstack/react-table";
import { RefreshCw } from "lucide-react";
import {
  getListAuditEventsQueryKey,
  runAuditRetention,
  useListAuditEvents,
} from "@/api/generated/audit/audit";
import type { ListAuditEventsOutcome } from "@/api/generated/models";
import { Page } from "@/components/Page";
import { QueryError } from "@/components/QueryError";
import { DataTable } from "@/components/DataTable";
import { CursorPager } from "@/components/CursorPager";
import { NativeSelect } from "@/components/NativeSelect";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/AppDialog";
import {
  formatDateTime,
  formatMilliseconds,
  formatRelative,
  plural,
} from "@/lib/format";
import { cn } from "@/lib/utils";
import {
  describeAuditEvent,
  groupAuditEvents,
  statusClass,
  type AuditRow,
} from "./audit-events";

const PAGE_SIZE = 25;

const ranges = {
  "": { label: "Any time", ms: 0 },
  "1h": { label: "Last hour", ms: 3_600_000 },
  "24h": { label: "Last 24 hours", ms: 86_400_000 },
  "7d": { label: "Last 7 days", ms: 7 * 86_400_000 },
} as const;
type Range = keyof typeof ranges;

const outcomes: [string, string][] = [
  ["", "All"],
  ["failed", "Failed"],
  ["succeeded", "Succeeded"],
];

const moreFilters = [
  ["principal", "Principal", "Exact principal"],
  ["workspace", "Workspace", "Exact recorded workspace"],
  ["credential_id", "API-key ID", "Non-secret credential ID"],
] as const;

const statusTone = {
  ok: "text-green-800 dark:text-success",
  client: "text-red-700 dark:text-destructive",
  server: "text-red-700 dark:text-destructive",
} as const;

const columns: ColumnDef<AuditRow, unknown>[] = [
  {
    id: "time",
    header: () => "Time",
    enableSorting: false,
    cell: ({ row }) => {
      const { event, count, earliest } = row.original;
      return (
        <time
          dateTime={event.timestamp}
          title={`${formatDateTime(event.timestamp, { seconds: true })} · ${formatRelative(event.timestamp)}`}
          className="font-mono text-xs whitespace-nowrap"
        >
          {/* Cards have little width: the date moves into the tooltip. */}
          <span className="@2xl/table:hidden">
            {formatDateTime(event.timestamp, {
              timeOnly: true,
              seconds: true,
            })}
          </span>
          <span className="hidden @2xl/table:inline">
            {formatDateTime(event.timestamp, { seconds: true })}
          </span>
          {count > 1 && earliest !== event.timestamp && (
            <span className="block text-muted-foreground">
              since{" "}
              {formatDateTime(earliest, { timeOnly: true, seconds: true })}
            </span>
          )}
        </time>
      );
    },
  },
  {
    id: "status",
    header: () => "Status",
    enableSorting: false,
    cell: ({ row }) => {
      const status = row.original.event.status;
      const kind = statusClass(status);
      return (
        <span
          className={cn("font-mono text-xs font-semibold", statusTone[kind])}
          data-outcome={kind === "ok" ? "succeeded" : "failed"}
        >
          {status}
          <span className="sr-only">
            {kind === "ok" ? " succeeded" : " failed"}
          </span>
        </span>
      );
    },
  },
  {
    id: "operation",
    header: () => "Event",
    enableSorting: false,
    cell: ({ row }) => {
      const { event, count } = row.original;
      const { who, what } = describeAuditEvent(event);
      return (
        <div className="min-w-0 space-y-0.5">
          <p className="break-words">
            <span className="font-medium">{who}</span>
            <span className="text-muted-foreground"> · </span>
            {what}
            {count > 1 && (
              <span
                className="ml-2 rounded-md bg-muted px-1.5 py-0.5 font-mono text-xs"
                aria-label={`${count} identical events`}
              >
                ×{count}
              </span>
            )}
          </p>
          <p className="font-mono text-xs break-all text-muted-foreground">
            {event.method} {event.path}
          </p>
        </div>
      );
    },
  },
  {
    id: "workspace",
    header: () => "Workspace",
    enableSorting: false,
    cell: ({ row }) => row.original.event.workspace || "—",
  },
  {
    id: "credential_id",
    header: () => "API-key ID",
    enableSorting: false,
    cell: ({ row }) => (
      <span className="font-mono text-xs break-all">
        {row.original.event.credential_id || "—"}
      </span>
    ),
  },
  {
    id: "duration_ms",
    header: () => "Duration",
    enableSorting: false,
    cell: ({ row }) => (
      <span className="font-mono text-xs whitespace-nowrap">
        {formatMilliseconds(row.original.event.duration_ms)}
      </span>
    ),
  },
];

export function AuditPage() {
  const client = useQueryClient();
  const [search, setSearch] = useSearchParams();
  const range = (search.get("range") ?? "") as Range;
  const params = {
    credential_id: search.get("credential_id") || undefined,
    principal: search.get("principal") || undefined,
    workspace: search.get("workspace") || undefined,
    q: search.get("q") || undefined,
    outcome: (search.get("outcome") || undefined) as
      ListAuditEventsOutcome | undefined,
    since: search.get("since") || undefined,
    cursor: search.get("cursor") || undefined,
    limit: PAGE_SIZE,
  };
  const audit = useListAuditEvents(params);
  const rows = useMemo(
    () => groupAuditEvents(audit.data?.events ?? []),
    [audit.data],
  );
  const update = (patch: Record<string, string>) =>
    setSearch(
      (previous) => {
        const next = new URLSearchParams(previous);
        for (const [key, value] of Object.entries(patch)) {
          if (value) next.set(key, value);
          else next.delete(key);
        }
        if (!("cursor" in patch)) next.delete("cursor");
        return next;
      },
      { replace: true },
    );
  // The window is fixed when chosen (and on refresh) so the query key stays
  // stable between renders.
  const sinceFor = (value: Range) =>
    ranges[value]?.ms
      ? new Date(Date.now() - ranges[value].ms).toISOString()
      : "";
  const events = audit.data?.events.length ?? 0;
  const filtered = moreFilters.some(([key]) => search.get(key));
  return (
    <Page
      title="Audit"
      description="Sign-ins, catalog changes and protocol requests from the durable audit log. Filters are part of the URL, so a view can be shared."
      action={
        <div className="flex flex-wrap gap-2">
          <RetentionAction />
          <Button
            variant="outline"
            size="sm"
            disabled={audit.isFetching}
            onClick={() => {
              if (range) update({ since: sinceFor(range) });
              void client.invalidateQueries({
                queryKey: getListAuditEventsQueryKey(),
              });
            }}
          >
            <RefreshCw /> Refresh
          </Button>
        </div>
      }
    >
      <QueryError
        error={audit.error}
        retry={() => audit.refetch()}
        context="Audit events could not be loaded"
      />
      <DataTable
        data={rows}
        columns={columns}
        isLoading={audit.isLoading}
        serverMode
        urlKey="events"
        getRowId={(row) => row.id}
        emptyState="No events match these filters."
        columnClassNames={{
          time: "w-24 @2xl/table:w-44",
          status: "w-16",
          operation: "whitespace-normal",
          workspace: "hidden @2xl/table:table-cell",
          credential_id: "hidden @5xl/table:table-cell",
          duration_ms: "hidden @3xl/table:table-cell",
        }}
        tableClassName="table-auto"
        toolbar={
          <div className="flex w-full flex-wrap items-center gap-2 @4xl/table:w-auto @4xl/table:flex-1">
            <div
              role="group"
              aria-label="Outcome"
              className="inline-flex rounded-lg border p-0.5"
            >
              {outcomes.map(([value, label]) => (
                <Button
                  key={label}
                  size="sm"
                  variant={
                    (params.outcome ?? "") === value ? "secondary" : "ghost"
                  }
                  aria-pressed={(params.outcome ?? "") === value}
                  onClick={() => update({ outcome: value })}
                >
                  {label}
                </Button>
              ))}
            </div>
            <NativeSelect
              aria-label="Time range"
              className="h-8 w-auto"
              value={range}
              onChange={(event) => {
                const value = event.target.value as Range;
                update({ range: value, since: sinceFor(value) });
              }}
            >
              {Object.entries(ranges).map(([value, { label }]) => (
                <option key={value} value={value}>
                  {label}
                </option>
              ))}
            </NativeSelect>
            <Input
              className="h-8 max-w-72"
              aria-label="Search all retained events"
              placeholder="Search operation, path, status…"
              value={search.get("q") ?? ""}
              onChange={(event) => update({ q: event.target.value })}
            />
            <details className="group w-full" open={filtered || undefined}>
              <summary className="cursor-pointer text-xs text-muted-foreground">
                More filters{filtered ? " (active)" : ""}
              </summary>
              <div className="mt-2 grid gap-3 sm:grid-cols-3">
                {moreFilters.map(([key, label, placeholder]) => (
                  <div key={key} className="space-y-1.5">
                    <Label htmlFor={`audit-${key}`}>{label}</Label>
                    <Input
                      id={`audit-${key}`}
                      value={search.get(key) ?? ""}
                      placeholder={placeholder}
                      onChange={(event) =>
                        update({ [key]: event.target.value })
                      }
                    />
                  </div>
                ))}
              </div>
            </details>
          </div>
        }
        footer={
          <CursorPager
            busy={audit.isFetching}
            summary={`${plural(events, "event")} on this page${
              rows.length < events ? ` in ${plural(rows.length, "line")}` : ""
            } · newest first${
              audit.data && !audit.data.next_cursor
                ? " · end of retained history"
                : ""
            }`}
            newerLabel="Newer events"
            olderLabel="Older events"
            onNewer={params.cursor ? () => update({ cursor: "" }) : undefined}
            onOlder={
              audit.data?.next_cursor
                ? () => update({ cursor: audit.data!.next_cursor! })
                : undefined
            }
          />
        }
      />
    </Page>
  );
}

function RetentionAction() {
  const client = useQueryClient();
  const [open, setOpen] = useState(false);
  const retention = useMutation({
    meta: { suppressErrorToast: true },
    mutationFn: () => runAuditRetention(),
    onSuccess: async () => {
      setOpen(false);
      await client.invalidateQueries({
        queryKey: getListAuditEventsQueryKey(),
      });
    },
  });
  return (
    <>
      <Button variant="outline" size="sm" onClick={() => setOpen(true)}>
        Run retention
      </Button>
      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Purge expired audit events?</DialogTitle>
            <DialogDescription>
              Events older than the configured retention period are deleted
              permanently. Current and in-window events remain available.
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setOpen(false)}>
              Cancel
            </Button>
            <Button
              variant="destructive"
              disabled={retention.isPending}
              onClick={() => retention.mutate()}
            >
              Purge expired events
            </Button>
          </DialogFooter>
          {retention.error && (
            <p className="text-sm text-destructive">
              {retention.error.message}
            </p>
          )}
        </DialogContent>
      </Dialog>
    </>
  );
}
