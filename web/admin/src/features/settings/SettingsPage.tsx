import { useState, useCallback } from "react";
import {
  useMutation,
  useQuery,
  useQueryClient,
  type QueryKey,
} from "@tanstack/react-query";
import { useParams } from "react-router";
import { toast } from "sonner";
import * as api from "@/api/generated/settings/settings";
import { getGetWorkspaceSummaryQueryKey } from "@/api/generated/workspaces/workspaces";
import type { ConsoleConfigServices } from "@/api/generated/models";
import { useAuth } from "@/auth/auth-context";
import { Page } from "@/components/Page";
import { QueryError } from "@/components/QueryError";
import { ObjectEditor } from "@/components/SchemaFields";
import { validateFields } from "@/lib/schema-fields";
import { protocolSchema } from "@/lib/resource-schemas";
import { useUnsavedChangesGuard } from "@/lib/forms";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { SwitchRow } from "@/components/SwitchRow";
import { confirmDiscard } from "@/lib/confirm";

interface Settings {
  enabled: boolean;
  [key: string]: unknown;
}
interface Protocol {
  id: string;
  name: string;
  schema: string;
  description: string;
  flag: keyof ConsoleConfigServices;
  configKey?: string;
  read: (workspace: string) => Promise<{ enabled: boolean }>;
  write: (
    workspace: string,
    settings: Settings,
  ) => Promise<{ enabled: boolean }>;
  key: (workspace: string) => QueryKey;
}
function protocol<T extends { enabled: boolean }>(
  meta: Omit<Protocol, "read" | "write" | "key">,
  read: (workspace: string) => Promise<T>,
  write: (workspace: string, data: T) => Promise<T>,
  key: Protocol["key"],
): Protocol {
  // The server-owned schema is validated before submitting this generic form.
  return { ...meta, read, write: (ws, value) => write(ws, value as T), key };
}
const protocols = [
  protocol(
    {
      id: "ogcapi",
      name: "OGC API – Features",
      schema: "OGCAPISettings",
      flag: "ogcapi",
      description: "Publish feature collections and control paging limits.",
    },
    api.getOGCAPISettings,
    api.updateOGCAPISettings,
    api.getGetOGCAPISettingsQueryKey,
  ),
  protocol(
    {
      id: "wms",
      name: "Web Map Service (WMS)",
      schema: "WMSSettings",
      flag: "wms",
      configKey: "NEOSRV_WMS_ENABLED",
      description:
        "Render styled map images; configure image and rendering limits.",
    },
    api.getWMSSettings,
    api.updateWMSSettings,
    api.getGetWMSSettingsQueryKey,
  ),
  protocol(
    {
      id: "wfs",
      name: "Web Feature Service (WFS)",
      schema: "WFSSettings",
      flag: "wfs",
      configKey: "NEOSRV_WFS_ENABLED",
      description:
        "Query and transact features; configure feature counts and timeouts.",
    },
    api.getWFSSettings,
    api.updateWFSSettings,
    api.getGetWFSSettingsQueryKey,
  ),
  protocol(
    {
      id: "wcs",
      name: "Web Coverage Service (WCS)",
      schema: "WCSSettings",
      flag: "wcs",
      configKey: "NEOSRV_WCS_ENABLED",
      description:
        "Publish raster coverages with processing, CRS and output limits.",
    },
    api.getWCSSettings,
    api.updateWCSSettings,
    api.getGetWCSSettingsQueryKey,
  ),
  protocol(
    {
      id: "wmts",
      name: "Web Map Tile Service (WMTS)",
      schema: "WMTSSettings",
      flag: "wmts",
      configKey: "NEOSRV_WMTS_ENABLED",
      description:
        "Serve cached map tiles and configure service-provider metadata.",
    },
    api.getWMTSSettings,
    api.updateWMTSSettings,
    api.getGetWMTSSettingsQueryKey,
  ),
  protocol(
    {
      id: "ogc-tiles",
      name: "OGC API – Tiles",
      schema: "OGCTilesSettings",
      flag: "tiles",
      configKey: "NEOSRV_TILES_ENABLED",
      description:
        "Configure vector/map tiles, formats, matrix sets and cache limits.",
    },
    api.getOGCTilesAPISettings,
    api.updateOGCTilesAPISettings,
    api.getGetOGCTilesAPISettingsQueryKey,
  ),
];

function ServiceSettings({
  protocol,
  workspace,
  onDirty,
}: {
  protocol: Protocol;
  workspace: string;
  onDirty: (id: string, dirty: boolean) => void;
}) {
  const { config } = useAuth();
  const client = useQueryClient();
  const query = useQuery({
    queryKey: protocol.key(workspace),
    queryFn: () => protocol.read(workspace),
  });
  const settings = query.data;
  const serverEnabled = config?.services[protocol.flag] === true;
  const [draft, setDraft] = useState<string | null>(null);
  const schema = protocolSchema(protocol.schema);
  const update = useMutation({
    meta: { suppressErrorToast: true },
    mutationFn: ({
      kind,
      enabled,
    }: {
      kind: "toggle" | "form";
      enabled?: boolean;
    }) => {
      if (!settings) throw new Error("Settings are not loaded.");
      if (kind === "toggle")
        return protocol.write(workspace, {
          ...settings,
          enabled: Boolean(enabled),
        });
      const parsed = JSON.parse(draft ?? JSON.stringify(settings)) as Record<
        string,
        unknown
      >;
      const errors = validateFields(schema, parsed);
      if (
        typeof parsed.limit_default === "number" &&
        typeof parsed.limit_max === "number" &&
        parsed.limit_default > parsed.limit_max
      )
        errors.push("Default page size cannot exceed maximum page size.");
      if (
        typeof parsed.default_count === "number" &&
        typeof parsed.max_features === "number" &&
        parsed.default_count > parsed.max_features
      )
        errors.push("Default count cannot exceed maximum features.");
      if (errors.length) throw new Error(errors.join(" "));
      return protocol.write(workspace, {
        ...settings,
        ...parsed,
        enabled: settings.enabled,
      });
    },
    onSuccess: async (updated, variables) => {
      client.setQueryData(protocol.key(workspace), updated);
      if (variables.kind === "form") {
        setDraft(null);
        onDirty(protocol.id, false);
        toast.success(protocol.name + " settings saved");
      }
      await client.invalidateQueries({
        queryKey: getGetWorkspaceSummaryQueryKey(workspace),
      });
    },
  });
  const checked =
    update.isPending && update.variables?.kind === "toggle"
      ? update.variables.enabled
      : (settings?.enabled ?? false);
  const root =
    (config?.url_base || location.origin).replace(/\/$/, "") +
    (config?.base_path ?? "") +
    "/workspaces/" +
    encodeURIComponent(workspace);
  const endpoint =
    protocol.id === "ogcapi"
      ? root + "/ogc/"
      : protocol.id === "ogc-tiles"
        ? root + "/ogc-tiles/"
        : root +
          "/" +
          protocol.id +
          "?service=" +
          protocol.id.toUpperCase() +
          "&request=GetCapabilities";
  return (
    <section aria-label={protocol.name} className="space-y-3 px-4 py-4">
      {query.isLoading ? (
        <div className="flex items-center justify-between gap-4">
          <div>
            <p className="text-base font-medium">{protocol.name}</p>
            <p className="mt-1 text-sm text-muted-foreground">
              {protocol.description}
            </p>
          </div>
          <Skeleton className="h-5 w-8" />
        </div>
      ) : (
        <SwitchRow
          id={"service-" + protocol.id}
          label={protocol.name}
          labelClassName="text-base"
          description={protocol.description}
          aria-label={"Enable " + protocol.name}
          checked={Boolean(checked)}
          disabled={!serverEnabled || !settings || update.isPending}
          onCheckedChange={(enabled) =>
            update.mutate({ kind: "toggle", enabled })
          }
          stateLabel={["On", "Off"]}
        />
      )}
      {!serverEnabled && (
        <p className="text-sm text-warning">
          Disabled on the server.
          {protocol.configKey && (
            <>
              {" "}
              Enable <code>{protocol.configKey}=true</code> and restart the
              server.
            </>
          )}
        </p>
      )}
      <QueryError
        error={query.error}
        retry={() => query.refetch()}
        context="Service settings could not be loaded"
      />
      <QueryError error={update.error} context="Settings were not saved" />
      {settings && (
        <details>
          <summary className="cursor-pointer text-sm font-medium">
            Configure {protocol.name}
            {draft !== null ? " — unsaved changes" : ""}
          </summary>
          <div className="mt-3 flex flex-wrap items-center gap-2 rounded-md bg-muted/50 p-2 text-sm">
            <span className="text-muted-foreground">Endpoint</span>
            {serverEnabled && settings.enabled ? (
              <>
                <a
                  className="min-w-0 flex-1 font-mono text-xs break-all underline-offset-4 hover:underline"
                  href={endpoint}
                  target="_blank"
                  rel="noreferrer"
                >
                  {endpoint}
                </a>
                <Button
                  type="button"
                  size="sm"
                  variant="outline"
                  onClick={() => {
                    void navigator.clipboard.writeText(endpoint).then(
                      () => toast.success("Endpoint copied"),
                      () =>
                        toast.error("Could not copy; use the endpoint link."),
                    );
                  }}
                >
                  Copy URL
                </Button>
              </>
            ) : (
              <span className="text-muted-foreground">
                available once this service is on.
              </span>
            )}
          </div>
          <form
            className="mt-4 space-y-4"
            onSubmit={(event) => {
              event.preventDefault();
              update.mutate({ kind: "form" });
            }}
          >
            <fieldset disabled={!serverEnabled || update.isPending}>
              <ObjectEditor
                schema={schema}
                draft={draft ?? JSON.stringify(settings, null, 2)}
                label={protocol.name}
                disabled={!serverEnabled || update.isPending}
                onChange={(next) => {
                  setDraft(next);
                  onDirty(protocol.id, true);
                }}
              />
            </fieldset>
            <div className="flex flex-wrap gap-2">
              <Button
                type="submit"
                disabled={!serverEnabled || update.isPending || draft === null}
              >
                Save settings
              </Button>
              <Button
                type="button"
                variant="outline"
                disabled={draft === null || update.isPending}
                onClick={async () => {
                  if (await confirmDiscard("Discard unsaved settings?")) {
                    setDraft(null);
                    onDirty(protocol.id, false);
                    update.reset();
                  }
                }}
              >
                Discard changes
              </Button>
            </div>
          </form>
        </details>
      )}
    </section>
  );
}

export function SettingsPage() {
  const { ws = "" } = useParams();
  return <WorkspaceSettings key={ws} workspace={ws} />;
}
function WorkspaceSettings({ workspace }: { workspace: string }) {
  const [dirty, setDirty] = useState<Set<string>>(new Set());
  useUnsavedChangesGuard(dirty.size > 0);
  const onDirty = useCallback(
    (id: string, changed: boolean) =>
      setDirty((current) => {
        const next = new Set(current);
        if (changed) next.add(id);
        else next.delete(id);
        return next;
      }),
    [],
  );
  return (
    <Page
      title="Service settings"
      description="Switches save immediately. Expand a service to configure it; each form saves independently."
    >
      <div className="divide-y rounded-xl border bg-card">
        {protocols.map((protocol) => (
          <ServiceSettings
            key={protocol.id}
            protocol={protocol}
            workspace={workspace}
            onDirty={onDirty}
          />
        ))}
      </div>
    </Page>
  );
}
