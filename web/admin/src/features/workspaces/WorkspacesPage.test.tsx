import { cleanup, screen } from "@testing-library/react";
import { renderScreen } from "@/test/render";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { WorkspacesPage } from "./WorkspacesPage";

const { apiFetchMock } = vi.hoisted(() => ({ apiFetchMock: vi.fn() }));
vi.mock("@/api/client", () => ({
  apiFetch: apiFetchMock,
  jsonBody: (value: unknown) => JSON.stringify(value),
}));
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));

function renderPage() {
  return renderScreen(<WorkspacesPage />, {
    path: "/workspaces",
    entry: "/workspaces",
  });
}

beforeEach(() => {
  apiFetchMock.mockReset();
  apiFetchMock.mockResolvedValue({
    workspaces: [
      {
        id: "1",
        name: "demo",
        description: "Sample catalog",
        created_at: "2026-01-01T00:00:00Z",
        updated_at: "2026-01-01T00:00:00Z",
        counts: { services: 2, layers: 7 },
      },
    ],
  });
});

afterEach(cleanup);

describe("WorkspacesPage", () => {
  it("lists workspaces with their descriptions", async () => {
    renderPage();
    expect(await screen.findByText("demo")).toBeInTheDocument();
    expect(screen.getByText("Sample catalog")).toBeInTheDocument();
  });

  it("renders the per-workspace counts the server returns", async () => {
    // These were undocumented in the OpenAPI schema until the generated types
    // exposed the gap; the panel is empty if the field goes missing again.
    renderPage();
    await screen.findByText("demo");
    expect(screen.getByText("7")).toBeInTheDocument();
    expect(screen.getByText("Layers")).toBeInTheDocument();
  });
});
