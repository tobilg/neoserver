import type { AuditEvent } from "@/api/generated/models";

export interface AuditRow {
  id: string;
  event: AuditEvent;
  /** How many consecutive identical events this row stands for. */
  count: number;
  /** Timestamp of the oldest event in the run (the list is newest first). */
  earliest: string;
}

const signature = (event: AuditEvent) =>
  JSON.stringify([
    event.status,
    event.method,
    event.path.split("?")[0],
    event.operation ?? "",
    event.principal ?? "",
    event.workspace ?? "",
    event.credential_id ?? "",
  ]);

/** Collapse runs of identical requests so bursts read as one line. */
export function groupAuditEvents(events: AuditEvent[]): AuditRow[] {
  const rows: AuditRow[] = [];
  for (const event of events) {
    const last = rows.at(-1);
    if (last && signature(last.event) === signature(event)) {
      last.count += 1;
      last.earliest = event.timestamp;
    } else
      rows.push({ id: event.id, event, count: 1, earliest: event.timestamp });
  }
  return rows;
}

const protocolNames: Record<string, string> = {
  GETMAP: "GetMap",
  GETCAPABILITIES: "GetCapabilities",
  GETFEATUREINFO: "GetFeatureInfo",
  GETLEGENDGRAPHIC: "GetLegendGraphic",
  GETFEATURE: "GetFeature",
  GETPROPERTYVALUE: "GetPropertyValue",
  DESCRIBEFEATURETYPE: "DescribeFeatureType",
  GETCOVERAGE: "GetCoverage",
  DESCRIBECOVERAGE: "DescribeCoverage",
  GETTILE: "GetTile",
  TRANSACTION: "Transaction",
};

/** Human description: "operator · signed in (password)", "anonymous · GetMap". */
export function describeAuditEvent(event: AuditEvent) {
  const who = event.principal || "anonymous";
  const operation = event.operation || event.method;
  let what = operation;
  if (operation === "login")
    what = `signed in${event.auth_method ? ` (${event.auth_method})` : ""}`;
  else if (operation === "logout") what = "signed out";
  else if (operation === "refresh") what = "refreshed the session";
  else if (protocolNames[operation]) what = protocolNames[operation];
  return { who, what };
}

export type StatusClass = "ok" | "client" | "server";

export const statusClass = (status: number): StatusClass =>
  status >= 500 ? "server" : status >= 400 ? "client" : "ok";
