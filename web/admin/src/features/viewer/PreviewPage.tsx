import { useEffect, useMemo, useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Link, useParams, useSearchParams } from "react-router";
import { maplibre } from "@/lib/maplibre";
import type { FeatureCollection } from "geojson";
import "maplibre-gl/dist/maplibre-gl.css";
import { basePath } from "@/api/client";
import { useGetWorkspaceSummary } from "@/api/generated/workspaces/workspaces";
import { useAuth } from "@/auth/auth-context";
import { useCatalogChoices } from "@/features/catalog/use-catalog-choices";
import { Page } from "@/components/Page";
import { QueryError } from "@/components/QueryError";
import { WMSPreview } from "@/components/WMSPreview";
import { Button } from "@/components/ui/button";
import { Switch } from "@/components/ui/switch";
import { toast } from "sonner";
import {
  collectionBounds,
  sharedBounds,
  unionBounds,
  mapPalette,
  type MapBounds,
} from "./map-bounds";
import {
  defaultPublication,
  defaultSource,
  graticule,
  groupPreviewResources,
  legendTargets,
  resolveSource,
  type PreviewGrouping,
  sampleBounds,
  sourcesFor,
  type PreviewSource,
} from "./preview-selection";
import { NativeSelect } from "@/components/NativeSelect";
import { Input } from "@/components/ui/input";

interface Collection {
  id: string;
  kind?: "feature" | "coverage" | "group";
  title?: string;
  /** Name of the store the publication reads from; layer groups have none. */
  store?: string;
  unavailable?: string;
  managePath?: string;
  updatedAt?: string;
  extent?: { spatial?: { bbox?: number[][]; crs?: string } };
}
type Source = PreviewSource;
interface Entry {
  id: string;
  source: Source;
  style: string;
  opacity: number;
}
async function readJSON(url: string, signal?: AbortSignal) {
  const response = await fetch(url, { credentials: "same-origin", signal });
  if (!response.ok)
    throw new Error(
      (await response.text()).slice(0, 500) ||
        "Request failed (" + response.status + ")",
    );
  return response.json();
}

const sourceLabels: Record<Source, string> = {
  geojson: "GeoJSON sample",
  wms: "WMS (server style)",
  tiles: "Vector tiles (diagnostic)",
};

export function PreviewPage() {
  const { ws = "" } = useParams();
  return <WorkspacePreview key={ws} workspace={ws} />;
}
function WorkspacePreview({ workspace }: { workspace: string }) {
  const { config, me } = useAuth();
  const [params, setParams] = useSearchParams();
  const container = useRef<HTMLDivElement>(null);
  const map = useRef<maplibre.Map | null>(null);
  const [ready, setReady] = useState(false);
  const [moving, setMoving] = useState(false);
  const [revision, setRevision] = useState(0);
  const [basemap, setBasemap] = useState(false);
  const [layersOpen, setLayersOpen] = useState(false);
  const [layerQuery, setLayerQuery] = useState("");
  const [groupBy, setGroupBy] = useState<PreviewGrouping>("store");
  const [errors, setErrors] = useState<string[]>([]);
  const [samples, setSamples] = useState<Record<string, number>>({});
  const [sampleExtents, setSampleExtents] = useState<Record<string, MapBounds>>(
    {},
  );
  const [properties, setProperties] = useState<unknown>();
  const [position, setPosition] = useState("");
  const root = basePath + "/workspaces/" + encodeURIComponent(workspace);
  const summary = useGetWorkspaceSummary(workspace);
  const catalog = useCatalogChoices(workspace);
  const active = {
    geojson:
      config?.services?.ogcapi !== false &&
      summary.data?.protocols?.ogcapi !== false,
    wms:
      config?.services?.wms === true && summary.data?.protocols?.wms === true,
    tiles:
      config?.services?.tiles === true &&
      summary.data?.protocols?.ogc_tiles === true,
  };
  const collections = useQuery({
    queryKey: [root + "/ogc/collections"],
    enabled: active.geojson,
    queryFn: ({ signal }) =>
      readJSON(root + "/ogc/collections", signal) as Promise<{
        collections: Collection[];
      }>,
  });
  const resources = useMemo<Collection[]>(() => {
    const merged = new Map(
      (collections.data?.collections ?? []).map((item) => [item.id, item]),
    );
    const storeNames = new Map(
      catalog.services.map((service) => [service.id, service.name]),
    );
    for (const resource of catalog.allResources ?? catalog.resources) {
      const existing = merged.get(resource.public_id);
      merged.set(resource.public_id, {
        id: resource.public_id,
        title: resource.title,
        kind: resource.kind,
        store:
          "service_id" in resource && resource.service_id
            ? storeNames.get(resource.service_id)
            : undefined,
        updatedAt: (resource as { updated_at?: string }).updated_at,
        unavailable: !resource.storeEnabled
          ? "Its store is disabled. Enable the store to preview this publication."
          : resource.enabled === false
            ? "This publication is disabled. Enable it to preview its data."
            : undefined,
        managePath: !resource.storeEnabled
          ? "stores"
          : resource.kind === "coverage"
            ? "coverages"
            : resource.kind === "group"
              ? "layer-groups"
              : "layers",
        extent:
          resource.native_extent?.srid === 4326
            ? {
                spatial: {
                  bbox: [
                    [
                      resource.native_extent.min_x,
                      resource.native_extent.min_y,
                      resource.native_extent.max_x,
                      resource.native_extent.max_y,
                    ],
                  ],
                },
              }
            : existing?.extent,
      });
    }
    return Array.from(merged.values());
  }, [
    catalog.allResources,
    catalog.resources,
    catalog.services,
    collections.data,
  ]);
  const resourceKinds = useMemo(
    () =>
      new Map(
        resources.map((resource) => [resource.id, resource.kind ?? "feature"]),
      ),
    [resources],
  );
  const selectionKey = [
    params.get("layers"),
    params.get("sources"),
    params.get("styles"),
    params.get("opacity"),
  ].join("|");
  const entries = useMemo<Entry[]>(() => {
    const [layers, sources, styles, opacities] = selectionKey
      .split("|")
      .map((value) => value.split(","));
    return layers.filter(Boolean).map((id, i) => ({
      id,
      source: resolveSource(sources[i], resourceKinds.get(id) ?? "feature"),
      style: styles[i] ?? "",
      opacity:
        opacities[i] === "" || opacities[i] === undefined
          ? 1
          : Math.max(0, Math.min(1, Number(opacities[i]) || 0)),
    }));
  }, [selectionKey, resourceKinds]);
  const groupedResources = useMemo(
    () =>
      groupPreviewResources(resources, {
        query: layerQuery,
        shown: new Set(entries.map((entry) => entry.id)),
        groupBy,
      }),
    [resources, layerQuery, entries, groupBy],
  );
  const unavailable = useMemo(
    () =>
      new Map(
        entries.flatMap((entry) => {
          const resource = resources.find((item) => item.id === entry.id);
          if (resource?.unavailable)
            return [
              [
                entry.id,
                {
                  message: resource.unavailable,
                  path: resource.managePath ?? "layers",
                },
              ],
            ];
          if (
            !resource &&
            !catalog.isLoading &&
            !collections.isLoading &&
            !catalog.error &&
            !collections.error
          )
            return [
              [
                entry.id,
                {
                  message:
                    "This publication no longer exists or is not accessible to your account.",
                  path: "layers",
                },
              ],
            ];
          return [];
        }),
      ),
    [
      entries,
      resources,
      catalog.isLoading,
      catalog.error,
      collections.isLoading,
      collections.error,
    ],
  );
  function select(next: Entry[]) {
    setParams(
      (previous) => {
        const value = new URLSearchParams(previous);
        for (const [key, items] of [
          ["layers", next.map((item) => item.id)],
          ["sources", next.map((item) => item.source)],
          ["styles", next.map((item) => item.style)],
          ["opacity", next.map((item) => String(item.opacity))],
        ] as const) {
          if (items.length) value.set(key, items.join(","));
          else value.delete(key);
        }
        return value;
      },
      { replace: true },
    );
  }
  const defaulted = useRef(params.has("layers"));
  const summaryLoaded = !summary.isLoading;
  useEffect(() => {
    if (
      defaulted.current ||
      !summaryLoaded ||
      catalog.isLoading ||
      collections.isLoading
    )
      return;
    defaulted.current = true;
    const pick = defaultPublication(resources, active);
    if (pick)
      select([
        {
          id: pick.id,
          source: defaultSource(pick.kind ?? "feature", active),
          style: "",
          opacity: 1,
        },
      ]);
    // `active` and `select` are derived each render; the ref runs this once.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [summaryLoaded, catalog.isLoading, collections.isLoading, resources]);
  const boundsFor = (resource: Collection) =>
    collectionBounds(resource.extent) ?? sampleExtents[resource.id];
  // Publications currently drawn, in drawing order; the panel pins them.
  const shownResources = entries.flatMap((entry) => {
    const resource = resources.find((item) => item.id === entry.id);
    return resource ? [resource] : [];
  });
  const shownBounds = unionBounds(
    shownResources.flatMap((resource) => {
      const bounds = boundsFor(resource);
      return bounds ? [bounds] : [];
    }),
  );
  // Generous padding keeps point data off the map edge, scaled for phones.
  const fitPadding = () =>
    Math.round(Math.min(60, (container.current?.clientWidth || 480) / 8));
  const initialBounds = useRef(sharedBounds(params.get("bbox")));
  const framed = useRef(false);
  const interacted = useRef(false);
  const selection = useRef(entries);
  useEffect(() => {
    selection.current = entries.filter((entry) => !unavailable.has(entry.id));
  }, [entries, unavailable]);
  useEffect(() => {
    if (!container.current) return;
    const instance = new maplibre.Map({
      container: container.current,
      center: [0, 20],
      zoom: 1,
      style: {
        version: 8,
        sources: {
          graticule: {
            type: "geojson",
            data: { type: "FeatureCollection", features: [] },
          },
        },
        layers: [
          {
            id: "background",
            type: "background",
            paint: { "background-color": mapPalette().background },
          },
          {
            id: "graticule",
            type: "line",
            source: "graticule",
            paint: { "line-color": mapPalette().graticule, "line-width": 0.75 },
          },
        ],
      },
    });
    map.current = instance;
    instance.addControl(new maplibre.NavigationControl(), "top-right");
    // Repaint the base layers when the console theme changes.
    const themeObserver = new MutationObserver(() => {
      if (!instance.getLayer("background")) return;
      const palette = mapPalette();
      instance.setPaintProperty(
        "background",
        "background-color",
        palette.background,
      );
      instance.setPaintProperty("graticule", "line-color", palette.graticule);
    });
    themeObserver.observe(document.documentElement, {
      attributes: true,
      attributeFilter: ["class"],
    });
    const drawGraticule = () => {
      const view = instance.getBounds();
      const source = instance.getSource("graticule");
      if (source?.type === "geojson")
        (source as maplibre.GeoJSONSource).setData(
          graticule(
            [
              [view.getWest(), view.getSouth()],
              [view.getEast(), view.getNorth()],
            ],
            instance.getZoom(),
          ),
        );
    };
    instance.on("load", () => {
      drawGraticule();
      setReady(true);
      const b = initialBounds.current;
      if (b && !interacted.current)
        instance.fitBounds(b, { duration: 0, padding: fitPadding() });
    });
    instance.on("error", (event) =>
      setErrors((current) => [...new Set([...current, event.error.message])]),
    );
    instance.on("movestart", () => setMoving(true));
    instance.on("moveend", () => {
      setMoving(false);
      drawGraticule();
      const c = instance.getCenter();
      setPosition(
        c.lng.toFixed(4) +
          ", " +
          c.lat.toFixed(4) +
          " · zoom " +
          instance.getZoom().toFixed(1),
      );
    });
    const clickAbort = new AbortController();
    instance.on("click", (event) => {
      const features = instance
        // Small symbols and thin lines need a usable click target too.
        .queryRenderedFeatures([
          [event.point.x - 10, event.point.y - 10],
          [event.point.x + 10, event.point.y + 10],
        ])
        .filter((feature) => feature.layer.id.startsWith("publication-"));
      if (features.length) {
        setProperties(features.map((feature) => feature.properties));
        return;
      }
      const wms = selection.current.filter((entry) => entry.source === "wms");
      if (!wms.length) {
        setProperties("No feature at this location.");
        return;
      }
      const bounds = instance.getBounds();
      function project(lng: number, lat: number) {
        return [
          (lng * 20037508.342789244) / 180,
          Math.log(
            Math.tan(
              ((90 + Math.max(-85.0511, Math.min(85.0511, lat))) * Math.PI) /
                360,
            ),
          ) * 6378137,
        ];
      }
      const bbox = [
        ...project(bounds.getWest(), bounds.getSouth()),
        ...project(bounds.getEast(), bounds.getNorth()),
      ].join(",");
      const query = new URLSearchParams({
        SERVICE: "WMS",
        VERSION: "1.3.0",
        REQUEST: "GetFeatureInfo",
        LAYERS: wms.map((e) => e.id).join(","),
        QUERY_LAYERS: wms.map((e) => e.id).join(","),
        STYLES: wms.map((e) => e.style).join(","),
        CRS: "EPSG:3857",
        BBOX: bbox,
        WIDTH: String(instance.getContainer().clientWidth),
        HEIGHT: String(instance.getContainer().clientHeight),
        I: String(Math.round(event.point.x)),
        J: String(Math.round(event.point.y)),
        INFO_FORMAT: "application/json",
        FEATURE_COUNT: "10",
      });
      void readJSON(root + "/wms?" + query, clickAbort.signal).then(
        setProperties,
        (error) => {
          if (!clickAbort.signal.aborted) setProperties(error.message);
        },
      );
    });
    return () => {
      clickAbort.abort();
      themeObserver.disconnect();
      instance.remove();
      map.current = null;
    };
  }, [root]);
  useEffect(() => {
    if (
      !ready ||
      !map.current ||
      initialBounds.current ||
      framed.current ||
      interacted.current ||
      catalog.isLoading ||
      collections.isLoading
    )
      return;
    const bounds = unionBounds(
      entries.flatMap((entry) => {
        const resource = resources.find((item) => item.id === entry.id);
        if (!resource || unavailable.has(entry.id)) return [];
        const bounds = boundsFor(resource);
        return bounds ? [bounds] : [];
      }),
    );
    if (bounds) {
      framed.current = true;
      map.current.fitBounds(bounds, {
        duration: 0,
        padding: fitPadding(),
        maxZoom: 15,
      });
    }
    // boundsFor only reads resources and sampleExtents, listed below.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [
    ready,
    entries,
    resources,
    sampleExtents,
    unavailable,
    catalog.isLoading,
    collections.isLoading,
  ]);
  const availabilityKey = JSON.stringify(active);
  useEffect(() => {
    const target = map.current;
    if (!ready || !target || catalog.isLoading || collections.isLoading) return;
    const abort = new AbortController();
    const sources: string[] = [],
      layers: string[] = [];
    const available = JSON.parse(availabilityKey) as typeof active;
    void (async () => {
      setErrors([]);
      setSamples({});
      if (basemap && config?.basemap_url) {
        target.addSource("operator-basemap", {
          type: "raster",
          tiles: [config.basemap_url],
          tileSize: 256,
        });
        sources.push("operator-basemap");
        target.addLayer({
          id: "operator-basemap",
          type: "raster",
          source: "operator-basemap",
        });
        layers.push("operator-basemap");
      }
      for (const [index, entry] of entries.entries()) {
        if (abort.signal.aborted) break;
        if (unavailable.has(entry.id) || !resourceKinds.has(entry.id)) continue;
        const id = "publication-" + index;
        try {
          if (
            entry.source !== "wms" &&
            resourceKinds.get(entry.id) !== "feature"
          )
            throw new Error(
              "Coverages and groups require WMS preview; choose WMS or enable it in Service settings.",
            );
          if (!available[entry.source])
            throw new Error(
              "This service is disabled; enable it in Service settings or choose another source.",
            );
          if (entry.source === "wms") {
            const url =
              root +
              "/wms?SERVICE=WMS&VERSION=1.3.0&REQUEST=GetMap&LAYERS=" +
              encodeURIComponent(entry.id) +
              "&STYLES=" +
              encodeURIComponent(entry.style) +
              "&CRS=EPSG:3857&BBOX={bbox-epsg-3857}&WIDTH=256&HEIGHT=256&FORMAT=image/png&TRANSPARENT=TRUE";
            target.addSource(id, {
              type: "raster",
              tiles: [url],
              tileSize: 256,
            });
            sources.push(id);
            target.addLayer({
              id,
              source: id,
              type: "raster",
              paint: { "raster-opacity": entry.opacity },
            });
            layers.push(id);
          } else {
            if (entry.source === "geojson") {
              const data = (await readJSON(
                root +
                  "/ogc/collections/" +
                  encodeURIComponent(entry.id) +
                  "/items?limit=1000",
                abort.signal,
              )) as FeatureCollection;
              if (abort.signal.aborted) break;
              if (
                data.type !== "FeatureCollection" ||
                !Array.isArray(data.features)
              )
                throw new Error(
                  "The server did not return a feature collection.",
                );
              target.addSource(id, { type: "geojson", data });
              setSamples((current) => ({
                ...current,
                [entry.id]: data.features.length,
              }));
              const extent = sampleBounds(data);
              if (extent)
                setSampleExtents((current) =>
                  current[entry.id]
                    ? current
                    : { ...current, [entry.id]: extent },
                );
            } else {
              const metadata = (await readJSON(
                root +
                  "/ogc-tiles/collections/" +
                  encodeURIComponent(entry.id) +
                  "/tilejson.json",
                abort.signal,
              )) as { vector_layers?: { id: string }[] };
              if (abort.signal.aborted) break;
              if (
                !metadata.vector_layers?.some((layer) => layer.id === entry.id)
              ) {
                throw new Error(
                  `Vector tile metadata is missing the published layer “${entry.id}”.`,
                );
              }
              target.addSource(id, {
                type: "vector",
                tiles: [
                  root +
                    "/ogc-tiles/collections/" +
                    encodeURIComponent(entry.id) +
                    "/tiles/WebMercatorQuad/{z}/{y}/{x}",
                ],
              });
            }
            sources.push(id);
            const sourceLayer =
              entry.source === "tiles" ? { "source-layer": entry.id } : {};
            target.addLayer({
              id: id + "-fill",
              source: id,
              ...sourceLayer,
              type: "fill",
              filter: ["==", ["geometry-type"], "Polygon"],
              paint: {
                "fill-color": "#4f46e5",
                "fill-opacity": 0.35 * entry.opacity,
              },
            });
            layers.push(id + "-fill");
            target.addLayer({
              id: id + "-line",
              source: id,
              ...sourceLayer,
              type: "line",
              filter: ["!=", ["geometry-type"], "Point"],
              paint: {
                "line-color": "#3730a3",
                "line-opacity": entry.opacity,
                "line-width": 2,
              },
            });
            layers.push(id + "-line");
            target.addLayer({
              id: id + "-point",
              source: id,
              ...sourceLayer,
              type: "circle",
              filter: ["==", ["geometry-type"], "Point"],
              paint: {
                "circle-color": "#4f46e5",
                "circle-opacity": entry.opacity,
                "circle-radius": 5,
              },
            });
            layers.push(id + "-point");
          }
        } catch (error) {
          if (!abort.signal.aborted)
            setErrors((current) => [
              ...current,
              entry.id +
                ": " +
                (error instanceof Error ? error.message : "Preview failed"),
            ]);
        }
      }
    })();
    return () => {
      abort.abort();
      [...layers].reverse().forEach((id) => {
        if (target.getLayer(id)) target.removeLayer(id);
      });
      sources.forEach((id) => {
        if (target.getSource(id)) target.removeSource(id);
      });
    };
  }, [
    ready,
    entries,
    root,
    revision,
    basemap,
    config?.basemap_url,
    availabilityKey,
    resourceKinds,
    unavailable,
    catalog.isLoading,
    collections.isLoading,
  ]);

  const renderResource = (resource: Collection) => {
    const entry = entries.find((item) => item.id === resource.id);
    const bounds = boundsFor(resource);
    const kind = resource.kind ?? "feature";
    const feature = kind === "feature";
    function change(patch: Partial<Entry>) {
      select(
        entries.map((item) =>
          item.id === resource.id ? { ...item, ...patch } : item,
        ),
      );
    }
    return (
      <div
        key={resource.id}
        className={`space-y-2 rounded-md border px-2.5 py-2 ${entry ? "bg-muted/40" : ""}`}
      >
        <label className="flex items-center justify-between gap-3 text-sm font-medium">
          <span className="min-w-0">
            <span className="block truncate">
              {resource.title || resource.id}
            </span>
            {((resource.title && resource.title !== resource.id) ||
              (groupBy === "store" && kind !== "feature")) && (
              <span className="block truncate font-mono text-xs font-normal text-muted-foreground">
                {resource.title && resource.title !== resource.id
                  ? resource.id
                  : ""}
                {groupBy === "store" && kind === "coverage"
                  ? `${resource.title && resource.title !== resource.id ? " · " : ""}coverage`
                  : ""}
              </span>
            )}
          </span>
          <Switch
            aria-label={"Show " + resource.id}
            checked={Boolean(entry)}
            disabled={
              !entry &&
              (Boolean(resource.unavailable) ||
                (!active.wms &&
                  (!feature || (!active.geojson && !active.tiles))))
            }
            onCheckedChange={(checked) => {
              select(
                checked
                  ? [
                      ...entries,
                      {
                        id: resource.id,
                        source: defaultSource(kind, active),
                        style: "",
                        opacity: 1,
                      },
                    ]
                  : entries.filter((item) => item.id !== resource.id),
              );
              if (checked && bounds) {
                framed.current = true;
                map.current?.fitBounds(bounds, {
                  padding: fitPadding(),
                  maxZoom: 15,
                });
              }
            }}
          />
        </label>
        {resource.unavailable && !entry && (
          <p className="text-sm text-muted-foreground">
            {resource.unavailable}{" "}
            <Link
              className="underline"
              to={`/workspaces/${encodeURIComponent(workspace)}/${resource.managePath}`}
            >
              Manage {resource.managePath?.replaceAll("-", " ")}
            </Link>
          </p>
        )}
        {!resource.unavailable && !feature && !active.wms && (
          <p className="text-sm text-muted-foreground">
            Enable WMS in{" "}
            <Link
              to={`/workspaces/${encodeURIComponent(workspace)}/settings`}
              className="underline"
            >
              Service settings
            </Link>{" "}
            to preview this {resource.kind}.
          </p>
        )}
        {entry && !resource.unavailable && (
          <>
            <Button
              variant="outline"
              size="sm"
              disabled={!bounds}
              onClick={() => {
                if (bounds)
                  map.current?.fitBounds(bounds, {
                    padding: fitPadding(),
                    maxZoom: 15,
                    duration: 0,
                  });
              }}
            >
              Fit {resource.id}
            </Button>
            {!bounds && (
              <p className="text-xs text-muted-foreground">
                No geographic extent is available yet.
                {feature
                  ? " Fit becomes available once the GeoJSON sample loads."
                  : " Pan or zoom to locate this publication."}
              </p>
            )}
            <label className="block text-sm">
              Source
              <NativeSelect
                aria-label={"Source for " + resource.id}
                className="mt-1 w-full"
                value={entry.source}
                onChange={(e) => change({ source: e.target.value as Source })}
              >
                {sourcesFor(kind).map((source) => (
                  <option
                    key={source}
                    value={source}
                    disabled={!active[source]}
                  >
                    {sourceLabels[source]}
                  </option>
                ))}
              </NativeSelect>
            </label>
            <label className="block text-sm">
              Opacity
              <input
                className="mt-1 w-full"
                aria-label={"Opacity for " + resource.id}
                type="range"
                min="0"
                max="1"
                step=".05"
                value={entry.opacity}
                onChange={(e) => change({ opacity: Number(e.target.value) })}
              />
            </label>
            {entry.source === "wms" && (
              <>
                <label className="block text-sm">
                  Style
                  <NativeSelect
                    aria-label={"Style for " + resource.id}
                    className="mt-1 w-full"
                    value={entry.style}
                    onChange={(e) => change({ style: e.target.value })}
                  >
                    <option value="">Default style</option>
                    {catalog.choices.styles.map((style) => (
                      <option key={style.value} value={style.value}>
                        {style.label}
                      </option>
                    ))}
                  </NativeSelect>
                </label>
                {kind === "group" && (
                  <p className="text-xs text-muted-foreground">
                    Legends of the group's member layers:
                  </p>
                )}
                {legendTargets(entry.id, entry.style, catalog.groups).map(
                  (target) => (
                    <div
                      key={`${target.layer}:${target.style}`}
                      className="space-y-1"
                    >
                      {kind === "group" && (
                        <p className="truncate font-mono text-xs">
                          {target.layer}
                        </p>
                      )}
                      <WMSPreview
                        url={
                          root +
                          "/wms?SERVICE=WMS&VERSION=1.3.0&REQUEST=GetLegendGraphic&FORMAT=image/png&LAYER=" +
                          encodeURIComponent(target.layer) +
                          "&STYLE=" +
                          encodeURIComponent(target.style)
                        }
                        revision={revision}
                        alt={"Legend for " + target.layer}
                      />
                    </div>
                  ),
                )}
              </>
            )}
            {samples[entry.id] !== undefined && (
              <p className="text-xs text-muted-foreground">
                {samples[entry.id]} sampled features (maximum 1,000; not the
                full dataset).
              </p>
            )}
          </>
        )}
      </div>
    );
  };
  return (
    <Page
      title="Preview"
      description="Inspect publication sources. WMS uses the saved server style; vectors use diagnostic colors. GeoJSON is limited to the first 1,000 features."
      action={
        <div className="flex flex-wrap gap-2">
          <Button variant="outline" asChild>
            <Link
              to={`/workspaces/${encodeURIComponent(workspace)}/endpoints${entries[0] && resourceKinds.get(entries[0].id) === "feature" ? `?${new URLSearchParams({ layer: entries[0].id })}` : ""}`}
            >
              Connect a client
            </Link>
          </Button>
          <Button
            variant="outline"
            onClick={() => {
              void catalog.retry();
              if (active.geojson) void collections.refetch();
              void summary.refetch();
              setRevision((value) => value + 1);
            }}
          >
            Refresh preview
          </Button>
          <Button
            variant="outline"
            onClick={() => {
              const bounds = map.current?.getBounds();
              const next = new URLSearchParams(params);
              if (bounds)
                next.set(
                  "bbox",
                  [
                    bounds.getWest(),
                    bounds.getSouth(),
                    bounds.getEast(),
                    bounds.getNorth(),
                  ].join(","),
                );
              setParams(next, { replace: true });
              const url = new URL(location.href);
              url.search = next.toString();
              void navigator.clipboard.writeText(url.href).then(
                () => toast.success("Preview link copied"),
                () =>
                  toast.error(
                    "Could not copy. The address bar now contains the view link.",
                  ),
              );
            }}
          >
            Copy view link
          </Button>
        </div>
      }
    >
      <QueryError
        error={collections.error || catalog.error || summary.error}
        retry={() => {
          void collections.refetch();
          void catalog.retry();
          void summary.refetch();
        }}
      />
      {Array.from(unavailable, ([id, problem]) => (
        <div
          key={id}
          role="alert"
          className="mb-3 rounded-lg border p-3 text-sm"
        >
          <p>
            {id}: {problem.message}
          </p>
          <Link
            className="underline"
            to={`/workspaces/${encodeURIComponent(workspace)}/${problem.path}`}
          >
            Manage {problem.path.replaceAll("-", " ")}
          </Link>
          <Button
            className="ml-2"
            variant="ghost"
            size="sm"
            onClick={() => select(entries.filter((entry) => entry.id !== id))}
          >
            Remove from preview
          </Button>
        </div>
      ))}
      {errors.length > 0 && (
        <div
          role="alert"
          className="mb-3 rounded-lg border border-destructive p-3 text-sm"
        >
          {errors.map((error, i) => (
            <p key={i}>{error}</p>
          ))}
          <Link
            className="underline"
            to={"/workspaces/" + encodeURIComponent(workspace) + "/settings"}
          >
            Service settings
          </Link>
        </div>
      )}
      <div className="grid gap-4 lg:grid-cols-[300px_minmax(0,1fr)]">
        <aside
          id="preview-layer-controls"
          aria-label="Layer controls"
          tabIndex={0}
          className={`${layersOpen ? "block" : "hidden"} order-2 max-h-[45dvh] space-y-3 overflow-auto focus-visible:outline-2 focus-visible:outline-ring lg:order-1 lg:block lg:max-h-[75vh]`}
        >
          {collections.isLoading && <p role="status">Loading layers…</p>}
          {!collections.isLoading &&
            !collections.error &&
            !catalog.error &&
            resources.length === 0 && (
              <p>No publications available. Add a layer to begin.</p>
            )}
          {config?.basemap_url ? (
            <Button
              variant="outline"
              onClick={() => setBasemap((value) => !value)}
            >
              {basemap ? "Hide basemap" : "Show basemap"}
            </Button>
          ) : (
            <div className="space-y-2">
              <Button
                variant="outline"
                size="sm"
                disabled
                aria-describedby="basemap-unavailable"
              >
                Show basemap
              </Button>
              <details
                id="basemap-unavailable"
                className="min-w-0 text-xs text-muted-foreground [&_code]:break-all"
                open={me?.super_admin || undefined}
              >
                <summary className="cursor-pointer">
                  No basemap configured
                </summary>
                <p className="mt-1 rounded-md border border-warning/40 bg-warning/10 p-2 break-words text-foreground">
                  The map has no background. To add one, set{" "}
                  <code>Website.BasemapUrl</code> (or{" "}
                  <code>NEOSRV_WEBSITE_BASEMAPURL</code>) to an XYZ tile URL
                  such as{" "}
                  <code>https://tiles.example.com/{"{z}/{x}/{y}"}.png</code> and
                  restart the server.
                </p>
              </details>
            </div>
          )}
          {shownResources.length > 0 && (
            <section aria-label="On the map" className="space-y-1.5">
              <div className="flex items-center justify-between gap-2 px-1">
                <h2 className="text-xs font-medium text-muted-foreground">
                  On the map ({shownResources.length})
                </h2>
                <Button
                  variant="ghost"
                  size="sm"
                  disabled={!shownBounds}
                  onClick={() => {
                    if (shownBounds)
                      map.current?.fitBounds(shownBounds, {
                        padding: fitPadding(),
                        maxZoom: 15,
                        duration: 0,
                      });
                  }}
                >
                  Fit all
                </Button>
              </div>
              {shownResources.map(renderResource)}
            </section>
          )}
          {resources.length > 0 && (
            <div className="space-y-2 border-t pt-3">
              <Input
                type="search"
                aria-label="Find a publication"
                placeholder="Find a publication…"
                value={layerQuery}
                onChange={(event) => setLayerQuery(event.target.value)}
              />
              <label className="flex items-center justify-end gap-2 text-sm">
                Group by
                <NativeSelect
                  aria-label="Group publications by"
                  className="h-8 w-auto"
                  value={groupBy}
                  onChange={(event) =>
                    setGroupBy(event.target.value as PreviewGrouping)
                  }
                >
                  <option value="store">Store</option>
                  <option value="kind">Type</option>
                </NativeSelect>
              </label>
            </div>
          )}
          {resources.length > shownResources.length &&
            groupedResources.length === 0 && (
              <p role="status" className="text-sm text-muted-foreground">
                No publications match this search.
              </p>
            )}
          {groupedResources.map((group) => (
            <section
              key={group.key}
              aria-label={group.label}
              className="space-y-1.5"
            >
              <h2 className="truncate px-1 text-xs font-medium text-muted-foreground">
                {group.label} ({group.items.length})
              </h2>
              {group.items.map(renderResource)}
            </section>
          ))}
        </aside>
        <div className="order-1 min-w-0 space-y-2 lg:order-2">
          <div className="relative">
            {ready &&
              entries.length === 0 &&
              !catalog.isLoading &&
              !collections.isLoading && (
                <div className="pointer-events-none absolute inset-0 z-10 grid place-items-center p-4">
                  <div className="pointer-events-auto max-w-sm rounded-lg border bg-background/95 p-4 text-center shadow-sm">
                    <p className="font-medium">No layers shown</p>
                    <p className="mt-1 text-sm text-muted-foreground">
                      {resources.length
                        ? "Choose a publication in Layer controls to draw it on the map."
                        : "Publish a layer or coverage first, then preview it here."}
                    </p>
                    {resources.length > 0 ? (
                      <Button
                        className="mt-3 lg:hidden"
                        variant="outline"
                        aria-controls="preview-layer-controls"
                        onClick={() => {
                          setLayersOpen(true);
                          document
                            .getElementById("preview-layer-controls")
                            ?.focus();
                        }}
                      >
                        Choose layers
                      </Button>
                    ) : (
                      <Button className="mt-3" variant="outline" asChild>
                        <Link
                          to={`/workspaces/${encodeURIComponent(workspace)}/layers`}
                        >
                          Go to Layers
                        </Link>
                      </Button>
                    )}
                  </div>
                </div>
              )}
            <div
              ref={container}
              // Matches the map background beyond the world's latitude range.
              className="h-[55dvh] min-h-72 max-h-[650px] rounded-lg border bg-[#e5e7eb] lg:h-[650px] dark:bg-[#1f2329]"
              aria-label="Workspace map preview"
              aria-busy={!ready || moving}
              role="region"
              onPointerDownCapture={() => {
                interacted.current = true;
              }}
              onWheelCapture={() => {
                interacted.current = true;
              }}
              onKeyDownCapture={() => {
                interacted.current = true;
              }}
            />
          </div>
          <Button
            variant="outline"
            className="min-h-11 w-full lg:hidden"
            aria-controls="preview-layer-controls"
            aria-expanded={layersOpen}
            onClick={() => setLayersOpen(!layersOpen)}
          >
            {layersOpen ? "Hide" : "Show"} layer controls ({entries.length}{" "}
            selected)
          </Button>
          <p className="font-mono text-xs">
            {position ||
              (entries.length
                ? "Preview selected publications. Use Fit when an extent is available."
                : "Select a publication to begin.")}
          </p>
          <p className="text-xs text-muted-foreground">
            Click a rendered feature to inspect attributes.
          </p>
          {properties !== undefined && (
            <pre className="max-h-64 overflow-auto rounded-lg border p-3 text-xs">
              {JSON.stringify(properties, null, 2)}
            </pre>
          )}
        </div>
      </div>
    </Page>
  );
}
