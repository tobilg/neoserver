import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiError } from "./client";

const { toastError } = vi.hoisted(() => ({ toastError: vi.fn() }));
vi.mock("sonner", () => ({ toast: { error: toastError, success: vi.fn() } }));

const { queryClient } = await import("./queryClient");

/** Drives the shared client's MutationCache the way a failing screen would. */
async function failWith(
  error: unknown,
  meta?: { suppressErrorToast?: boolean },
) {
  await queryClient
    .getMutationCache()
    .build(queryClient, {
      mutationFn: () => Promise.reject(error),
      retry: false,
      meta,
    })
    .execute(undefined)
    .catch(() => {});
}

afterEach(() => toastError.mockReset());

describe("global mutation error reporting", () => {
  it("toasts an ApiError with its message and detail", async () => {
    await failWith(new ApiError(422, "Invalid store", "database is required"));
    expect(toastError).toHaveBeenCalledWith(
      "Invalid store: database is required",
    );
  });

  it("toasts a plain error message", async () => {
    await failWith(new Error("network down"));
    expect(toastError).toHaveBeenCalledWith("network down");
  });

  it("stays silent when the screen already reports the failure", async () => {
    // Forms that map server errors onto fields opt out, otherwise the operator
    // sees the same message twice.
    await failWith(new ApiError(422, "Invalid store"), {
      suppressErrorToast: true,
    });
    expect(toastError).not.toHaveBeenCalled();
  });

  it("stays silent on 401, which redirects to login instead", async () => {
    await failWith(new ApiError(401, "authentication required"));
    expect(toastError).not.toHaveBeenCalled();
  });

  it("falls back to a generic message for a thrown non-Error", async () => {
    await failWith("boom");
    expect(toastError).toHaveBeenCalledWith("The request failed.");
  });
});
