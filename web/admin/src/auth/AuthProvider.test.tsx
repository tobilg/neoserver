import {
  act,
  render,
  screen,
  waitFor,
  cleanup,
  fireEvent,
} from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, describe, expect, it, vi } from "vitest";
import { AuthProvider } from "./AuthProvider";
import { useAuth } from "./auth-context";
import type { AuthMe } from "@/api/generated/models";

const { apiFetchMock, cookie } = vi.hoisted(() => ({
  apiFetchMock: vi.fn(),
  cookie: { csrf: "csrf" },
}));

vi.mock("@/api/client", () => ({
  apiFetch: apiFetchMock,
  csrfToken: () => cookie.csrf,
  jsonBody: (value: unknown) => JSON.stringify(value),
}));

const identity: AuthMe = {
  authenticated: true,
  principal: "operator",
  session_id: "s1",
  subject: "operator",
  auth_method: "jwt",
  super_admin: true,
  console_access: true,
  workspaces: [],
  capabilities: {},
};

function Probe() {
  const { me, loading, login } = useAuth();
  if (loading) return <p>loading</p>;
  return (
    <>
      <p>{me ? `signed in as ${me.subject}` : "signed out"}</p>
      <button onClick={() => void login({ method: "token", token: "test" })}>
        Login
      </button>
    </>
  );
}

function renderProvider(
  client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  }),
) {
  return render(
    <QueryClientProvider client={client}>
      <AuthProvider>
        <Probe />
      </AuthProvider>
    </QueryClientProvider>,
  );
}

afterEach(() => {
  cleanup();
  apiFetchMock.mockReset();
});

describe("AuthProvider", () => {
  it("evicts private catalog data on expiry and before a different login", async () => {
    apiFetchMock.mockImplementation((path: string) =>
      Promise.resolve(
        path === "/auth/me"
          ? identity
          : path === "/auth/login"
            ? { ...identity, subject: "second" }
            : {},
      ),
    );
    const client = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    renderProvider(client);
    await screen.findByText("signed in as operator");
    client.setQueryData(["/workspaces/demo/services"], {
      secret: "first catalog",
    });
    act(() => {
      window.dispatchEvent(new CustomEvent("neoserver:unauthorized"));
    });
    await screen.findByText("signed out");
    expect(client.getQueryData(["/workspaces/demo/services"])).toBeUndefined();
    fireEvent.click(screen.getByText("Login"));
    await screen.findByText("signed in as second");
    expect(client.getQueryData(["/workspaces/demo/services"])).toBeUndefined();
  });
  it("exposes the resolved identity", async () => {
    apiFetchMock.mockImplementation((path: string) =>
      path === "/auth/me" ? Promise.resolve(identity) : Promise.resolve({}),
    );
    renderProvider();
    await screen.findByText("signed in as operator");
  });

  it("drops the session when a request reports 401", async () => {
    apiFetchMock.mockImplementation((path: string) =>
      path === "/auth/me" ? Promise.resolve(identity) : Promise.resolve({}),
    );
    renderProvider();
    await screen.findByText("signed in as operator");

    // apiFetch fires this when the server rejects a request as unauthorised —
    // an expired session, or a revoked API key. The console must stop
    // presenting the user as signed in, otherwise every subsequent action
    // fails against a UI that still looks fully functional.
    act(() => {
      window.dispatchEvent(new CustomEvent("neoserver:unauthorized"));
    });

    await waitFor(() => {
      expect(screen.getByText("signed out")).toBeInTheDocument();
    });
  });

  it("does not probe for a session without the session's CSRF cookie", async () => {
    cookie.csrf = "";
    try {
      apiFetchMock.mockImplementation(async (url: string) => {
        if (url.endsWith("/console/config")) return { auth: { enabled: true } };
        throw new Error(`unexpected request ${url}`);
      });
      renderProvider();
      expect(await screen.findByText("signed out")).toBeVisible();
      expect(
        apiFetchMock.mock.calls.some(([url]) =>
          String(url).includes("/auth/me"),
        ),
      ).toBe(false);
    } finally {
      cookie.csrf = "csrf";
    }
  });
});
