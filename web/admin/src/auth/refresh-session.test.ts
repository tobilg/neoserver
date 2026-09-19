import { afterEach, expect, it, vi } from "vitest";
import { ApiError } from "@/api/client";
import { coordinatedRefresh } from "./refresh-session";

const mocks = vi.hoisted(() => ({ refresh: vi.fn(), me: vi.fn() }));
vi.mock("@/api/generated/authentication/authentication", () => ({
  refreshSession: mocks.refresh,
  getAuthMe: mocks.me,
}));
afterEach(() => {
  vi.clearAllMocks();
  vi.unstubAllGlobals();
});

it("shares an in-flight refresh and takes the cross-tab lock", async () => {
  let finish!: (value: object) => void;
  mocks.refresh.mockReturnValue(
    new Promise((resolve) => {
      finish = resolve;
    }),
  );
  const lock = vi.fn((_name: string, callback: () => unknown) => callback());
  vi.stubGlobal("navigator", { locks: { request: lock } });
  const first = coordinatedRefresh();
  const second = coordinatedRefresh();
  expect(first).toBe(second);
  expect(lock).toHaveBeenCalledTimes(1);
  expect(mocks.refresh).toHaveBeenCalledTimes(1);
  finish({ principal: "operator" });
  await expect(first).resolves.toEqual({ principal: "operator" });
});

it("recovers a rotation conflict by loading current identity", async () => {
  vi.stubGlobal("navigator", {});
  mocks.refresh.mockRejectedValue(new ApiError(409, "Session refreshed"));
  mocks.me.mockResolvedValue({ principal: "operator" });
  await expect(coordinatedRefresh()).resolves.toEqual({
    principal: "operator",
  });
  expect(mocks.me).toHaveBeenCalledTimes(1);
});

it("does not conceal expired or revoked sessions", async () => {
  vi.stubGlobal("navigator", {});
  mocks.refresh.mockRejectedValue(new ApiError(401, "Session expired"));
  await expect(coordinatedRefresh()).rejects.toMatchObject({ status: 401 });
  expect(mocks.me).not.toHaveBeenCalled();
});

it("does not treat a storage outage as a successful refresh race", async () => {
  vi.stubGlobal("navigator", {});
  mocks.refresh.mockRejectedValue(new ApiError(503, "Session refresh failed"));
  await expect(coordinatedRefresh()).rejects.toMatchObject({ status: 503 });
  expect(mocks.me).not.toHaveBeenCalled();
});
