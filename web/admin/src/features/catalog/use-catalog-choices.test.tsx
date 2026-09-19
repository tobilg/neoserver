import { cleanup, screen } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { useCatalogChoices } from "./use-catalog-choices";
import { renderScreen } from "@/test/render";

const { apiFetchMock } = vi.hoisted(() => ({ apiFetchMock: vi.fn() }));
vi.mock("@/api/client", () => ({ apiFetch: apiFetchMock }));
afterEach(cleanup);

function CatalogProbe() {
  const catalog = useCatalogChoices("demo");
  if (catalog.isLoading) return <p>Loading</p>;
  return (
    <>
      <p>
        All publications:{" "}
        {catalog.allResources.map((item) => item.public_id).join(", ")}
      </p>
      <ul>
        {catalog.resources.map((item) => (
          <li key={item.public_id}>
            {item.public_id}: {item.kind}
          </li>
        ))}
      </ul>
    </>
  );
}

it("includes PostGIS and file coverages without querying unsupported feature endpoints", async () => {
  apiFetchMock.mockImplementation(async (path: string) => {
    if (path.endsWith("/services"))
      return {
        services: [
          { id: "pg", type: "postgis", enabled: true },
          { id: "file", type: "rasterfile", enabled: true },
          { id: "off", type: "rasterfile", enabled: false },
        ],
      };
    if (path.endsWith("/pg/layers"))
      return { layers: [{ public_id: "roads", enabled: true }] };
    if (path.endsWith("/pg/coverages"))
      return {
        coverages: [
          { public_id: "terrain", enabled: true },
          { public_id: "hidden", enabled: false },
        ],
      };
    if (path.endsWith("/file/coverages"))
      return { coverages: [{ public_id: "imagery", enabled: true }] };
    if (path.endsWith("/off/coverages"))
      return {
        coverages: [{ public_id: "offline", service_id: "off", enabled: true }],
      };
    if (path.endsWith("/layer-groups"))
      return { layer_groups: [{ public_id: "basemap", enabled: true }] };
    return {};
  });
  renderScreen(<CatalogProbe />);
  expect(await screen.findByText("terrain: coverage")).toBeInTheDocument();
  expect(screen.getByText("imagery: coverage")).toBeInTheDocument();
  expect(screen.getByText("roads: feature")).toBeInTheDocument();
  expect(screen.getByText("basemap: group")).toBeInTheDocument();
  expect(screen.queryByText("hidden: coverage")).not.toBeInTheDocument();
  expect(screen.queryByText("offline: coverage")).not.toBeInTheDocument();
  expect(screen.getByText(/^All publications:/)).toHaveTextContent("hidden");
  expect(screen.getByText(/^All publications:/)).toHaveTextContent("offline");
  expect(
    apiFetchMock.mock.calls.some(([path]) =>
      /\/file\/layers|\/off\/layers/.test(path),
    ),
  ).toBe(false);
});
