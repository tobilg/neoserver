import { cleanup, fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { ResponseCacheCard } from "./CachePage";
import { renderScreen } from "@/test/render";

const { apiFetchMock } = vi.hoisted(() => ({ apiFetchMock: vi.fn() }));
vi.mock("@/api/client", () => ({
  apiFetch: apiFetchMock,
  jsonBody: (value: unknown) => JSON.stringify(value),
}));
const { toastSuccess } = vi.hoisted(() => ({ toastSuccess: vi.fn() }));
vi.mock("sonner", () => ({
  toast: { success: toastSuccess, error: vi.fn() },
}));

function renderPage() {
  return renderScreen(<ResponseCacheCard workspace="demo" />, {
    path: "/workspaces/:ws/cache",
    entry: "/workspaces/demo/cache",
  });
}

beforeEach(() => {
  apiFetchMock.mockReset();
  apiFetchMock.mockResolvedValue({});
  toastSuccess.mockReset();
});

afterEach(cleanup);

describe("ResponseCacheCard", () => {
  it("clears the workspace cache through the API", async () => {
    renderPage();
    fireEvent.click(await screen.findByRole("button", { name: /clear/i }));
    await waitFor(() =>
      expect(apiFetchMock).toHaveBeenCalledWith(
        "/workspaces/demo/cache/clear",
        expect.objectContaining({ method: "POST" }),
      ),
    );
  });

  it("confirms the action, which otherwise leaves no visible trace", async () => {
    renderPage();
    fireEvent.click(await screen.findByRole("button", { name: /clear/i }));
    await waitFor(() =>
      expect(toastSuccess).toHaveBeenCalledWith("Response cache cleared"),
    );
  });
});
