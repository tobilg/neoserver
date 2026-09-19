import type { ConsoleConfig, WorkspaceSummary } from "@/api/generated/models";

type ServerKey = keyof NonNullable<ConsoleConfig["services"]>;
type ProtocolKey = keyof WorkspaceSummary["protocols"];

export const SERVICES: {
  key: ProtocolKey;
  server: ServerKey;
  label: string;
  env?: string;
}[] = [
  { key: "ogcapi", server: "ogcapi", label: "OGC API – Features" },
  {
    key: "ogc_tiles",
    server: "tiles",
    label: "OGC API – Tiles",
    env: "NEOSRV_TILES_ENABLED",
  },
  { key: "wms", server: "wms", label: "WMS", env: "NEOSRV_WMS_ENABLED" },
  { key: "wfs", server: "wfs", label: "WFS", env: "NEOSRV_WFS_ENABLED" },
  { key: "wcs", server: "wcs", label: "WCS", env: "NEOSRV_WCS_ENABLED" },
  { key: "wmts", server: "wmts", label: "WMTS", env: "NEOSRV_WMTS_ENABLED" },
];

export type ServiceStateValue =
  "on" | "on_no_publications" | "off_workspace" | "off_server";

/** One vocabulary for service availability, from server to workspace. */
export function serviceState(
  serverEnabled: boolean,
  workspaceEnabled: boolean,
  publications: number,
): ServiceStateValue {
  if (!serverEnabled) return "off_server";
  if (!workspaceEnabled) return "off_workspace";
  return publications ? "on" : "on_no_publications";
}
