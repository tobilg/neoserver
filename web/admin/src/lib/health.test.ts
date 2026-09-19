import { afterEach, describe, expect, it, vi } from "vitest";
import { fetchReadiness, fetchLiveness } from "./health";
vi.mock("@/api/client", () => ({ basePath: "" }));
afterEach(() => vi.unstubAllGlobals());
describe("server health contract", () => {
  it.each([
    [200, "ready"],
    [503, "not_ready"],
  ])("accepts HTTP %s %s", async (status, value) => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(
        new Response(
          JSON.stringify({
            status: value,
            checks: { catalog: status === 200 ? "ok" : "failed" },
            durations_ms: { catalog: 1 },
          }),
          { status: Number(status) },
        ),
      ),
    );
    expect((await fetchReadiness()).status).toBe(value);
  });
  it.each([
    [200, "healthy"],
    [503, "ready"],
    [500, "not_ready"],
  ])("rejects mismatched HTTP %s %s", async (status, value) => {
    vi.stubGlobal(
      "fetch",
      vi
        .fn()
        .mockResolvedValue(
          new Response(
            JSON.stringify({ status: value, checks: {}, durations_ms: {} }),
            { status: Number(status) },
          ),
        ),
    );
    await expect(fetchReadiness()).rejects.toThrow();
  });
  it("does not turn a network failure into ready", async () => {
    vi.stubGlobal("fetch", vi.fn().mockRejectedValue(new Error("offline")));
    await expect(fetchReadiness()).rejects.toThrow("offline");
  });
  it("accepts actual liveness payload", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(new Response('{"status":"ok"}')),
    );
    expect(await fetchLiveness()).toEqual({ status: "ok" });
  });
});
