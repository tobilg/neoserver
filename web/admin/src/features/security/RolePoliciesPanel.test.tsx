import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { createMemoryRouter, RouterProvider } from "react-router";
import { afterEach, expect, it, vi } from "vitest";
import { RolesPage } from "./SecurityPages";

const { apiFetch } = vi.hoisted(() => ({ apiFetch: vi.fn() }));
vi.mock("@/api/client", () => ({
  apiFetch,
  jsonBody: (value: unknown) => JSON.stringify(value),
}));
afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

it("submits workspace IDs and matching operation actions, and marks legacy scopes", async () => {
  apiFetch.mockImplementation(async (path: string, options?: RequestInit) => {
    if (options?.method === "POST") return undefined;
    if (path === "/roles")
      return { roles: [{ id: "custom", name: "Custom", is_system: false }] };
    if (path === "/workspaces")
      return { workspaces: [{ id: "ws-id", name: "Test" }] };
    if (path === "/roles/custom/policies")
      return {
        policies: [["custom", "old-name", "operation:wfs:GETFEATURE", "read"]],
      };
    return {};
  });
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  render(
    <QueryClientProvider client={client}>
      <RouterProvider
        router={createMemoryRouter([{ path: "/", element: <RolesPage /> }])}
      />
    </QueryClientProvider>,
  );
  await screen.findByText(/Unmatched workspace scope/);
  expect(screen.getByRole("button", { name: "Remove policy" })).toBeEnabled();
  fireEvent.change(screen.getByRole("combobox", { name: /^Workspace/ }), {
    target: { value: "ws-id" },
  });
  fireEvent.change(screen.getByRole("combobox", { name: /^Service/ }), {
    target: { value: "wfs" },
  });
  fireEvent.change(screen.getByRole("combobox", { name: /^Operation/ }), {
    target: { value: "DROPSTOREDQUERY" },
  });
  expect(screen.getByRole("combobox", { name: /^Action/ })).toHaveValue(
    "manage",
  );
  fireEvent.click(screen.getByRole("button", { name: "Add policy" }));
  await waitFor(() =>
    expect(apiFetch).toHaveBeenCalledWith(
      "/roles/custom/policies",
      expect.objectContaining({
        method: "POST",
        body: JSON.stringify({
          workspace: "ws-id",
          service: "wfs",
          operation: "DROPSTOREDQUERY",
          action: "manage",
        }),
      }),
    ),
  );
});
