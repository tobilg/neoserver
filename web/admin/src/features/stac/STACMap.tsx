import { useEffect, useRef, useState } from "react";
import { maplibre as maplibregl } from "@/lib/maplibre";
import "maplibre-gl/dist/maplibre-gl.css";
import type { FeatureCollection } from "geojson";
import type { STACDocument } from "@/api/generated/models";
import { Button } from "@/components/ui/button";
import { QueryError } from "@/components/QueryError";

export default function STACMap({
  items,
  onBounds,
}: {
  items: STACDocument[];
  onBounds: (bounds: number[]) => void;
}) {
  const container = useRef<HTMLDivElement>(null);
  const map = useRef<maplibregl.Map>(null);
  const [error, setError] = useState<unknown>();
  useEffect(() => {
    if (!container.current) return;
    try {
      const value = new maplibregl.Map({
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
              paint: { "background-color": "#e9eef2" },
            },
          ],
        },
      });
      map.current = value;
      value.addControl(new maplibregl.NavigationControl());
      value.on("error", (event) => setError(event.error));
      return () => {
        value.remove();
        map.current = null;
      };
    } catch (cause) {
      // MapLibre may throw while initializing WebGL; surface that failure so the coordinate fields remain usable.
      // eslint-disable-next-line react-hooks/set-state-in-effect
      setError(cause);
    }
  }, []);
  useEffect(() => {
    const value = map.current;
    if (!value) return;
    const update = () => {
      const data = {
        type: "FeatureCollection",
        features: items.filter((d) => d.geometry),
      } as unknown as FeatureCollection;
      const existing = value.getSource("items") as
        maplibregl.GeoJSONSource | undefined;
      if (existing) existing.setData(data);
      else {
        value.addSource("items", { type: "geojson", data });
        value.addLayer({
          id: "footprints",
          type: "fill",
          source: "items",
          paint: { "fill-color": "#0369a1", "fill-opacity": 0.25 },
          filter: ["==", "$type", "Polygon"],
        });
        value.addLayer({
          id: "edges",
          type: "line",
          source: "items",
          paint: { "line-color": "#075985", "line-width": 2 },
          filter: ["!=", "$type", "Point"],
        });
        value.addLayer({
          id: "points",
          type: "circle",
          source: "items",
          paint: { "circle-color": "#0369a1", "circle-radius": 5 },
          filter: ["==", "$type", "Point"],
        });
      }
    };
    if (value.isStyleLoaded()) update();
    else value.once("load", update);
    return () => {
      value.off("load", update);
    };
  }, [items]);
  return (
    <section className="space-y-2" aria-label="Item footprint map">
      <QueryError
        error={error}
        context="The map could not be displayed; use the bounding box fields to search"
      />
      <div ref={container} className="h-80 rounded-xl border" />
      <Button
        variant="outline"
        onClick={() => {
          const bounds = map.current?.getBounds();
          if (bounds)
            onBounds([
              Math.max(-180, bounds.getWest()),
              Math.max(-90, bounds.getSouth()),
              Math.min(180, bounds.getEast()),
              Math.min(90, bounds.getNorth()),
            ]);
        }}
      >
        Use map bounds in search
      </Button>
    </section>
  );
}
