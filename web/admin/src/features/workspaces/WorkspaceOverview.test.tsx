import { cleanup, fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { WorkspaceOverview } from "./WorkspaceOverview";
import { renderScreen } from "@/test/render";

const { apiFetchMock } = vi.hoisted(() => ({ apiFetchMock: vi.fn() }));
vi.mock("@/api/client", () => ({
  apiFetch: apiFetchMock,
  jsonBody: (value: unknown) => JSON.stringify(value),
}));
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));
vi.mock("@/auth/auth-context", () => ({
  useAuth: () => ({ config: { features: { imports: true } } }),
}));
beforeEach(() => {
  let workspace = {
    id: "ws-1",
    name: "demo",
    description: "Original description",
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
  };
  apiFetchMock.mockReset();
  apiFetchMock.mockImplementation(
    async (path: string, options?: RequestInit) => {
      if (options?.method === "PUT") {
        workspace = { ...workspace, ...JSON.parse(String(options.body)) };
        return workspace;
      }
      if (path.endsWith("/summary"))
        return { protocols: {}, active_jobs: {}, counts: {} };
      if (path.endsWith("/auth/me")) return {};
      if (path.endsWith("/services")) return { services: [] };
      return workspace;
    },
  );
});
afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});
function renderPage() {
  return renderScreen(<WorkspaceOverview />, {
    path: "/workspaces/:ws",
    entry: "/workspaces/demo",
  });
}

it("renders and reopens the authoritative description after saving and clearing", async () => {
  renderPage();
  await screen.findByText("Original description");
  fireEvent.click(screen.getByRole("button", { name: "Edit workspace" }));
  fireEvent.change(screen.getByLabelText("Description"), {
    target: { value: "Updated description" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Save workspace" }));
  await screen.findByText("Updated description");
  await waitFor(() =>
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument(),
  );
  fireEvent.click(screen.getByRole("button", { name: "Edit workspace" }));
  expect(screen.getByLabelText("Description")).toHaveValue(
    "Updated description",
  );
  fireEvent.change(screen.getByLabelText("Description"), {
    target: { value: "" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Save workspace" }));
  await waitFor(() =>
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument(),
  );
  expect(screen.queryByText("Updated description")).not.toBeInTheDocument();
});

it("guards closing a dirty workspace dialog and retains rejected-close drafts", async () => {
  const confirm = vi.spyOn(window, "confirm").mockReturnValue(false);
  renderPage();
  await screen.findByText("Original description");
  fireEvent.click(screen.getByRole("button", { name: "Edit workspace" }));
  fireEvent.change(screen.getByLabelText("Description"), {
    target: { value: "Unsaved draft" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Close" }));
  await waitFor(() =>
    expect(confirm).toHaveBeenCalledWith(
      expect.stringContaining("Discard unsaved workspace changes?"),
    ),
  );
  expect(screen.getByLabelText("Description")).toHaveValue("Unsaved draft");
  confirm.mockReturnValue(true);
  fireEvent.click(screen.getByRole("button", { name: "Close" }));
  await waitFor(() =>
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument(),
  );
});
