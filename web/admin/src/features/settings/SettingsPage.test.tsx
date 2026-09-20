import {
  cleanup,
  fireEvent,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
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
        if (endpoint.endsWith("/layer-groups"))
          return {
            layer_groups: [
              {
                id: "map-id",
                public_id: "map",
                title: "Curated map",
                enabled: true,
              },
              {
                id: "disabled-id",
                public_id: "old",
                title: "Old map",
                enabled: false,
              },
            ],
          };
        if (endpoint.endsWith("/ogc-tiles"))
          return {
            enabled: true,
            public: true,
            settings: {
              dataset_map_layer_group_id: "",
              tile_matrix_sets: ["WebMercatorQuad"],
              vector_tiles: { enabled: false },
              map_tiles: { enabled: true, formats: ["image/png"] },
              cache_enabled: true,
            },
          };
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
  it("selects and explicitly clears the workspace map without losing tile settings", async () => {
    renderPage();
    const region = await screen.findByRole("region", {
      name: "OGC API – Tiles",
    });
    fireEvent.click(
      await within(region).findByText("Configure OGC API – Tiles"),
    );
    const select = await within(region).findByRole("combobox", {
      name: "Workspace map",
    });
    await within(select).findByRole("option", { name: "Curated map" });
    expect(
      within(select).getByRole("option", { name: "Old map (disabled)" }),
    ).toBeInTheDocument();
    fireEvent.change(select, { target: { value: "map-id" } });
    fireEvent.click(within(region).getByRole("button", { name: /Save/ }));
    await waitFor(() =>
      expect(apiFetchMock).toHaveBeenCalledWith(
        "/workspaces/Test/settings/ogc-tiles",
        expect.objectContaining({
          method: "PUT",
          body: expect.stringContaining(
            '"dataset_map_layer_group_id":"map-id"',
          ),
        }),
      ),
    );
    await waitFor(() =>
      expect(
        within(region).queryByText(/unsaved changes/),
      ).not.toBeInTheDocument(),
    );
    fireEvent.change(select, { target: { value: "" } });
    fireEvent.click(within(region).getByRole("button", { name: /Save/ }));
    await waitFor(() =>
      expect(apiFetchMock).toHaveBeenCalledWith(
        "/workspaces/Test/settings/ogc-tiles",
        expect.objectContaining({
          method: "PUT",
          body: expect.stringContaining('"dataset_map_layer_group_id":""'),
        }),
      ),
    );
    const calls = apiFetchMock.mock.calls.filter(
      ([url, options]) =>
        url.endsWith("/ogc-tiles") && options?.method === "PUT",
    );
    for (const [, options] of calls) {
      const saved = JSON.parse(options.body);
      expect(saved.settings.map_tiles.formats).toEqual(["image/png"]);
      expect(saved.settings.cache_enabled).toBe(true);
    }
  });

  it("keeps the selected map draft when saving fails", async () => {
    const implementation = apiFetchMock.getMockImplementation()!;
    apiFetchMock.mockImplementation(async (url, options) => {
      if (url.endsWith("/ogc-tiles") && options?.method === "PUT")
        throw new Error("Selected group is being deleted");
      return implementation(url, options);
    });
    renderPage();
    const region = await screen.findByRole("region", {
      name: "OGC API – Tiles",
    });
    fireEvent.click(
      await within(region).findByText("Configure OGC API – Tiles"),
    );
    const select = await within(region).findByRole("combobox", {
      name: "Workspace map",
    });
    await within(select).findByRole("option", { name: "Curated map" });
    fireEvent.change(select, { target: { value: "map-id" } });
    fireEvent.click(within(region).getByRole("button", { name: /Save/ }));
    expect(
      await within(region).findByText(/Selected group is being deleted/),
    ).toBeInTheDocument();
    expect(select).toHaveValue("map-id");
  });
});
