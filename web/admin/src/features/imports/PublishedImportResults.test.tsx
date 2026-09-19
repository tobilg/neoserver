import { cleanup, screen } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { renderScreen } from "@/test/render";
import type { ImportJob } from "@/api/generated/models";
import { PublishedImportResults } from "./PublishedImportResults";

const { list } = vi.hoisted(() => ({ list: vi.fn() }));
vi.mock("@/api/generated/layers/layers", () => ({ useListLayers: list }));
afterEach(cleanup);
const job = {
  id: "import-1",
  service_id: "import-store",
  plan: {
    service_name: "Imported",
    layers: [{ public_id: "imported_places" }, { public_id: "imported_areas" }],
  },
} as ImportJob;

it("links only this import's results using current public IDs, including renamed publications", () => {
  list.mockReturnValue({
    data: {
      layers: [
        {
          id: "1",
          source_layer: "imported_places",
          public_id: "renamed_places",
        },
        {
          id: "2",
          source_layer: "imported_areas",
          public_id: "imported_areas",
        },
        { id: "3", source_layer: "unrelated", public_id: "unrelated" },
      ],
    },
  });
  renderScreen(<PublishedImportResults workspace="demo" job={job} />);
  expect(list).toHaveBeenCalledWith("demo", "import-store", expect.anything());
  expect(
    screen
      .getAllByRole("link", { name: "Connect a client" })
      .map((a) => a.getAttribute("href")),
  ).toEqual([
    "/workspaces/demo/endpoints?layer=renamed_places",
    "/workspaces/demo/endpoints?layer=imported_areas",
  ]);
  expect(
    screen.getAllByRole("link", { name: "Preview on map" })[0],
  ).toHaveAttribute("href", "/workspaces/demo/preview?layers=renamed_places");
  expect(
    screen.getByRole("link", { name: "View created store" }),
  ).toHaveAttribute("href", "/workspaces/demo/stores?store=import-store");
  expect(
    screen.getByRole("link", { name: "View published layers" }),
  ).toHaveAttribute("href", "/workspaces/demo/layers?store=import-store");
  expect(screen.queryByText("unrelated")).not.toBeInTheDocument();
});

it("does not link to another dataset when imported layers were unpublished", () => {
  list.mockReturnValue({ data: { layers: [] } });
  renderScreen(<PublishedImportResults workspace="demo" job={job} />);
  expect(screen.getByRole("status")).toHaveTextContent(
    "No matching layers remain",
  );
  expect(
    screen.queryByRole("link", { name: "Connect a client" }),
  ).not.toBeInTheDocument();
});

it("provides retry rather than stale connection links after a catalog failure", () => {
  list.mockReturnValue({
    error: new Error("unavailable"),
    refetch: vi.fn(),
    data: {
      layers: [
        { id: "1", source_layer: "imported_places", public_id: "places" },
      ],
    },
  });
  renderScreen(<PublishedImportResults workspace="demo" job={job} />);
  expect(screen.getByRole("alert")).toHaveTextContent(
    "Published import results could not be loaded",
  );
  expect(
    screen.queryByRole("link", { name: "Connect a client" }),
  ).not.toBeInTheDocument();
});
