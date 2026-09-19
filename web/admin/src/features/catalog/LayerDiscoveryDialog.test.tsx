import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { Service } from "@/api/generated/models";
import { Button } from "@/components/ui/button";
import { LayerDiscoveryDialog } from "./LayerDiscoveryDialog";
import { invalidateSessionRequests } from "@/api/session";

const { apiFetchMock } = vi.hoisted(() => ({ apiFetchMock: vi.fn() }));

vi.mock("@/api/client", () => ({
  apiFetch: apiFetchMock,
  jsonBody: (value: unknown) => JSON.stringify(value),
}));

const workspace = "Test";
const featureStore: Service = {
  id: "store-1",
  workspace_id: "workspace-1",
  name: "Primary data",
  type: "postgis",
  enabled: true,
  created_at: "2026-01-01T00:00:00Z",
  updated_at: "2026-01-01T00:00:00Z",
};

function renderDialog({
  services = [featureStore],
  onPublished,
}: {
  services?: Service[];
  onPublished?: () => void;
} = {}) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <MemoryRouter>
      <QueryClientProvider client={client}>
        <LayerDiscoveryDialog
          workspace={workspace}
          services={services}
          manageStoresPath="/workspaces/Test/stores"
          onPublished={onPublished}
          trigger={<Button>Add layer</Button>}
        />
      </QueryClientProvider>
    </MemoryRouter>,
  );
}

async function openAndDiscover({ selectAll = true } = {}) {
  fireEvent.click(screen.getByRole("button", { name: "Add layer" }));
  const discover = await screen.findByRole("button", {
    name: "Discover layers",
  });
  await waitFor(() => expect(discover).toBeEnabled());
  fireEvent.click(discover);
  if (!selectAll) return;
  // Discovery starts with nothing selected; most tests publish everything.
  const all = await screen.findByRole("button", { name: "Select all" });
  await waitFor(() => expect(all).toBeEnabled());
  fireEvent.click(all);
}

describe("LayerDiscoveryDialog", () => {
  beforeEach(() => {
    apiFetchMock.mockReset();
  });
  afterEach(cleanup);

  it("retains names, selection and removed-source drafts across refresh failures and changes", async () => {
    let layers = [{ name: "roads" }, { name: "buildings" }];
    let fail = false;
    apiFetchMock.mockImplementation(async (endpoint: string) => {
      if (endpoint.endsWith("/discover")) {
        if (fail) throw new Error("Discovery unavailable");
        return { layers };
      }
      return { layers: [] };
    });
    renderDialog();
    await openAndDiscover();
    const id = await screen.findByLabelText("Public ID for roads");
    fireEvent.change(id, { target: { value: "important-name" } });
    fireEvent.change(screen.getByLabelText("Title for roads"), {
      target: { value: "My title" },
    });
    fireEvent.click(
      screen.getByRole("checkbox", { name: "Publish buildings" }),
    );
    const refresh = () =>
      fireEvent.click(
        screen.getByRole("button", { name: "Refresh discovery" }),
      );
    fail = true;
    refresh();
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Discovery unavailable",
    );
    expect(screen.getByLabelText("Public ID for roads")).toHaveValue(
      "important-name",
    );
    fail = false;
    layers = [...layers, { name: "parks" }];
    refresh();
    // Newly discovered sources are not selected automatically.
    expect(
      await screen.findByRole("checkbox", { name: "Publish parks" }),
    ).not.toBeChecked();
    expect(
      screen.getByRole("checkbox", { name: "Publish roads" }),
    ).toBeChecked();
    expect(
      screen.getByRole("checkbox", { name: "Publish buildings" }),
    ).not.toBeChecked();
    expect(screen.getByLabelText("Title for roads")).toHaveValue("My title");
    layers = [{ name: "parks" }];
    refresh();
    expect(await screen.findByRole("status")).toHaveTextContent(
      "no longer available",
    );
    expect(
      screen.queryByLabelText("Public ID for roads"),
    ).not.toBeInTheDocument();
    layers = [{ name: "parks" }, { name: "roads" }];
    refresh();
    expect(await screen.findByLabelText("Public ID for roads")).toHaveValue(
      "important-name",
    );
    expect(screen.getByLabelText("Title for roads")).toHaveValue("My title");
  });

  it.each(["discovery", "publication"])(
    "locks controls during delayed %s",
    async (stage) => {
      let release!: () => void;
      const gate = new Promise<void>((resolve) => {
        release = resolve;
      });
      const writes: string[] = [];
      apiFetchMock.mockImplementation(
        async (endpoint: string, options?: RequestInit) => {
          if (endpoint.endsWith("/discover")) {
            if (stage === "discovery") await gate;
            return { layers: [{ name: "roads" }, { name: "parks" }] };
          }
          if (options?.method === "POST") {
            writes.push(endpoint);
            if (writes.length === 1) await gate;
            return {};
          }
          return { layers: [] };
        },
      );
      renderDialog();
      await openAndDiscover({ selectAll: stage === "publication" });
      if (stage === "publication")
        fireEvent.click(
          await screen.findByRole("button", { name: "Publish 2 layers" }),
        );
      await waitFor(() => expect(screen.getByRole("combobox")).toBeDisabled());
      expect(screen.getByRole("button", { name: "Cancel" })).toBeDisabled();
      fireEvent.keyDown(screen.getByRole("dialog"), { key: "Escape" });
      expect(screen.getByRole("dialog")).toBeVisible();
      release();
      if (stage === "publication") {
        expect(
          await screen.findByRole("dialog", { name: "Layers published" }),
        ).toBeVisible();
        expect(
          screen.getAllByRole("link", { name: "Connect a client" }),
        ).toHaveLength(2);
        fireEvent.click(screen.getByRole("button", { name: "Done" }));
        expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
        expect(writes).toHaveLength(2);
        expect(
          writes.every((endpoint) =>
            endpoint.includes("/services/store-1/layers"),
          ),
        ).toBe(true);
      } else {
        // Once discovery completes, the controls unlock with nothing selected.
        const all = await screen.findByRole("button", { name: "Select all" });
        await waitFor(() => expect(all).toBeEnabled());
        fireEvent.click(all);
        expect(
          await screen.findByRole("button", { name: "Publish 2 layers" }),
        ).toBeEnabled();
      }
    },
  );

  it.each(["navigation", "logout"])(
    "stops queued publications after %s",
    async (action) => {
      let release!: () => void;
      const gate = new Promise<void>((resolve) => {
        release = resolve;
      });
      let writes = 0;
      apiFetchMock.mockImplementation(
        async (endpoint: string, options?: RequestInit) => {
          if (endpoint.endsWith("/discover"))
            return { layers: [{ name: "roads" }, { name: "parks" }] };
          if (options?.method === "POST") {
            writes++;
            await gate;
            return {};
          }
          return { layers: [] };
        },
      );
      const view = renderDialog();
      await openAndDiscover();
      fireEvent.click(
        await screen.findByRole("button", { name: "Publish 2 layers" }),
      );
      await waitFor(() => expect(writes).toBe(1));
      if (action === "navigation") view.unmount();
      else invalidateSessionRequests();
      release();
      await new Promise((resolve) => setTimeout(resolve, 20));
      expect(writes).toBe(1);
    },
  );

  it.each([false, true])(
    "publishes schema-qualified names once (already qualified: %s)",
    async (qualified) => {
      const publishedBodies: Array<Record<string, unknown>> = [];
      apiFetchMock.mockImplementation(
        async (endpoint: string, options?: RequestInit) => {
          if (
            endpoint.endsWith("/layers") &&
            (options?.method ?? "GET") === "GET"
          )
            return { layers: [] };
          if (endpoint.endsWith("/discover"))
            return {
              layers: [
                {
                  name: qualified ? "public.roads" : "roads",
                  schema: "public",
                  srid: 4326,
                },
                {
                  name: qualified ? "archive.roads" : "roads",
                  schema: "archive",
                  srid: 3857,
                },
              ],
            };
          if (endpoint.endsWith("/layers") && options?.method === "POST") {
            publishedBodies.push(
              JSON.parse(String(options.body)) as Record<string, unknown>,
            );
            return {};
          }
          throw new Error(`Unexpected request: ${endpoint}`);
        },
      );
      const onPublished = vi.fn();
      renderDialog({ onPublished });

      await openAndDiscover();
      expect(
        await screen.findByRole("checkbox", { name: "Publish public.roads" }),
      ).toBeChecked();
      expect(
        screen.getByRole("checkbox", { name: "Publish archive.roads" }),
      ).toBeChecked();
      fireEvent.click(screen.getByRole("button", { name: "Publish 2 layers" }));

      await waitFor(() => expect(onPublished).toHaveBeenCalledOnce());
      expect(publishedBodies).toEqual([
        expect.objectContaining({
          source_layer: "public.roads",
          public_id: "public-roads",
          enabled: true,
          public: false,
        }),
        expect.objectContaining({
          source_layer: "archive.roads",
          public_id: "archive-roads",
          enabled: true,
          public: false,
        }),
      ]);
      await waitFor(() =>
        expect(
          screen.queryByRole("heading", { name: "Add layers" }),
        ).not.toBeInTheDocument(),
      );
    },
  );

  it("keeps only failed layers selected for retry", async () => {
    const attempts = new Map<string, number>();
    apiFetchMock.mockImplementation(
      async (endpoint: string, options?: RequestInit) => {
        if (
          endpoint.endsWith("/layers") &&
          (options?.method ?? "GET") === "GET"
        )
          return { layers: [] };
        if (endpoint.endsWith("/discover"))
          return {
            layers: [
              { name: "roads", srid: 4326 },
              { name: "buildings", srid: 4326 },
            ],
          };
        if (endpoint.endsWith("/layers") && options?.method === "POST") {
          const body = JSON.parse(String(options.body)) as {
            source_layer: string;
          };
          const count = (attempts.get(body.source_layer) ?? 0) + 1;
          attempts.set(body.source_layer, count);
          if (body.source_layer === "buildings" && count === 1)
            throw Object.assign(new Error("Conflict"), {
              status: 409,
              detail: "Public ID is already in use",
            });
          return {};
        }
        throw new Error(`Unexpected request: ${endpoint}`);
      },
    );
    renderDialog();

    await openAndDiscover();
    fireEvent.click(
      await screen.findByRole("button", { name: "Publish 2 layers" }),
    );

    expect(
      await screen.findByText("Conflict: Public ID is already in use"),
    ).toBeInTheDocument();
    // The published source moves out of the way until asked for.
    expect(
      screen.queryByRole("checkbox", { name: "Publish roads" }),
    ).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Show 1 published" }));
    expect(
      screen.getByRole("checkbox", { name: "Publish roads" }),
    ).toBeDisabled();
    expect(
      screen.getByRole("checkbox", { name: "Publish buildings" }),
    ).toBeChecked();
    fireEvent.click(screen.getByRole("button", { name: "Publish 1 layer" }));

    await waitFor(() => expect(attempts.get("roads")).toBe(1));
    await waitFor(() => expect(attempts.get("buildings")).toBe(2));
    await waitFor(() =>
      expect(
        screen.queryByRole("heading", { name: "Add layers" }),
      ).not.toBeInTheDocument(),
    );
  });

  it("marks an already-published source as unavailable", async () => {
    apiFetchMock.mockImplementation(
      async (endpoint: string, options?: RequestInit) => {
        if (
          endpoint.endsWith("/layers") &&
          (options?.method ?? "GET") === "GET"
        )
          return {
            layers: [
              {
                id: "layer-1",
                service_id: "store-1",
                source_layer: "public.roads",
                public_id: "roads",
                enabled: true,
                crs_default: 4326,
                public: false,
              },
            ],
          };
        if (endpoint.endsWith("/discover"))
          return {
            layers: [
              { name: "roads", schema: "public", srid: 4326 },
              { name: "buildings", schema: "public", srid: 4326 },
            ],
          };
        throw new Error(`Unexpected request: ${endpoint}`);
      },
    );
    renderDialog();

    await openAndDiscover();
    expect(
      await screen.findByText("1 new · 1 already published"),
    ).toBeInTheDocument();
    expect(
      screen.queryByRole("checkbox", { name: "Publish public.roads" }),
    ).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Show 1 published" }));
    expect(
      screen.getByRole("checkbox", { name: "Publish public.roads" }),
    ).toBeDisabled();
    expect(
      screen.getByRole("checkbox", { name: "Publish public.buildings" }),
    ).toBeChecked();
    expect(screen.getByText("Published")).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "Publish 1 layer" }),
    ).toBeEnabled();
  });

  it("links to store management when no enabled feature store exists", async () => {
    renderDialog({
      services: [
        { ...featureStore, enabled: false },
        {
          ...featureStore,
          id: "raster-1",
          name: "Elevation",
          type: "rasterfile",
        },
      ],
    });
    fireEvent.click(screen.getByRole("button", { name: "Add layer" }));

    expect(
      await screen.findByText("No enabled feature store is available"),
    ).toBeInTheDocument();
    expect(screen.getByRole("link", { name: /Manage stores/ })).toHaveAttribute(
      "href",
      "/workspaces/Test/stores",
    );
    expect(apiFetchMock).not.toHaveBeenCalled();
  });

  it("starts with nothing selected and suggests IDs without the schema", async () => {
    apiFetchMock.mockImplementation(async (endpoint: string) =>
      endpoint.endsWith("/discover")
        ? {
            layers: [
              { name: "cite.Autos", schema: "cite", title: "Autos" },
              { name: "cite.places", schema: "cite" },
              { name: "public.places", schema: "public" },
            ],
          }
        : { layers: [] },
    );
    renderDialog();
    await openAndDiscover({ selectAll: false });
    expect(
      await screen.findByLabelText("Public ID for cite.Autos"),
    ).toHaveValue("autos");
    expect(screen.getByLabelText("Public ID for cite.places")).toHaveValue(
      "cite-places",
    );
    expect(
      screen.getByRole("button", { name: "Select layers to publish" }),
    ).toBeDisabled();
    fireEvent.click(
      screen.getByRole("checkbox", { name: "Publish cite.Autos" }),
    );
    expect(
      screen.getByRole("button", { name: "Publish 1 layer" }),
    ).toBeEnabled();
  });

  it("explains when every discovered source is already published", async () => {
    apiFetchMock.mockImplementation(
      async (endpoint: string, options?: RequestInit) => {
        if (
          endpoint.endsWith("/layers") &&
          (options?.method ?? "GET") === "GET"
        )
          return {
            layers: [
              {
                id: "layer-1",
                service_id: "store-1",
                source_layer: "public.roads",
                public_id: "roads",
                enabled: true,
                crs_default: 4326,
                public: false,
              },
            ],
          };
        if (endpoint.endsWith("/discover"))
          return { layers: [{ name: "roads", schema: "public", srid: 4326 }] };
        throw new Error(`Unexpected request: ${endpoint}`);
      },
    );
    renderDialog();
    await openAndDiscover({ selectAll: false });
    expect(
      await screen.findByText(/All 1 sources in .* are published\./),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "Nothing new to publish" }),
    ).toBeDisabled();
    expect(screen.getByRole("link", { name: "View layers" })).toBeVisible();
  });
});
