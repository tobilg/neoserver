import { describe, expect, it } from "vitest";
import {
  formatBytes,
  formatDuration,
  formatList,
  formatMilliseconds,
  formatRelative,
  plural,
} from "./format";
import { booleanLabel, statusLabel, statusTone } from "./display";

describe("formatters", () => {
  it("formats sizes in binary units", () => {
    expect(formatBytes(451)).toBe("451 B");
    expect(formatBytes(20973)).toBe("20 KiB");
    expect(formatBytes(1536)).toBe("1.5 KiB");
    expect(formatBytes(1024 ** 3)).toBe("1 GiB");
  });
  it("formats durations, latencies, relative times and counts", () => {
    expect(formatDuration(42)).toBe("42 s");
    expect(formatDuration(3610)).toBe("1 h 0 min");
    expect(formatDuration(90061)).toBe("1 d 1 h");
    expect(formatMilliseconds(0.02)).toBe("<0.1 ms");
    expect(formatMilliseconds(12.4)).toBe("12 ms");
    const now = Date.parse("2026-09-17T12:00:00Z");
    expect(formatRelative("2026-09-17T11:59:50Z", now)).toBe("just now");
    expect(formatRelative("2026-09-16T18:00:00Z", now)).toBe("18 h ago");
    expect(formatRelative("2026-09-20T12:00:00Z", now)).toBe("in 3 d");
    expect(plural(1, "import")).toBe("1 import");
    expect(plural(2, "import")).toBe("2 imports");
  });
});

describe("display vocabulary", () => {
  it("names states consistently", () => {
    expect(statusLabel("enabled")).toBe("On");
    expect(statusLabel("private")).toBe("Restricted");
    expect(statusLabel("awaiting_plan")).toBe("Awaiting plan");
    expect(statusLabel("3 running")).toBe("3 running");
    expect(statusTone("expired")).toBe("warning");
    expect(statusTone("2 running")).toBe("running");
    expect(booleanLabel("revoked", false)).toBe("Active");
    expect(booleanLabel("dry_run", true)).toBe("Yes");
  });
});

describe("formatList", () => {
  it("joins plain values and named objects", () => {
    expect(formatList(["admin", "viewer"])).toBe("admin, viewer");
    expect(
      formatList([
        { resource: "cite.Lakes", opacity: 0.5 },
        { resource: "public.places" },
      ]),
    ).toBe("cite.Lakes, public.places");
    expect(formatList([{ name: "one" }, { public_id: "two" }])).toBe(
      "one, two",
    );
    expect(formatList([])).toBe("");
  });

  it("falls back when an item has no readable name", () => {
    expect(formatList([{ opacity: 1 }])).toBeUndefined();
    expect(formatList([["nested"]])).toBeUndefined();
  });
});
