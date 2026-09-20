import { useEffect, useId, useRef, useState } from "react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import { advancedFields, fieldOrder } from "@/lib/form-layout";

import {
  emptyChoiceLabels,
  enumLabel,
  fieldLabel,
  fieldDefaults,
  type FieldSchema,
  type FieldChoices,
} from "@/lib/schema-fields";
import { NativeSelect } from "@/components/NativeSelect";
import { SwitchRow } from "@/components/SwitchRow";
import { ByteSizeInput } from "@/components/ByteSizeInput";
import { ToggleChips } from "@/components/ToggleChips";

export function SchemaFields({
  schema,
  value,
  onChange,
  choices = {},
  disabled = false,
  path = "",
}: {
  schema: FieldSchema;
  value: Record<string, unknown>;
  onChange: (value: Record<string, unknown>) => void;
  choices?: FieldChoices;
  disabled?: boolean;
  path?: string;
}) {
  const entries = Object.entries(schema.properties ?? {}).sort(
    ([a], [b]) => fieldOrder(a) - fieldOrder(b),
  );
  const advanced =
    entries.some(([key]) => ["public_id", "name", "title"].includes(key)) &&
    entries.length > 6
      ? entries.filter(
          ([key]) => advancedFields.has(key) && !schema.required?.includes(key),
        )
      : [];
  function fields(items: typeof entries) {
    return items.map(([key, child]) => (
      <SchemaField
        key={key}
        name={key}
        schema={child}
        value={value[key]}
        required={schema.required?.includes(key)}
        choices={choices}
        disabled={disabled}
        path={`${path}${key}`}
        onChange={(next) => {
          const updated = { ...value };
          if (next === undefined) delete updated[key];
          else updated[key] = next;
          onChange(updated);
        }}
      />
    ));
  }
  return (
    <div className="space-y-4">
      <div className="grid gap-4 sm:grid-cols-2">
        {fields(entries.filter((item) => !advanced.includes(item)))}
      </div>
      {advanced.length > 0 && (
        <details className="rounded-lg border p-3">
          <summary className="cursor-pointer text-sm font-medium">
            Advanced options ({advanced.length})
          </summary>
          <div className="mt-4 grid gap-4 sm:grid-cols-2">
            {fields(advanced)}
          </div>
        </details>
      )}
    </div>
  );
}

function SchemaField({
  name,
  schema,
  value,
  onChange,
  choices,
  disabled,
  required,
  path,
}: {
  name: string;
  schema: FieldSchema;
  value: unknown;
  onChange: (value: unknown) => void;
  choices: FieldChoices;
  disabled: boolean;
  required?: boolean;
  path: string;
}) {
  const id = useId();
  const overrideDraft = useRef<unknown>(undefined);
  const [search, setSearch] = useState("");
  const label = fieldLabel(name);
  // "Members" → "member", for per-item actions.
  const itemLabel = label.toLowerCase().replace(/ies$/, "y").replace(/s$/, "");
  const options =
    choices[path] ??
    choices[name] ??
    schema.enum?.map((value) => ({
      value: String(value),
      label: enumLabel(String(value)),
    }));
  if (schema.type === "object")
    return (
      <fieldset
        disabled={disabled}
        className="min-w-0 space-y-3 rounded-lg border p-3 sm:col-span-2"
      >
        <legend className="px-1 text-sm font-medium">{label}</legend>
        {!required && (
          <SwitchRow
            id={`${id}-override`}
            label={`Override ${label.toLowerCase()}`}
            checked={value !== undefined && value !== null}
            onCheckedChange={(checked) => {
              if (!checked) {
                overrideDraft.current = value;
                onChange(undefined);
              } else onChange(overrideDraft.current ?? fieldDefaults(schema));
            }}
          />
        )}
        {required || value != null ? (
          <SchemaFields
            schema={schema}
            value={(value ?? {}) as Record<string, unknown>}
            onChange={onChange}
            choices={choices}
            disabled={disabled}
            path={`${path}.`}
          />
        ) : (
          <p className="text-xs text-muted-foreground">
            Not overridden. The server uses its default or inherited settings.
          </p>
        )}
      </fieldset>
    );
  if (schema.type === "array") {
    const items = Array.isArray(value) ? value : [];
    const listChoices =
      options ??
      schema.items?.enum?.map((value) => ({
        value: String(value),
        label: enumLabel(String(value)),
      }));
    return (
      <fieldset
        disabled={disabled}
        className="min-w-0 space-y-2 rounded-lg border p-3 sm:col-span-2"
      >
        <legend className="px-1 text-sm font-medium">{label}</legend>
        {listChoices ? (
          listChoices.length || items.length ? (
            <ToggleChips
              options={listChoices}
              value={items.map(String)}
              disabled={disabled}
              onChange={onChange}
            />
          ) : (
            <p className="text-xs text-muted-foreground">
              {/styles?$/.test(name)
                ? "No styles in this workspace yet. Create one on the Styles page."
                : "Nothing to choose from yet."}
            </p>
          )
        ) : (
          items.map((item, i) => (
            <div key={i} className="space-y-2 rounded border p-3">
              <p className="text-xs font-medium text-muted-foreground">
                {itemLabel.charAt(0).toUpperCase() + itemLabel.slice(1)} {i + 1}
              </p>
              {schema.items?.type === "object" ? (
                <SchemaFields
                  schema={schema.items}
                  value={item as Record<string, unknown>}
                  onChange={(next) =>
                    onChange(items.map((v, j) => (j === i ? next : v)))
                  }
                  choices={choices}
                  disabled={disabled}
                  path={`${path}.${i}.`}
                />
              ) : (
                <Input
                  aria-label={`${label} ${i + 1}`}
                  value={String(item)}
                  type={
                    schema.items?.type === "number" ||
                    schema.items?.type === "integer"
                      ? "number"
                      : "text"
                  }
                  step={schema.items?.type === "integer" ? 1 : "any"}
                  onChange={(e) =>
                    onChange(
                      items.map((v, j) =>
                        j === i
                          ? schema.items?.type === "number" ||
                            schema.items?.type === "integer"
                            ? Number(e.target.value)
                            : e.target.value
                          : v,
                      ),
                    )
                  }
                />
              )}
              <div className="mt-2 flex gap-2">
                <Button
                  type="button"
                  size="sm"
                  variant="outline"
                  disabled={i === 0}
                  onClick={() => {
                    const next = [...items];
                    [next[i - 1], next[i]] = [next[i], next[i - 1]];
                    onChange(next);
                  }}
                >
                  Move up
                </Button>
                <Button
                  type="button"
                  size="sm"
                  variant="outline"
                  disabled={i === items.length - 1}
                  onClick={() => {
                    const next = [...items];
                    [next[i + 1], next[i]] = [next[i], next[i + 1]];
                    onChange(next);
                  }}
                >
                  Move down
                </Button>
                <Button
                  type="button"
                  size="sm"
                  variant="outline"
                  onClick={() => onChange(items.filter((_, j) => j !== i))}
                >
                  Remove {itemLabel} {i + 1}
                </Button>
              </div>
            </div>
          ))
        )}
        {!listChoices && (
          <Button
            type="button"
            size="sm"
            variant="outline"
            onClick={() =>
              onChange([
                ...items,
                fieldDefaults(schema.items ?? { type: "string" }),
              ])
            }
          >
            Add {itemLabel}
          </Button>
        )}
        {name === "allowed_roles" && (
          <p className="text-xs text-muted-foreground">
            No roles selected: any principal with workspace access. Public
            access bypasses this restriction.
          </p>
        )}
      </fieldset>
    );
  }
  if (schema.type === "boolean")
    return (
      // A switch gets its own full-width row, so rows stay aligned.
      <div className="space-y-2 border-b pb-3 sm:col-span-2">
        <SwitchRow
          id={id}
          label={label}
          description={schema.description}
          checked={Boolean(value)}
          disabled={disabled}
          onCheckedChange={onChange}
        />
        {name === "public" && Boolean(value) && (
          <p className="text-sm text-warning" role="status">
            This permits anonymous reads wherever the service allows public
            access.
          </p>
        )}
      </div>
    );
  return (
    <div className="space-y-1.5">
      <Label htmlFor={id}>
        {label}
        {required ? " *" : ""}
      </Label>
      {options ? (
        <>
          {options.length > 12 && (
            <Input
              aria-label={`Search ${label.toLowerCase()} choices`}
              placeholder="Find by name or ID…"
              value={search}
              disabled={disabled}
              onChange={(event) => setSearch(event.target.value)}
            />
          )}
          <NativeSelect
            id={id}
            disabled={disabled}
            required={required}
            value={String(value ?? "")}
            className="w-full"
            onChange={(e) =>
              onChange(
                e.target.value === ""
                  ? name === "dataset_map_layer_group_id"
                    ? ""
                    : undefined
                  : schema.type === "number" || schema.type === "integer"
                    ? Number(e.target.value)
                    : e.target.value,
              )
            }
          >
            <option value="">
              {required
                ? "Choose…"
                : (emptyChoiceLabels[name] ?? "Server default")}
            </option>
            {value && !options.some((o) => o.value === value) ? (
              <option value={String(value)}>{String(value)} (current)</option>
            ) : null}
            {options
              .filter(
                (option) =>
                  option.value === value ||
                  `${option.label} ${option.value}`
                    .toLowerCase()
                    .includes(search.toLowerCase()),
              )
              .map((option) => (
                <option key={option.value} value={option.value}>
                  {option.label}
                </option>
              ))}
          </NativeSelect>
        </>
      ) : /_bytes$/.test(name) &&
        (schema.type === "integer" || schema.type === "number") ? (
        <ByteSizeInput
          id={id}
          value={typeof value === "number" ? value : undefined}
          disabled={disabled}
          required={required}
          onChange={onChange}
        />
      ) : name === "description" ||
        name === "abstract" ||
        name === "sld_body" ? (
        <Textarea
          id={id}
          disabled={disabled}
          value={String(value ?? "")}
          onChange={(e) => onChange(e.target.value)}
        />
      ) : (
        <Input
          id={id}
          disabled={disabled}
          required={required}
          type={
            schema.type === "number" || schema.type === "integer"
              ? "number"
              : /password|secret|connection_string/.test(name)
                ? "password"
                : schema.format === "email"
                  ? "email"
                  : "text"
          }
          autoComplete={
            /password|secret|connection_string/.test(name)
              ? "new-password"
              : undefined
          }
          min={schema.minimum}
          max={schema.maximum}
          step={schema.type === "integer" ? 1 : "any"}
          value={String(value ?? "")}
          onChange={(e) =>
            onChange(
              schema.type === "number" || schema.type === "integer"
                ? e.target.value === ""
                  ? name === "dataset_map_layer_group_id"
                    ? ""
                    : undefined
                  : Number(e.target.value)
                : e.target.value,
            )
          }
        />
      )}
      {name === "password" && (
        <p className="text-xs text-muted-foreground">
          Leave untouched to preserve the saved secret.
        </p>
      )}
      {schema.description && (
        <p className="text-xs text-muted-foreground">{schema.description}</p>
      )}
      {!required && (schema.type === "number" || schema.type === "integer") && (
        <p className="text-xs text-muted-foreground">
          {schema.default !== undefined
            ? `Default: ${String(schema.default)}.`
            : value === undefined || value === ""
              ? "Empty: the server default applies."
              : "Clear the field to use the server default."}
        </p>
      )}
    </div>
  );
}

/** JSON is an escape hatch, never the default interface for routine edits. */
export function ObjectEditor({
  schema,
  draft,
  onChange,
  label,
  choices,
  disabled,
  primaryFields,
  revealAdvanced = false,
}: {
  schema: FieldSchema;
  draft: string;
  onChange: (draft: string) => void;
  label: string;
  choices?: FieldChoices;
  disabled?: boolean;
  /** Keep routine fields visible and disclose the rest without losing values. */
  primaryFields?: readonly string[];
  revealAdvanced?: boolean;
}) {
  const [advanced, setAdvanced] = useState(false);
  const details = useRef<HTMLDetailsElement>(null);
  useEffect(() => {
    if (revealAdvanced && details.current) details.current.open = true;
  }, [revealAdvanced, advanced]);
  let value: Record<string, unknown> = {};
  let error = "";
  try {
    const parsed: unknown = JSON.parse(draft);
    if (!parsed || typeof parsed !== "object" || Array.isArray(parsed))
      throw new Error("Expected an object");
    value = parsed as Record<string, unknown>;
  } catch {
    error = "Enter valid JSON before returning to the form or saving.";
  }
  const primarySchema = primaryFields
    ? {
        ...schema,
        properties: Object.fromEntries(
          primaryFields
            .filter((key) => schema.properties?.[key])
            .map((key) => [key, schema.properties![key]]),
        ),
      }
    : schema;
  const secondarySchema = {
    ...schema,
    properties: Object.fromEntries(
      Object.entries(schema.properties ?? {}).filter(
        ([key]) => !primaryFields?.includes(key),
      ),
    ),
  };
  return (
    <div className="space-y-4">
      {advanced || error ? (
        <Textarea
          aria-label={`${label} payload`}
          disabled={disabled}
          className="min-h-72 font-mono text-xs"
          value={draft}
          onChange={(e) => onChange(e.target.value)}
        />
      ) : (
        <>
          <SchemaFields
            schema={primarySchema}
            value={value}
            choices={choices}
            disabled={disabled}
            onChange={(next) => onChange(JSON.stringify(next, null, 2))}
          />
          {primaryFields &&
            Object.keys(secondarySchema.properties).length > 0 && (
              <details ref={details} className="min-w-0 rounded-lg border p-3">
                <summary className="cursor-pointer rounded-sm font-medium focus-visible:outline-2 focus-visible:outline-ring">
                  Advanced publication settings
                </summary>
                <p className="my-3 text-sm text-muted-foreground">
                  Additional styles, spatial configuration and cache policy.
                  Hidden fields keep their saved values.
                </p>
                <SchemaFields
                  schema={secondarySchema}
                  value={value}
                  choices={choices}
                  disabled={disabled}
                  onChange={(next) => onChange(JSON.stringify(next, null, 2))}
                />
              </details>
            )}
        </>
      )}
      <div className="flex justify-end">
        <Button
          type="button"
          variant="ghost"
          size="sm"
          className="text-muted-foreground"
          disabled={advanced && Boolean(error)}
          onClick={() => setAdvanced((v) => !v)}
        >
          {advanced ? "Use form" : "Advanced JSON"}
        </Button>
      </div>
      {error && (
        <p role="alert" className="text-sm text-destructive">
          {error}
        </p>
      )}
    </div>
  );
}
