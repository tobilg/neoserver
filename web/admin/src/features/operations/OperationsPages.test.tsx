import { cleanup, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { OperationsPage } from "./OperationsPages";
import { renderScreen } from "@/test/render";

const { apiFetchMock } = vi.hoisted(() => ({ apiFetchMock: vi.fn() }));
vi.mock("@/api/client", () => ({
  apiFetch: apiFetchMock,
  basePath: "",
  jsonBody: (value: unknown) => JSON.stringify(value),
}));
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));
vi.mock("@/auth/auth-context", () => ({
  useAuth: () => ({
    config: { version: "test", server_started_at: "2026-08-06T09:00:00Z" },
  }),
}));

function renderPage() {
  return renderScreen(<OperationsPage />, {
    path: "/operations",
    entry: "/operations",
  });
}

beforeEach(() => {
  apiFetchMock.mockReset();
  apiFetchMock.mockImplementation(async (path: string) =>
    path.startsWith("/deletions")
      ? { deletions: [] }
      : path.includes("integrity")
        ? { healthy: true, issues: [] }
        : {},
  );
  vi.stubGlobal(
    "fetch",
    vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({
        status: "ready",
        checks: { catalog: "ok", audit: "ok" },
        durations_ms: { catalog: 1.2, audit: 0.4 },
      }),
    }),
  );
});

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

describe("OperationsPage", () => {
  it("names each readiness check the server reports", async () => {
    renderPage();
    expect(await screen.findByText("catalog")).toBeInTheDocument();
    expect(screen.getByText("audit")).toBeInTheDocument();
  });

  it("states the single-active topology so operators do not expect failover", async () => {
    renderPage();
    const matches = await screen.findAllByText(/single-active/i);
    expect(matches.length).toBeGreaterThan(0);
    // The Topology field states it explicitly, not just the page description.
    expect(
      matches.some((node) =>
        node.textContent?.includes("one process owns catalog"),
      ),
    ).toBe(true);
  });
});
