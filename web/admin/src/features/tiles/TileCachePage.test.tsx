import { cleanup, render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter, Route, Routes } from "react-router";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { TileCachePage } from "./TileCachePage";

const { apiFetchMock } = vi.hoisted(() => ({ apiFetchMock: vi.fn() }));
vi.mock("@/api/client", () => ({
  apiFetch: apiFetchMock,
  jsonBody: (value: unknown) => JSON.stringify(value),
}));
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));

function renderPage() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={client}>
      <MemoryRouter initialEntries={["/workspaces/demo/tile-cache"]}>
        <Routes>
          <Route
            path="/workspaces/:ws/tile-cache"
            element={<TileCachePage />}
          />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  apiFetchMock.mockReset();
  apiFetchMock.mockImplementation(async (path: string) => {
    if (path.endsWith("/stats"))
      return {
        enabled: true,
        workspace: { size_bytes: 1024, quota_bytes: 4096, utilization: 0.25 },
        hits: 10,
        misses: 2,
      };
    if (path.endsWith("/jobs")) return { jobs: [] };
    return {};
  });
});

afterEach(cleanup);

describe("TileCachePage", () => {
  it("does not present denied statistics as zero usage", async () => {
    apiFetchMock.mockImplementation(async (path: string) => {
      if (path.endsWith("/stats"))
        throw Object.assign(new Error("Stats denied"), { status: 403 });
      return { jobs: [] };
    });
    renderPage();
    expect(await screen.findByRole("alert")).toHaveTextContent("Stats denied");
    expect(screen.queryByText("0 bytes")).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "New job" })).toBeDisabled();
  });
  it("renders live cache statistics", async () => {
    renderPage();
    expect(
      await screen.findByRole("heading", { name: /caching/i }),
    ).toBeInTheDocument();
  });

  it("explains an empty job list rather than spinning", async () => {
    renderPage();
    expect(await screen.findByText(/no tile jobs yet/i)).toBeInTheDocument();
  });

  it("does not poll when no job is active", async () => {
    renderPage();
    await screen.findByText(/no tile jobs yet/i);
    const before = apiFetchMock.mock.calls.length;
    await new Promise((resolve) => setTimeout(resolve, 60));
    expect(apiFetchMock.mock.calls.length).toBe(before);
  });
});
