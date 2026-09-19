import { cleanup, render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter, Route, Routes } from "react-router";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { AppShell } from "./AppShell";

const { useAuthMock } = vi.hoisted(() => ({ useAuthMock: vi.fn() }));

vi.mock("@/auth/auth-context", () => ({ useAuth: useAuthMock }));

const workspaces = [
  { id: "1", name: "demo", role: "admin", console_access: true },
  { id: "2", name: "public", role: "viewer", console_access: false },
];

function renderShell(me: Record<string, unknown>) {
  useAuthMock.mockReturnValue({
    me,
    loading: false,
    config: {
      version: "test",
      auth: { enabled: true, oidc: { enabled: false } },
      services: {},
      features: {},
    },
    logout: vi.fn(),
  });
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={client}>
      <MemoryRouter initialEntries={["/workspaces/demo"]}>
        <Routes>
          <Route path="/workspaces/:ws" element={<AppShell />} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  vi.stubGlobal(
    "fetch",
    vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({
        status: "ready",
        checks: { catalog: "ok" },
        durations_ms: { catalog: 1 },
      }),
    }),
  );
  vi.stubGlobal(
    "matchMedia",
    vi.fn().mockReturnValue({
      matches: false,
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
    }),
  );
});

afterEach(() => {
  cleanup();
  useAuthMock.mockReset();
  vi.unstubAllGlobals();
});

describe("AppShell capability gating", () => {
  it("hides every server-scope entry from a workspace admin", () => {
    renderShell({
      subject: "ada",
      super_admin: false,
      console_access: true,
      workspaces,
      capabilities: {},
    });

    // Decision D1/D6: these are super-admin only and must be absent, not
    // merely disabled, so a workspace admin never reaches a 403.
    for (const label of [
      "Workspaces",
      "Roles & policies",
      "Tile matrix sets",
      "Audit",
    ]) {
      expect(
        screen.queryByRole("link", { name: label }),
        `${label} should be hidden`,
      ).not.toBeInTheDocument();
    }
  });

  it("shows server-scope entries to a super admin", () => {
    renderShell({
      subject: "root",
      super_admin: true,
      console_access: true,
      workspaces,
      capabilities: { manage_workspaces: true },
    });
    expect(screen.getByText("Server administration")).toBeInTheDocument();
    expect(
      screen
        .getAllByRole("link")
        .filter((link) => link.getAttribute("aria-current") === "page"),
    ).toHaveLength(1);
  });

  it("lists only workspaces the principal can administer", () => {
    renderShell({
      subject: "ada",
      super_admin: false,
      console_access: true,
      workspaces,
      capabilities: {},
    });
    // `public` is reachable over OGC but not administrable, so it must not
    // appear as a switcher option that would dead-end in a 403.
    expect(screen.queryByText(/public · viewer/)).not.toBeInTheDocument();
  });
});
