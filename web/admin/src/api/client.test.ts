import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiError, apiFetch, csrfToken, runtimeBase } from "./client";

afterEach(() => {
  vi.restoreAllMocks();
  document.cookie = "neosrv_csrf=; Max-Age=0; Path=/";
});

describe("apiFetch", () => {
  it("derives a runtime prefix from the admin URL", () => {
    history.replaceState({}, "", "/geo/admin/workspaces/demo/layers");
    expect(runtimeBase()).toBe("/geo");
  });

  it("attaches the session CSRF token to unsafe requests", async () => {
    document.cookie = "neosrv_csrf=bound-token; Path=/";
    expect(csrfToken()).toBe("bound-token");
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(
      new Response("{}", {
        status: 200,
        headers: { "Content-Type": "application/json" },
      }),
    );
    await apiFetch("/resource", { method: "POST", body: "{}" });
    const options = fetchMock.mock.calls[0][1];
    expect(new Headers(options?.headers).get("X-CSRF-Token")).toBe(
      "bound-token",
    );
    expect(options?.credentials).toBe("same-origin");
  });

  it("maps structured API errors without losing their detail", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(
      new Response(
        JSON.stringify({
          code: "invalid",
          message: "Store rejected connection",
          detail: "password failed",
        }),
        { status: 422 },
      ),
    );
    await expect(apiFetch("/resource")).rejects.toMatchObject({
      status: 422,
      message: "Store rejected connection",
      detail: "password failed",
      code: "invalid",
    } satisfies Partial<ApiError>);
  });

  it("announces a rejected session exactly once", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(
      new Response(JSON.stringify({ message: "Session expired" }), {
        status: 401,
      }),
    );
    const listener = vi.fn();
    window.addEventListener("neoserver:unauthorized", listener);
    await expect(apiFetch("/resource")).rejects.toMatchObject({ status: 401 });
    expect(listener).toHaveBeenCalledOnce();
    window.removeEventListener("neoserver:unauthorized", listener);
  });
});
