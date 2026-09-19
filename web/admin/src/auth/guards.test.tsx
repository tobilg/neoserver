import { cleanup, render, screen } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router";
import { afterEach, describe, expect, it, vi } from "vitest";
import {
  AuthGuard,
  ConsoleGuard,
  SuperAdminGuard,
  WorkspaceGuard,
} from "./guards";

const { useAuthMock } = vi.hoisted(() => ({ useAuthMock: vi.fn() }));

vi.mock("./auth-context", () => ({ useAuth: useAuthMock }));

type Auth = {
  me?: {
    console_access?: boolean;
    super_admin?: boolean;
    workspaces?: Array<{ id: string; name: string; console_access: boolean }>;
  } | null;
  loading?: boolean;
};

function renderGuard(
  Guard: React.ComponentType,
  auth: Auth,
  entry = "/workspaces/demo/layers",
) {
  useAuthMock.mockReturnValue({ loading: false, ...auth });
  return render(
    <MemoryRouter initialEntries={[entry]}>
      <Routes>
        <Route element={<Guard />}>
          <Route
            path="/workspaces/:ws/layers"
            element={<p>protected content</p>}
          />
        </Route>
        {/* Redirect targets must sit outside the guard, or the guard re-runs
            against them and the redirect loops. */}
        <Route path="/" element={<p>home</p>} />
        <Route path="/login" element={<p>login page</p>} />
        <Route path="/no-access" element={<p>no access page</p>} />
      </Routes>
    </MemoryRouter>,
  );
}

afterEach(() => {
  cleanup();
  useAuthMock.mockReset();
});

describe("AuthGuard", () => {
  it("renders a skeleton while the session is resolving", () => {
    renderGuard(AuthGuard, { loading: true, me: null });
    expect(screen.queryByText("protected content")).not.toBeInTheDocument();
    expect(screen.queryByText("login page")).not.toBeInTheDocument();
  });

  it("redirects an unauthenticated visitor to login", () => {
    renderGuard(AuthGuard, { me: null });
    expect(screen.getByText("login page")).toBeInTheDocument();
  });

  it("preserves the attempted route so login can return to it", () => {
    useAuthMock.mockReturnValue({ loading: false, me: null });
    render(
      <MemoryRouter initialEntries={["/workspaces/demo/layers?layers_q=roads"]}>
        <Routes>
          <Route element={<AuthGuard />}>
            <Route
              path="/workspaces/:ws/layers"
              element={<p>protected content</p>}
            />
          </Route>
          <Route
            path="/login"
            element={
              <p>
                next=
                {new URLSearchParams(window.location.search).get("next") ??
                  "captured"}
              </p>
            }
          />
        </Routes>
      </MemoryRouter>,
    );
    expect(screen.getByText(/next=/)).toBeInTheDocument();
  });

  it("renders the route once a session exists", () => {
    renderGuard(AuthGuard, { me: { console_access: true } });
    expect(screen.getByText("protected content")).toBeInTheDocument();
  });
});

describe("ConsoleGuard", () => {
  it("sends a principal without console access to the no-access page", () => {
    renderGuard(ConsoleGuard, { me: { console_access: false } });
    expect(screen.getByText("no access page")).toBeInTheDocument();
  });

  it("admits a principal with console access", () => {
    renderGuard(ConsoleGuard, { me: { console_access: true } });
    expect(screen.getByText("protected content")).toBeInTheDocument();
  });
});

describe("SuperAdminGuard", () => {
  it("redirects a workspace admin away from server-scope routes", () => {
    renderGuard(SuperAdminGuard, {
      me: { console_access: true, super_admin: false },
    });
    expect(screen.getByText("home")).toBeInTheDocument();
    expect(screen.queryByText("protected content")).not.toBeInTheDocument();
  });

  it("admits a super admin", () => {
    renderGuard(SuperAdminGuard, {
      me: { console_access: true, super_admin: true },
    });
    expect(screen.getByText("protected content")).toBeInTheDocument();
  });
});

describe("WorkspaceGuard", () => {
  it("denies a viewer even when another workspace grants console access", () => {
    renderGuard(WorkspaceGuard, {
      me: {
        console_access: true,
        workspaces: [
          { id: "one", name: "demo", console_access: false },
          { id: "two", name: "other", console_access: true },
        ],
      },
    });
    expect(screen.getByText("no access page")).toBeInTheDocument();
  });
  it("accepts workspace IDs as well as names", () => {
    renderGuard(WorkspaceGuard, {
      me: { workspaces: [{ id: "demo", name: "Test", console_access: true }] },
    });
    expect(screen.getByText("protected content")).toBeInTheDocument();
  });
});
