import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { ImportsPage } from "./ImportsPage";
import { renderScreen } from "@/test/render";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { createMemoryRouter, RouterProvider } from "react-router";

const { apiFetchMock } = vi.hoisted(() => ({ apiFetchMock: vi.fn() }));
vi.mock("@/api/client", () => ({
  apiFetch: apiFetchMock,
  apiBase: "/api/v1",
  basePath: "",
  csrfToken: () => "csrf",
  jsonBody: (value: unknown) => JSON.stringify(value),
}));
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));
vi.mock("@/auth/auth-context", () => ({
  useAuth: () => ({ config: { limits: { upload_bytes: 1024 * 1024 } } }),
}));

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
  return renderScreen(<ImportsPage />, {
    path: "/workspaces/:ws/imports",
    entry: "/workspaces/demo/imports",
  });
}

beforeEach(() => {
  apiFetchMock.mockReset();
  apiFetchMock.mockImplementation(async (path: string) => {
    if (/\/imports(\?|$)/.test(path)) return { imports: [job] };
    return {};
  });
});

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});

describe("ImportsPage", () => {
  it("links each job to its own detail page, keeping list filters", async () => {
    renderScreen(<ImportsPage />, {
      path: "/workspaces/:ws/imports",
      entry: "/workspaces/demo/imports?status=actionable",
    });
    const link = await screen.findByRole("link", { name: "roads.gpkg" });
    expect(link).toHaveAttribute("href", "/workspaces/demo/imports/imp-1");
  });

  it("redirects older ?import= links to the detail route", async () => {
    const router = createMemoryRouter(
      [
        { path: "/workspaces/:ws/imports", element: <ImportsPage /> },
        {
          path: "/workspaces/:ws/imports/:importId",
          element: <p>detail page</p>,
        },
      ],
      { initialEntries: ["/workspaces/demo/imports?import=imp-1"] },
    );
    render(
      <QueryClientProvider client={new QueryClient()}>
        <RouterProvider router={router} />
      </QueryClientProvider>,
    );
    expect(await screen.findByText("detail page")).toBeInTheDocument();
    expect(router.state.location.pathname).toBe(
      "/workspaces/demo/imports/imp-1",
    );
  });

  it("lists jobs with their lifecycle status", async () => {
    renderPage();
    expect(await screen.findByText("roads.gpkg")).toBeInTheDocument();
  });

  it("offers an acquisition action", async () => {
    renderPage();
    expect(
      await screen.findByRole("button", { name: /new import/i }),
    ).toBeInTheDocument();
  });

  it("does not poll a job list with nothing in flight", async () => {
    renderPage();
    await screen.findByText("roads.gpkg");
    const before = apiFetchMock.mock.calls.length;
    await new Promise((resolve) => setTimeout(resolve, 60));
    // ready_to_publish is a terminal-ish state; polling would be wasted work.
    expect(apiFetchMock.mock.calls.length).toBe(before);
  });
});

describe("upload dialog", () => {
  function openUpload() {
    renderScreen(<ImportsPage />, {
      path: "/workspaces/:ws/imports",
      entry: "/workspaces/demo/imports?new=upload",
    });
  }

  it("fills the import name from the chosen file", async () => {
    openUpload();
    const input = await screen.findByLabelText("Dataset or archive");
    fireEvent.change(input, {
      target: { files: [new File(["{}"], "My Roads.geojson")] },
    });
    expect(screen.getByLabelText("Import name")).toHaveValue("My_Roads");
    expect(screen.getByText("My Roads.geojson")).toBeInTheDocument();
  });

  it("keeps a name the user typed and explains missing files inline", async () => {
    openUpload();
    fireEvent.change(await screen.findByLabelText("Import name"), {
      target: { value: "chosen" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Upload and inspect" }));
    expect(screen.getByRole("alert")).toHaveTextContent(
      "Choose a dataset or archive to upload.",
    );
    fireEvent.change(screen.getByLabelText("Dataset or archive"), {
      target: { files: [new File(["{}"], "other.gpkg")] },
    });
    expect(screen.getByLabelText("Import name")).toHaveValue("chosen");
  });

  it("rejects files above the server limit before uploading", async () => {
    openUpload();
    const big = new File(["x"], "huge.gpkg");
    Object.defineProperty(big, "size", { value: 3 * 1024 * 1024 });
    fireEvent.change(await screen.findByLabelText("Dataset or archive"), {
      target: { files: [big] },
    });
    fireEvent.click(screen.getByRole("button", { name: "Upload and inspect" }));
    expect(screen.getByRole("alert")).toHaveTextContent(
      "This file is 3 MiB; the server accepts up to 1 MiB.",
    );
  });
});
