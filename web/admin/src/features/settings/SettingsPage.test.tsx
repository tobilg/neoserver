import { cleanup, fireEvent, screen, waitFor } from "@testing-library/react";
import { renderScreen } from "@/test/render";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { SettingsPage } from "./SettingsPage";

const { apiFetchMock } = vi.hoisted(() => ({
  apiFetchMock: vi.fn(),
}));

vi.mock("@/api/client", () => ({
  apiFetch: apiFetchMock,
  jsonBody: (value: unknown) => JSON.stringify(value),
}));

vi.mock("@/auth/auth-context", () => ({
  useAuth: () => ({
    config: {
      services: {
        ogcapi: true,
        wms: true,
        wfs: true,
        wcs: false,
        wmts: true,
        tiles: true,
      },
    },
  }),
}));

function renderPage() {
  return renderScreen(<SettingsPage />, {
    path: "/workspaces/:ws/settings",
    entry: "/workspaces/Test/settings",
  });
}

describe("SettingsPage", () => {
  afterEach(cleanup);

  beforeEach(() => {
    apiFetchMock.mockReset();
    apiFetchMock.mockImplementation(
      async (endpoint: string, options?: RequestInit) => {
        if (options?.method === "PUT") {
          return JSON.parse(String(options.body)) as Record<string, unknown>;
        }
        return {
          enabled: false,
          title: endpoint.endsWith("/wms") ? "Test maps" : "",
        };
      },
    );
  });

  it("enables a service with a switch while preserving its other settings", async () => {
    renderPage();

    const wms = await screen.findByRole("switch", {
      name: "Enable Web Map Service (WMS)",
    });
    expect(wms).not.toBeChecked();
    fireEvent.click(wms);

    await waitFor(() =>
      expect(apiFetchMock).toHaveBeenCalledWith(
        "/workspaces/Test/settings/wms",
        expect.objectContaining({
          method: "PUT",
          body: JSON.stringify({
            enabled: true,
            title: "Test maps",
          }),
        }),
      ),
    );
    expect(wms).toBeChecked();
  });

  it("explains and locks services disabled at server level", async () => {
    renderPage();

    const wcs = await screen.findByRole("switch", {
      name: "Enable Web Coverage Service (WCS)",
    });
    expect(wcs).toBeDisabled();
    expect(screen.getByText("NEOSRV_WCS_ENABLED=true")).toBeInTheDocument();
  });
});
