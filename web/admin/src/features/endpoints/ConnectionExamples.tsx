import { useState } from "react";
import { cn } from "@/lib/utils";
import { WrapText } from "lucide-react";
import { useQueries, useQuery } from "@tanstack/react-query";
import { Link, useSearchParams } from "react-router";
import { copyText } from "@/lib/clipboard";
import { useListServices } from "@/api/generated/services/services";
import { getListLayersQueryOptions } from "@/api/generated/layers/layers";
import { basePath } from "@/api/client";
import { QueryError } from "@/components/QueryError";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Label } from "@/components/ui/label";
import { datasourceCapabilities } from "@/lib/datasource-capabilities";
import { connectionExamples, featureURL } from "./connection-examples";
import { NativeSelect } from "@/components/NativeSelect";

export function ConnectionExamples({
  workspace,
  root,
  enabled,
}: {
  workspace: string;
  root: string;
  enabled: boolean;
}) {
  const [params, setParams] = useSearchParams();
  const services = useListServices(workspace);
  const stores = (services.data?.services ?? []).filter(
    (item) => datasourceCapabilities(item.type).features,
  );
  const queries = useQueries({
    queries: stores.map((store) =>
      getListLayersQueryOptions(workspace, store.id),
    ),
  });
  const layers = queries.flatMap((query, index) =>
    (query.data?.layers ?? []).map((layer) => ({
      ...layer,
      store: stores[index],
    })),
  );
  const requested = params.get("layer");
  const selected = requested
    ? layers.find((layer) => layer.public_id === requested)
    : layers[0];
  const [language, setLanguage] =
    useState<keyof ReturnType<typeof connectionExamples>>("curl");
  const [wrap, setWrap] = useState(true);
  const path = `/workspaces/${encodeURIComponent(workspace)}`;
  const error = services.error || queries.find((query) => query.error)?.error;
  const loading =
    services.isLoading || queries.some((query) => query.isLoading);
  const usable =
    !error &&
    !loading &&
    enabled &&
    selected?.enabled === true &&
    selected.store.enabled === true;
  const examples = selected
    ? connectionExamples(
        featureURL(root, selected.public_id),
        root,
        selected.public_id,
      )
    : undefined;
  // Same-origin read-only probe, never sends session credentials to url_base.
  const probe = useQuery({
    queryKey: [
      "connection-probe",
      workspace,
      selected?.id,
      selected?.updated_at,
      selected?.store.updated_at,
      usable,
    ],
    enabled: false,
    retry: false,
    queryFn: async ({ signal }) => {
      const url = `${basePath}${path}/ogc/collections/${encodeURIComponent(selected!.public_id)}/items?limit=1`;
      const response = await fetch(url, { credentials: "same-origin", signal });
      if (!response.ok) {
        const advice: Record<number, string> = {
          401: "Sign in again; the current session is not accepted.",
          403: "Check the layer's allowed roles and this user's workspace role.",
          404: "Check that the store, layer and Features service are enabled, then refresh the catalog.",
          503: "The server or data source is unavailable. Test the store connection and retry.",
        };
        throw new Error(
          `HTTP ${response.status}. ${advice[response.status] ?? "Test the store connection, then check the server logs with an operator."}`,
        );
      }
      const data = await response.json();
      if (data.type !== "FeatureCollection" || !Array.isArray(data.features))
        throw new Error(
          "The response is not a GeoJSON FeatureCollection. Check the server URL and reverse proxy.",
        );
      return data.features.length as number;
    },
  });
  return (
    <Card className="mb-4">
      <CardHeader>
        <CardTitle>Connect to a layer</CardTitle>
      </CardHeader>
      <CardContent className="space-y-4">
        <p className="text-sm">
          Use a workspace viewer API key, not an administrator token or
          NEOSRV_STORE_KEY. The key secret is shown only when created; its ID is
          not a credential.{" "}
          <Link
            className="underline"
            to={`${path}/api-keys?${new URLSearchParams({ connect: selected?.public_id ?? "" })}`}
          >
            Manage API keys
          </Link>
          .
        </p>
        <QueryError
          error={error}
          retry={() => {
            void services.refetch();
            queries.forEach((query) => void query.refetch());
          }}
          context="Could not load all published layers"
        />
        {loading && <p role="status">Loading published layers…</p>}
        {!loading && !error && layers.length === 0 && (
          <p>
            No feature layers published yet.{" "}
            <Link className="underline" to={`${path}/layers`}>
              Add a layer
            </Link>{" "}
            or{" "}
            <Link className="underline" to={`${path}/coverages`}>
              manage raster coverages
            </Link>
            . Raster clients use the WMS/WCS/WMTS URLs below.
          </p>
        )}
        {layers.length > 0 && (
          <div>
            <Label htmlFor="connection-layer">Published layer</Label>
            <NativeSelect
              id="connection-layer"
              className="mt-1 w-full"
              value={selected?.public_id ?? ""}
              onChange={(event) =>
                setParams(
                  (current) => {
                    const next = new URLSearchParams(current);
                    next.set("layer", event.target.value);
                    return next;
                  },
                  { replace: true },
                )
              }
            >
              {!selected && <option value="">Choose a layer</option>}
              {layers.map((layer) => (
                <option key={layer.id} value={layer.public_id}>
                  {layer.public_id} ({layer.store.name})
                </option>
              ))}
            </NativeSelect>
          </div>
        )}
        {!loading && !error && requested && !selected && (
          <p role="alert">
            The linked layer is not in the current catalog. Choose another layer
            or check its store.
          </p>
        )}
        {selected && (
          <>
            <p className="text-sm">
              {selected.public
                ? "Public access is configured; server authentication policy may still require credentials."
                : "Private layer. Use a workspace client key with an allowed role."}
            </p>
            <div className="flex flex-wrap items-center gap-3">
              <Label htmlFor="connection-language">Client example</Label>
              <NativeSelect
                id="connection-language"
                className="w-full"
                value={language}
                onChange={(event) =>
                  setLanguage(event.target.value as typeof language)
                }
              >
                <option value="curl">curl</option>
                <option value="javascript">JavaScript (server-side)</option>
                <option value="python">Python</option>
                <option value="qgis">QGIS</option>
                <option value="maplibre">MapLibre (integration snippet)</option>
              </NativeSelect>
            </div>
            {!usable && (
              <p role="status" className="text-sm">
                Resolve the store, layer or service status in the details below
                before using these examples.
              </p>
            )}
            <div className="flex justify-end">
              <Button
                variant="ghost"
                size="sm"
                aria-pressed={wrap}
                onClick={() => setWrap((value) => !value)}
              >
                <WrapText /> {wrap ? "Wrapping lines" : "Wrap lines"}
              </Button>
            </div>
            <pre
              className={cn(
                "max-h-96 overflow-auto rounded-md border bg-muted p-3 text-xs",
                // Wrapped by default so the URL at the end stays readable.
                wrap && "break-all whitespace-pre-wrap",
              )}
              tabIndex={0}
              aria-label="Connection example"
            >
              <code>{examples?.[language]}</code>
            </pre>
            <Button
              variant="outline"
              size="sm"
              disabled={!usable}
              onClick={() => void copyText(examples![language])}
            >
              Copy example
            </Button>
            <p className="text-xs text-muted-foreground">
              Examples request a small sample. Follow response links for
              pagination. Keys belong in environment variables or a client's
              credential store, never URLs, committed files or browser bundles.
            </p>
            <details
              key={selected.id}
              open={!usable || undefined}
              className="rounded-lg border p-3"
            >
              <summary className="cursor-pointer text-sm font-medium">
                Publication, access and rendering details
              </summary>
              <dl className="grid gap-3 text-sm sm:grid-cols-2">
                <div>
                  <dt className="font-medium">Publication</dt>
                  <dd>
                    {selected.enabled
                      ? "Published and enabled"
                      : "Published, but layer is disabled"}
                    .{" "}
                    {selected.store.enabled
                      ? "Store enabled."
                      : "Store disabled."}{" "}
                    <Link
                      className="underline"
                      to={`${path}/${selected.store.enabled ? "layers" : "stores"}`}
                    >
                      Manage {selected.store.enabled ? "layer" : "store"}
                    </Link>
                  </dd>
                </div>
                <div>
                  <dt className="font-medium">Features service</dt>
                  <dd>
                    {enabled ? "Enabled" : "Disabled or status unavailable"}.{" "}
                    <Link className="underline" to={`${path}/settings`}>
                      Service settings
                    </Link>
                  </dd>
                </div>
                <div>
                  <dt className="font-medium">Client access policy</dt>
                  <dd>
                    {selected.public
                      ? "Public layer; server authentication policy may still require credentials."
                      : `Private layer; requires an authorized workspace role${selected.allowed_roles?.length ? ` (${selected.allowed_roles.join(", ")})` : ""}.`}{" "}
                    A console session does not sign in external clients.
                  </dd>
                </div>
                <div>
                  <dt className="font-medium">Map rendering</dt>
                  <dd>
                    Not verified by publication or a data request.{" "}
                    <Link
                      className="underline"
                      to={`${path}/preview?${new URLSearchParams({ layers: selected.public_id })}`}
                    >
                      Preview this layer
                    </Link>{" "}
                    to check geometry, extent and style.
                  </dd>
                </div>
              </dl>
            </details>
            <div className="space-y-2">
              <Button
                variant="outline"
                disabled={!usable || probe.isFetching}
                onClick={() => void probe.refetch()}
              >
                {probe.isFetching
                  ? "Checking…"
                  : "Check access with my session"}
              </Button>
              {usable && probe.data !== undefined && !probe.error && (
                <p role="status" className="text-sm">
                  Feature request succeeded with your console session (
                  {probe.data} sampled feature{probe.data === 1 ? "" : "s"}).
                  Test the client key separately using an example above.
                </p>
              )}
              {usable && (
                <QueryError
                  error={probe.error}
                  context="Feature request failed"
                  retry={() => probe.refetch()}
                />
              )}
              <p className="text-xs text-muted-foreground">
                For connection failures,{" "}
                <Link className="underline" to={`${path}/stores`}>
                  test the store connection
                </Link>
                . This check does not verify the public URL, a different user's
                permissions, or map rendering.
              </p>
            </div>
          </>
        )}
      </CardContent>
    </Card>
  );
}
