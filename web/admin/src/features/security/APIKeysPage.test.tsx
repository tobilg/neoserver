import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter, Route, Routes } from "react-router";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { APIKeysPage } from "./APIKeysPage";

const { apiFetchMock } = vi.hoisted(() => ({ apiFetchMock: vi.fn() }));
vi.mock("@/api/client", () => ({
  apiFetch: apiFetchMock,
  jsonBody: (value: unknown) => JSON.stringify(value),
}));
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));

const key = {
  id: "key-1",
  name: "ci-pipeline",
  key_prefix: "nsk_abc",
  owner_name: "CI",
  role_id: "viewer",
  revoked: false,
  created_at: "2026-01-01T00:00:00Z",
};

function renderPage() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={client}>
      <MemoryRouter initialEntries={["/workspaces/demo/api-keys"]}>
        <Routes>
          <Route path="/workspaces/:ws/api-keys" element={<APIKeysPage />} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  apiFetchMock.mockReset();
  apiFetchMock.mockImplementation(async (path: string) => {
    if (path.endsWith("/apikeys")) return { api_keys: [key] };
    if (path.endsWith("/roles"))
      return { roles: [{ id: "viewer", name: "Viewer" }] };
    return {};
  });
});

afterEach(cleanup);

describe("APIKeysPage", () => {
  it("lists keys by prefix rather than secret", async () => {
    renderPage();
    expect(await screen.findByText("ci-pipeline")).toBeInTheDocument();
    expect(screen.getByText("nsk_abc")).toBeInTheDocument();
  });

  it("shows the secret exactly once after creation", async () => {
    apiFetchMock.mockImplementation(
      async (path: string, options?: RequestInit) => {
        if (path.endsWith("/apikeys") && options?.method === "POST") {
          return { ...key, id: "key-2", key: "nsk_the_only_time" };
        }
        if (path.endsWith("/apikeys")) return { api_keys: [key] };
        if (path.endsWith("/roles"))
          return { roles: [{ id: "viewer", name: "Viewer" }] };
        return {};
      },
    );
    renderPage();

    fireEvent.click(await screen.findByRole("button", { name: /create key/i }));
    fireEvent.change(await screen.findByLabelText(/key name/i), {
      target: { value: "new-key" },
    });
    fireEvent.click(
      screen.getAllByRole("button", { name: /create key/i }).at(-1)!,
    );

    // The secret is unrecoverable afterwards, so it must be presented plainly.
    expect(await screen.findByText("nsk_the_only_time")).toBeInTheDocument();

    // Dismissing it is gated on the operator saying they have stored it.
    const done = screen.getByRole("button", { name: "Done" });
    expect(done).toBeDisabled();
    fireEvent.click(screen.getByLabelText(/i have stored this key/i));
    expect(done).toBeEnabled();
    fireEvent.click(done);
    await waitFor(() =>
      expect(screen.queryByText("nsk_the_only_time")).not.toBeInTheDocument(),
    );
  });

  it("only revokes a key after confirmation", async () => {
    renderPage();
    await screen.findByText("ci-pipeline");
    fireEvent.click(
      screen.getByRole("button", { name: /revoke ci-pipeline/i }),
    );
    expect(
      apiFetchMock.mock.calls.some(
        ([, options]) => options?.method === "DELETE",
      ),
    ).toBe(false);
    expect(screen.getByRole("button", { name: "Cancel" })).toHaveFocus();
    fireEvent.click(screen.getByRole("button", { name: "Revoke key" }));
    await waitFor(() =>
      expect(apiFetchMock).toHaveBeenCalledWith(
        expect.stringContaining("/apikeys/key-1"),
        expect.objectContaining({ method: "DELETE" }),
      ),
    );
  });
});
