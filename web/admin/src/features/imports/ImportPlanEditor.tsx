import { useState } from "react";
import { scrollHorizontally } from "@/lib/keyboard-scroll";
import type { ImportJob, ImportPlan } from "@/api/generated/models";
import { SchemaFields } from "@/components/SchemaFields";
import { validateFields, type FieldSchema } from "@/lib/schema-fields";
import { formSchemas } from "@/lib/resource-schemas";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import { SwitchRow } from "@/components/SwitchRow";
import { useCatalogChoices } from "@/features/catalog/use-catalog-choices";
import { QueryError } from "@/components/QueryError";
import { revisionPlan } from "./import-plan";

export function ImportPlanEditor({
  workspace,
  job,
  initialPlan,
  busy,
  submit,
  onDirty,
}: {
  workspace: string;
  job: ImportJob;
  initialPlan: ImportPlan;
  busy: boolean;
  submit: (plan: ImportPlan) => void;
  onDirty: (dirty: boolean) => void;
}) {
  const [revision] = useState(() => revisionPlan(job, initialPlan));
  const [plan, setPlan] = useState(revision.plan);
  const [excluded, setExcluded] = useState<Set<string>>(revision.excluded);
  const [prefix, setPrefix] = useState("");
  const [error, setError] = useState("");
  const catalog = useCatalogChoices(workspace);
  const layerSchema: FieldSchema = {
    ...formSchemas.ImportPlan.properties?.layers.items,
    properties: Object.fromEntries(
      Object.entries(
        formSchemas.ImportPlan.properties?.layers.items?.properties ?? {},
      ).filter(([name]) => name !== "source_layer" && name !== "fields"),
    ),
    required: ["public_id", "enabled", "public"],
  };
  function change(next: ImportPlan) {
    setPlan(next);
    onDirty(true);
    setError("");
  }
  function validate() {
    const output = {
      ...plan,
      layers: plan.layers.filter((layer) => !excluded.has(layer.source_layer)),
    };
    const errors = validateFields(formSchemas.ImportPlan, output);
    if (!output.layers.length) errors.push("Include at least one layer.");
    const ids = output.layers.map((layer) => layer.public_id);
    if (new Set(ids).size !== ids.length)
      errors.push("Public IDs must be unique within this import.");
    for (const layer of output.layers) {
      if (
        [...catalog.publications, ...catalog.groups].some(
          (item) => item.public_id === layer.public_id,
        )
      )
        errors.push(
          `${layer.source_layer}: public ID "${layer.public_id}" is already published in this workspace.`,
        );
      if (!/^[A-Za-z_][A-Za-z0-9_.-]*$/.test(layer.public_id))
        errors.push(`${layer.source_layer}: use a valid public ID.`);
      const names = (layer.fields ?? [])
        .filter((field) => field.include)
        .map((field) => field.target);
      if (
        names.some((name) => !name.trim()) ||
        new Set(names).size !== names.length
      )
        errors.push(
          `${layer.source_layer}: included target field names must be non-empty and unique.`,
        );
    }
    if (errors.length) {
      setError(errors.join(" "));
      return;
    }
    submit(output);
  }
  return (
    <div className="space-y-4">
      <QueryError
        error={catalog.error}
        retry={catalog.retry}
        context="Publication and role choices could not be loaded"
      />
      <div className="space-y-1">
        <Label htmlFor="import-service-name">Target store name</Label>
        <Input
          id="import-service-name"
          disabled={busy}
          value={plan.service_name}
          onChange={(e) => change({ ...plan, service_name: e.target.value })}
        />
      </div>
      <div className="flex flex-wrap gap-2">
        <Button
          size="sm"
          variant="outline"
          disabled={busy}
          onClick={() => {
            setExcluded(new Set());
            onDirty(true);
          }}
        >
          Include all
        </Button>
        <Button
          size="sm"
          variant="outline"
          disabled={busy}
          onClick={() => {
            setExcluded(
              new Set(plan.layers.map((layer) => layer.source_layer)),
            );
            onDirty(true);
          }}
        >
          Exclude all
        </Button>
        <Input
          className="max-w-48"
          aria-label="Public ID prefix"
          placeholder="Prefix target IDs…"
          value={prefix}
          onChange={(e) => setPrefix(e.target.value)}
        />
        <Button
          size="sm"
          variant="outline"
          disabled={busy || !prefix}
          onClick={() =>
            change({
              ...plan,
              layers: plan.layers.map((layer) => ({
                ...layer,
                public_id: prefix + layer.public_id,
              })),
            })
          }
        >
          Apply prefix
        </Button>
      </div>
      {plan.layers.map((layer, index) => {
        const source = job.discovery?.layers?.find(
          (item) => item.name === layer.source_layer,
        );
        return (
          <details
            key={layer.source_layer}
            className="rounded-lg border p-3"
            open={plan.layers.length === 1}
          >
            <summary className="cursor-pointer text-sm font-medium">
              {layer.source_layer} → {layer.public_id || "unnamed"}
              {excluded.has(layer.source_layer) ? " (excluded)" : ""}
            </summary>
            <div className="mt-3 space-y-4">
              <SwitchRow
                label="Include this layer"
                aria-label={`Include ${layer.source_layer}`}
                checked={!excluded.has(layer.source_layer)}
                disabled={busy}
                onCheckedChange={(checked) => {
                  setExcluded((current) => {
                    const next = new Set(current);
                    if (checked) next.delete(layer.source_layer);
                    else next.add(layer.source_layer);
                    return next;
                  });
                  onDirty(true);
                }}
              />
              <fieldset
                disabled={busy || excluded.has(layer.source_layer)}
                className="min-w-0 space-y-4"
              >
                <SchemaFields
                  schema={layerSchema}
                  value={{ ...layer }}
                  choices={{
                    allowed_roles: catalog.choices.allowed_roles,
                  }}
                  disabled={busy || excluded.has(layer.source_layer)}
                  onChange={(next) =>
                    change({
                      ...plan,
                      layers: plan.layers.map((old, i) =>
                        // SchemaFields returns the full draft, including removals;
                        // merging old values back would undo clearing optional fields.
                        i === index ? (next as unknown as typeof old) : old,
                      ),
                    })
                  }
                />
                {layer.source_srid &&
                layer.target_srid &&
                layer.source_srid !== layer.target_srid ? (
                  <p role="status" className="text-sm text-warning">
                    Coordinates will be reprojected from EPSG:
                    {layer.source_srid} to EPSG:{layer.target_srid}.
                  </p>
                ) : null}
                <div
                  className="min-w-0 overflow-x-auto focus-visible:outline-2 focus-visible:outline-ring"
                  tabIndex={0}
                  role="region"
                  aria-label={`Field mapping for ${layer.source_layer}, scroll to see all columns`}
                  onKeyDown={scrollHorizontally}
                >
                  <table className="w-full min-w-[36rem] text-left text-sm">
                    <caption className="mb-2 text-left font-medium">
                      Field mapping
                    </caption>
                    <thead>
                      <tr>
                        {[
                          "Include",
                          "Source",
                          "Source type",
                          "Target name",
                          "Target type",
                        ].map((name) => (
                          <th key={name} className="p-2">
                            {name}
                          </th>
                        ))}
                      </tr>
                    </thead>
                    <tbody>
                      {(layer.fields ?? []).map((field, fieldIndex) => {
                        function update(patch: Partial<typeof field>) {
                          change({
                            ...plan,
                            layers: plan.layers.map((old, i) =>
                              i === index
                                ? {
                                    ...old,
                                    fields: old.fields?.map((entry, j) =>
                                      j === fieldIndex
                                        ? { ...entry, ...patch }
                                        : entry,
                                    ),
                                  }
                                : old,
                            ),
                          });
                        }
                        return (
                          <tr key={field.source}>
                            <td className="p-2">
                              <Switch
                                aria-label={`Include field ${layer.source_layer}.${field.source}`}
                                checked={field.include}
                                onCheckedChange={(include) =>
                                  update({ include })
                                }
                              />
                            </td>
                            <td className="p-2 font-mono">{field.source}</td>
                            <td className="p-2">
                              {source?.properties?.find(
                                (p) => p.name === field.source,
                              )?.type ?? "—"}
                            </td>
                            <td className="p-2">
                              <Input
                                className="min-w-32"
                                aria-label={`Target name for ${layer.source_layer}.${field.source}`}
                                value={field.target}
                                onChange={(e) =>
                                  update({ target: e.target.value })
                                }
                              />
                            </td>
                            <td className="p-2">
                              <Input
                                className="min-w-40"
                                aria-label={`Target type for ${layer.source_layer}.${field.source}`}
                                value={field.type ?? ""}
                                onChange={(e) =>
                                  update({ type: e.target.value })
                                }
                                placeholder="Preserve source type"
                              />
                            </td>
                          </tr>
                        );
                      })}
                    </tbody>
                  </table>
                </div>
              </fieldset>
            </div>
          </details>
        );
      })}
      {error && (
        <p role="alert" className="text-sm text-destructive">
          {error}
        </p>
      )}
      <Button
        disabled={busy || catalog.isLoading || Boolean(catalog.error)}
        onClick={validate}
      >
        Validate plan
      </Button>
    </div>
  );
}
