import { datasourceCapabilities } from "@/lib/datasource-capabilities";
import {
  createContext,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import { useMutation, useQueries, useQueryClient } from "@tanstack/react-query";
import type { UseQueryOptions } from "@tanstack/react-query";
import { Link, useParams, useSearchParams } from "react-router";
import { QueryError } from "@/components/QueryError";
import { ObjectEditor } from "@/components/SchemaFields";
import { validateFields, type FieldChoices } from "@/lib/schema-fields";
import { resourceSchema } from "@/lib/resource-schemas";
import { useUnsavedChangesGuard } from "@/lib/forms";
import { useCatalogChoices } from "./use-catalog-choices";
import { invalidatePreview } from "./invalidate-preview";
import { Eye, MoreHorizontal, Pencil, Plus } from "lucide-react";
import { isAccessError } from "@/lib/query-access";
import {
  getListServicesQueryKey,
  getService,
  useListServices,
  useTestExistingServiceConnection,
  useUpdateService,
} from "@/api/generated/services/services";
import {
  getLayerGroup,
  getListLayerGroupsQueryKey,
  useCreateLayerGroup,
  useDeleteLayerGroup,
  useListLayerGroups,
  useUpdateLayerGroup,
} from "@/api/generated/layer-groups/layer-groups";
import {
  getListStylesQueryKey,
  useCreateStyle,
  useListStyles,
} from "@/api/generated/styles/styles";
import {
  getListClaimMappingsQueryKey,
  useCreateClaimMapping,
  useDeleteClaimMapping,
  useGetClaimMapping,
  useListClaimMappings,
} from "@/api/generated/claim-mappings/claim-mappings";
import {
  getListLayersQueryKey,
  getLayer,
  getListLayersQueryOptions,
  deleteLayer,
  updateLayer,
} from "@/api/generated/layers/layers";
import {
  getListCoveragesQueryKey,
  getCoverage,
  getListCoveragesQueryOptions,
  deleteCoverage,
  updateCoverage,
} from "@/api/generated/coverages/coverages";
import type {
  ServiceConnectionInput,
  CreateClaimMappingBody,
  CreateStyleBody,
  Layer,
  Service,
  UpdateLayerBody,
  UpdateServiceBody,
} from "@/api/generated/models";
import type {
  CreateLayerGroupMutationBody,
  UpdateLayerGroupMutationBody,
} from "@/api/generated/layer-groups/layer-groups";
import type { UpdateCoverageMutationBody } from "@/api/generated/coverages/coverages";
import { ExtentStrip } from "@/components/ExtentStrip";
import { Page } from "@/components/Page";
import { StatusChip } from "@/components/StatusChip";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/AppDialog";
import { DataTable } from "@/components/DataTable";
import type { ColumnDef } from "@tanstack/react-table";
import { ResourcePage } from "@/features/shared/ResourcePage";
import { LayerDiscoveryDialog } from "./LayerDiscoveryDialog";

import { StoreCreateDialog } from "./StoreCreateDialog";
import {
  MosaicManagerDialog,
  SQLViewDialog,
  StoreDeletionDialog,
  type DialogHandle,
} from "./StoreOperations";
import { connectionMessage } from "@/lib/connection-errors";
import { toast } from "sonner";
import { CoverageDiscoveryDialog } from "./CoverageDiscoveryDialog";
import { confirmDiscard } from "@/lib/confirm";

// Keep table cell components stable when catalog choices refresh. Defining
// their renderers again would remount open dialogs and discard their drafts.
const PublicationChoices = createContext<FieldChoices>({});

/**
 * A published layer or coverage, joined with the name of its owning store. The
 * index signature is deliberate: coverage rows carry fields beyond the `Layer`
 * shape (range fields, resampling, WCS subtype) and the edit dialog reads them
 * generically.
 */
type PublicationRow = Layer & {
  store?: string;
  store_enabled?: boolean;
} & Record<string, unknown>;

/** Either publication list response, narrowed where the rows are read. */
type PublicationListResponse = { layers?: Layer[]; coverages?: Layer[] };

export function StoresPage() {
  const { ws = "" } = useParams();
  const [params, setParams] = useSearchParams();
  const storeFilter = params.get("store");
  const client = useQueryClient();
  const services = useListServices(ws);
  const update = useUpdateService();
  const invalidate = () =>
    Promise.all([
      client.invalidateQueries({ queryKey: getListServicesQueryKey(ws) }),
      invalidatePreview(client, ws),
    ]);

  return (
    <ResourcePage
      title="Stores"
      description="Connect PostGIS, DuckDB, GeoParquet, vector, raster, or mosaic data sources."
      rows={(services.data?.services ?? []).filter(
        (store) => !storeFilter || store.id === storeFilter,
      )}
      isLoading={services.isLoading}
      error={services.error}
      urlKey="services"
      columns={["name", "type", "enabled", "cache_settings", "updated_at"]}
      onRefresh={invalidate}
      extraAction={(onDirty) => (
        <StoreCreateDialog key={ws} workspace={ws} onDirty={onDirty} />
      )}
      tableToolbar={
        storeFilter && (
          <Button
            variant="outline"
            size="sm"
            onClick={() =>
              setParams((current) => {
                const next = new URLSearchParams(current);
                next.delete("store");
                return next;
              })
            }
          >
            Show all stores
          </Button>
        )
      }
      onLoadItem={(row) => getService(ws, String(row.id))}
      editValue={(row) => ({
        name: row.name,
        enabled: row.enabled,
        connection_info: row.connection_info ?? {},
        cache_settings: row.cache_settings,
      })}
      onUpdate={async (row, body) => {
        await update.mutateAsync({
          workspace: ws,
          service: String(row.id),
          data: body as unknown as UpdateServiceBody,
        });
        await invalidate();
      }}
      renderActions={(row, refresh, onDirty) => (
        <StoreActions
          workspace={ws}
          row={row}
          refresh={refresh}
          onDirty={onDirty}
        />
      )}
    />
  );
}

function StoreActions({
  workspace,
  row,
  refresh,
  onDirty,
}: {
  workspace: string;
  row: Record<string, unknown>;
  refresh: () => void | Promise<unknown>;
  onDirty: (dirty: boolean) => void;
}) {
  const id = String(row.id);
  const type = String(row.type);
  const [testResult, setTestResult] = useState<string>("");
  const service: Service = {
    id,
    workspace_id: String(row.workspace_id ?? ""),
    name: String(row.name),
    type: type as Service["type"],
    enabled: row.enabled === true,
    created_at: String(row.created_at ?? ""),
    updated_at: String(row.updated_at ?? ""),
  };
  const sqlView = useRef<DialogHandle>(null);
  const mosaic = useRef<DialogHandle>(null);
  const deletion = useRef<DialogHandle>(null);
  const capabilities = datasourceCapabilities(type);
  const test = useTestExistingServiceConnection({
    mutation: {
      meta: { suppressErrorToast: true },
      onSuccess: (result) => {
        const message = result.ok
          ? `Connected in ${(result.duration_ms ?? 0).toFixed(1)} ms`
          : connectionMessage(result.message || "Connection failed");
        setTestResult(message);
        if (result.ok) toast.success(`${service.name}: ${message}`);
        else
          toast.error(`${service.name}: ${message}`, {
            description: result.message,
          });
      },
      onError: (error) =>
        toast.error(`${service.name}: connection test failed`, {
          description: (error as unknown as Error).message,
        }),
    },
  });
  return (
    <div className="inline-flex items-center justify-end gap-1 whitespace-nowrap">
      {capabilities.features && (
        <LayerDiscoveryDialog
          key={`${workspace}:${id}`}
          workspace={workspace}
          services={[service]}
          initialServiceID={service.id}
          lockService
          onDirty={onDirty}
          onPublished={refresh}
          trigger={
            <Button
              size="sm"
              variant="outline"
              disabled={!service.enabled}
              title={
                service.enabled
                  ? "Discover feature layers"
                  : "Enable this store before discovering layers"
              }
            >
              Discover
            </Button>
          }
        />
      )}
      {capabilities.coverages && (
        <CoverageDiscoveryDialog
          workspace={workspace}
          services={[service]}
          initialServiceID={id}
          onDirty={onDirty}
          trigger={
            <Button size="sm" variant="outline" disabled={!service.enabled}>
              Discover coverages
            </Button>
          }
        />
      )}
      {/* In the narrow row menu the secondary actions are plain items. */}
      <Button
        size="sm"
        variant="ghost"
        className="@2xl/table:hidden"
        disabled={test.isPending}
        onClick={() =>
          test.mutate({
            workspace,
            service: id,
            data: {} as ServiceConnectionInput,
          })
        }
      >
        {test.isPending ? "Testing connection…" : "Test connection"}
      </Button>
      {capabilities.sql && (
        <Button
          size="sm"
          variant="ghost"
          className="@2xl/table:hidden"
          onClick={() => sqlView.current?.open()}
        >
          Add SQL view…
        </Button>
      )}
      {type === "raster_mosaic" && (
        <Button
          size="sm"
          variant="ghost"
          className="@2xl/table:hidden"
          onClick={() => mosaic.current?.open()}
        >
          Granules & harvests…
        </Button>
      )}
      <Button
        size="sm"
        variant="ghost"
        className="text-red-700! @2xl/table:hidden dark:text-destructive!"
        onClick={() => deletion.current?.open()}
      >
        Delete store…
      </Button>
      <DropdownMenu modal={false}>
        <DropdownMenuTrigger asChild>
          <Button
            size="icon-sm"
            variant="ghost"
            className="@max-2xl/table:hidden!"
            aria-label={`More actions for ${service.name}`}
            title={testResult || undefined}
          >
            <MoreHorizontal />
          </Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end">
          <DropdownMenuItem
            disabled={test.isPending}
            onSelect={() =>
              test.mutate({
                workspace,
                service: id,
                data: {} as ServiceConnectionInput,
              })
            }
          >
            {test.isPending ? "Testing connection…" : "Test connection"}
          </DropdownMenuItem>
          {capabilities.sql && (
            <DropdownMenuItem onSelect={() => sqlView.current?.open()}>
              Add SQL view…
            </DropdownMenuItem>
          )}
          {type === "raster_mosaic" && (
            <DropdownMenuItem onSelect={() => mosaic.current?.open()}>
              Granules & harvests…
            </DropdownMenuItem>
          )}
          <DropdownMenuSeparator />
          <DropdownMenuItem
            variant="destructive"
            onSelect={() => deletion.current?.open()}
          >
            Delete store…
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
      {capabilities.sql && (
        <SQLViewDialog
          ref={sqlView}
          hideTrigger
          workspace={workspace}
          service={id}
          onDirty={onDirty}
        />
      )}
      {type === "raster_mosaic" && (
        <MosaicManagerDialog
          ref={mosaic}
          hideTrigger
          workspace={workspace}
          service={id}
          onDirty={onDirty}
        />
      )}
      <StoreDeletionDialog
        ref={deletion}
        hideTrigger
        workspace={workspace}
        row={row}
        refresh={refresh}
      />
    </div>
  );
}
export function LayerGroupsPage() {
  const { ws = "" } = useParams();
  const client = useQueryClient();
  const groups = useListLayerGroups(ws);
  const create = useCreateLayerGroup();
  const catalog = useCatalogChoices(ws);

  const update = useUpdateLayerGroup();
  const remove = useDeleteLayerGroup();
  const invalidate = () =>
    client.invalidateQueries({ queryKey: getListLayerGroupsQueryKey(ws) });

  return (
    <ResourcePage
      createDescription="Stack published layers and groups into one map layer; members draw in list order."
      title="Layer groups"
      description="Compose ordered render stacks from published layers and nested groups."
      rows={groups.data?.layer_groups ?? []}
      isLoading={groups.isLoading}
      error={groups.error}
      urlKey="layer_groups"
      choices={catalog.choices}
      extraContent={
        <QueryError
          error={catalog.error}
          retry={catalog.retry}
          context="Some member, style, or role choices could not be loaded"
        />
      }
      validate={(value) => {
        const candidate = String(value.public_id);
        const graph = new Map(
          catalog.groups.map((group) => [group.public_id, group.members ?? []]),
        );
        graph.set(candidate, (value.members ?? []) as { resource: string }[]);
        function cycle(node: string, path = new Set<string>()): boolean {
          if (path.has(node)) return true;
          const next = new Set(path).add(node);
          return (graph.get(node) ?? []).some((member) =>
            cycle(member.resource, next),
          );
        }
        return cycle(candidate)
          ? "A group cannot contain itself, directly or through another group."
          : undefined;
      }}
      columns={[
        "public_id",
        "title",
        "enabled",
        "public",
        "members",
        "default_style",
      ]}
      onRefresh={invalidate}
      createLabel="Create group"
      createTemplate={{
        public_id: "",
        title: "",
        enabled: true,
        public: false,
        members: [],
      }}
      onCreate={async (body) => {
        await create.mutateAsync({
          workspace: ws,
          data: body as unknown as CreateLayerGroupMutationBody,
        });
        await invalidate();
      }}
      onLoadItem={(row) => getLayerGroup(ws, String(row.id))}
      onUpdate={async (row, body) => {
        await update.mutateAsync({
          workspace: ws,
          group: String(row.id),
          data: body as unknown as UpdateLayerGroupMutationBody,
        });
        await invalidate();
      }}
      onDelete={async (row) => {
        await remove.mutateAsync({ workspace: ws, group: String(row.id) });
        await invalidate();
      }}
    />
  );
}
export function StylesPage() {
  const { ws = "" } = useParams();
  const client = useQueryClient();
  const styles = useListStyles(ws);
  const create = useCreateStyle();

  const invalidate = () =>
    client.invalidateQueries({ queryKey: getListStylesQueryKey(ws) });
  return (
    <ResourcePage
      title="Styles"
      description="Author and bind SLD portrayals. Open a style to use the XML editor and live WMS preview."
      rows={styles.data?.styles ?? []}
      isLoading={styles.isLoading}
      error={styles.error}
      urlKey="styles"
      columns={["name", "title", "format", "created_at", "updated_at"]}
      onRefresh={invalidate}
      onCreate={async (body) => {
        await create.mutateAsync({
          workspace: ws,
          data: body as unknown as CreateStyleBody,
        });
        await invalidate();
      }}
      createLabel="Create style"
      createTemplate={{
        name: "",
        title: "",
        format: "sld_1.1.0",
        sld_body:
          '<?xml version="1.0" encoding="UTF-8"?>\n<StyledLayerDescriptor version="1.1.0"></StyledLayerDescriptor>',
      }}
    />
  );
}
export function ClaimMappingsPage() {
  const { ws = "" } = useParams();
  const client = useQueryClient();
  const mappings = useListClaimMappings(ws);
  const create = useCreateClaimMapping();
  const catalog = useCatalogChoices(ws);

  const remove = useDeleteClaimMapping();
  const invalidate = () =>
    client.invalidateQueries({ queryKey: getListClaimMappingsQueryKey(ws) });

  return (
    <ResourcePage
      title="Claim mappings"
      description="Map an exact identity-provider claim value to a workspace role."
      rows={mappings.data?.claim_mappings ?? []}
      isLoading={mappings.isLoading}
      error={mappings.error}
      urlKey="claim_mappings"
      choices={catalog.choices}
      columns={[
        "claim_name",
        "claim_value",
        "role_id",
        "priority",
        "created_at",
      ]}
      onRefresh={invalidate}
      createLabel="Create mapping"
      createTemplate={{
        claim_name: "groups",
        claim_value: "",
        role_id: "viewer",
        priority: 0,
      }}
      onCreate={async (body) => {
        await create.mutateAsync({
          workspace: ws,
          data: body as unknown as CreateClaimMappingBody,
        });
        await invalidate();
      }}
      onDelete={async (row) => {
        await remove.mutateAsync({ workspace: ws, mappingId: String(row.id) });
        await invalidate();
      }}
      renderActions={(row) => <ClaimMappingDetails workspace={ws} row={row} />}
    />
  );
}

function ClaimMappingDetails({
  workspace,
  row,
}: {
  workspace: string;
  row: Record<string, unknown>;
}) {
  const [open, setOpen] = useState(false);
  const query = useGetClaimMapping(workspace, String(row.id), {
    query: { enabled: open },
  });
  return (
    <>
      <Button
        variant="ghost"
        size="icon-sm"
        aria-label="Inspect claim mapping"
        onClick={() => setOpen(true)}
      >
        <Eye />
      </Button>
      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Claim mapping</DialogTitle>
            <DialogDescription>
              Exact claim value, scope, role, and precedence.
            </DialogDescription>
          </DialogHeader>
          <pre className="max-h-72 overflow-auto rounded-lg border bg-muted p-3 text-xs">
            {JSON.stringify(query.data ?? row, null, 2)}
          </pre>
        </DialogContent>
      </Dialog>
    </>
  );
}

export function LayersPage() {
  return <ServiceChildrenPage kind="layers" title="Layers" />;
}
export function CoveragesPage() {
  return <ServiceChildrenPage kind="coverages" title="Coverages" />;
}

function ServiceChildrenPage({
  kind,
  title,
}: {
  kind: "layers" | "coverages";
  title: string;
}) {
  const { ws = "" } = useParams();
  const [params, setParams] = useSearchParams();
  const storeFilter = params.get("store");
  const services = useListServices(ws);
  const catalog = useCatalogChoices(ws);
  const [dirty, setDirty] = useState(false);
  useUnsavedChangesGuard(dirty);
  // One list request per store, fanned out with the generated query options so
  // each still carries its own typed key and cache entry.
  const queries = useQueries({
    queries: (services.data?.services ?? []).map(
      (service) =>
        (kind === "layers"
          ? getListLayersQueryOptions(ws, service.id)
          : getListCoveragesQueryOptions(ws, service.id)) as UseQueryOptions<
          PublicationListResponse,
          Error
        >,
    ),
  });
  const rows: PublicationRow[] = queries.flatMap((query, index) => {
    const payload = query.data as PublicationListResponse | undefined;
    const items =
      (kind === "layers" ? payload?.layers : payload?.coverages) ?? [];
    return items.map((item) => ({
      ...item,
      store: services.data?.services?.[index]?.name,
      store_enabled: services.data?.services?.[index]?.enabled,
    })) as PublicationRow[];
  });
  const columns = useMemo<ColumnDef<PublicationRow, unknown>[]>(
    () => [
      {
        id: "extent",
        header: () => "Extent",
        enableSorting: false,
        cell: ({ row }) => <ExtentStrip extent={row.original.native_extent} />,
      },
      {
        id: "public_id",
        accessorFn: (row) => row.public_id,
        header: () => "Public ID",
        enableHiding: false,
        cell: ({ row }) => (
          <div className="min-w-0 space-y-1">
            <span className="font-mono">{row.original.public_id}</span>
            {row.original.title &&
              row.original.title !==
                row.original.public_id.split(".").at(-1) && (
                <span className="hidden text-xs text-muted-foreground @2xl/table:block">
                  {row.original.title}
                </span>
              )}
            <div className="space-y-2 text-sm @2xl/table:hidden">
              {row.original.title && <p>{row.original.title}</p>}
              <p className="text-muted-foreground">
                Store: {row.original.store || "—"}
              </p>
              <div className="flex flex-wrap gap-1">
                <StatusChip
                  value={
                    row.original.enabled === false ? "disabled" : "enabled"
                  }
                />
                <StatusChip
                  value={row.original.public ? "public" : "restricted"}
                />
                {row.original.store_enabled === false && (
                  <StatusChip value="store disabled" />
                )}
              </div>
            </div>
          </div>
        ),
      },
      {
        id: "store",
        accessorFn: (row) => row.store ?? "",
        header: () => "Store",
        cell: ({ row }) => (
          <div className="space-y-1">
            <div>{row.original.store || "—"}</div>
            {row.original.store_enabled === false && (
              <StatusChip value="store disabled" />
            )}
          </div>
        ),
      },
      {
        id: "title",
        accessorFn: (row) => row.title ?? "",
        header: () => "Title",
        cell: ({ row }) => row.original.title || "—",
      },
      {
        id: "crs",
        // Coverages report their CRS through the native extent.
        accessorFn: (row) => row.crs_default ?? row.native_extent?.srid,
        header: () => "CRS",
        cell: ({ getValue }) => {
          const srid = getValue() as number | undefined;
          return srid ? (
            <span className="font-mono">EPSG:{srid}</span>
          ) : (
            <span className="text-muted-foreground">—</span>
          );
        },
      },
      {
        id: "status",
        accessorFn: (row) => (row.enabled === false ? "disabled" : "enabled"),
        header: () => "Status",
        cell: ({ getValue }) => <StatusChip value={String(getValue())} />,
      },
      {
        id: "visibility",
        accessorFn: (row) => (row.public ? "public" : "restricted"),
        header: () => "Access",
        cell: ({ row }) => (
          <StatusChip value={row.original.public ? "public" : "restricted"} />
        ),
      },
      {
        id: "style",
        accessorFn: (row) => row.default_style ?? "",
        header: () => "Style",
        cell: ({ row }) => row.original.default_style || "default",
      },
      {
        id: "generation",
        accessorFn: (row) => row.tile_cache_generation ?? 1,
        header: () => "Generation",
        cell: ({ row }) => (
          <span className="block text-right font-mono">
            {row.original.tile_cache_generation ?? 1}
          </span>
        ),
      },
      {
        id: "actions",
        header: () => "Actions",
        enableSorting: false,
        enableHiding: false,
        cell: ({ row }) => (
          <div className="flex items-center justify-end gap-1">
            <PublicationActions
              workspace={ws}
              kind={kind}
              row={row.original}
              onDirty={setDirty}
            />
          </div>
        ),
      },
    ],
    [kind, ws],
  );
  return (
    <Page
      title={title}
      description={
        kind === "layers"
          ? "Cross-store publication catalog with access, CRS, style, extent, and cache identity."
          : "Raster publications with range fields, resampling, and WCS subtype."
      }
      action={
        kind === "layers" ? (
          <LayerDiscoveryDialog
            key={ws}
            workspace={ws}
            services={services.data?.services ?? []}
            servicesLoading={services.isLoading}
            servicesError={services.error as globalThis.Error | null}
            retryServices={() => void services.refetch()}
            manageStoresPath={`/workspaces/${encodeURIComponent(ws)}/stores`}
            onDirty={setDirty}
            trigger={
              <Button size="sm">
                <Plus /> Add layer
              </Button>
            }
          />
        ) : (
          <CoverageDiscoveryDialog
            workspace={ws}
            services={services.data?.services ?? []}
            servicesLoading={services.isLoading}
            servicesError={services.error}
            retryServices={() => services.refetch()}
            onDirty={setDirty}
            trigger={
              <Button size="sm">
                <Plus /> Add coverage
              </Button>
            }
          />
        )
      }
    >
      <QueryError
        error={services.error || queries.find((query) => query.error)?.error}
        retry={() => {
          void services.refetch();
          queries.forEach((query) => void query.refetch());
        }}
        context="The publication catalog could not be fully loaded. Any rows below are partial results."
      />
      <QueryError
        error={catalog.error}
        retry={catalog.retry}
        context="Some style or role choices could not be loaded"
      />
      {!isAccessError(services.error) &&
        !queries.some((query) => isAccessError(query.error)) &&
        ((!services.error && !queries.some((query) => query.error)) ||
          rows.length > 0) && (
          <PublicationChoices.Provider value={catalog.choices}>
            <DataTable
              data={rows.filter(
                (row) => !storeFilter || row.service_id === storeFilter,
              )}
              toolbar={
                storeFilter && (
                  <Button
                    variant="outline"
                    size="sm"
                    onClick={() =>
                      setParams((current) => {
                        const next = new URLSearchParams(current);
                        next.delete("store");
                        return next;
                      })
                    }
                  >
                    Show all publications
                  </Button>
                )
              }
              columns={columns}
              tableClassName="table-fixed @2xl/table:table-auto"
              columnPickerClassName="hidden @2xl/table:flex"
              columnClassNames={{
                // Identity, store and state first; detail columns need room.
                ...Object.fromEntries(
                  ["store", "status", "visibility"].map((key) => [
                    key,
                    "hidden @2xl/table:table-cell",
                  ]),
                ),
                ...Object.fromEntries(
                  ["extent", "title", "crs", "style", "generation"].map(
                    (key) => [key, "hidden @4xl/table:table-cell"],
                  ),
                ),
                public_id:
                  "whitespace-normal break-words [overflow-wrap:anywhere] @2xl/table:whitespace-nowrap",
                actions:
                  "w-20 align-top @2xl/table:w-auto @2xl/table:align-middle",
              }}
              isLoading={
                services.isLoading || queries.some((query) => query.isLoading)
              }
              urlKey={kind}
              defaultHiddenColumns={["title", "generation"]}
              filterPlaceholder={`Filter ${kind}…`}
              getRowId={(row, index) => String(row.id ?? index)}
              emptyState={`No published ${kind}.`}
            />
          </PublicationChoices.Provider>
        )}
    </Page>
  );
}

function PublicationActions({
  workspace,
  kind,
  row,
  onDirty,
}: {
  workspace: string;
  kind: "layers" | "coverages";
  row: Record<string, unknown>;
  onDirty: (dirty: boolean) => void;
}) {
  const client = useQueryClient();
  const choices = useContext(PublicationChoices);
  const mobileTrigger = useRef<HTMLButtonElement>(null);
  const editTrigger = useRef<HTMLButtonElement>(null);
  const [editing, setEditing] = useState(false);
  const [confirmDelete, setConfirmDelete] = useState(false);
  const service = String(row.service_id);
  const id = String(row.id);
  const mutable = { ...row };
  for (const key of [
    "id",
    "service_id",
    "source_layer",
    "source_coverage",
    "created_at",
    "updated_at",
    "tile_cache_generation",
    "store",
    "store_enabled",
  ])
    delete mutable[key];
  const [draft, setDraft] = useState(JSON.stringify(mutable, null, 2));
  const [pristine, setPristine] = useState(draft);
  const [loadError, setLoadError] = useState<unknown>();
  const [loading, setLoading] = useState(false);
  const schema = resourceSchema(kind)!;
  const pendingRead = useRef<AbortController | null>(null);
  useEffect(() => () => pendingRead.current?.abort(), []);
  async function beginEdit() {
    pendingRead.current?.abort();
    const request = new AbortController();
    pendingRead.current = request;
    update.reset();
    setLoading(true);
    setEditing(true);
    setLoadError(undefined);
    try {
      const full =
        kind === "layers"
          ? await getLayer(workspace, service, id, { signal: request.signal })
          : await getCoverage(workspace, service, id, {
              signal: request.signal,
            });
      if (request.signal.aborted) return;
      const next = JSON.stringify(full, null, 2);
      setDraft(next);
      setPristine(next);
    } catch (error) {
      if (!request.signal.aborted) setLoadError(error);
    } finally {
      if (!request.signal.aborted) setLoading(false);
    }
  }
  async function close() {
    if (
      !update.isPending &&
      (loading ||
        loadError ||
        draft === pristine ||
        (await confirmDiscard("Discard unsaved publication changes?")))
    ) {
      pendingRead.current?.abort();
      setLoading(false);
      setLoadError(undefined);
      setEditing(false);
      onDirty(false);
    }
  }
  // Both publication kinds are listed per store, so invalidate this store's
  // list for the kind being edited.
  const refresh = () =>
    Promise.all([
      invalidatePreview(client, workspace),
      client.invalidateQueries({
        queryKey:
          kind === "layers"
            ? getListLayersQueryKey(workspace, service)
            : getListCoveragesQueryKey(workspace, service),
      }),
    ]);

  const update = useMutation({
    meta: { suppressErrorToast: true },
    mutationFn: async (): Promise<unknown> => {
      const data = JSON.parse(draft) as UpdateLayerBody &
        UpdateCoverageMutationBody;
      const errors = validateFields(schema, data);
      if (errors.length) throw new Error(errors.join(" "));
      return kind === "layers"
        ? updateLayer(workspace, service, id, data)
        : updateCoverage(workspace, service, id, data);
    },
    onSuccess: async () => {
      setEditing(false);
      onDirty(false);
      await refresh();
    },
  });
  const remove = useMutation({
    meta: { suppressErrorToast: true },
    mutationFn: async (): Promise<unknown> =>
      kind === "layers"
        ? deleteLayer(workspace, service, id)
        : deleteCoverage(workspace, service, id),
    onSuccess: async () => {
      setConfirmDelete(false);
      await refresh();
    },
  });
  const previewPath = `/workspaces/${encodeURIComponent(workspace)}/preview?${new URLSearchParams({ layers: String(row.public_id), ...(kind === "coverages" ? { sources: "wms" } : {}) })}`;
  const connectPath = `/workspaces/${encodeURIComponent(workspace)}/endpoints?${new URLSearchParams({ layer: String(row.public_id) })}`;
  function restoreFocus(event: Event, desktop: HTMLButtonElement | null) {
    event.preventDefault();
    // Return focus to the control that opened the dialog: the visible inline
    // button on wide tables, otherwise the actions menu.
    const target = desktop?.getClientRects().length
      ? desktop
      : mobileTrigger.current;
    if (target?.isConnected) target.focus();
    else document.querySelector<HTMLElement>("main h1")?.focus();
  }
  return (
    <>
      <span className="hidden items-center gap-1 @2xl/table:inline-flex">
        <Button variant="outline" size="sm" asChild>
          <Link to={previewPath}>Preview</Link>
        </Button>
      </span>
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <Button
            ref={mobileTrigger}
            className="size-11 @2xl/table:size-8 @2xl/table:border-transparent @2xl/table:bg-transparent @2xl/table:shadow-none @2xl/table:hover:bg-accent"
            variant="outline"
            size="icon"
            aria-label={`Actions for ${String(row.public_id)}`}
            disabled={loading}
          >
            <MoreHorizontal />
          </Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end">
          <DropdownMenuItem onSelect={() => void beginEdit()}>
            Edit publication
          </DropdownMenuItem>
          <DropdownMenuItem asChild>
            <Link to={previewPath}>Preview</Link>
          </DropdownMenuItem>
          {kind === "layers" && (
            <DropdownMenuItem asChild>
              <Link to={connectPath}>Connect a client</Link>
            </DropdownMenuItem>
          )}
          <DropdownMenuSeparator />
          <DropdownMenuItem onSelect={() => setConfirmDelete(true)}>
            Unpublish…
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
      <span className="hidden @2xl/table:inline-flex">
        <Button
          ref={editTrigger}
          variant="ghost"
          size="icon-sm"
          aria-label={`Edit ${String(row.public_id)}`}
          disabled={loading}
          onClick={() => void beginEdit()}
        >
          <Pencil />
        </Button>
      </span>
      <Dialog
        open={editing}
        onOpenChange={(value) => {
          if (!value) close();
        }}
      >
        <DialogContent
          className="max-h-[90vh] overflow-y-auto sm:max-w-3xl"
          onCloseAutoFocus={(event) => restoreFocus(event, editTrigger.current)}
        >
          <DialogHeader>
            <DialogTitle>Edit {String(row.public_id)}</DialogTitle>
            <DialogDescription>
              Configure access, CRS, style, dimensions, and cache policy.
              Untouched fields are preserved.
            </DialogDescription>
          </DialogHeader>
          {loading && <p role="status">Loading publication editor…</p>}
          <QueryError
            error={loadError}
            retry={beginEdit}
            context="Publication could not be loaded"
          />
          {!loading && !loadError && (
            <ObjectEditor
              schema={schema}
              draft={draft}
              label="Publication"
              disabled={update.isPending}
              choices={choices}
              primaryFields={[
                "public_id",
                "title",
                "description",
                "enabled",
                "public",
                "allowed_roles",
                "default_style",
              ]}
              revealAdvanced={Boolean(update.error)}
              onChange={(next) => {
                setDraft(next);
                onDirty(next !== pristine);
              }}
            />
          )}
          <DialogFooter>
            <Button
              variant="outline"
              disabled={update.isPending}
              onClick={() => void close()}
            >
              Cancel
            </Button>
            <Button
              disabled={loading || Boolean(loadError) || update.isPending}
              onClick={() => update.mutate()}
            >
              Save publication
            </Button>
          </DialogFooter>
          <QueryError
            error={update.error}
            context="Publication could not be saved"
          />
        </DialogContent>
      </Dialog>
      <Dialog open={confirmDelete} onOpenChange={setConfirmDelete}>
        <DialogContent
          onCloseAutoFocus={(event) =>
            restoreFocus(event, mobileTrigger.current)
          }
        >
          <DialogHeader>
            <DialogTitle>Unpublish {String(row.public_id)}?</DialogTitle>
            <DialogDescription>
              The source data is not deleted. References such as layer groups
              must be removed first.
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setConfirmDelete(false)}>
              Cancel
            </Button>
            <Button
              variant="destructive"
              disabled={remove.isPending}
              onClick={() => remove.mutate()}
            >
              Unpublish
            </Button>
          </DialogFooter>
          {remove.error && (
            <p role="alert" className="text-sm text-destructive">
              {remove.error.message}
            </p>
          )}
        </DialogContent>
      </Dialog>
    </>
  );
}
