import { useState } from "react";
import { Link } from "react-router";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Map as MapIcon } from "lucide-react";
import { toast } from "sonner";
import {
  getListLayersQueryKey,
  updateLayer,
} from "@/api/generated/layers/layers";
import type { UpdateLayerBody } from "@/api/generated/models";
import { useListStyles } from "@/api/generated/styles/styles";
import { useListWorkspaceRoles } from "@/api/generated/roles/roles";
import { NativeSelect } from "@/components/NativeSelect";
import { ToggleChips } from "@/components/ToggleChips";
import { Button } from "@/components/ui/button";
import { Label } from "@/components/ui/label";
import { confirmAction } from "@/lib/confirm";

export interface PublishedLayer {
  id: string;
  public_id: string;
}

type BulkChange =
  | { kind: "style"; style: string }
  | { kind: "roles"; roles: string[] }
  | { kind: "public" };

const describe = (change: BulkChange) =>
  change.kind === "style"
    ? `Default style set to ${change.style}`
    : change.kind === "roles"
      ? change.roles.length
        ? `Access limited to ${change.roles.join(", ")}`
        : "Role restriction removed"
      : "Made public";

/**
 * The "Layers published" step: per-layer next steps plus bulk changes for
 * everything that was just published.
 */
export function PublishedLayersPanel({
  workspace,
  serviceID,
  layers,
}: {
  workspace: string;
  serviceID: string;
  layers: PublishedLayer[];
}) {
  const client = useQueryClient();
  const root = `/workspaces/${encodeURIComponent(workspace)}`;
  const styles = useListStyles(workspace);
  const roles = useListWorkspaceRoles(workspace);
  const [style, setStyle] = useState("");
  const [allowedRoles, setAllowedRoles] = useState<string[]>([]);
  // Newly published layers allow any role.
  const [appliedRoles, setAppliedRoles] = useState<string[]>([]);
  const [isPublic, setIsPublic] = useState(false);
  const rolesChanged =
    [...allowedRoles].sort().join() !== [...appliedRoles].sort().join();
  const plural = layers.length === 1 ? "layer" : "layers";
  const bulk = useMutation({
    meta: { suppressErrorToast: true },
    mutationFn: async (change: BulkChange) => {
      const body: UpdateLayerBody =
        change.kind === "style"
          ? { default_style: change.style }
          : change.kind === "roles"
            ? { allowed_roles: change.roles }
            : { public: true };
      // Sequential, so a failure names how far the change got.
      let done = 0;
      try {
        for (const layer of layers) {
          await updateLayer(workspace, serviceID, layer.id, body);
          done += 1;
        }
      } catch (reason) {
        const message =
          reason instanceof Error ? reason.message : String(reason);
        throw new Error(
          `Updated ${done} of ${layers.length} ${plural}: ${message}`,
          { cause: reason },
        );
      } finally {
        await client.invalidateQueries({
          queryKey: getListLayersQueryKey(workspace, serviceID),
        });
      }
    },
    onSuccess: (_result, change) => {
      if (change.kind === "public") setIsPublic(true);
      if (change.kind === "roles") setAppliedRoles(change.roles);
      toast.success(`${describe(change)} for ${layers.length} ${plural}.`);
    },
  });
  const previewAll = `${root}/preview?${new URLSearchParams({
    layers: layers.map((layer) => layer.public_id).join(","),
  })}`;
  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <p role="status">
          Published {layers.length} {isPublic ? "public" : "private"} {plural}.
          Check the map, then configure access for your client.
        </p>
        <Button size="sm" asChild>
          <Link to={previewAll}>
            <MapIcon /> Preview {layers.length === 1 ? "on map" : "all on map"}
          </Link>
        </Button>
      </div>
      <section
        aria-labelledby="bulk-layer-actions"
        className="space-y-3 rounded-lg border bg-muted/30 p-3"
      >
        <h3 id="bulk-layer-actions" className="text-sm font-medium">
          Apply to all {layers.length} {plural}
        </h3>
        <div className="flex flex-wrap items-end gap-2">
          <div className="min-w-48 flex-1 space-y-1">
            <Label htmlFor="bulk-layer-style">Default style</Label>
            <NativeSelect
              id="bulk-layer-style"
              className="w-full"
              value={style}
              disabled={bulk.isPending}
              onChange={(event) => setStyle(event.target.value)}
            >
              <option value="">
                {styles.isLoading ? "Loading styles…" : "Choose a style"}
              </option>
              {(styles.data?.styles ?? []).map((item) => (
                <option key={item.id} value={item.name}>
                  {item.title && item.title !== item.name
                    ? `${item.title} (${item.name})`
                    : item.name}
                </option>
              ))}
            </NativeSelect>
          </div>
          <Button
            size="sm"
            variant="outline"
            disabled={!style || bulk.isPending}
            onClick={() => bulk.mutate({ kind: "style", style })}
          >
            Set style
          </Button>
        </div>
        <fieldset className="space-y-2" disabled={bulk.isPending}>
          <legend className="text-sm font-medium">Allowed roles</legend>
          {(roles.data?.roles ?? []).length ? (
            <ToggleChips
              options={(roles.data?.roles ?? []).map((role) => ({
                value: role.id,
                label: role.name || role.id,
              }))}
              value={allowedRoles}
              disabled={bulk.isPending}
              onChange={setAllowedRoles}
            />
          ) : (
            <p className="text-xs text-muted-foreground">
              {roles.isLoading ? "Loading roles…" : "No workspace roles."}
            </p>
          )}
          <div className="flex flex-wrap items-center gap-2">
            <Button
              size="sm"
              variant="outline"
              disabled={!rolesChanged || bulk.isPending}
              onClick={() =>
                bulk.mutate({ kind: "roles", roles: allowedRoles })
              }
            >
              {allowedRoles.length ? "Limit access" : "Allow any role"}
            </Button>
            <span className="text-xs text-muted-foreground">
              No roles selected: any principal with workspace access.
            </span>
          </div>
        </fieldset>
        {!isPublic && (
          <Button
            size="sm"
            variant="outline"
            disabled={bulk.isPending}
            onClick={async () => {
              const confirmed = await confirmAction({
                title: `Make ${layers.length} ${plural} public?`,
                description:
                  "Anyone can read public layers wherever the service allows anonymous access.",
                confirmLabel: "Make public",
                destructive: true,
              });
              if (confirmed) bulk.mutate({ kind: "public" });
            }}
          >
            Make all public…
          </Button>
        )}
        {bulk.isPending && (
          <p role="status" className="text-sm text-muted-foreground">
            Updating {layers.length} {plural}…
          </p>
        )}
        {bulk.error && (
          <p role="alert" className="text-sm text-destructive">
            {bulk.error.message}
          </p>
        )}
      </section>
      <ul aria-label="Published layers" className="space-y-2">
        {layers.map((layer) => (
          <li key={layer.id} className="space-y-2 rounded-lg border p-3">
            <code className="break-all text-sm">{layer.public_id}</code>
            <div className="flex flex-wrap gap-3 text-sm">
              <Link
                className="underline"
                to={`${root}/preview?${new URLSearchParams({ layers: layer.public_id })}`}
              >
                Preview
              </Link>
              <Link
                className="underline"
                to={`${root}/endpoints?${new URLSearchParams({ layer: layer.public_id })}`}
              >
                Connect a client
              </Link>
              <Link
                className="underline"
                to={`${root}/layers?${new URLSearchParams({ layers_q: layer.public_id })}`}
              >
                Manage access
              </Link>
            </div>
          </li>
        ))}
      </ul>
    </div>
  );
}
