import { useCallback, useMemo, useState } from "react";
import { toast } from "sonner";
import type { ReactNode } from "react";
import { Eye, Pencil, Plus, RefreshCw, Trash2 } from "lucide-react";
import { Page } from "@/components/Page";
import { DisplayValue, ResourceDetails } from "@/components/ResourceDetails";
import { columnLabel } from "@/lib/schema-fields";
import { Button } from "@/components/ui/button";
import { isAccessError } from "@/lib/query-access";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@/components/AppDialog";
import { Textarea } from "@/components/ui/textarea";
import { DataTable } from "@/components/DataTable";
import type { ColumnDef } from "@tanstack/react-table";
import { useUnsavedChangesGuard } from "@/lib/forms";
import { QueryError } from "@/components/QueryError";
import { ObjectEditor } from "@/components/SchemaFields";
import { validateFields, type FieldChoices } from "@/lib/schema-fields";
import { resourceSchema } from "@/lib/resource-schemas";
import { confirmDiscard } from "@/lib/confirm";

/**
 * Rows arrive as generated API interfaces, which have no index signature, so
 * the public props accept plain objects and the table reads them as records
 * internally.
 */
export type ResourceRow = Record<string, unknown>;

/**
 * Presentational list screen: table, create/edit/delete dialogs, and the
 * unsaved-changes guard.
 *
 * Deliberately owns no data fetching. Callers supply rows and callbacks from
 * the generated API hooks, which keeps every request typed against the server's
 * OpenAPI document instead of a hand-built endpoint string.
 */
export function ResourcePage({
  title,
  description,
  rows: incomingRows,
  isLoading = false,
  error,
  columns,
  urlKey,
  onRefresh,
  createTemplate,
  createLabel = "Create",
  onCreate,
  onLoadItem,
  onUpdate,
  onDelete,
  editValue = (row) => row,
  deleteDescription = "This action cannot be undone.",
  renderActions,
  extraAction,
  extraContent,
  choices,
  validate,
  serverMode,
  tableToolbar,
  tableFooter,
  createDescription,
  canUpdate,
  canDelete,
  embedded,
}: {
  title: string;
  description: string;
  rows: readonly object[];
  isLoading?: boolean;
  error?: unknown;
  columns: string[];
  /** Namespace for the table's URL state; required when a route has two. */
  urlKey: string;
  onRefresh: () => void | Promise<unknown>;
  /** Seed JSON for the create dialog. Omit to hide the create action. */
  createTemplate?: ResourceRow;
  createLabel?: string;
  onCreate?: (body: ResourceRow) => Promise<unknown>;
  /** Fetches the full representation to edit. Falls back to the table row. */
  onLoadItem?: (row: ResourceRow) => Promise<object>;
  /** Omit to hide the edit action. */
  onUpdate?: (row: ResourceRow, body: unknown) => Promise<unknown>;
  /** Omit to hide the delete action. */
  onDelete?: (row: ResourceRow) => Promise<unknown>;
  editValue?: (row: ResourceRow) => unknown;
  deleteDescription?: string;
  renderActions?: (
    row: ResourceRow,
    refresh: () => void | Promise<unknown>,
    onDirty: (dirty: boolean) => void,
  ) => ReactNode;
  extraAction?: ReactNode | ((onDirty: (dirty: boolean) => void) => ReactNode);
  extraContent?: ReactNode;
  choices?: FieldChoices;
  validate?: (value: ResourceRow) => string | undefined;
  serverMode?: boolean;
  tableToolbar?: ReactNode;
  tableFooter?: ReactNode;
  /** One sentence under the create dialog title. */
  createDescription?: string;
  canUpdate?: (row: ResourceRow) => boolean;
  canDelete?: (row: ResourceRow) => boolean;
  embedded?: boolean;
}) {
  const rows = incomingRows as ResourceRow[];
  const [open, setOpen] = useState(false);
  const [draft, setDraft] = useState("");
  const [pristineDraft, setPristineDraft] = useState("");
  const [editing, setEditing] = useState<ResourceRow | null>(null);
  const [deleting, setDeleting] = useState<ResourceRow | null>(null);
  const [draftError, setDraftError] = useState("");
  const [busy, setBusy] = useState(false);
  const [actionDirty, setActionDirty] = useState(false);
  const [extraActionDirty, setExtraActionDirty] = useState(false);
  const dirty = draft !== pristineDraft;
  const schema = resourceSchema(urlKey, editing ?? undefined);
  const readOnly = Boolean(editing && canUpdate && !canUpdate(editing));
  function validateDraft(value: ResourceRow) {
    const message =
      (schema ? validateFields(schema, value).join(" ") : "") ||
      validate?.(value);
    if (message) {
      setDraftError(message);
      return false;
    }
    return true;
  }
  async function canClose() {
    return (
      !busy && (!dirty || (await confirmDiscard("Discard unsaved changes?")))
    );
  }

  // An open editor whose JSON has diverged from what was loaded holds unsaved
  // work; block navigation rather than dropping it silently.
  useUnsavedChangesGuard(
    actionDirty ||
      extraActionDirty ||
      ((open || Boolean(editing)) && draft !== pristineDraft),
    "This editor has unsaved changes. Discard them?",
  );

  const editable = Boolean(onUpdate);
  const deletable = Boolean(onDelete);
  const hasActions = editable || deletable || Boolean(renderActions);

  /**
   * Keep failed drafts open with a persistent explanation, even after the
   * mutation notification has disappeared.
   */
  async function run(action: () => Promise<unknown>, onDone: () => void) {
    setBusy(true);
    setDraftError("");
    try {
      await action();
      onDone();
    } catch (error) {
      setDraftError(
        error instanceof Error
          ? error.message
          : "The change could not be saved. Check the values and try again.",
      );
    } finally {
      setBusy(false);
    }
  }

  const beginEdit = useCallback(
    async (row: ResourceRow) => {
      try {
        const value = ((await onLoadItem?.(row)) ?? row) as ResourceRow;
        const loaded = JSON.stringify(editValue(value), null, 2);
        setDraft(loaded);
        setPristineDraft(loaded);
        setDraftError("");
        setEditing(row);
      } catch (reason) {
        setDraftError(
          reason instanceof Error ? reason.message : "Failed to load resource",
        );
      }
    },
    [onLoadItem, editValue],
  );

  const tableColumns = useMemo<ColumnDef<ResourceRow, unknown>[]>(() => {
    const defs: ColumnDef<ResourceRow, unknown>[] = columns.map((column) => ({
      id: column,
      accessorFn: (row) => row[column],
      header: () => columnLabel(column),
      cell: ({ row }) => (
        <DisplayValue field={column} value={row.original[column]} />
      ),
      // Values are heterogeneous (booleans, objects, ids), so sort on a stable
      // string projection rather than the raw value.
      sortingFn: (a, b) =>
        String(a.original[column] ?? "").localeCompare(
          String(b.original[column] ?? ""),
        ),
    }));
    if (!hasActions) {
      return defs;
    }
    defs.push({
      id: "actions",
      header: () => "Actions",
      enableSorting: false,
      enableHiding: false,
      cell: ({ row }) => {
        const value = row.original;
        const label = String(
          value.name ?? value.public_id ?? value.id ?? "resource",
        );
        return (
          <div className="flex items-center justify-end gap-1">
            {renderActions?.(value, onRefresh, setActionDirty)}
            {editable && (
              <Button
                variant="ghost"
                size="icon-sm"
                aria-label={`${canUpdate?.(value) === false ? "Inspect" : "Edit"} ${label}`}
                onClick={() => void beginEdit(value)}
              >
                {canUpdate?.(value) === false ? <Eye /> : <Pencil />}
              </Button>
            )}
            {deletable && canDelete?.(value) !== false && (
              <Button
                variant="ghost"
                size="icon-sm"
                aria-label={`Delete ${label}`}
                onClick={() => setDeleting(value)}
              >
                <Trash2 />
              </Button>
            )}
          </div>
        );
      },
    });
    return defs;
  }, [
    columns,
    hasActions,
    editable,
    deletable,
    renderActions,
    beginEdit,
    onRefresh,
    canUpdate,
    canDelete,
  ]);

  return (
    <Page
      embedded={embedded}
      title={title}
      description={description}
      action={
        <div className="flex flex-wrap gap-2">
          {typeof extraAction === "function"
            ? extraAction(setExtraActionDirty)
            : extraAction}
          <Button variant="outline" size="sm" onClick={() => void onRefresh()}>
            <RefreshCw /> Refresh
          </Button>
          {createTemplate && onCreate && (
            <Dialog
              open={open}
              onOpenChange={async (value) => {
                if (value || (await canClose())) setOpen(value);
              }}
            >
              <DialogTrigger asChild>
                <Button
                  size="sm"
                  onClick={() => {
                    const template = JSON.stringify(createTemplate, null, 2);
                    setDraft(template);
                    setPristineDraft(template);
                    setDraftError("");
                  }}
                >
                  <Plus /> {createLabel}
                </Button>
              </DialogTrigger>
              <DialogContent className="max-h-[90vh] overflow-y-auto sm:max-w-2xl">
                <DialogHeader>
                  <DialogTitle>{createLabel}</DialogTitle>
                  <DialogDescription>
                    {createDescription ??
                      "Fields marked * are required. Uncommon options are under Advanced JSON."}
                  </DialogDescription>
                </DialogHeader>
                {schema ? (
                  <ObjectEditor
                    schema={schema}
                    draft={draft}
                    label={createLabel}
                    disabled={busy}
                    choices={choices}
                    onChange={(next) => {
                      setDraft(next);
                      setDraftError("");
                    }}
                  />
                ) : (
                  <Textarea
                    className="min-h-72 font-mono text-xs"
                    aria-label={`${createLabel} payload`}
                    value={draft}
                    onChange={(event) => {
                      setDraft(event.target.value);
                      setDraftError("");
                    }}
                  />
                )}
                <DialogFooter>
                  <Button
                    variant="outline"
                    disabled={busy}
                    onClick={async () => {
                      if (await canClose()) setOpen(false);
                    }}
                  >
                    Cancel
                  </Button>
                  <Button
                    disabled={busy}
                    onClick={() => {
                      let body: ResourceRow;
                      try {
                        body = JSON.parse(draft) as ResourceRow;
                      } catch (reason) {
                        setDraftError(
                          reason instanceof Error
                            ? reason.message
                            : "Invalid JSON",
                        );
                        return;
                      }
                      if (!validateDraft(body)) return;
                      void run(
                        () => onCreate(body),
                        () => setOpen(false),
                      );
                    }}
                  >
                    {createLabel}
                  </Button>
                </DialogFooter>
                {draftError && (
                  <p className="text-sm text-destructive">{draftError}</p>
                )}
              </DialogContent>
            </Dialog>
          )}
        </div>
      }
    >
      <QueryError error={error} retry={onRefresh} />
      {!isAccessError(error) && (!error || rows.length > 0) && (
        <DataTable
          serverMode={serverMode}
          toolbar={tableToolbar}
          footer={tableFooter}
          data={rows}
          columns={tableColumns}
          isLoading={isLoading}
          urlKey={urlKey}
          filterPlaceholder={`Filter ${title.toLowerCase()}…`}
          getRowId={(row, index) =>
            String(row.id ?? row.name ?? row.public_id ?? index)
          }
          emptyState={
            <div className="space-y-2 p-3">
              <p>No {title.toLowerCase()} yet.</p>
              <p className="text-sm">
                {urlKey === "services"
                  ? "Add a store to connect your database or an allowed file. Then discover and publish its layers."
                  : urlKey === "layer_groups"
                    ? "Publish layers or coverages first, then create a group to combine their portrayal."
                    : onCreate
                      ? `Choose ${createLabel} to get started.`
                      : "Records will appear here when there is activity in this scope."}
              </p>
            </div>
          }
        />
      )}
      {draftError && !open && !editing && (
        <p className="mt-2 text-sm text-destructive">{draftError}</p>
      )}
      {extraContent}

      <Dialog
        open={Boolean(editing)}
        onOpenChange={async (value) => {
          if (!value && (await canClose())) setEditing(null);
        }}
      >
        <DialogContent className="max-h-[90vh] overflow-y-auto sm:max-w-2xl">
          <DialogHeader>
            <DialogTitle>
              {readOnly ? "Inspect" : "Edit"} {title.toLowerCase()}
            </DialogTitle>
            <DialogDescription>
              {readOnly
                ? "This built-in resource is read-only. Create a custom resource to use different settings."
                : "Update the resource below. Untouched settings and saved secrets remain unchanged."}
            </DialogDescription>
          </DialogHeader>
          {readOnly ? (
            <ResourceDetails value={JSON.parse(draft || "{}")} />
          ) : schema ? (
            <ObjectEditor
              schema={schema}
              draft={draft}
              label={`Edit ${title.toLowerCase()}`}
              disabled={busy}
              choices={choices}
              onChange={(next) => {
                setDraft(next);
                setDraftError("");
              }}
            />
          ) : (
            <Textarea
              className="min-h-80 font-mono text-xs"
              aria-label={`Edit ${title.toLowerCase()} payload`}
              value={draft}
              onChange={(event) => {
                setDraft(event.target.value);
                setDraftError("");
              }}
            />
          )}
          <DialogFooter>
            <Button
              variant="outline"
              onClick={async () => {
                if (await canClose()) setEditing(null);
              }}
            >
              {readOnly ? "Close" : "Cancel"}
            </Button>
            {!readOnly && (
              <>
                <Button
                  disabled={busy}
                  onClick={() => {
                    if (!editing || !onUpdate) return;
                    let body: unknown;
                    try {
                      body = JSON.parse(draft);
                    } catch (reason) {
                      setDraftError(
                        reason instanceof Error
                          ? reason.message
                          : "Invalid JSON",
                      );
                      return;
                    }
                    if (!validateDraft(body as ResourceRow)) return;
                    void run(
                      () => onUpdate(editing, body),
                      () => setEditing(null),
                    );
                  }}
                >
                  Save changes
                </Button>
              </>
            )}
          </DialogFooter>
          {draftError && (
            <p className="text-sm text-destructive">{draftError}</p>
          )}
        </DialogContent>
      </Dialog>

      <Dialog
        open={Boolean(deleting)}
        onOpenChange={(value) => !value && setDeleting(null)}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Delete resource?</DialogTitle>
            <DialogDescription>{deleteDescription}</DialogDescription>
          </DialogHeader>
          <pre className="max-h-48 overflow-auto rounded-lg border bg-muted p-3 text-xs">
            {JSON.stringify(deleting, null, 2)}
          </pre>
          <DialogFooter>
            <Button variant="outline" onClick={() => setDeleting(null)}>
              Cancel
            </Button>
            <Button
              variant="destructive"
              disabled={busy}
              onClick={() => {
                if (!deleting || !onDelete) return;
                const label = String(
                  deleting.name ??
                    deleting.public_id ??
                    deleting.id ??
                    "resource",
                );
                void run(
                  () => onDelete(deleting),
                  () => {
                    setDeleting(null);
                    toast.success(`Deleted ${label}`);
                  },
                );
              }}
            >
              Delete
            </Button>
          </DialogFooter>
          {draftError && (
            <p className="text-sm text-destructive">{draftError}</p>
          )}
        </DialogContent>
      </Dialog>
    </Page>
  );
}
