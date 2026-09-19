import type { CreateStyleBodyFormat } from "@/api/generated/models";

export const styleFormats: { value: CreateStyleBodyFormat; label: string }[] = [
  { value: "sld_1.0.0", label: "SLD 1.0" },
  { value: "sld_1.1.0", label: "SLD 1.1" },
  { value: "se_1.1.0", label: "SE 1.1" },
  { value: "css", label: "CSS" },
  { value: "ysld", label: "YSLD" },
  { value: "mapbox", label: "Mapbox JSON" },
];
export const styleGeometries = ["point", "line", "polygon", "raster"] as const;
export type StyleGeometry = (typeof styleGeometries)[number];

export function styleTemplate(
  format: CreateStyleBodyFormat,
  geometry: StyleGeometry,
): string {
  const properties = {
    point: { mark: "circle", "mark-size": 10, "mark-color": "#5fa8cc" },
    line: { stroke: "#5fa8cc", "stroke-width": 2 },
    polygon: { fill: "#5fa8cc", "fill-opacity": 0.7 },
    raster: { "raster-opacity": 1 },
  }[geometry];
  if (format === "css")
    return `* {\n${Object.entries(properties)
      .map(([key, value]) => `  ${key}: ${value};`)
      .join("\n")}\n}\n`;
  if (format === "mapbox") {
    const layer = {
      point: {
        type: "circle",
        paint: { "circle-radius": 5, "circle-color": "#5fa8cc" },
      },
      line: {
        type: "line",
        paint: { "line-color": "#5fa8cc", "line-width": 2 },
      },
      polygon: {
        type: "fill",
        paint: { "fill-color": "#5fa8cc", "fill-opacity": 0.7 },
      },
      raster: { type: "raster", paint: { "raster-opacity": 1 } },
    }[geometry];
    return (
      JSON.stringify(
        {
          version: 8,
          name: "New style",
          sources: {},
          layers: [{ id: "layer", ...layer }],
        },
        null,
        2,
      ) + "\n"
    );
  }
  if (format === "ysld") {
    const symbolizer = {
      point:
        "mark:\n              well-known-name: circle\n              size: 10\n              color: '#5fa8cc'",
      line: "line:\n              color: '#5fa8cc'\n              width: 2",
      polygon:
        "fill:\n              color: '#5fa8cc'\n              opacity: 0.7",
      raster: "raster:\n              opacity: 1",
    }[geometry];
    return `name: New style\nfeature-styles:\n  - rules:\n      - symbolizers:\n          - ${symbolizer}\n`;
  }
  const symbolizer = {
    point:
      '<PointSymbolizer><Graphic><Mark><WellKnownName>circle</WellKnownName><Fill><CssParameter name="fill">#5fa8cc</CssParameter></Fill></Mark><Size>10</Size></Graphic></PointSymbolizer>',
    line: '<LineSymbolizer><Stroke><CssParameter name="stroke">#5fa8cc</CssParameter><CssParameter name="stroke-width">2</CssParameter></Stroke></LineSymbolizer>',
    polygon:
      '<PolygonSymbolizer><Fill><CssParameter name="fill">#5fa8cc</CssParameter><CssParameter name="fill-opacity">0.7</CssParameter></Fill></PolygonSymbolizer>',
    raster: "<RasterSymbolizer><Opacity>1</Opacity></RasterSymbolizer>",
  }[geometry];
  const version = format === "sld_1.0.0" ? "1.0.0" : "1.1.0";
  const content =
    version === "1.0.0"
      ? symbolizer
      : symbolizer.replaceAll("CssParameter", "SvgParameter");
  return `<?xml version="1.0" encoding="UTF-8"?>\n<StyledLayerDescriptor version="${version}" xmlns="http://www.opengis.net/sld" xmlns:se="http://www.opengis.net/se">\n  <NamedLayer><Name>layer</Name><UserStyle><Title>New style</Title>\n    <FeatureTypeStyle${version === "1.1.0" ? ' xmlns="http://www.opengis.net/se"' : ""}><Rule>\n      ${content}\n    </Rule></FeatureTypeStyle>\n  </UserStyle></NamedLayer>\n</StyledLayerDescriptor>\n`;
}
