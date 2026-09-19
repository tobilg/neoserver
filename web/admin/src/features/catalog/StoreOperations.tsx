import {
  useEffect,
  useImperativeHandle,
  useRef,
  useState,
  type Ref,
} from "react";
import { useNavigate } from "react-router";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Braces, ImagePlus, Trash2 } from "lucide-react";
import { toast } from "sonner";
import {
  createLayer,
  getListLayersQueryKey,
} from "@/api/generated/layers/layers";
import { getGetWorkspaceSummaryQueryKey } from "@/api/generated/workspaces/workspaces";
import { ConfirmDialog } from "@/components/ConfirmDialog";
import { ObjectEditor } from "@/components/SchemaFields";
import { QueryError } from "@/components/QueryError";
import { formSchemas } from "@/lib/resource-schemas";
import { validateHarvest } from "@/lib/operational-forms";
import {
  deleteService,
  validateSQLView,
} from "@/api/generated/services/services";
import { useGetServiceDeletionPlan } from "@/api/generated/catalog-lifecycle/catalog-lifecycle";
import {
  cancelMosaicHarvestJob,
  createMosaicHarvestJob,
  deleteMosaicGranule,
  getListMosaicGranulesQueryKey,
  getListMosaicHarvestJobsQueryKey,
  useGetMosaicGranule,
  useGetMosaicHarvestJob,
  useListMosaicGranules,
  useListMosaicHarvestJobs,
} from "@/api/generated/mosaic-catalog/mosaic-catalog";
import type {
  CreateLayerBody,
  MosaicHarvestRequest,
} from "@/api/generated/models";
import {
  isActiveStatus,
  statusSignature,
  useActivePolling,
} from "@/hooks/use-active-polling";
import { StatusChip } from "@/components/StatusChip";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/AppDialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { publishBlocker, suggestPublicID } from "./sql-view";
import { SQLEditor } from "./SQLEditor";
import { confirmDiscard } from "@/lib/confirm";

interface SQLDiscovery {
  valid: boolean;
  error?: string;
  discovered?: {
    columns: Array<{ name: string; type: string }>;
    geometry_column: string;
    geometry_type: string;
    srid: number;
    suggested_id_column: string;
  };
}

/** Lets a row menu open a dialog whose own trigger is hidden. */
export interface DialogHandle {
  open: () => void;
}

export function SQLViewDialog({
  workspace,
  service,
  onDirty,
  ref,
  hideTrigger = false,
}: {
  workspace: string;
  service: string;
  onDirty?: (dirty: boolean) => void;
  ref?: Ref<DialogHandle>;
  hideTrigger?: boolean;
}) {
  const client = useQueryClient();
  const [open, setOpen] = useState(false);
  useImperativeHandle(ref, () => ({ open: () => setOpen(true) }), []);
  const [sql, setSQL] = useState("SELECT id, geom FROM source_table");
  const [publicID, setPublicID] = useState("");
  const [idColumn, setIDColumn] = useState("");
  const validationController = useRef<AbortController | null>(null);
  const [validationCancelled, setValidationCancelled] = useState(false);
  useEffect(
    () => () => validationController.current?.abort(),
    [workspace, service],
  );
  const validate = useMutation({
    meta: { suppressErrorToast: true },
    mutationFn: async () => {
      const controller = new AbortController();
      validationController.current = controller;
      setValidationCancelled(false);
      try {
        return (await validateSQLView(
          workspace,
          service,
          { sql },
          { signal: controller.signal },
        )) as SQLDiscovery;
      } finally {
        if (validationController.current === controller)
          validationController.current = null;
      }
    },
    onSuccess: (result) => {
      setIDColumn(result.discovered?.suggested_id_column ?? "");
      if (result.valid)
        setPublicID((current) => current || suggestPublicID(sql));
    },
  });
  const publish = useMutation({
    meta: { suppressErrorToast: true },
    mutationFn: () => {
      const metadata = validate.data?.discovered;
      if (!validate.data?.valid || !metadata)
        throw new Error("Validate the SQL before publishing");
      if (
        !idColumn ||
        !metadata.columns.some((column) => column.name === idColumn)
      )
        throw new Error("Choose a unique, non-null feature ID column.");
      return createLayer(workspace, service, {
        public_id: publicID,
        title: publicID,
        enabled: true,
        public: false,
        crs_default: metadata.srid,
        sql_view: {
          sql,
          geometry_column: metadata.geometry_column,
          geometry_type: metadata.geometry_type,
          srid: metadata.srid,
          id_column: idColumn,
        },
      } as unknown as CreateLayerBody);
    },
    onSuccess: async () => {
      onDirty?.(false);
      setOpen(false);
      setSQL("SELECT id, geom FROM source_table");
      setPublicID("");
      setIDColumn("");
      validate.reset();
      toast.success("SQL view published. It is now available in Layers.");
      await Promise.all([
        client.invalidateQueries({
          queryKey: getListLayersQueryKey(workspace, service),
        }),
        client.invalidateQueries({
          queryKey: getGetWorkspaceSummaryQueryKey(workspace),
        }),
      ]);
    },
  });
  const busy = validate.isPending || publish.isPending;
  const blocker = publishBlocker({
    validated: Boolean(validate.data?.valid),
    idColumn,
    publicID,
  });
  const dirty =
    publicID !== "" ||
    idColumn !== "" ||
    sql !== "SELECT id, geom FROM source_table";
  async function changeOpen(value: boolean) {
    if (
      !value &&
      (busy ||
        (dirty && !(await confirmDiscard("Discard this SQL view draft?"))))
    )
      return;
    setOpen(value);
    if (!value) {
      setValidationCancelled(false);
      setSQL("SELECT id, geom FROM source_table");
      setPublicID("");
      setIDColumn("");
      validate.reset();
      publish.reset();
      onDirty?.(false);
    }
  }
  return (
    <>
      {!hideTrigger && (
        <Button size="sm" variant="ghost" onClick={() => setOpen(true)}>
          SQL view
        </Button>
      )}
      <Dialog open={open} onOpenChange={changeOpen}>
        <DialogContent className="max-h-[88vh] overflow-y-auto sm:max-w-3xl">
          <DialogHeader>
            <DialogTitle>Add SQL view layer</DialogTitle>
            <DialogDescription>
              Validate a read-only query, inspect its geometry metadata, then
              publish it as a virtual layer.
            </DialogDescription>
          </DialogHeader>
          <div>
            <Label id={`sql-label-${service}`}>SQL query</Label>
            <SQLEditor
              labelledBy={`sql-label-${service}`}
              value={sql}
              disabled={busy}
              onChange={(value) => {
                onDirty?.(true);
                setSQL(value);
                setIDColumn("");
                validate.reset();
              }}
            />
          </div>
          <div>
            <Label htmlFor={`sql-public-${service}`}>Public layer ID</Label>
            <Input
              id={`sql-public-${service}`}
              value={publicID}
              disabled={busy}
              onChange={(event) => {
                onDirty?.(true);
                setPublicID(event.target.value);
              }}
              placeholder="active_buildings"
            />
            <p className="mt-1 text-xs text-muted-foreground">
              Suggested from the query's table after validation. Start with a
              letter or underscore; use letters, digits, dots, underscores or
              hyphens.
            </p>
          </div>
          {validate.data?.discovered && (
            <div className="overflow-x-auto rounded-lg border">
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>Column</TableHead>
                    <TableHead>Type</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {validate.data.discovered.columns.map((column) => (
                    <TableRow key={column.name}>
                      <TableCell className="font-mono">{column.name}</TableCell>
                      <TableCell className="font-mono">{column.type}</TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
              <p className="border-t p-2 font-mono text-xs">
                {validate.data.discovered.geometry_column} ·{" "}
                {validate.data.discovered.geometry_type} · EPSG:
                {validate.data.discovered.srid}
              </p>
              <div className="space-y-2 border-t p-3">
                <Label htmlFor={`sql-id-${service}`}>Feature ID column</Label>
                <Select
                  value={idColumn}
                  disabled={busy}
                  onValueChange={(value) => {
                    onDirty?.(true);
                    setIDColumn(value);
                  }}
                >
                  <SelectTrigger id={`sql-id-${service}`} className="w-full">
                    <SelectValue placeholder="Choose a unique ID column" />
                  </SelectTrigger>
                  <SelectContent>
                    {validate.data.discovered.columns
                      .filter(
                        (column) =>
                          column.name !==
                          validate.data?.discovered?.geometry_column,
                      )
                      .map((column) => (
                        <SelectItem key={column.name} value={column.name}>
                          {column.name}
                        </SelectItem>
                      ))}
                  </SelectContent>
                </Select>
                <p className="text-xs text-muted-foreground">
                  Required for feature links and item lookup. Choose a stable,
                  unique, non-null value for every row. These constraints are
                  checked again when publishing.
                </p>
              </div>
            </div>
          )}
          {validate.data && !validate.data.valid && (
            <p role="alert" className="text-sm text-destructive">
              {validate.data.error}
            </p>
          )}
          {validationCancelled && (
            <p role="status" className="text-sm text-muted-foreground">
              Validation cancelled. Your draft is preserved; validate again
              before publishing.
            </p>
          )}
          {((validate.error && !validationCancelled) || publish.error) && (
            <p role="alert" className="text-sm text-destructive">
              {(validate.error || publish.error)?.message}
            </p>
          )}
          <DialogFooter>
            {validate.isPending && (
              <Button
                variant="outline"
                onClick={() => {
                  validationController.current?.abort();
                  setValidationCancelled(true);
                  validate.reset();
                }}
              >
                Cancel validation
              </Button>
            )}
            <Button
              variant="outline"
              disabled={!sql.trim() || busy}
              onClick={() => validate.mutate()}
            >
              <Braces /> Validate SQL
            </Button>
            <Button
              disabled={Boolean(blocker) || busy}
              aria-describedby={
                blocker ? `sql-publish-hint-${service}` : undefined
              }
              onClick={() => publish.mutate()}
            >
              Publish SQL view
            </Button>
          </DialogFooter>
          {blocker && (
            <p
              id={`sql-publish-hint-${service}`}
              className="text-right text-xs text-muted-foreground"
            >
              {blocker}
            </p>
          )}
        </DialogContent>
      </Dialog>
    </>
  );
}

// Raster publishing is shared by store actions and the Coverages page.

interface MosaicJob {
  id: string;
  status: string;
  processed_granules: number;
  total_granules: number;
  error_message?: string;
}

const initialHarvest = JSON.stringify(
  { mode: "append", directory: "", pattern: "*.tif", granules: [] },
  null,
  2,
);

export function MosaicManagerDialog({
  workspace,
  service,
  onDirty,
  ref,
  hideTrigger = false,
}: {
  workspace: string;
  service: string;
  onDirty?: (dirty: boolean) => void;
  ref?: Ref<DialogHandle>;
  hideTrigger?: boolean;
}) {
  const [open, setOpen] = useState(false);
  useImperativeHandle(ref, () => ({ open: () => setOpen(true) }), []);
  const client = useQueryClient();
  const [removeTarget, setRemoveTarget] = useState<{
    id: string;
    source_uri: string;
  }>();
  const [draft, setDraft] = useState(initialHarvest);
  const [harvestError, setHarvestError] = useState("");
  const [confirmHarvest, setConfirmHarvest] = useState(false);
  const [granulePage, setGranulePage] = useState(0);
  const granules = useListMosaicGranules(
    workspace,
    service,
    { limit: 50, offset: granulePage * 50 },
    { query: { enabled: open } },
  );
  const jobs = useListMosaicHarvestJobs(workspace, service, undefined, {
    query: {
      enabled: open,
      ...useActivePolling<{ jobs?: MosaicJob[] }>({
        isActive: (data) =>
          Boolean(data?.jobs?.some((job) => isActiveStatus(job.status))),
        signature: (data) => statusSignature(data?.jobs),
      }),
    },
  });
  const invalidateJobs = () =>
    client.invalidateQueries({
      queryKey: getListMosaicHarvestJobsQueryKey(workspace, service),
    });
  const create = useMutation({
    meta: { suppressErrorToast: true },
    mutationFn: () =>
      createMosaicHarvestJob(
        workspace,
        service,
        JSON.parse(draft) as MosaicHarvestRequest,
      ),
    onSuccess: async () => {
      setConfirmHarvest(false);
      setDraft(initialHarvest);
      onDirty?.(false);
      toast.success("Mosaic harvest queued");
      await invalidateJobs();
    },
  });
  const cancel = useMutation({
    meta: { suppressErrorToast: true },
    mutationFn: (job: string) =>
      cancelMosaicHarvestJob(workspace, service, job),
    onSuccess: invalidateJobs,
  });
  const [jobDetailID, setJobDetailID] = useState("");
  const jobDetail = useGetMosaicHarvestJob(workspace, service, jobDetailID, {
    query: { enabled: Boolean(jobDetailID) },
  });
  const remove = useMutation({
    meta: { suppressErrorToast: true },
    mutationFn: (granule: string) =>
      deleteMosaicGranule(workspace, service, granule),
    onSuccess: async () => {
      setRemoveTarget(undefined);
      await client.invalidateQueries({
        queryKey: getListMosaicGranulesQueryKey(workspace, service),
      });
    },
  });
  const [detailID, setDetailID] = useState("");
  const detail = useGetMosaicGranule(workspace, service, detailID, {
    query: { enabled: Boolean(detailID) },
  });
  const jobSignature = statusSignature(jobs.data?.jobs);
  useEffect(() => {
    if (open)
      void client.invalidateQueries({
        queryKey: getListMosaicGranulesQueryKey(workspace, service),
      });
  }, [client, workspace, service, open, jobSignature]);
  const busy = create.isPending || remove.isPending || cancel.isPending;
  async function changeOpen(value: boolean) {
    if (
      !value &&
      (busy ||
        (draft !== initialHarvest &&
          !(await confirmDiscard("Discard this harvest draft?"))))
    )
      return;
    setOpen(value);
    if (!value) {
      setDraft(initialHarvest);
      onDirty?.(false);
      setDetailID("");
      setJobDetailID("");
    }
  }
  function submitHarvest() {
    try {
      const value = JSON.parse(draft) as MosaicHarvestRequest;
      if (!value || typeof value !== "object" || Array.isArray(value))
        throw new Error("Enter a valid harvest request.");
      const error = validateHarvest(value);
      if (error) throw new Error(error);
      setHarvestError("");
      if (value.mode === "synchronize") setConfirmHarvest(true);
      else create.mutate();
    } catch (error) {
      setHarvestError((error as Error).message);
    }
  }
  return (
    <>
      {!hideTrigger && (
        <Button size="sm" variant="ghost" onClick={() => setOpen(true)}>
          Mosaic
        </Button>
      )}
      <Dialog open={open} onOpenChange={changeOpen}>
        <DialogContent className="max-h-[90vh] overflow-y-auto sm:max-w-5xl">
          <DialogHeader>
            <DialogTitle>Granules & harvests</DialogTitle>
            <DialogDescription>
              Append or synchronize a durable mosaic generation, inspect
              granules, and cancel active harvests.
            </DialogDescription>
          </DialogHeader>
          <div className="grid gap-4 lg:grid-cols-2">
            <div>
              <p className="mb-3 text-sm text-muted-foreground">
                Use a directory and pattern, or add individual granule paths. A
                blank directory uses the store’s configured source directory.
                Append keeps existing granules; synchronize replaces catalog
                membership with the discovered set.
              </p>
              <ObjectEditor
                schema={formSchemas.MosaicHarvestRequest}
                draft={draft}
                label="Harvest request"
                disabled={busy || confirmHarvest}
                onChange={(value) => {
                  setDraft(value);
                  onDirty?.(value !== initialHarvest);
                  setHarvestError("");
                }}
              />
              {harvestError && (
                <p role="alert" className="text-sm text-destructive">
                  {harvestError}
                </p>
              )}
              <Button className="mt-2" disabled={busy} onClick={submitHarvest}>
                <ImagePlus /> Start harvest
              </Button>
              <div className="mt-4 space-y-2">
                {jobs.data?.jobs?.map((job) => (
                  <div
                    className="flex items-center gap-2 border p-2"
                    key={job.id}
                  >
                    <StatusChip value={job.status} />
                    <button
                      className="min-w-0 flex-1 truncate text-left font-mono text-xs"
                      onClick={() => setJobDetailID(job.id)}
                    >
                      {job.id} · {job.processed_granules}/{job.total_granules}
                    </button>
                    {["queued", "running"].includes(job.status) && (
                      <Button
                        size="sm"
                        variant="outline"
                        disabled={cancel.isPending}
                        onClick={() => cancel.mutate(job.id)}
                      >
                        Cancel
                      </Button>
                    )}
                  </div>
                ))}
              </div>
            </div>
            <div className="max-h-[58vh] overflow-auto border">
              {granules.isLoading && (
                <p role="status" className="p-3">
                  Loading granules…
                </p>
              )}
              {!granules.error && granules.data?.granules?.length === 0 && (
                <p className="p-3">No granules on this page.</p>
              )}
              {!granules.error &&
                granules.data?.granules?.map((granule) => (
                  <div
                    className="flex items-center gap-2 border-b p-2"
                    key={granule.id}
                  >
                    <button
                      className="min-w-0 flex-1 text-left"
                      onClick={() => setDetailID(granule.id)}
                    >
                      <span className="block truncate font-mono text-xs">
                        {granule.source_uri}
                      </span>
                      <span className="text-xs text-muted-foreground">
                        generation {granule.generation}
                        {granule.time ? ` · ${granule.time}` : ""}
                      </span>
                    </button>
                    <Button
                      size="icon-sm"
                      variant="ghost"
                      aria-label={`Delete ${granule.source_uri}`}
                      disabled={remove.isPending}
                      onClick={() => {
                        remove.reset();
                        setRemoveTarget(granule);
                      }}
                    >
                      <Trash2 />
                    </Button>
                  </div>
                ))}
            </div>
          </div>
          <div className="flex items-center justify-end gap-2">
            <Button
              size="sm"
              variant="outline"
              disabled={granulePage === 0 || granules.isLoading}
              onClick={() => setGranulePage(granulePage - 1)}
            >
              Previous granules
            </Button>
            <span className="text-xs">Page {granulePage + 1}</span>
            <Button
              size="sm"
              variant="outline"
              disabled={
                granules.isLoading ||
                (granules.data?.granules?.length ?? 0) < 50
              }
              onClick={() => setGranulePage(granulePage + 1)}
            >
              Next granules
            </Button>
          </div>
          <ConfirmDialog
            open={Boolean(removeTarget)}
            onOpenChange={(value) => {
              if (!value) setRemoveTarget(undefined);
            }}
            title="Delete mosaic granule?"
            description={`Remove ${removeTarget?.source_uri ?? "this granule"} from the mosaic catalog. Coverage results may change. The source file is not deleted.`}
            confirmLabel="Delete granule"
            pending={remove.isPending}
            error={remove.error}
            onConfirm={() => {
              if (removeTarget) remove.mutate(removeTarget.id);
            }}
          />
          <ConfirmDialog
            open={confirmHarvest}
            onOpenChange={setConfirmHarvest}
            title="Synchronize mosaic catalog?"
            description="Granules missing from the discovered source set will be removed from the active mosaic catalog. Source files are retained. Use Append to keep existing catalog membership."
            confirmLabel="Synchronize mosaic"
            pending={create.isPending}
            error={create.error}
            onConfirm={() => create.mutate()}
          />
          {detailID && (
            <div>
              <div className="mb-1 flex items-center justify-between">
                <Label>Selected granule</Label>
                <Button
                  size="sm"
                  variant="ghost"
                  onClick={() => setDetailID("")}
                >
                  Close detail
                </Button>
              </div>
              <pre className="max-h-48 overflow-auto rounded-lg border bg-muted p-3 text-xs">
                {JSON.stringify(detail.data ?? {}, null, 2)}
              </pre>
            </div>
          )}
          {jobDetailID && (
            <div>
              <div className="mb-1 flex items-center justify-between">
                <Label>Selected harvest</Label>
                <Button
                  size="sm"
                  variant="ghost"
                  onClick={() => setJobDetailID("")}
                >
                  Close detail
                </Button>
              </div>
              <pre className="max-h-48 overflow-auto rounded-lg border bg-muted p-3 text-xs">
                {JSON.stringify(jobDetail.data ?? {}, null, 2)}
              </pre>
            </div>
          )}
          <QueryError
            error={
              create.error ||
              cancel.error ||
              granules.error ||
              jobs.error ||
              detail.error ||
              jobDetail.error
            }
            retry={() => {
              void granules.refetch();
              void jobs.refetch();
              if (detailID) void detail.refetch();
              if (jobDetailID) void jobDetail.refetch();
            }}
            context="Mosaic operation could not be completed"
          />
        </DialogContent>
      </Dialog>
    </>
  );
}

export function StoreDeletionDialog({
  workspace,
  row,
  refresh,
  ref,
  hideTrigger = false,
}: {
  workspace: string;
  row: Record<string, unknown>;
  refresh: () => void | Promise<unknown>;
  ref?: Ref<DialogHandle>;
  hideTrigger?: boolean;
}) {
  const navigate = useNavigate();
  const service = encodeURIComponent(String(row.id));
  const name = String(row.name ?? row.id);
  const [open, setOpen] = useState(false);
  useImperativeHandle(ref, () => ({ open: () => setOpen(true) }), []);
  const [confirmation, setConfirmation] = useState("");
  const plan = useGetServiceDeletionPlan(workspace, service, {
    query: { enabled: open },
  });
  const remove = useMutation({
    meta: { suppressErrorToast: true },
    mutationFn: () => deleteService(workspace, service, { recurse: true }),
    onSuccess: async (operation) => {
      setOpen(false);
      setConfirmation("");
      if (operation?.id) {
        navigate(
          `/workspaces/${encodeURIComponent(workspace)}/deletions?operation=${encodeURIComponent(operation.id)}`,
        );
      } else {
        toast.success(`Deleted store ${name}`);
      }
      await refresh();
    },
  });
  return (
    <>
      {!hideTrigger && (
        <Button
          size="icon-sm"
          variant="ghost"
          aria-label={`Delete ${name}`}
          onClick={() => setOpen(true)}
        >
          <Trash2 />
        </Button>
      )}
      <Dialog
        open={open}
        onOpenChange={(value) => !remove.isPending && setOpen(value)}
      >
        <DialogContent className="max-w-2xl">
          <DialogHeader>
            <DialogTitle>Delete {name}?</DialogTitle>
            <DialogDescription>
              Recursive deletion removes publications and dependent groups from
              neoserver. The source database or files remain untouched.
            </DialogDescription>
          </DialogHeader>
          <pre className="max-h-64 overflow-auto rounded-lg border bg-muted p-3 text-xs">
            {JSON.stringify(
              plan.data ?? { status: "Loading dependency plan…" },
              null,
              2,
            )}
          </pre>
          <div>
            <Label htmlFor={`delete-store-${service}`}>
              Type {name} to confirm
            </Label>
            <Input
              id={`delete-store-${service}`}
              disabled={remove.isPending}
              value={confirmation}
              onChange={(event) => setConfirmation(event.target.value)}
            />
          </div>
          <DialogFooter>
            <Button
              variant="outline"
              disabled={remove.isPending}
              onClick={() => setOpen(false)}
            >
              Cancel
            </Button>
            <Button
              variant="destructive"
              disabled={
                remove.isPending ||
                plan.isLoading ||
                Boolean(plan.error) ||
                confirmation !== name
              }
              onClick={() => remove.mutate()}
            >
              {remove.isPending ? "Accepting deletion…" : "Delete store"}
            </Button>
          </DialogFooter>
          {(plan.error || remove.error) && (
            <p className="text-sm text-destructive">
              {(plan.error || remove.error)?.message}
            </p>
          )}
        </DialogContent>
      </Dialog>
    </>
  );
}
