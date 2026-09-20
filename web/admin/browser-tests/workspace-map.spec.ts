import { test, expect } from "./fixture";

test("workspace map can be selected, saved, and cleared", async ({
  page,
  fixture,
}) => {
  fixture.settings["ogc-tiles"] = {
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
  await page.route("**/api/v1/workspaces/demo/layer-groups", (route) =>
    route.fulfill({
      json: {
        layer_groups: [
          {
            id: "map-id",
            public_id: "map",
            title: "Curated map",
            enabled: true,
          },
          {
            id: "disabled-id",
            public_id: "disabled",
            title: "Old map",
            enabled: false,
          },
        ],
      },
    }),
  );
  const errors: string[] = [];
  page.on("pageerror", (error) => errors.push(error.message));
  await page.goto("/admin/workspaces/demo/settings");
  await page.getByText("Configure OGC API – Tiles", { exact: true }).click();
  const section = page.getByRole("region", { name: "OGC API – Tiles" });
  const selector = section.getByRole("combobox", { name: "Workspace map" });
  await expect(
    selector.getByRole("option", { name: "Old map (disabled)" }),
  ).toHaveCount(1);
  const saved = () =>
    fixture.settings["ogc-tiles"].settings as Record<string, unknown>;
  await selector.selectOption("map-id");
  const save = section.getByRole("button", {
    name: "Save settings",
    exact: true,
  });
  await save.click();
  await expect.poll(() => saved().dataset_map_layer_group_id).toBe("map-id");
  await expect(save).toBeDisabled();
  await selector.selectOption("");
  await save.click();
  await expect.poll(() => saved().dataset_map_layer_group_id).toBe("");
  expect(saved().cache_enabled).toBe(true);
  expect(saved().map_tiles).toEqual({ enabled: true, formats: ["image/png"] });
  expect(errors).toEqual([]);
});
