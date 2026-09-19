import { useEffect, useMemo, useRef, useState } from "react";
import { maplibre } from "@/lib/maplibre";
import type { FeatureCollection } from "geojson";
import "maplibre-gl/dist/maplibre-gl.css";
import { DataTable } from "@/components/DataTable";
import type { ColumnDef } from "@tanstack/react-table";
import { QueryError } from "@/components/QueryError";

type Feature = {
  id?: string | number;
  properties?: Record<string, unknown> | null;
  geometry?: unknown;
};
export function FeaturePreview({
  data,
  urlKey = "preview",
}: {
  data: { type: string; features: Feature[] };
  urlKey?: string;
}) {
  const container = useRef<HTMLDivElement>(null);
  const [error, setError] = useState<unknown>();
  const [status, setStatus] = useState<
    "loading" | "rendered" | "empty" | "unrenderable" | "failed"
  >("loading");
  const columns = useMemo<ColumnDef<Feature, unknown>[]>(
    () => [
      {
        id: "id",
        header: "Feature",
        accessorFn: (feature) => feature.id ?? "—",
      },
      ...Array.from(
        new Set(
          data.features.flatMap((feature) =>
            Object.keys(feature.properties ?? {}),
          ),
        ),
      ).map((name) => ({
        id: name,
        header: name,
        accessorFn: (feature: Feature) => {
          const value = feature.properties?.[name];
          return typeof value === "object"
            ? JSON.stringify(value)
            : String(value ?? "");
        },
      })),
    ],
    [data],
  );
  useEffect(() => {
    if (!container.current) return;
    setError(undefined);
    setStatus("loading");
    let active = true;
    let failed = false;
    function fail(error: unknown) {
      if (!active) return;
      failed = true;
      setError(error);
      setStatus("failed");
    }
    let map: InstanceType<typeof maplibre.Map>;
    try {
      map = new maplibre.Map({
        container: container.current,
        center: [0, 20],
        zoom: 1,
        style: {
          version: 8,
          sources: {},
          layers: [
            {
              id: "background",
              type: "background",
              paint: { "background-color": "#e5e7eb" },
            },
          ],
        },
      });
    } catch (error) {
      fail(error);
      return;
    }
    map.addControl(new maplibre.NavigationControl());
    map.on("error", (event) => fail(event.error));
    map.on("load", () => {
      if (!active) return;
      try {
        map.addSource("features", {
          type: "geojson",
          data: data as FeatureCollection,
        });
        map.addLayer({
          id: "polygons",
          source: "features",
          type: "fill",
          filter: ["==", ["geometry-type"], "Polygon"],
          paint: { "fill-color": "#4f46e5", "fill-opacity": 0.35 },
        });
        map.addLayer({
          id: "lines",
          source: "features",
          type: "line",
          filter: ["!=", ["geometry-type"], "Point"],
          paint: { "line-color": "#3730a3", "line-width": 2 },
        });
        map.addLayer({
          id: "points",
          source: "features",
          type: "circle",
          filter: ["==", ["geometry-type"], "Point"],
          paint: { "circle-color": "#4f46e5", "circle-radius": 5 },
        });
        const bounds = new maplibre.LngLatBounds();
        function coordinates(value: unknown) {
          if (!Array.isArray(value)) return;
          if (typeof value[0] === "number" && typeof value[1] === "number") {
            if (
              Number.isFinite(value[0]) &&
              Number.isFinite(value[1]) &&
              Math.abs(value[0]) <= 180 &&
              Math.abs(value[1]) <= 90
            )
              bounds.extend([value[0], value[1]]);
          } else value.forEach(coordinates);
        }
        function geometry(value: unknown) {
          if (!value || typeof value !== "object") return;
          if ("coordinates" in value) coordinates(value.coordinates);
          if ("geometries" in value && Array.isArray(value.geometries))
            value.geometries.forEach(geometry);
        }
        for (const feature of data.features) geometry(feature.geometry);
        if (!bounds.isEmpty())
          map.fitBounds(bounds, { padding: 30, maxZoom: 15, duration: 0 });
        map.once("idle", () => {
          if (!active || failed) return;
          try {
            setStatus(
              data.features.length === 0
                ? "empty"
                : map.queryRenderedFeatures({
                      layers: ["points", "lines", "polygons"],
                    }).length > 0
                  ? "rendered"
                  : "unrenderable",
            );
          } catch (error) {
            fail(error);
          }
        });
      } catch (error) {
        fail(error);
      }
    });
    return () => {
      active = false;
      map.remove();
    };
  }, [data]);
  return (
    <div className="space-y-3">
      <p className="text-sm text-muted-foreground">
        Bounded sample: {data.features.length} features. The map and table show
        the same features; this is not the complete dataset. No external basemap
        requests are made.
      </p>
      <QueryError error={error} context="Map preview could not be rendered" />
      <p role="status" className="text-xs text-muted-foreground">
        {
          {
            loading: "Rendering sample…",
            rendered: "Sample rendered on map",
            empty: "No features in this sample.",
            unrenderable:
              "Sample loaded, but no drawable geometry is visible. Null, empty or unsupported geometries remain available in the table.",
            failed:
              "Map preview failed. Sample attributes remain available in the table.",
          }[status]
        }
      </p>
      <div className="@container">
        <div className="grid gap-3 @4xl:grid-cols-2 @4xl:items-start">
          <div
            ref={container}
            className="h-80 rounded-lg border @4xl:h-[28rem]"
            aria-label="Feature preview map"
            role="region"
          />
          <DataTable
            data={data.features}
            columns={columns}
            urlKey={urlKey}
            emptyState="No features in this preview sample."
          />
        </div>
      </div>
    </div>
  );
}
