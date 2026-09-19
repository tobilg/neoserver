import { useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import {
  createTileCacheJob,
  getListTileCacheJobsQueryKey,
} from "@/api/generated/tile-cache/tile-cache";
import { useListWorkspaceTileMatrixSets } from "@/api/generated/tile-matrix-sets/tile-matrix-sets";
import type { TileCacheJobRequest } from "@/api/generated/models";
import { useCatalogChoices } from "@/features/catalog/use-catalog-choices";
import { ObjectEditor } from "@/components/SchemaFields";
import { QueryError } from "@/components/QueryError";
import { ConfirmDialog } from "@/components/ConfirmDialog";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { SwitchRow } from "@/components/SwitchRow";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/AppDialog";
import { formSchemas } from "@/lib/resource-schemas";
import { validateTileJob } from "@/lib/operational-forms";
import { useUnsavedChangesGuard } from "@/lib/forms";
import { confirmDiscard } from "@/lib/confirm";
import { NativeSelect } from "@/components/NativeSelect";

const initial = JSON.stringify(
  {
    operation: "seed",
    resource: "",
    tile_type: "vector",
    tile_matrix_set: "WebMercatorQuad",
    format: "application/vnd.mapbox-vector-tile",
    min_zoom: 0,
    max_zoom: 8,
  },
  null,
  2,
);
const jobSchema = {
  ...formSchemas.TileCacheJobRequest,
  properties: Object.fromEntries(
    [
      "operation",
      "tile_type",
      "resource",
      "all_resources",
      "tile_matrix_set",
      "format",
      "style",
      "min_zoom",
      "max_zoom",
    ].map((name) => [name, formSchemas.TileCacheJobRequest.properties![name]]),
  ),
};
export function TileJobDialog({
  workspace,
  onClose,
}: {
  workspace: string;
  onClose: () => void;
}) {
  const client = useQueryClient();
  const [draft, setDraft] = useState(initial);
  const [confirm, setConfirm] = useState(false);
  const [error, setError] = useState("");
  const [note, setNote] = useState("");
  const catalog = useCatalogChoices(workspace, true);
  const sets = useListWorkspaceTileMatrixSets(workspace);
  let value: TileCacheJobRequest | undefined;
  try {
    value = JSON.parse(draft) as TileCacheJobRequest;
  } catch {
    /* ObjectEditor reports JSON syntax errors. */
  }
  const grid = sets.data?.tile_matrix_sets?.find(
    (set) => set.id === value?.tile_matrix_set,
  );
  const resources =
    value?.tile_type === "vector"
      ? catalog.resources.filter((resource) => resource.kind === "feature")
      : catalog.resources;
  const create = useMutation({
    meta: { suppressErrorToast: true },
    mutationFn: (request: TileCacheJobRequest) =>
      createTileCacheJob(workspace, request),
    onSuccess: async () => {
      setDraft(initial);
      await client.invalidateQueries({
        queryKey: getListTileCacheJobsQueryKey(workspace),
      });
      onClose();
    },
  });
  useUnsavedChangesGuard(draft !== initial || create.isPending);
  async function close() {
    if (
      !create.isPending &&
      (draft === initial ||
        (await confirmDiscard("Discard this tile-cache job draft?")))
    )
      onClose();
  }
  function submit() {
    if (!value || typeof value !== "object" || Array.isArray(value)) {
      setError("Enter a valid job request.");
      return;
    }
    const message =
      validateTileJob(value, grid) ||
      ((value.operation !== "truncate" || value.bounds) && !grid
        ? "Choose a tile matrix set enabled in this workspace’s service settings."
        : "") ||
      (!value.all_resources &&
      !resources.some((resource) => resource.public_id === value.resource)
        ? "Choose an available resource compatible with the tile type."
        : "");
    setError(message);
    if (message) return;
    if (value.operation === "truncate") setConfirm(true);
    else create.mutate(value);
  }
  const readError = catalog.error || sets.error;
  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open) close();
      }}
    >
      <DialogContent className="max-h-[88vh] overflow-y-auto sm:max-w-3xl">
        <DialogHeader>
          <DialogTitle>New tile-cache job</DialogTitle>
          <DialogDescription>
            Seed or reseed one published resource, or truncate cached tiles.
            Optional bounds are min X, min Y, max X, max Y in the stated CRS.
            Large jobs remain subject to server limits.
          </DialogDescription>
        </DialogHeader>
        <QueryError
          error={readError}
          retry={() => {
            void catalog.retry();
            void sets.refetch();
          }}
        />
        <ObjectEditor
          schema={jobSchema}
          draft={draft}
          label="Tile-cache job"
          disabled={create.isPending || confirm}
          onChange={(next) => {
            try {
              const parsed = JSON.parse(next) as TileCacheJobRequest;
              // "All resources" only applies to truncation.
              if (parsed.all_resources && parsed.operation !== "truncate") {
                parsed.all_resources = false;
                setNote(
                  "All resources is only available when truncating. Choose one resource to seed or reseed.",
                );
              } else setNote("");
              if (parsed.tile_type !== value?.tile_type)
                parsed.format =
                  parsed.tile_type === "map"
                    ? "image/png"
                    : "application/vnd.mapbox-vector-tile";
              setDraft(JSON.stringify(parsed, null, 2));
            } catch {
              setDraft(next);
            }
            setError("");
          }}
          choices={{
            ...catalog.choices,
            resource: resources.map((resource) => ({
              value: resource.public_id,
              label: `${resource.title || resource.public_id} (${resource.public_id}, ${resource.kind})`,
            })),
            tile_matrix_set: (sets.data?.tile_matrix_sets ?? []).map((set) => ({
              value: set.id,
              label: set.title || set.id,
            })),
            format: (value?.tile_type === "map"
              ? ["image/png", "image/jpeg", "image/webp"]
              : ["application/vnd.mapbox-vector-tile"]
            ).map((format) => ({
              value: format,
              label:
                format === "application/vnd.mapbox-vector-tile"
                  ? "Mapbox vector tile (MVT)"
                  : format.replace("image/", "").toUpperCase(),
            })),
          }}
        />
        {note && (
          <p role="status" className="text-sm text-muted-foreground">
            {note}
          </p>
        )}
        {!catalog.isLoading && resources.length === 0 && (
          <p
            role="status"
            className="rounded-md border-l-2 border-warning bg-warning/10 p-2 text-sm"
          >
            {value?.tile_type === "vector"
              ? "No feature layers to cache as vector tiles. Publish a layer, or choose the map tile type for coverages and layer groups."
              : "No enabled publications to cache yet. Publish a layer or coverage first."}
          </p>
        )}
        <div className="space-y-3 rounded-lg border p-3">
          <SwitchRow
            id="tile-job-bounds"
            label="Limit to an extent (optional)"
            checked={Boolean(value?.bounds)}
            disabled={
              !value || create.isPending || confirm || value.all_resources
            }
            onCheckedChange={(checked) => {
              if (value)
                setDraft(
                  JSON.stringify(
                    {
                      ...value,
                      bounds: checked
                        ? { bbox: [-180, -85, 180, 85], crs: "CRS84" }
                        : undefined,
                    },
                    null,
                    2,
                  ),
                );
            }}
          />
          {value?.bounds && (
            <fieldset
              disabled={create.isPending || confirm}
              className="grid grid-cols-2 gap-3"
            >
              <legend className="sr-only">Extent coordinates</legend>
              {["Min X", "Min Y", "Max X", "Max Y"].map((label, index) => (
                <div key={label}>
                  <Label htmlFor={`tile-bound-${index}`}>{label}</Label>
                  <Input
                    id={`tile-bound-${index}`}
                    type="number"
                    step="any"
                    value={value.bounds?.bbox?.[index] ?? ""}
                    onChange={(event) => {
                      const bbox = [...(value.bounds?.bbox ?? [])];
                      bbox[index] =
                        event.target.value === ""
                          ? Number.NaN
                          : Number(event.target.value);
                      setDraft(
                        JSON.stringify(
                          { ...value, bounds: { ...value.bounds, bbox } },
                          null,
                          2,
                        ),
                      );
                    }}
                  />
                </div>
              ))}
              <div className="col-span-2">
                <Label htmlFor="tile-bounds-crs">Bounds CRS</Label>
                <NativeSelect
                  id="tile-bounds-crs"
                  className="w-full"
                  value={value.bounds.crs}
                  onChange={(event) =>
                    setDraft(
                      JSON.stringify(
                        {
                          ...value,
                          bounds: { ...value.bounds, crs: event.target.value },
                        },
                        null,
                        2,
                      ),
                    )
                  }
                >
                  <option value="CRS84">CRS84 (longitude / latitude)</option>
                  <option value="EPSG:3857">
                    EPSG:3857 (Web Mercator metres)
                  </option>
                  {!["CRS84", "EPSG:3857"].includes(value.bounds.crs) && (
                    <option value={value.bounds.crs}>{value.bounds.crs}</option>
                  )}
                </NativeSelect>
              </div>
            </fieldset>
          )}
        </div>
        {error && (
          <p role="alert" className="text-sm text-destructive">
            {error}
          </p>
        )}
        <QueryError error={create.error} context="Job creation failed" />
        <DialogFooter>
          <Button variant="outline" disabled={create.isPending} onClick={close}>
            Cancel
          </Button>
          <Button
            disabled={
              create.isPending ||
              Boolean(readError) ||
              catalog.isLoading ||
              sets.isLoading
            }
            onClick={submit}
          >
            Start job
          </Button>
        </DialogFooter>
        <ConfirmDialog
          open={confirm}
          onOpenChange={setConfirm}
          title="Truncate cached tiles?"
          description={`Delete cached tiles for ${value?.all_resources ? "all resources in this workspace" : value?.resource}. Source data is retained, but clients may experience slower requests while tiles are rebuilt.`}
          confirmLabel="Truncate tiles"
          pending={create.isPending}
          error={create.error}
          onConfirm={() => {
            if (value) create.mutate(value);
          }}
        />
      </DialogContent>
    </Dialog>
  );
}
