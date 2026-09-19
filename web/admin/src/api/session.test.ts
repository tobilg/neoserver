import { afterEach, expect, it, vi } from "vitest";
import { apiFetch } from "./client";
import { invalidateSessionRequests } from "./session";
afterEach(() => vi.unstubAllGlobals());
it("rejects an earlier session's late successful response", async () => {
  let respond!: (value: Response) => void;
  vi.stubGlobal(
    "fetch",
    vi.fn(
      () =>
        new Promise<Response>((resolve) => {
          respond = resolve;
        }),
    ),
  );
  const request = apiFetch("/workspaces/demo/services");
  invalidateSessionRequests();
  respond(
    new Response(JSON.stringify({ private: "old catalog" }), { status: 200 }),
  );
  await expect(request).rejects.toMatchObject({ name: "AbortError" });
});
it("does not expire the current login for an earlier session's late 401", async () => {
  let respond!: (value: Response) => void;
  vi.stubGlobal(
    "fetch",
    vi.fn(
      () =>
        new Promise<Response>((resolve) => {
          respond = resolve;
        }),
    ),
  );
  const listener = vi.fn();
  window.addEventListener("neoserver:unauthorized", listener);
  const request = apiFetch("/workspaces/demo/services");
  invalidateSessionRequests();
  respond(new Response("{}", { status: 401 }));
  await expect(request).rejects.toMatchObject({ name: "AbortError" });
  expect(listener).not.toHaveBeenCalled();
  window.removeEventListener("neoserver:unauthorized", listener);
});
