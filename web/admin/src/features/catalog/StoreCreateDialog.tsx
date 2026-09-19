import { useEffect, useState } from "react";
import {
  useForm,
  type FieldErrors,
  type Path,
  type UseFormRegisterReturn,
} from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Database, FlaskConical, Plus } from "lucide-react";
import {
  createService,
  getListServicesQueryKey,
  testNewServiceConnection,
} from "@/api/generated/services/services";
import type {
  ConnectionTest,
  CreateServiceBody,
  ServiceConnectionInput,
} from "@/api/generated/models";
import { useAuth } from "@/auth/auth-context";
import { getGetWorkspaceSummaryQueryKey } from "@/api/generated/workspaces/workspaces";
import { Button } from "@/components/ui/button";
import { ConnectionResult } from "@/components/ConnectionResult";
import { connectionMessage } from "@/lib/connection-errors";
import { Checkbox } from "@/components/ui/checkbox";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@/components/AppDialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Textarea } from "@/components/ui/textarea";
import { applyServerError } from "@/lib/forms";
import {
  connectionDefaults,
  STORE_LABELS,
  STORE_TYPES,
  storeFormSchema,
  type StoreFormValues,
  type StoreType,
} from "./store-schemas";
import { confirmDiscard } from "@/lib/confirm";
import { NativeSelect } from "@/components/NativeSelect";

/** Form fields whose names may appear in a server error message. */
const SERVER_ERROR_FIELDS = [
  "name",
  "connection_info.host",
  "connection_info.port",
  "connection_info.database",
  "connection_info.user",
  "connection_info.password",
  "connection_info.path",
  "connection_info.directory",
] as unknown as readonly Path<StoreFormValues>[];

export function StoreCreateDialog({
  workspace,
  onDirty,
}: {
  workspace: string;
  onDirty?: (dirty: boolean) => void;
}) {
  const { config } = useAuth();
  const client = useQueryClient();
  const [open, setOpen] = useState(false);
  const [advanced, setAdvanced] = useState(
    JSON.stringify(connectionDefaults("postgis"), null, 2),
  );
  const [jsonError, setJSONError] = useState("");

  const form = useForm<StoreFormValues>({
    resolver: zodResolver(storeFormSchema),
    mode: "onBlur",
    reValidateMode: "onChange",
    defaultValues: {
      name: "",
      type: "postgis",
      enabled: true,
      connection_info: connectionDefaults("postgis"),
    } as StoreFormValues,
  });

  // `watch()` returns a fresh subscription each render, so the React Compiler
  // declines to memoize this component. Harmless here: the compiler is not
  // enabled for this build, and re-rendering on every keystroke is the point.
  // eslint-disable-next-line react-hooks/incompatible-library
  const type = form.watch("type");
  const connection = form.watch("connection_info") as Record<string, unknown>;
  const advancedDirty = advanced !== JSON.stringify(connection, null, 2);
  const dirty = open && (form.formState.isDirty || advancedDirty);
  // Only connection details are lost when the source type changes.
  const connectionDirty =
    advancedDirty ||
    JSON.stringify(connection) !== JSON.stringify(connectionDefaults(type));
  // Keep responses tied to the exact draft that was tested, including unapplied
  // JSON. An old in-flight response must not validate newer connection settings.
  const fingerprint = JSON.stringify([type, connection, advanced]);
  useEffect(() => {
    onDirty?.(dirty);
    return () => onDirty?.(false);
  }, [dirty, onDirty]);

  const test = useMutation({
    meta: { suppressErrorToast: true },
    mutationFn: (snapshot: {
      input: ServiceConnectionInput;
      fingerprint: string;
    }): Promise<ConnectionTest> =>
      testNewServiceConnection(workspace, snapshot.input),
  });
  const currentTest = test.variables?.fingerprint === fingerprint;
  const testResult =
    currentTest && test.data
      ? test.data.ok
        ? `Connected in ${test.data.duration_ms.toFixed(1)} ms`
        : test.data.message || "The store rejected the connection."
      : "";
  const testError = currentTest ? test.error : null;

  const create = useMutation({
    mutationFn: (values: StoreFormValues) =>
      createService(workspace, values as unknown as CreateServiceBody),
    onSuccess: async () => {
      setOpen(false);
      resetForm("postgis", true);
      setJSONError("");
      test.reset();
      await client.invalidateQueries({
        queryKey: getListServicesQueryKey(workspace),
      });
      await client.invalidateQueries({
        queryKey: getGetWorkspaceSummaryQueryKey(workspace),
      });
    },
    onError: (error) => applyServerError(form, error, SERVER_ERROR_FIELDS),
    // The failure is shown against the offending field; a toast would repeat it.
    meta: { suppressErrorToast: true },
  });

  function resetForm(next: StoreType, fresh = false) {
    const defaults = connectionDefaults(next);
    form.reset(
      {
        name: fresh ? "" : form.getValues("name"),
        type: next,
        enabled: fresh ? true : form.getValues("enabled"),
        connection_info: defaults,
      } as StoreFormValues,
      { keepDefaultValues: !fresh },
    );
    setAdvanced(JSON.stringify(defaults, null, 2));
  }

  async function changeType(next: StoreType) {
    if (
      connectionDirty &&
      !(await confirmDiscard(
        "Change the source type?",
        "The connection details you entered for this type will be cleared.",
      ))
    )
      return;
    resetForm(next);
    test.reset();
    setJSONError("");
  }

  function setField(field: string, value: unknown) {
    form.setValue(
      `connection_info.${field}` as Path<StoreFormValues>,
      value as never,
      { shouldDirty: true, shouldValidate: form.formState.isSubmitted },
    );
    setAdvanced(JSON.stringify({ ...connection, [field]: value }, null, 2));
  }

  function applyAdvanced() {
    try {
      const parsed = JSON.parse(advanced) as unknown;
      if (!parsed || Array.isArray(parsed) || typeof parsed !== "object") {
        throw new Error("Connection JSON must be an object");
      }
      form.setValue("connection_info", parsed as never, {
        shouldDirty: true,
        shouldValidate: true,
      });
      setAdvanced(JSON.stringify(parsed, null, 2));
      setJSONError("");
    } catch (reason) {
      setJSONError(reason instanceof Error ? reason.message : "Invalid JSON");
    }
  }

  const errors = form.formState.errors as FieldErrors<StoreFormValues> & {
    connection_info?: Record<string, { message?: string }>;
    root?: { server?: { message?: string } };
  };
  const connectionError = (field: string) =>
    errors.connection_info?.[field]?.message;
  const serverError = errors.root?.server?.message;

  return (
    <Dialog
      open={open}
      onOpenChange={async (value) => {
        if (create.isPending || test.isPending) return;
        // A restored draft should not greet the user with old validation errors.
        if (value) form.clearErrors();
        if (
          !value &&
          dirty &&
          !(await confirmDiscard("Close the unsaved store form?"))
        )
          return;
        setOpen(value);
      }}
    >
      <DialogTrigger asChild>
        <Button size="sm">
          <Plus /> Add store
        </Button>
      </DialogTrigger>
      <DialogContent className="max-h-[88vh] overflow-y-auto sm:max-w-3xl">
        <form
          onSubmit={form.handleSubmit((values) => {
            if (!advancedDirty) create.mutate(values);
          })}
        >
          <DialogHeader>
            <DialogTitle>Connect a store</DialogTitle>
            <DialogDescription>
              Choose the source type, enter its connection details, and test it
              before adding it to this workspace.
            </DialogDescription>
          </DialogHeader>
          <div className="grid gap-4 py-5 md:grid-cols-2">
            <div>
              <Label htmlFor="store-type">Source type</Label>
              <Select
                value={type}
                onValueChange={(value) => changeType(value as StoreType)}
              >
                <SelectTrigger id="store-type" className="w-full">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {STORE_TYPES.map((value) => (
                    <SelectItem value={value} key={value}>
                      {STORE_LABELS[value]}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
            <Field
              id="store-name"
              label="Store name"
              placeholder="e.g. production-data"
              error={errors.name?.message}
              register={form.register("name")}
            />
            <StoreFields
              type={type}
              value={connection}
              setField={setField}
              error={connectionError}
            />
            <label className="flex items-center gap-2 text-sm md:col-span-2">
              <Checkbox
                checked={form.watch("enabled")}
                onCheckedChange={(value) =>
                  form.setValue("enabled", value === true, {
                    shouldDirty: true,
                  })
                }
              />
              Enable this store immediately
            </label>
          </div>
          {type !== "postgis" && (
            <div className="mb-4 rounded-lg border-l-2 border-warning bg-warning/10 p-3 text-xs">
              Allowed server paths:{" "}
              {config?.allowed_paths?.join(", ") || "none configured"}
            </div>
          )}
          <details className="mb-4 border p-3">
            <summary className="cursor-pointer text-sm font-medium">
              Advanced connection JSON
            </summary>
            <Textarea
              className="mt-3 min-h-48 font-mono text-xs"
              aria-label="Advanced connection JSON"
              value={advanced}
              onChange={(event) => setAdvanced(event.target.value)}
            />
            <Button
              type="button"
              variant="outline"
              size="sm"
              className="mt-2"
              onClick={applyAdvanced}
            >
              Apply JSON
            </Button>
            {jsonError && (
              <p className="text-sm text-destructive">{jsonError}</p>
            )}
          </details>
          {advancedDirty && (
            <p role="status" className="mb-3 text-sm">
              Apply the edited JSON before testing or adding this store.
            </p>
          )}
          {test.variables && !currentTest && (
            <p role="status" className="mb-3 text-sm">
              Connection not tested for these settings.
            </p>
          )}
          {(testResult || testError) && (
            <ConnectionResult
              ok={Boolean(test.data?.ok && !testError)}
              detail={testResult || testError?.message || "Connection failed"}
              allowedPaths={config?.allowed_paths}
            />
          )}
          {serverError && (
            <div role="alert" className="space-y-1 text-sm text-destructive">
              <p>
                {/could not be opened/i.test(serverError)
                  ? connectionMessage(serverError, config?.allowed_paths)
                  : serverError}
              </p>
              {/could not be opened/i.test(serverError) && (
                <details>
                  <summary className="cursor-pointer">
                    Technical details
                  </summary>
                  <pre className="mt-1 whitespace-pre-wrap break-all text-xs">
                    {serverError}
                  </pre>
                </details>
              )}
            </div>
          )}
          <DialogFooter>
            <Button
              type="button"
              variant="outline"
              disabled={create.isPending || test.isPending}
              onClick={async () => {
                if (
                  !dirty ||
                  (await confirmDiscard("Close the unsaved store form?"))
                )
                  setOpen(false);
              }}
            >
              Cancel
            </Button>
            <Button
              type="button"
              variant="outline"
              disabled={test.isPending || create.isPending || advancedDirty}
              onClick={() =>
                test.mutate({
                  fingerprint,
                  input: structuredClone({
                    name: form.getValues("name") || "connection-test",
                    type,
                    connection_info: connection,
                  }) as unknown as ServiceConnectionInput,
                })
              }
            >
              <FlaskConical /> Test connection
            </Button>
            <Button disabled={create.isPending || advancedDirty}>
              <Database /> Add store
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

function StoreFields({
  type,
  value,
  setField,
  error,
}: {
  type: StoreType;
  value: Record<string, unknown>;
  setField: (field: string, value: unknown) => void;
  error: (field: string) => string | undefined;
}) {
  if (type === "postgis")
    return (
      <>
        <Field
          id="store-host"
          label="Host"
          value={String(value.host ?? "")}
          onChange={(next) => setField("host", next)}
          error={error("host")}
        />
        <Field
          id="store-port"
          label="Port"
          type="number"
          value={String(value.port ?? 5432)}
          onChange={(next) => setField("port", Number(next))}
          error={error("port")}
        />
        <Field
          id="store-database"
          label="Database"
          value={String(value.database ?? "")}
          onChange={(next) => setField("database", next)}
          error={error("database")}
        />
        <Field
          id="store-user"
          label="User"
          value={String(value.user ?? "")}
          onChange={(next) => setField("user", next)}
          error={error("user")}
        />
        <Field
          id="store-password"
          label="Password"
          type="password"
          value={String(value.password ?? "")}
          onChange={(next) => setField("password", next)}
          error={error("password")}
        />
        <div>
          <Label htmlFor="store-sslmode">SSL mode</Label>
          <NativeSelect
            id="store-sslmode"
            className="w-full"
            value={String(value.sslmode ?? "prefer")}
            onChange={(event) => setField("sslmode", event.target.value)}
          >
            {[
              "disable",
              "allow",
              "prefer",
              "require",
              "verify-ca",
              "verify-full",
            ].map((mode) => (
              <option key={mode} value={mode}>
                {mode}
              </option>
            ))}
          </NativeSelect>
          <p className="text-xs text-muted-foreground">
            Use verify-full for certificate and hostname verification. Disable
            is intended for trusted local development.
          </p>
        </div>
        <Field
          id="store-schemas"
          label="Schemas (comma separated)"
          value={Array.isArray(value.schemas) ? value.schemas.join(", ") : ""}
          onChange={(next) =>
            setField(
              "schemas",
              next
                .split(",")
                .map((item) => item.trim())
                .filter(Boolean),
            )
          }
          error={error("schemas")}
          className="md:col-span-2"
        />
      </>
    );
  if (type === "raster_mosaic")
    return (
      <>
        <Field
          id="mosaic-title"
          label="Mosaic name"
          value={String(value.name ?? "")}
          onChange={(next) => setField("name", next)}
          error={error("name")}
        />
        <Field
          id="mosaic-directory"
          label="Directory"
          value={String(value.directory ?? "")}
          onChange={(next) => setField("directory", next)}
          error={error("directory")}
        />
        <Field
          id="mosaic-pattern"
          label="File pattern"
          value={String(value.pattern ?? "*.tif")}
          onChange={(next) => setField("pattern", next)}
          error={error("pattern")}
        />
        <p className="self-end text-xs text-muted-foreground">
          Explicit granules, priorities, and dimensions can be supplied in
          advanced JSON.
        </p>
      </>
    );
  return (
    <>
      <Field
        id="store-path"
        label={type === "rasterfile" ? "Raster path or URL" : "Path or URL"}
        value={String(value.path ?? "")}
        onChange={(next) => setField("path", next)}
        error={error("path")}
        className="md:col-span-2"
      />
      {type === "duckdb" && (
        <>
          <Field
            id="store-native-srid"
            label="Native source SRID (if metadata is absent)"
            type="number"
            value={String(value.srid ?? "")}
            onChange={(next) =>
              setField("srid", next ? Number(next) : undefined)
            }
            error={error("srid")}
          />
          <p className="self-end text-xs text-muted-foreground">
            Use the EPSG code of the stored coordinates, not the desired output
            CRS. Mixed-CRS databases can set layer_srids in advanced JSON.
          </p>
        </>
      )}
      {type === "geoparquet" || type === "vectorfile" ? (
        <>
          <Field
            id="store-geometry"
            label="Geometry column"
            value={String(value.geometry_column ?? "")}
            onChange={(next) => setField("geometry_column", next)}
            error={error("geometry_column")}
          />
          <Field
            id="store-id-column"
            label="ID column"
            value={String(value.id_column ?? "")}
            onChange={(next) => setField("id_column", next)}
            error={error("id_column")}
          />
          <Field
            id="store-srid"
            label="Source SRID"
            type="number"
            value={String(value.srid ?? 4326)}
            onChange={(next) => setField("srid", Number(next))}
            error={error("srid")}
          />
          {type === "vectorfile" && (
            <Field
              id="store-layer"
              label="Layer override"
              value={String(value.layer ?? "")}
              onChange={(next) => setField("layer", next)}
              error={error("layer")}
            />
          )}
        </>
      ) : null}
      {type === "rasterfile" && (
        <Field
          id="store-variables"
          label="Variables (comma separated, optional)"
          value={
            Array.isArray(value.variables) ? value.variables.join(", ") : ""
          }
          onChange={(next) =>
            setField(
              "variables",
              next
                .split(",")
                .map((item) => item.trim())
                .filter(Boolean),
            )
          }
          error={error("variables")}
          className="md:col-span-2"
        />
      )}
    </>
  );
}

/**
 * Input with a label and an inline validation message.
 *
 * Two mutually exclusive modes: pass `register` for a react-hook-form field, or
 * `value`/`onChange` for one the form owns indirectly (the connection object,
 * whose shape varies by store type).
 */
function Field({
  id,
  label,
  value,
  onChange,
  register,
  type = "text",
  placeholder,
  className,
  error,
}: {
  id: string;
  label: string;
  value?: string;
  onChange?: (value: string) => void;
  register?: UseFormRegisterReturn;
  type?: string;
  placeholder?: string;
  className?: string;
  error?: string;
}) {
  const controlled =
    value === undefined
      ? {}
      : {
          value,
          onChange: (event: React.ChangeEvent<HTMLInputElement>) =>
            onChange?.(event.target.value),
        };
  return (
    <div className={className}>
      <Label htmlFor={id}>{label}</Label>
      <Input
        id={id}
        type={type}
        placeholder={placeholder}
        aria-invalid={error ? true : undefined}
        aria-describedby={error ? `${id}-error` : undefined}
        {...register}
        {...controlled}
      />
      {error && (
        <p id={`${id}-error`} className="mt-1 text-xs text-destructive">
          {error}
        </p>
      )}
    </div>
  );
}
