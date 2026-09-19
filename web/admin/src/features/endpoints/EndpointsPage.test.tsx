import {
  cleanup,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter, Route, Routes } from "react-router";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { EndpointsPage } from "./EndpointsPage";

const { apiFetchMock } = vi.hoisted(() => ({ apiFetchMock: vi.fn() }));

vi.mock("@/api/client", () => ({ apiFetch: apiFetchMock }));

vi.mock("@/auth/auth-context", () => ({
  useAuth: () => ({
    config: {
      url_base: "https://maps.example.test",
      base_path: "/geo",
      services: {
        ogcapi: true,
        wms: true,
        wfs: true,
        wcs: false,
        wmts: true,
        tiles: true,
      },
    },
    me: { api_key_id: "nsk_test_key" },
  }),
}));

function renderPage() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <MemoryRouter initialEntries={["/workspaces/Test/endpoints"]}>
      <QueryClientProvider client={client}>
        <Routes>
          <Route path="/workspaces/:ws/endpoints" element={<EndpointsPage />} />
        </Routes>
      </QueryClientProvider>
    </MemoryRouter>,
  );
}

function endpointCard(name: string) {
  const heading = screen.getByText(name);
  const card = heading.closest('[data-slot="card"]');
  if (!card) throw new Error(`Card not found for ${name}`);
  return card as HTMLElement;
}

describe("EndpointsPage", () => {
  beforeEach(() => {
    apiFetchMock.mockResolvedValue({
      protocols: {
        ogcapi: true,
        wms: false,
        wfs: true,
        wcs: true,
        wmts: false,
        ogc_tiles: false,
      },
    });
  });
  afterEach(cleanup);

  it("disables endpoints that are inactive for either server or workspace", async () => {
    renderPage();
    await waitFor(() =>
      expect(apiFetchMock).toHaveBeenCalledWith(
        "/workspaces/Test/summary",
        expect.objectContaining({ method: "GET" }),
      ),
    );

    await waitFor(() =>
      expect(endpointCard("OGC API – Features")).toHaveAttribute(
        "data-state",
        "active",
      ),
    );
    const ogc = endpointCard("OGC API – Features");
    expect(ogc).toHaveAttribute("data-state", "active");
    expect(
      within(ogc).getByRole("button", { name: "Copy OGC API – Features" }),
    ).toBeEnabled();

    const wms = endpointCard("WMS GetCapabilities");
    expect(wms).toHaveAttribute("data-state", "inactive");
    expect(within(wms).getByText(/Off in this workspace/)).toBeVisible();
    expect(
      within(wms).getByRole("link", { name: "Off · turn on" }),
    ).toHaveAttribute("href", expect.stringMatching(/\/settings$/));
    expect(
      within(wms).getByRole("button", { name: "Copy WMS GetCapabilities" }),
    ).toBeDisabled();

    const wcs = endpointCard("WCS GetCapabilities");
    expect(wcs).toHaveAttribute("data-state", "inactive");
    expect(within(wcs).getByText(/Not enabled on this server\./)).toBeVisible();
    expect(
      within(wcs).getByRole("button", { name: "Copy WCS GetCapabilities" }),
    ).toBeDisabled();
  });

  it("keeps every protocol visible even when inactive", async () => {
    renderPage();
    for (const name of [
      "OGC API – Features",
      "WMS GetCapabilities",
      "WFS GetCapabilities",
      "WCS GetCapabilities",
      "WMTS GetCapabilities",
      "OGC API – Tiles",
    ]) {
      expect(await screen.findByText(name)).toBeVisible();
    }
  });
});
