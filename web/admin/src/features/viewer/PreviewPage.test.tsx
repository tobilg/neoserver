import {
  act,
  cleanup,
  fireEvent,
  screen,
  within,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { renderScreen } from "@/test/render";

const { mapInstance } = vi.hoisted(() => ({
  mapInstance: {
    addControl: vi.fn(),
    on: vi.fn(),
    remove: vi.fn(),
    getSource: vi.fn(),
    getLayer: vi.fn(),
    addSource: vi.fn(),
    addLayer: vi.fn(),
    removeLayer: vi.fn(),
    removeSource: vi.fn(),
    fitBounds: vi.fn(),
    getZoom: vi.fn(() => 1),
    getBounds: vi.fn(() => ({
      getWest: () => -180,
      getSouth: () => -85,
      getEast: () => 180,
      getNorth: () => 85,
    })),
  },
}));
// The component calls `new maplibregl.Map(...)`, so the mock must be
// constructible; an arrow function is not.
vi.mock("maplibre-gl", () => ({
  setWorkerUrl: vi.fn(),
  Map: vi.fn(function MapMock() {
    return mapInstance;
  }),
  NavigationControl: vi.fn(function NavigationControlMock() {
    return {};
  }),
}));
vi.mock("maplibre-gl/dist/maplibre-gl.css", () => ({}));
vi.mock("@/auth/auth-context", () => ({
  useAuth: () => ({ config: { basemap_url: "" } }),
}));
vi.mock("@/api/generated/workspaces/workspaces", () => ({
  useGetWorkspaceSummary: () => ({
    data: { protocols: { ogcapi: true } },
    refetch: vi.fn(),
  }),
}));
vi.mock("@/features/catalog/use-catalog-choices", () => {
  const catalog = {
    publications: [],
    resources: [],
    coverages: [],
    services: [],
    groups: [],
    choices: { styles: [] },
    retry: vi.fn(),
  };
  return { useCatalogChoices: () => catalog };
});

const { PreviewPage } = await import("./PreviewPage");

function renderPage(query = "") {
  return renderScreen(<PreviewPage />, {
    path: "/workspaces/:ws/preview",
    entry: "/workspaces/demo/preview" + query,
  });
}

beforeEach(() => {
  vi.clearAllMocks();
  vi.stubGlobal(
    "fetch",
    vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({
        type: "FeatureCollection",
        features: [],
        collections: [
          {
            id: "roads",
            title: "Roads",
            extent: { spatial: { bbox: [[7, 51, 7, 51]] } },
          },
          {
            id: "parks",
            title: "Parks",
            extent: { spatial: { bbox: [[8, 50, 9, 52]] } },
          },
          { id: "unknown", title: "Unknown extent" },
        ],
      }),
    }),
  );
});

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

describe("PreviewPage", () => {
  async function loadMap() {
    await screen.findByText("Roads");
    act(() => {
      mapInstance.on.mock.calls.find(([event]) => event === "load")![1]();
    });
  }
  it("frames all initial selections once and keeps opacity edits and refreshes in place", async () => {
    renderPage("?layers=roads,parks");
    await loadMap();
    expect(mapInstance.fitBounds).toHaveBeenCalledWith(
      [
        [7, 50],
        [9, 52],
      ],
      { duration: 0, padding: 60, maxZoom: 15 },
    );
    fireEvent.change(screen.getByLabelText("Opacity for roads"), {
      target: { value: "0.5" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Refresh preview" }));
    await act(async () => {});
    expect(mapInstance.fitBounds).toHaveBeenCalledTimes(1);
  });
  it("preserves an explicit shared view", async () => {
    renderPage("?layers=roads&bbox=-20,-10,30,40");
    await loadMap();
    expect(mapInstance.fitBounds).toHaveBeenCalledExactlyOnceWith(
      [
        [-20, -10],
        [30, 40],
      ],
      { duration: 0, padding: 60 },
    );
  });
  it("falls back to the selected point for an invalid shared view", async () => {
    renderPage("?layers=roads&bbox=not-a-bbox");
    await loadMap();
    expect(mapInstance.fitBounds).toHaveBeenCalledWith(
      [
        [7, 51],
        [7, 51],
      ],
      expect.objectContaining({ maxZoom: 15 }),
    );
  });
  it("does not jump after the user interacts while the map loads", async () => {
    renderPage("?layers=roads");
    fireEvent.pointerDown(
      screen.getByRole("region", { name: "Workspace map preview" }),
    );
    await loadMap();
    expect(mapInstance.fitBounds).not.toHaveBeenCalled();
  });
  it("explains missing extents without asking to select an already selected publication", async () => {
    renderPage("?layers=unknown");
    await loadMap();
    expect(screen.getByRole("button", { name: "Fit unknown" })).toBeDisabled();
    expect(screen.getByText(/No geographic extent/)).toBeVisible();
    expect(
      screen.queryByText("Select a publication to begin."),
    ).not.toBeInTheDocument();
    expect(mapInstance.fitBounds).not.toHaveBeenCalled();
  });
  it("initialises a map for the workspace", async () => {
    renderPage();
    const maplibre = await import("maplibre-gl");
    expect(maplibre.Map).toHaveBeenCalled();
  });

  it("fetches collections with the session cookie rather than a token", async () => {
    renderPage();
    await vi.waitFor(() =>
      expect(fetch).toHaveBeenCalledWith(
        expect.stringContaining("/workspaces/demo/ogc/collections"),
        expect.objectContaining({ credentials: "same-origin" }),
      ),
    );
  });

  it("lists the workspace collections", async () => {
    renderPage();
    // The default selection moves Roads into "On the map" once it loads.
    await vi.waitFor(() =>
      expect(
        within(screen.getByRole("region", { name: "On the map" })).getByText(
          "Roads",
        ),
      ).toBeInTheDocument(),
    );
  });

  it("shows the first publication when the URL selects nothing", async () => {
    renderPage();
    await loadMap();
    await vi.waitFor(() =>
      expect(screen.getByLabelText("Show roads")).toBeChecked(),
    );
    expect(screen.getByLabelText("Show parks")).not.toBeChecked();
    expect(screen.queryByText("No layers shown")).not.toBeInTheDocument();
  });

  it("keeps an explicitly empty selection and explains it", async () => {
    renderPage("?layers=roads");
    await loadMap();
    fireEvent.click(screen.getByLabelText("Show roads"));
    expect(await screen.findByText("No layers shown")).toBeVisible();
    expect(screen.getByLabelText("Show roads")).not.toBeChecked();
  });

  it("fits a publication without extent to its loaded sample", async () => {
    vi.mocked(fetch).mockImplementation(async (input) => {
      const url = String(input);
      return {
        ok: true,
        json: async () =>
          url.includes("/items")
            ? {
                type: "FeatureCollection",
                features: [
                  {
                    type: "Feature",
                    properties: {},
                    geometry: { type: "Point", coordinates: [2, 48] },
                  },
                  {
                    type: "Feature",
                    properties: {},
                    geometry: { type: "Point", coordinates: [13, 52] },
                  },
                ],
              }
            : { collections: [{ id: "unknown", title: "Unknown extent" }] },
      } as Response;
    });
    renderPage("?layers=unknown");
    await screen.findByText("Unknown extent");
    act(() => {
      mapInstance.on.mock.calls.find(([event]) => event === "load")![1]();
    });
    const fit = await screen.findByRole("button", { name: "Fit unknown" });
    await vi.waitFor(() => expect(fit).toBeEnabled());
    await vi.waitFor(() =>
      expect(mapInstance.fitBounds).toHaveBeenCalledWith(
        [
          [2, 48],
          [13, 52],
        ],
        { duration: 0, padding: 60, maxZoom: 15 },
      ),
    );
  });
});
