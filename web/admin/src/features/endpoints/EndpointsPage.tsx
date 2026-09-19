import { useParams } from "react-router";
import { Copy } from "lucide-react";
import { useGetWorkspaceSummary } from "@/api/generated/workspaces/workspaces";
import type { WorkspaceSummaryProtocols } from "@/api/generated/models";
import type { ConsoleConfigServices } from "@/api/generated/models";
import { useAuth } from "@/auth/auth-context";
import { Page } from "@/components/Page";
import { StatusChip } from "@/components/StatusChip";
import { ServiceState } from "@/features/workspaces/service-state";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { cn } from "@/lib/utils";
import { QueryError } from "@/components/QueryError";
import { ConnectionExamples } from "./ConnectionExamples";
import { copyText } from "@/lib/clipboard";

interface EndpointDefinition {
  name: string;
  url: string;
  serverFlag: keyof ConsoleConfigServices;
  workspaceFlag: keyof WorkspaceSummaryProtocols;
}

function disabledReason({
  loading,
  error,
  serverEnabled,
  workspaceEnabled,
}: {
  loading: boolean;
  error: boolean;
  serverEnabled: boolean;
  workspaceEnabled: boolean;
}) {
  if (loading) return "Checking service status…";
  if (error) return "Service status unavailable";
  if (!serverEnabled)
    return "Not enabled on this server. An operator enables it in the server configuration.";
  if (!workspaceEnabled)
    return "Off in this workspace. Turn it on in Service settings.";
  return "";
}

export function EndpointsPage() {
  const { ws = "" } = useParams();
  const { config } = useAuth();
  const origin = (config?.url_base || location.origin).replace(/\/$/, "");
  const base = config?.base_path ?? "";
  const workspacePath = `/workspaces/${encodeURIComponent(ws)}`;
  const root = `${origin}${base}${workspacePath}`;
  const summary = useGetWorkspaceSummary(ws);
  const endpoints: EndpointDefinition[] = [
    {
      name: "OGC API – Features",
      url: `${root}/ogc/`,
      serverFlag: "ogcapi",
      workspaceFlag: "ogcapi",
    },
    {
      name: "WMS GetCapabilities",
      url: `${root}/wms?service=WMS&request=GetCapabilities`,
      serverFlag: "wms",
      workspaceFlag: "wms",
    },
    {
      name: "WFS GetCapabilities",
      url: `${root}/wfs?service=WFS&request=GetCapabilities`,
      serverFlag: "wfs",
      workspaceFlag: "wfs",
    },
    {
      name: "WCS GetCapabilities",
      url: `${root}/wcs?service=WCS&request=GetCapabilities`,
      serverFlag: "wcs",
      workspaceFlag: "wcs",
    },
    {
      name: "WMTS GetCapabilities",
      url: `${root}/wmts?service=WMTS&request=GetCapabilities`,
      serverFlag: "wmts",
      workspaceFlag: "wmts",
    },
    {
      name: "OGC API – Tiles",
      url: `${root}/ogc-tiles/`,
      serverFlag: "tiles",
      workspaceFlag: "ogc_tiles",
    },
  ];

  return (
    <Page
      title="Endpoints"
      description="Canonical client URLs derived from the configured public URL and base path. Inactive services remain visible for reference."
    >
      <QueryError
        error={summary.error}
        retry={() => summary.refetch()}
        context="Service status could not be loaded"
      />
      <ConnectionExamples
        key={ws}
        workspace={ws}
        root={root}
        enabled={
          !summary.isLoading &&
          !summary.isError &&
          config?.services.ogcapi === true &&
          summary.data?.protocols.ogcapi === true
        }
      />
      <div className="grid gap-3 lg:grid-cols-2">
        {endpoints.map((endpoint) => {
          const serverEnabled = config?.services[endpoint.serverFlag] === true;
          const workspaceEnabled =
            summary.data?.protocols[endpoint.workspaceFlag] === true;
          const active =
            !summary.isLoading &&
            !summary.isError &&
            serverEnabled &&
            workspaceEnabled;
          const reason = disabledReason({
            loading: summary.isLoading,
            error: summary.isError,
            serverEnabled,
            workspaceEnabled,
          });
          return (
            <Card
              key={endpoint.name}
              data-state={active ? "active" : "inactive"}
              className={cn(!active && "bg-muted/50 grayscale")}
            >
              <CardHeader className="grid-cols-[1fr_auto]">
                <CardTitle>{endpoint.name}</CardTitle>
                {summary.isLoading || summary.isError ? (
                  <StatusChip value="unknown" label="Unknown" />
                ) : (
                  <ServiceState
                    base={workspacePath}
                    state={
                      !serverEnabled
                        ? "off_server"
                        : workspaceEnabled
                          ? "on"
                          : "off_workspace"
                    }
                  />
                )}
                {!active && (
                  <p className="col-span-2 text-xs text-muted-foreground">
                    {reason}
                  </p>
                )}
              </CardHeader>
              <CardContent className="flex gap-2">
                <code
                  tabIndex={0}
                  aria-label={`${endpoint.name} URL`}
                  className={cn(
                    "min-w-0 flex-1 border bg-muted p-2 text-xs break-all",
                    !active && "select-none",
                  )}
                >
                  {endpoint.url}
                </code>
                <Button
                  variant="outline"
                  size="icon"
                  disabled={!active}
                  onClick={() => void copyText(endpoint.url)}
                  aria-label={`Copy ${endpoint.name}`}
                >
                  <Copy />
                </Button>
              </CardContent>
            </Card>
          );
        })}
      </div>
    </Page>
  );
}
