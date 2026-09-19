// Keep the UI's supported workflows explicit; unknown source types fail closed.
// Mirrors DataSource / SQLViewSource / CoverageSource implementations in Go.
const capabilities: Record<
  string,
  { features: boolean; sql: boolean; coverages: boolean }
> = {
  postgis: { features: true, sql: true, coverages: true },
  duckdb: { features: true, sql: true, coverages: false },
  geoparquet: { features: true, sql: false, coverages: false },
  vectorfile: { features: true, sql: false, coverages: false },
  rasterfile: { features: false, sql: false, coverages: true },
  raster_mosaic: { features: false, sql: false, coverages: true },
};
export function datasourceCapabilities(type: string) {
  return (
    capabilities[type] ?? { features: false, sql: false, coverages: false }
  );
}
