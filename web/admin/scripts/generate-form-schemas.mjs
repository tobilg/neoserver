import { readFile, writeFile } from "node:fs/promises";
import { format, resolveConfig } from "prettier";

const spec = JSON.parse(
  await readFile(new URL("../openapi.json", import.meta.url), "utf8"),
);
function resolve(schema) {
  if (schema.$ref)
    return resolve(spec.components.schemas[schema.$ref.split("/").at(-1)]);
  return Object.fromEntries(
    Object.entries(schema)
      .filter(([key]) =>
        [
          "type",
          "properties",
          "items",
          "required",
          "enum",
          "x-service-operations",
          "minimum",
          "maximum",
          "format",
          "description",
          "default",
        ].includes(key),
      )
      .map(([key, value]) => [
        key,
        key === "properties"
          ? Object.fromEntries(
              Object.entries(value).map(([name, child]) => [
                name,
                resolve(child),
              ]),
            )
          : key === "items"
            ? resolve(value)
            : value,
      ]),
  );
}
const schemas = Object.fromEntries(
  [
    "WMSSettings",
    "WFSSettings",
    "WCSSettings",
    "WMTSSettings",
    "OGCAPISettings",
    "OGCTilesSettings",
    "Layer",
    "LayerGroup",
    "CoverageUpdate",
    "ImportPlan",
    "RolePolicy",
    "TileCacheJobRequest",
    "MosaicHarvestRequest",
  ].map((name) => [name, resolve(spec.components.schemas[name])]),
);
for (const [name, route, method] of [
  ["Store", "/workspaces/{workspace}/services/{service}", "put"],
  ["ClaimMapping", "/workspaces/{workspace}/claim-mappings", "post"],
  ["Role", "/roles", "post"],
])
  schemas[name] = resolve(
    spec.paths[route][method].requestBody.content["application/json"].schema,
  );
const output = await format(JSON.stringify(schemas), {
  ...(await resolveConfig(
    new URL("../src/lib/form-schemas.generated.json", import.meta.url).pathname,
  )),
  parser: "json",
});
const target = new URL(
  "../src/lib/form-schemas.generated.json",
  import.meta.url,
);
if (process.argv.includes("--check")) {
  if ((await readFile(target, "utf8")) !== output)
    throw new Error("Form schemas drifted: npm run codegen");
} else await writeFile(target, output);
