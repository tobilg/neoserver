import { cleanup, fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { QueryClient } from "@tanstack/react-query";
import { ImportDetailPage } from "./ImportDetailPage";
import { renderScreen } from "@/test/render";
import { getListServicesQueryKey } from "@/api/generated/services/services";
import { getListLayersQueryKey } from "@/api/generated/layers/layers";
import { getGetWorkspaceSummaryQueryKey } from "@/api/generated/workspaces/workspaces";

const { apiFetchMock } = vi.hoisted(() => ({ apiFetchMock: vi.fn() }));
vi.mock("@/api/client", () => ({
  apiFetch: apiFetchMock,
  apiBase: "/api/v1",
  basePath: "",
  csrfToken: () => "csrf",
  jsonBody: (value: unknown) => JSON.stringify(value),
}));
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));

const job = {
  id: "imp-1",
  name: "roads.gpkg",
  status: "ready_to_publish",
  phase: "preview",
  source_kind: "upload",
  workspace_id: "demo",
  created_at: "2026-01-01T00:00:00Z",
  updated_at: "2026-01-01T00:00:00Z",
  processed_layers: 1,
  total_layers: 2,
};

function renderPage() {
  return renderScreen(<ImportDetailPage />, {
    path: "/workspaces/:ws/imports/:importId",
    entry: "/workspaces/demo/imports/imp-1",
  });
}

function serve(current: Record<string, unknown>) {
  apiFetchMock.mockImplementation(async (path: string) => {
    if (path.endsWith("/imports/imp-1")) return current;
    if (path.endsWith("/history")) return { events: [] };
    return {};
  });
}

beforeEach(() => {
  apiFetchMock.mockReset();
  serve(job);
});

afterEach(() => {
  cleanup();
  vi.useRealTimers();
  vi.restoreAllMocks();
});

describe("ImportDetailPage", () => {
  it("shows the job as a full page with a way back to the list", async () => {
    renderPage();
    expect(
      await screen.findByRole("heading", { name: "roads.gpkg" }),
    ).toBeInTheDocument();
    expect(screen.getByRole("link", { name: /all imports/i })).toHaveAttribute(
      "href",
      "/workspaces/demo/imports",
    );
    expect(
      screen.getByRole("button", { name: "Publish dataset" }),
    ).toBeEnabled();
  });

  for (const status of ["published", "rolled_back"]) {
    it(`refreshes catalog caches after asynchronous ${status} completion`, async () => {
      const invalidate = vi.spyOn(QueryClient.prototype, "invalidateQueries");
      serve({ ...job, status, service_id: "imported-store" });
      renderPage();
      await waitFor(() =>
        expect(invalidate).toHaveBeenCalledWith({
          queryKey: getListServicesQueryKey("demo"),
        }),
      );
      expect(invalidate).toHaveBeenCalledWith({
        queryKey: getListLayersQueryKey("demo", "imported-store"),
      });
      expect(invalidate).toHaveBeenCalledWith({
        queryKey: getGetWorkspaceSummaryQueryKey("demo"),
      });
      expect(invalidate).toHaveBeenCalledWith({
        queryKey: ["/workspaces/demo/ogc/collections"],
      });
    });
  }

  it("marks every phase done once published", async () => {
    serve({ ...job, status: "published", phase: "publish" });
    renderPage();
    expect(
      await screen.findByLabelText("Import phases complete"),
    ).toBeInTheDocument();
  });

  for (const status of ["ready_to_publish", "failed"]) {
    it(`allows correcting a plan when ${status}`, async () => {
      serve({
        ...job,
        status,
        phase: status === "failed" ? "transform" : "preview",
        discovery: { layers: [] },
        plan: { service_name: "dataset", layers: [] },
      });
      renderPage();
      if (status === "ready_to_publish")
        fireEvent.click(
          await screen.findByRole("button", { name: "Revise plan" }),
        );
      expect(await screen.findByLabelText("Target store name")).toHaveValue(
        "dataset",
      );
      if (status === "ready_to_publish")
        expect(
          screen.getByRole("button", { name: "Publish dataset" }),
        ).toBeDisabled();
    });
  }
});
