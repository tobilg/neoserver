import { cleanup, fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { renderScreen } from "@/test/render";
import { STACPage } from "./STACPage";

const { fetchMock } = vi.hoisted(() => ({ fetchMock: vi.fn() }));
vi.mock("@/api/client", () => ({ apiFetch: fetchMock }));
vi.mock("@/auth/auth-context", () => ({
  useAuth: () => ({
    config: {
      features: { stac: true },
      url_base: "https://maps.example.org",
      base_path: "/maps",
    },
  }),
}));

const collection = {
  document: {
    id: "scenes",
    type: "Collection",
    stac_version: "1.1.0",
    title: "Scene catalog",
    description: "Acquisition footprints",
    license: "other",
    "custom:source": "retained",
  },
  public: false,
  revision: 3,
  item_count: 0,
};
beforeEach(() => {
  fetchMock.mockReset();
  fetchMock.mockImplementation(async (path: string, options?: RequestInit) => {
    if (
      path.endsWith("/collections") &&
      (!options?.method || options.method === "GET")
    )
      return { collections: [collection] };
    if (path.endsWith("/resources"))
      return {
        resources: [
          {
            id: "layer",
            service_id: "source",
            public_id: "scenes",
            title: "Source scenes",
            kind: "layer",
            provider: "duckdb",
            modes: ["dataset", "mapped"],
            properties: [
              { name: "scene_id" },
              { name: "acquired" },
              { name: "href" },
            ],
          },
        ],
      };
    if (path.endsWith("/bindings/preview"))
      return { collection: collection.document, items: [], sample_only: true };
    if (path.endsWith("/bindings")) return { id: "job", status: "queued" };
    if (
      path.endsWith("/settings/stac") &&
      (!options?.method || options.method === "GET")
    )
      return { enabled: false, public: false };
    return {};
  });
});
afterEach(cleanup);
function renderPage() {
  return renderScreen(<STACPage />, {
    path: "/workspaces/:ws/stac",
    entry: "/workspaces/demo/stac",
  });
}
async function tab(name: string) {
  const button = await screen.findByRole("tab", { name });
  fireEvent.mouseDown(button, { button: 0, ctrlKey: false });
  fireEvent.click(button);
}

it("preserves extension metadata when editing a Collection", async () => {
  renderPage();
  fireEvent.click(await screen.findByRole("button", { name: "Edit metadata" }));
  fireEvent.change(screen.getByLabelText("Title"), {
    target: { value: "Updated title" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Save Collection" }));
  await waitFor(() =>
    expect(fetchMock).toHaveBeenCalledWith(
      expect.stringContaining("/stac/collections/scenes"),
      expect.objectContaining({ method: "PUT" }),
    ),
  );
  const call = fetchMock.mock.calls.find(
    ([, options]) => options?.method === "PUT",
  );
  expect(JSON.parse(call![1].body).document).toMatchObject({
    title: "Updated title",
    "custom:source": "retained",
  });
});

it("requires a mapping preview before publication and invalidates it on edits", async () => {
  renderPage();
  await tab("Publish existing data");
  fireEvent.change(await screen.findByLabelText("Collection"), {
    target: { value: "scenes" },
  });
  await screen.findByRole("option", { name: /Source scenes/ });
  fireEvent.change(screen.getByLabelText("Published layer or coverage"), {
    target: { value: "layer" },
  });
  expect(screen.getByRole("button", { name: "Publish" })).toBeDisabled();
  fireEvent.click(screen.getByRole("button", { name: "Preview publication" }));
  await waitFor(() =>
    expect(screen.getByRole("button", { name: "Publish" })).toBeEnabled(),
  );
  fireEvent.change(screen.getByLabelText("Refresh interval (seconds)"), {
    target: { value: "1800" },
  });
  expect(screen.getByRole("button", { name: "Publish" })).toBeDisabled();
  fireEvent.click(screen.getByRole("button", { name: "Preview publication" }));
  await waitFor(() =>
    expect(screen.getByRole("button", { name: "Publish" })).toBeEnabled(),
  );
  fireEvent.click(screen.getByRole("button", { name: "Publish" }));
  expect(await screen.findByText(/Publication queued/)).toBeInTheDocument();
  const call = fetchMock.mock.calls.find(
    ([path, options]) =>
      path.endsWith("/bindings") && options?.method === "POST",
  );
  expect(JSON.parse(call![1].body)).toMatchObject({
    collection_id: "scenes",
    revision: 3,
    binding: {
      mode: "dataset",
      refresh_interval_sec: 1800,
      resource_id: "layer",
    },
  });
});

it("keeps settings and client URLs scoped to the workspace and deployment prefix", async () => {
  renderPage();
  await tab("Settings");
  const enabled = await screen.findByLabelText("Enable this workspace catalog");
  fireEvent.click(enabled);
  fireEvent.click(screen.getByRole("button", { name: "Save settings" }));
  await waitFor(() =>
    expect(fetchMock).toHaveBeenCalledWith(
      "/workspaces/demo/settings/stac",
      expect.objectContaining({
        method: "PUT",
        body: expect.stringContaining('"enabled":true'),
      }),
    ),
  );
  expect(
    screen.getByRole("link", {
      name: "https://maps.example.org/maps/workspaces/demo/stac/",
    }),
  ).toHaveAttribute(
    "href",
    "https://maps.example.org/maps/workspaces/demo/stac/",
  );
});
