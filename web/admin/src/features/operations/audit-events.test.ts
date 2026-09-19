import { describe, expect, it } from "vitest";
import type { AuditEvent } from "@/api/generated/models";
import {
  describeAuditEvent,
  groupAuditEvents,
  statusClass,
} from "./audit-events";

const event = (id: string, patch: Partial<AuditEvent> = {}): AuditEvent => ({
  id,
  timestamp: `2026-09-17T12:00:${id.padStart(2, "0")}Z`,
  action: "security",
  method: "GET",
  path: "/workspaces/review/wms?request=GetMap",
  status: 401,
  duration_ms: 1,
  security_event: true,
  operation: "GETMAP",
  workspace: "review",
  ...patch,
});

describe("groupAuditEvents", () => {
  it("collapses consecutive identical requests only", () => {
    const rows = groupAuditEvents([
      event("30"),
      event("29", { path: "/workspaces/review/wms?request=GetMap&bbox=1" }),
      event("28"),
      event("27", { status: 200 }),
      event("26"),
    ]);
    expect(rows.map((row) => [row.id, row.count, row.earliest])).toEqual([
      ["30", 3, "2026-09-17T12:00:28Z"],
      ["27", 1, "2026-09-17T12:00:27Z"],
      ["26", 1, "2026-09-17T12:00:26Z"],
    ]);
  });
});

describe("describeAuditEvent", () => {
  it("reads like a sentence", () => {
    expect(describeAuditEvent(event("1"))).toEqual({
      who: "anonymous",
      what: "GetMap",
    });
    expect(
      describeAuditEvent(
        event("2", {
          principal: "operator",
          operation: "login",
          auth_method: "password",
        }),
      ),
    ).toEqual({ who: "operator", what: "signed in (password)" });
    expect(
      describeAuditEvent(event("3", { operation: "publish", principal: "ci" })),
    ).toEqual({ who: "ci", what: "publish" });
  });

  it("classifies statuses", () => {
    expect([200, 302, 404, 503].map(statusClass)).toEqual([
      "ok",
      "ok",
      "client",
      "server",
    ]);
  });
});
