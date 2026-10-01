import { lazy, Suspense, useId, useState, type ReactNode } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useParams } from "react-router";
import { Plus, RefreshCw } from "lucide-react";
import * as api from "@/api/generated/stac/stac";
import type {
  STACBinding,
  STACCollection,
  STACDocument,
  STACJob,
  STACMapping,
  STACPage as ItemPage,
  STACPreview,
  STACSearch,
  STACSettings,
} from "@/api/generated/models";
import { useAuth } from "@/auth/auth-context";
import { Page } from "@/components/Page";
import { QueryError } from "@/components/QueryError";
import { NativeSelect } from "@/components/NativeSelect";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { tabsRootFix, tabsTriggerFix } from "@/lib/tabsCompat";
import { confirmAction } from "@/lib/confirm";
import { formatDateTime } from "@/lib/format";

const FootprintMap = lazy(() => import("./STACMap"));
const pretty = (value: unknown) => JSON.stringify(value, null, 2);
const text = (value: unknown) => (typeof value === "string" ? value : "");
const panel = "space-y-4 rounded-xl border bg-card p-4 sm:p-6";

function Field({ label, children }: { label: string; children: ReactNode }) {
  return (
    <label className="grid min-w-0 gap-1.5 text-sm font-medium">
      {label}
      {children}
    </label>
  );
}
function JSONField({
  label,
  value,
  onChange,
  readOnly = false,
}: {
  label: string;
  value: string;
  onChange?: (value: string) => void;
  readOnly?: boolean;
}) {
  const id = useId();
  return (
    <div className="grid gap-1.5">
      <label htmlFor={id} className="text-sm font-medium">
        {label}
      </label>
      <Textarea
        id={id}
        spellCheck={false}
        className="min-h-48 font-mono text-xs"
        value={value}
        readOnly={readOnly}
        onChange={(e) => onChange?.(e.target.value)}
      />
    </div>
  );
}
function useTask(ws: string) {
  const client = useQueryClient();
  return useMutation({
    mutationFn: (task: () => Promise<unknown>) => task(),
    onSuccess: () => client.invalidateQueries({ queryKey: ["stac", ws] }),
  });
}
type Common = { ws: string; collections: STACCollection[] };
function CollectionSelect({
  collections,
  value,
  onChange,
  standalone = false,
}: {
  collections: STACCollection[];
  value: string;
  onChange: (value: string) => void;
  standalone?: boolean;
}) {
  return (
    <Field label="Collection">
      <NativeSelect
        className="w-full"
        value={value}
        onChange={(e) => onChange(e.target.value)}
      >
        <option value="">Select a Collection</option>
        {collections
          .filter((c) => !standalone || !c.binding)
          .map((c) => (
            <option key={text(c.document.id)} value={text(c.document.id)}>
              {text(c.document.title) || text(c.document.id)}
            </option>
          ))}
      </NativeSelect>
    </Field>
  );
}

export function STACPage() {
  const { ws = "" } = useParams();
  const { config } = useAuth();
  if (!config?.features.stac)
    return (
      <Page
        title="STAC"
        description="Publish and discover spatiotemporal assets."
      >
        <p className="text-sm text-muted-foreground">
          STAC is disabled on this server. Enable STAC in the server
          configuration to use this workspace catalog.
        </p>
      </Page>
    );
  return <WorkspaceSTAC key={ws} ws={ws} />;
}
function WorkspaceSTAC({ ws }: { ws: string }) {
  const collections = useQuery({
    queryKey: ["stac", ws, "collections"],
    queryFn: () => api.stacListCollections(ws),
  });
  const [tab, setTab] = useState("collections");
  return (
    <Page
      title="STAC"
      description="Discover existing datasets and publish searchable asset records in this workspace."
      action={
        <Button variant="outline" onClick={() => void collections.refetch()}>
          <RefreshCw />
          Refresh
        </Button>
      }
    >
      <QueryError error={collections.error} retry={collections.refetch} />
      {collections.isPending ? (
        <p role="status">Loading asset catalog…</p>
      ) : (
        <Tabs value={tab} onValueChange={setTab} className={tabsRootFix}>
          <TabsList className="mb-4 flex h-auto flex-wrap justify-start">
            {[
              ["collections", "Collections"],
              ["publish", "Publish existing data"],
              ["import", "Import metadata"],
              ["search", "Items"],
              ["jobs", "Jobs"],
              ["settings", "Settings"],
            ].map(([value, label]) => (
              <TabsTrigger key={value} value={value} className={tabsTriggerFix}>
                {label}
              </TabsTrigger>
            ))}
          </TabsList>
          <TabsContent value="collections">
            <Collections
              ws={ws}
              collections={collections.data?.collections ?? []}
            />
          </TabsContent>
          <TabsContent value="publish">
            <Publish
              ws={ws}
              collections={collections.data?.collections ?? []}
            />
          </TabsContent>
          <TabsContent value="import">
            <ImportMetadata
              ws={ws}
              collections={collections.data?.collections ?? []}
            />
          </TabsContent>
          <TabsContent value="search">
            <Items ws={ws} collections={collections.data?.collections ?? []} />
          </TabsContent>
          <TabsContent value="jobs">
            <Jobs ws={ws} />
          </TabsContent>
          <TabsContent value="settings">
            <Settings ws={ws} />
          </TabsContent>
        </Tabs>
      )}
    </Page>
  );
}
function Collections({ ws, collections }: Common) {
  const [selected, setSelected] = useState<STACCollection | "new" | null>(null);
  const task = useTask(ws);
  return (
    <div className="space-y-5">
      <div className="flex items-center justify-between gap-3">
        <p className="text-sm text-muted-foreground">
          Dataset Collections link to existing services. Asset Collections also
          contain searchable Items.
        </p>
        <Button onClick={() => setSelected("new")}>
          <Plus />
          New Collection
        </Button>
      </div>
      <QueryError
        error={task.error}
        context="The catalog change could not be saved"
      />
      {collections.length === 0 && (
        <div className={panel}>
          <h2 className="font-semibold">Create your first Collection</h2>
          <p className="text-sm text-muted-foreground">
            Add Collection metadata, then publish an existing layer or coverage,
            or import STAC JSON.
          </p>
        </div>
      )}
      {collections.map((c) => (
        <article key={text(c.document.id)} className={panel}>
          <div className="flex flex-wrap items-start justify-between gap-4">
            <div>
              <h2 className="font-semibold">
                {text(c.document.title) || text(c.document.id)}
              </h2>
              <p className="text-sm text-muted-foreground">
                {text(c.document.id)} ·{" "}
                {c.binding?.mode === "dataset"
                  ? "Dataset discovery"
                  : `${c.item_count.toLocaleString()} Items`}{" "}
                · {c.public ? "Public" : "Restricted"}
              </p>
              <p className="mt-2 text-sm">{text(c.document.description)}</p>
            </div>
            <div className="flex flex-wrap gap-2">
              <Button variant="outline" onClick={() => setSelected(c)}>
                Edit metadata
              </Button>
              {c.binding?.id && (
                <Button
                  variant="outline"
                  disabled={task.isPending}
                  onClick={() =>
                    task.mutate(() =>
                      api.stacRefreshBinding(ws, c.binding!.id!),
                    )
                  }
                >
                  Refresh source
                </Button>
              )}
              <Button
                variant="outline"
                disabled={task.isPending}
                onClick={async () => {
                  if (
                    await confirmAction({
                      title: `Delete ${text(c.document.id)}?`,
                      description:
                        "Its STAC metadata, Items and jobs will be removed. Source data will remain available.",
                      confirmLabel: "Delete Collection",
                      destructive: true,
                    })
                  )
                    task.mutate(() =>
                      api.stacDeleteCollection(ws, text(c.document.id)),
                    );
                }}
              >
                Delete
              </Button>
            </div>
          </div>
          {c.binding && (
            <p className="text-xs text-muted-foreground">
              Source refresh every{" "}
              {Math.round(c.binding.refresh_interval_sec / 60)} minutes. Changes
              made in neoserver also trigger refresh.
            </p>
          )}
        </article>
      ))}
      {selected && (
        <CollectionEditor
          key={
            selected === "new"
              ? "new"
              : `${selected.document.id}-${selected.revision}`
          }
          ws={ws}
          collection={selected === "new" ? undefined : selected}
          onClose={() => setSelected(null)}
        />
      )}
    </div>
  );
}
function CollectionEditor({
  ws,
  collection,
  onClose,
}: {
  ws: string;
  collection?: STACCollection;
  onClose: () => void;
}) {
  const [id, setID] = useState(text(collection?.document.id));
  const [title, setTitle] = useState(text(collection?.document.title));
  const [description, setDescription] = useState(
    text(collection?.document.description),
  );
  const [license, setLicense] = useState(
    text(collection?.document.license) || "other",
  );
  const [isPublic, setPublic] = useState(collection?.public ?? false);
  const [roles, setRoles] = useState(
    collection?.allowed_roles?.join(", ") ?? "",
  );
  const [extra, setExtra] = useState(pretty(collection?.document ?? {}));
  const task = useTask(ws);
  return (
    <form
      className={panel}
      onSubmit={(e) => {
        e.preventDefault();
        task.mutate(async () => {
          const extraDocument = JSON.parse(extra) as STACDocument;
          const document = {
            type: "Collection",
            stac_version: "1.1.0",
            links: [],
            extent: {
              spatial: { bbox: [[-180, -90, 180, 90]] },
              temporal: { interval: [[null, null]] },
            },
            ...extraDocument,
            id,
            title,
            description,
            license,
          };
          const input: STACCollection = {
            document,
            public: isPublic,
            allowed_roles: roles
              .split(",")
              .map((r) => r.trim())
              .filter(Boolean),
            revision: collection?.revision ?? 0,
            item_count: collection?.item_count ?? 0,
          };
          if (collection) await api.stacUpdateCollection(ws, id, input);
          else await api.stacCreateCollection(ws, input);
          onClose();
        });
      }}
    >
      <h2 className="font-semibold">
        {collection ? "Edit Collection" : "New Collection"}
      </h2>
      <div className="grid gap-4 sm:grid-cols-2">
        <Field label="Collection ID">
          <Input
            required
            value={id}
            disabled={Boolean(collection)}
            onChange={(e) => setID(e.target.value)}
          />
        </Field>
        <Field label="Title">
          <Input
            required
            value={title}
            onChange={(e) => setTitle(e.target.value)}
          />
        </Field>
        <Field label="Description">
          <Textarea
            required
            value={description}
            onChange={(e) => setDescription(e.target.value)}
          />
        </Field>
        <Field label="License (SPDX expression or other)">
          <Input
            required
            value={license}
            onChange={(e) => setLicense(e.target.value)}
          />
        </Field>
        <Field label="Allowed roles (comma separated)">
          <Input
            value={roles}
            onChange={(e) => setRoles(e.target.value)}
            placeholder="All workspace roles when empty"
          />
        </Field>
        <label className="flex items-center gap-2 text-sm">
          <input
            type="checkbox"
            checked={isPublic}
            onChange={(e) => setPublic(e.target.checked)}
          />
          Public Collection
        </label>
      </div>
      <details>
        <summary className="cursor-pointer text-sm font-medium">
          Additional metadata and extensions
        </summary>
        <div className="mt-3">
          <JSONField
            label="Collection JSON (unknown fields are preserved)"
            value={extra}
            onChange={setExtra}
          />
        </div>
      </details>
      <p className="text-xs text-muted-foreground">
        Workspace service access and source access rules also apply. Item
        extents are calculated when records are published.
      </p>
      <QueryError error={task.error} context="Collection could not be saved" />
      <div className="flex gap-2">
        <Button disabled={task.isPending} type="submit">
          {task.isPending ? "Saving…" : "Save Collection"}
        </Button>
        <Button type="button" variant="outline" onClick={onClose}>
          Cancel
        </Button>
      </div>
    </form>
  );
}

function Publish({ ws, collections }: Common) {
  const resources = useQuery({
    queryKey: ["stac", ws, "resources"],
    queryFn: () => api.stacListResources(ws),
  });
  const [collection, setCollection] = useState("");
  const [resourceID, setResourceID] = useState("");
  const [mode, setMode] = useState<STACBinding["mode"]>("dataset");
  const [idProperty, setIDProperty] = useState("");
  const [datetime, setDatetime] = useState("");
  const [constantDate, setConstantDate] = useState("");
  const [asset, setAsset] = useState("");
  const [interval, setInterval] = useState("900");
  const [filter, setFilter] = useState("");
  const [advanced, setAdvanced] = useState("{}");
  const [preview, setPreview] = useState<STACPreview>();
  const [published, setPublished] = useState(false);
  const task = useTask(ws);
  const resource = resources.data?.resources.find((r) => r.id === resourceID);
  const target = collections.find((c) => c.document.id === collection);
  const fields = resource
    ? Array.from(
        new Set(
          [
            resource.id_property,
            ...(resource.properties ?? []).map((p) => p.name),
          ].filter((s): s is string => Boolean(s)),
        ),
      )
    : [];
  const changed = () => {
    setPreview(undefined);
    setPublished(false);
  };
  const selectTarget = (id: string) => {
    setCollection(id);
    changed();
    setDatetime("");
    setConstantDate("");
    setAsset("");
    const b = collections.find((c) => c.document.id === id)?.binding;
    if (b) {
      setResourceID(b.resource_id);
      setMode(b.mode);
      setInterval(String(b.refresh_interval_sec));
      setFilter(b.filter ?? "");
      setIDProperty(b.mapping.id_property ?? "");
      setAdvanced(pretty(b.mapping));
    } else {
      setResourceID("");
      setMode("dataset");
      setInterval("900");
      setFilter("");
      setIDProperty("");
      setAdvanced("{}");
    }
  };
  const request = () => {
    if (!target || !resource)
      throw new Error("Select a Collection and a published resource.");
    const mapping: STACMapping = JSON.parse(advanced) as STACMapping;
    if (mode === "mapped") {
      mapping.id_property = idProperty;
      if (asset)
        mapping.assets = {
          ...mapping.assets,
          data: { href: { property: asset }, roles: ["data"] },
        };
    }
    if (datetime) mapping.datetime = { property: datetime };
    else if (constantDate) mapping.datetime = { constant: constantDate };
    return {
      collection_id: collection,
      revision: target.revision,
      binding: {
        service_id: resource.service_id,
        resource_id: resource.id,
        resource_kind: resource.kind as STACBinding["resource_kind"],
        mode,
        mapping,
        filter,
        refresh_interval_sec: Number(interval),
      },
    };
  };
  return (
    <form
      className={panel}
      onSubmit={(e) => {
        e.preventDefault();
        task.mutate(async () => {
          setPreview(await api.stacPreviewNewBinding(ws, request()));
        });
      }}
    >
      <h2 className="font-semibold">Publish existing data</h2>
      <p className="text-sm text-muted-foreground">
        Create a Collection first, then select a resource from this workspace.
        Preview the mapping before publishing.
      </p>
      <QueryError error={resources.error} retry={resources.refetch} />
      <div className="grid gap-4 sm:grid-cols-2">
        <CollectionSelect
          collections={collections}
          value={collection}
          onChange={selectTarget}
        />
        <Field label="Published layer or coverage">
          <NativeSelect
            className="w-full"
            value={resourceID}
            onChange={(e) => {
              setResourceID(e.target.value);
              setMode("dataset");
              setIDProperty("");
              setDatetime("");
              setConstantDate("");
              setAsset("");
              setAdvanced("{}");
              changed();
            }}
          >
            <option value="">Select a resource</option>
            {resources.data?.resources.map((r) => (
              <option key={r.id} value={r.id}>
                {r.title || r.public_id} · {r.provider}
              </option>
            ))}
          </NativeSelect>
        </Field>
        <Field label="Publication mode">
          <NativeSelect
            className="w-full"
            value={mode}
            onChange={(e) => {
              setMode(e.target.value as STACBinding["mode"]);
              changed();
            }}
          >
            {(resource?.modes ?? ["dataset"]).map((m) => (
              <option key={m} value={m}>
                {m === "dataset"
                  ? "Dataset discovery (Collection only)"
                  : m === "mapped"
                    ? "Mapped asset records (one Item per row)"
                    : "Raster assets (one Item per file or granule)"}
              </option>
            ))}
          </NativeSelect>
        </Field>
        <Field label="Refresh interval (seconds)">
          <Input
            type="number"
            min={60}
            required
            value={interval}
            onChange={(e) => {
              setInterval(e.target.value);
              changed();
            }}
          />
        </Field>
      </div>
      {mode === "dataset" ? (
        <p className="text-sm text-muted-foreground">
          This Collection describes the dataset and links to its enabled
          services. It will not contain synthetic Items or generate downloadable
          snapshots.
        </p>
      ) : (
        <div className="grid gap-4 sm:grid-cols-2">
          {mode === "mapped" && (
            <>
              <PropertySelect
                label="Stable unique Item ID property"
                fields={fields}
                value={idProperty}
                onChange={(v) => {
                  setIDProperty(v);
                  changed();
                }}
              />
              <PropertySelect
                label="Asset URL property"
                fields={fields}
                value={asset}
                onChange={(v) => {
                  setAsset(v);
                  changed();
                }}
              />
              <PropertySelect
                label="Acquisition datetime property"
                fields={fields}
                value={datetime}
                onChange={(v) => {
                  setDatetime(v);
                  changed();
                }}
              />
            </>
          )}
          <Field label="Constant acquisition datetime (optional)">
            <Input
              value={constantDate}
              placeholder="2026-01-01T00:00:00Z"
              onChange={(e) => {
                setConstantDate(e.target.value);
                changed();
              }}
            />
          </Field>
          <Field label="Source filter (CQL2, optional)">
            <Input
              value={filter}
              onChange={(e) => {
                setFilter(e.target.value);
                changed();
              }}
            />
          </Field>
        </div>
      )}
      {mode !== "dataset" && (
        <details>
          <summary className="cursor-pointer text-sm font-medium">
            Additional assets, time intervals and properties
          </summary>
          <div className="mt-3">
            <JSONField
              label="Additional mapping JSON"
              value={advanced}
              onChange={(v) => {
                setAdvanced(v);
                changed();
              }}
            />
            <p className="mt-2 text-xs text-muted-foreground">
              Map values with {`{"property":"column_name"}`} or{" "}
              {`{"constant":"value"}`}. Use start_datetime and end_datetime for
              time intervals. Core validation runs on every source record before
              publication.
            </p>
          </div>
        </details>
      )}
      <QueryError
        error={task.error}
        context="Publication could not be completed"
      />
      <div className="flex gap-2">
        <Button
          type="submit"
          variant="outline"
          disabled={task.isPending || !resource || !target}
        >
          Preview publication
        </Button>
        <Button
          type="button"
          disabled={task.isPending || !preview || published}
          onClick={() =>
            task.mutate(async () => {
              const input = request();
              if (target?.binding?.id)
                await api.stacUpdateBinding(ws, target.binding.id, input);
              else await api.stacCreateBinding(ws, input);
              setPublished(true);
            })
          }
        >
          Publish
        </Button>
      </div>
      {published && (
        <p role="status" className="text-sm">
          Publication queued. Follow progress in Jobs. The previous publication
          stays available until validation finishes.
        </p>
      )}
      {preview && (
        <JSONField
          label="Preview (up to 10 Items)"
          value={pretty(preview)}
          readOnly
        />
      )}
    </form>
  );
}
function PropertySelect({
  label,
  fields,
  value,
  onChange,
}: {
  label: string;
  fields: string[];
  value: string;
  onChange: (v: string) => void;
}) {
  return (
    <Field label={label}>
      <NativeSelect
        className="w-full"
        value={value}
        onChange={(e) => onChange(e.target.value)}
      >
        <option value="">Select a property</option>
        {fields.map((field) => (
          <option key={field} value={field}>
            {field}
          </option>
        ))}
      </NativeSelect>
    </Field>
  );
}

function ImportMetadata({ ws, collections }: Common) {
  const [collection, setCollection] = useState("");
  const [file, setFile] = useState<File>();
  const [baseURL, setBaseURL] = useState("");
  const [job, setJob] = useState<STACJob>();
  const [preview, setPreview] = useState<STACPreview>();
  const [upsert, setUpsert] = useState(false);
  const task = useTask(ws);
  return (
    <form
      className={panel}
      onSubmit={(e) => {
        e.preventDefault();
        if (!file) return;
        task.mutate(async () => {
          const created = await api.stacImport(ws, file, {
            collection_id: collection,
            base_url: baseURL || undefined,
          });
          setJob(created);
          setPreview(await api.stacPreviewImport(ws, created.id));
        });
      }}
    >
      <h2 className="font-semibold">Import STAC metadata</h2>
      <p className="text-sm text-muted-foreground">
        Upload Item or Collection JSON, a FeatureCollection, or NDJSON. Asset
        URLs reference existing data. Imported records stay unpublished until
        you review and publish them.
      </p>
      <CollectionSelect
        collections={collections}
        value={collection}
        standalone
        onChange={(v) => {
          setCollection(v);
          setJob(undefined);
          setPreview(undefined);
        }}
      />
      <Field label="Metadata file">
        <Input
          type="file"
          accept=".json,.geojson,.ndjson,.jsonl,application/json"
          required
          onChange={(e) => {
            setFile(e.target.files?.[0]);
            setJob(undefined);
            setPreview(undefined);
          }}
        />
      </Field>
      <Field label="Base URL for relative asset links (optional)">
        <Input
          type="url"
          value={baseURL}
          placeholder="https://data.example.org/scenes/"
          onChange={(e) => setBaseURL(e.target.value)}
        />
      </Field>
      <QueryError error={task.error} context="Import could not be completed" />
      <Button type="submit" disabled={task.isPending || !file || !collection}>
        {task.isPending ? "Validating…" : "Upload and validate"}
      </Button>
      {job && (
        <div className="space-y-4 border-t pt-4">
          <p role="status" className="text-sm">
            {job.processed.toLocaleString()} records validated · {job.status}
          </p>
          {preview && (
            <JSONField
              label="Import preview"
              value={pretty(preview)}
              readOnly
            />
          )}
          <label className="flex items-center gap-2 text-sm">
            <input
              type="checkbox"
              checked={upsert}
              onChange={(e) => setUpsert(e.target.checked)}
            />
            Replace existing Items with matching IDs (upsert)
          </label>
          <div className="flex gap-2">
            <Button
              type="button"
              disabled={task.isPending || job.status !== "ready"}
              onClick={() =>
                task.mutate(async () => {
                  setJob(await api.stacPublishImport(ws, job.id, { upsert }));
                })
              }
            >
              Publish import
            </Button>
            <Button
              type="button"
              variant="outline"
              disabled={task.isPending || job.status === "succeeded"}
              onClick={() =>
                task.mutate(async () => {
                  await api.stacCancelImport(ws, job.id);
                  setJob(undefined);
                  setPreview(undefined);
                })
              }
            >
              Cancel import
            </Button>
          </div>
        </div>
      )}
    </form>
  );
}

function Jobs({ ws }: { ws: string }) {
  const query = useQuery({
    queryKey: ["stac", ws, "jobs"],
    queryFn: () => api.stacListJobs(ws),
    refetchInterval: 5000,
  });
  const task = useTask(ws);
  return (
    <div className="space-y-4">
      <h2 className="font-semibold">Publication and import jobs</h2>
      <p className="text-sm text-muted-foreground">
        Refresh failures leave the last successful publication available. Jobs
        update every five seconds.
      </p>
      <QueryError error={query.error || task.error} retry={query.refetch} />
      {query.isPending && <p role="status">Loading jobs…</p>}
      {query.data?.jobs.length === 0 && (
        <p className="text-sm">No STAC jobs in this workspace.</p>
      )}
      {query.data?.jobs.map((j) => (
        <article key={j.id} className={panel}>
          <div className="flex flex-wrap items-center justify-between gap-3">
            <div>
              <h3 className="font-medium">
                {j.collection_id} · {j.kind}
              </h3>
              <p className="text-sm">
                {j.status} · {j.processed.toLocaleString()} records ·{" "}
                {formatDateTime(j.updated_at)}
              </p>
            </div>
            <div className="flex gap-2">
              {j.status === "failed" && j.kind === "refresh" && (
                <Button
                  variant="outline"
                  disabled={task.isPending}
                  onClick={() => task.mutate(() => api.stacRetryJob(ws, j.id))}
                >
                  Retry
                </Button>
              )}
              {!["succeeded", "cancelled"].includes(j.status) && (
                <Button
                  variant="outline"
                  disabled={task.isPending}
                  onClick={() => task.mutate(() => api.stacCancelJob(ws, j.id))}
                >
                  Cancel
                </Button>
              )}
              {j.status === "ready" && (
                <Button
                  disabled={task.isPending}
                  onClick={() =>
                    task.mutate(() =>
                      api.stacPublishImport(ws, j.id, { upsert: false }),
                    )
                  }
                >
                  Publish validated import
                </Button>
              )}
            </div>
          </div>
          {j.error && (
            <p className="break-words text-sm text-destructive">{j.error}</p>
          )}
          <details>
            <summary className="cursor-pointer text-xs">Job details</summary>
            <pre className="mt-2 overflow-auto text-xs">{pretty(j)}</pre>
          </details>
        </article>
      ))}
    </div>
  );
}

function Items({ ws, collections }: Common) {
  const [collection, setCollection] = useState("");
  const [datetime, setDatetime] = useState("");
  const [bbox, setBBox] = useState("");
  const [ids, setIDs] = useState("");
  const [result, setResult] = useState<ItemPage>();
  const [query, setQuery] = useState<STACSearch>();
  const [map, setMap] = useState(false);
  const [item, setItem] = useState<string>();
  const [editingID, setEditingID] = useState<string>();
  const task = useTask(ws);
  const selected = collections.find((c) => c.document.id === collection);
  const run = async (token?: string) => {
    const next: STACSearch =
      token && query
        ? { ...query, token }
        : {
            collections: collection ? [collection] : undefined,
            datetime: datetime || undefined,
            bbox: bbox ? bbox.split(",").map(Number) : undefined,
            ids: ids ? ids.split(",").map((v) => v.trim()) : undefined,
            limit: 100,
          };
    setResult(await api.stacSearch(ws, next));
    setQuery(next);
  };
  return (
    <div className="space-y-5">
      <form
        className={panel}
        onSubmit={(e) => {
          e.preventDefault();
          task.mutate(() => run());
        }}
      >
        <h2 className="font-semibold">Search Items</h2>
        <div className="grid gap-4 sm:grid-cols-2">
          <CollectionSelect
            collections={collections}
            value={collection}
            onChange={(v) => {
              setCollection(v);
              setItem(undefined);
            }}
          />
          <Field label="Datetime or interval">
            <Input
              value={datetime}
              onChange={(e) => setDatetime(e.target.value)}
              placeholder="2026-01-01T00:00:00Z/.."
            />
          </Field>
          <Field label="Bounding box (west, south, east, north)">
            <Input
              value={bbox}
              onChange={(e) => setBBox(e.target.value)}
              placeholder="-180,-90,180,90"
            />
          </Field>
          <Field label="Item IDs (comma separated)">
            <Input value={ids} onChange={(e) => setIDs(e.target.value)} />
          </Field>
        </div>
        <QueryError
          error={task.error}
          context="Item operation could not be completed"
        />
        <div className="flex flex-wrap gap-2">
          <Button type="submit" disabled={task.isPending}>
            Search
          </Button>
          <Button
            type="button"
            variant="outline"
            onClick={() => setMap((v) => !v)}
          >
            {map ? "Hide map" : "Show footprints"}
          </Button>
          <Button
            type="button"
            variant="outline"
            disabled={!selected || Boolean(selected.binding)}
            onClick={() => {
              setEditingID(undefined);
              setItem(
                pretty({
                  type: "Feature",
                  stac_version: "1.1.0",
                  id: "",
                  collection,
                  geometry: null,
                  properties: { datetime: "" },
                  assets: {},
                  links: [],
                }),
              );
            }}
          >
            New Item
          </Button>
        </div>
      </form>
      {map && (
        <Suspense fallback={<p role="status">Loading footprint map…</p>}>
          <FootprintMap
            items={result?.items ?? []}
            onBounds={(value) => setBBox(value.join(","))}
          />
        </Suspense>
      )}
      {result && (
        <div className={panel}>
          <p role="status" className="text-sm">
            {result.items.length} Items on this page
            {result.items.length === 0
              ? ". Dataset-only Collections have no Items."
              : "."}
          </p>
          {result.items.map((d) => (
            <div
              key={`${d.collection}/${d.id}`}
              className="flex flex-wrap items-center justify-between gap-2 border-b py-3"
            >
              <div>
                <p className="font-medium">{text(d.id)}</p>
                <p className="text-xs text-muted-foreground">
                  {text(d.collection)}
                </p>
              </div>
              <Button
                variant="outline"
                onClick={() => {
                  setCollection(text(d.collection));
                  setEditingID(text(d.id));
                  setItem(pretty(d));
                }}
              >
                Inspect Item
              </Button>
            </div>
          ))}
          <Button
            variant="outline"
            disabled={!result.next_token || task.isPending}
            onClick={() => task.mutate(() => run(result.next_token))}
          >
            Next page
          </Button>
        </div>
      )}
      {item !== undefined && (
        <form
          className={panel}
          onSubmit={(e) => {
            e.preventDefault();
            task.mutate(async () => {
              const d = JSON.parse(item) as STACDocument;
              if (editingID)
                await api.stacUpdateItem(ws, collection, editingID, d);
              else await api.stacCreateItem(ws, collection, d);
              setItem(undefined);
              await run();
            });
          }}
        >
          <JSONField
            label="Item JSON"
            value={item}
            onChange={setItem}
            readOnly={false}
          />
          {selected?.binding && (
            <p className="text-sm text-muted-foreground">
              This Item is generated from its source. Update the publication
              mapping to change source-owned fields.
            </p>
          )}
          <div className="flex gap-2">
            <Button disabled={task.isPending} type="submit">
              Save Item
            </Button>
            {editingID && !selected?.binding && (
              <Button
                type="button"
                variant="outline"
                onClick={async () => {
                  if (
                    await confirmAction({
                      title: `Delete Item ${editingID}?`,
                      confirmLabel: "Delete Item",
                      destructive: true,
                    })
                  )
                    task.mutate(async () => {
                      await api.stacDeleteItem(ws, collection, editingID);
                      setItem(undefined);
                      await run();
                    });
                }}
              >
                Delete Item
              </Button>
            )}
            <Button
              type="button"
              variant="outline"
              onClick={() => setItem(undefined)}
            >
              Close
            </Button>
          </div>
        </form>
      )}
      <LocalAssets ws={ws} collections={collections} />
    </div>
  );
}
function LocalAssets({ ws, collections }: Common) {
  const [collection, setCollection] = useState("");
  const [itemID, setItemID] = useState("");
  const [key, setKey] = useState("data");
  const [path, setPath] = useState("");
  const [type, setType] = useState("application/octet-stream");
  const [whole, setWhole] = useState(false);
  const task = useTask(ws);
  return (
    <details className={panel}>
      <summary className="cursor-pointer text-sm font-medium">
        Authorize an existing local asset
      </summary>
      <form
        className="space-y-4"
        onSubmit={(e) => {
          e.preventDefault();
          task.mutate(() =>
            api.stacBindAsset(ws, {
              collection_id: collection,
              item_id: itemID || undefined,
              key,
              path,
              media_type: type,
              whole_file_authorized: whole,
            }),
          );
        }}
      >
        <p className="text-sm text-muted-foreground">
          Files must be allowed by the server. Arbitrary files and containers
          require a super administrator. No data is copied.
        </p>
        <CollectionSelect
          collections={collections}
          value={collection}
          onChange={setCollection}
        />
        <div className="grid gap-4 sm:grid-cols-2">
          <Field label="Item ID (empty for a Collection asset)">
            <Input value={itemID} onChange={(e) => setItemID(e.target.value)} />
          </Field>
          <Field label="Asset key">
            <Input
              required
              value={key}
              onChange={(e) => setKey(e.target.value)}
            />
          </Field>
          <Field label="Existing file path">
            <Input
              required
              value={path}
              onChange={(e) => setPath(e.target.value)}
            />
          </Field>
          <Field label="Media type">
            <Input
              required
              value={type}
              onChange={(e) => setType(e.target.value)}
            />
          </Field>
        </div>
        <label className="flex items-center gap-2 text-sm">
          <input
            required
            type="checkbox"
            checked={whole}
            onChange={(e) => setWhole(e.target.checked)}
          />
          The entire file is authorized for readers of this Collection.
        </label>
        <QueryError
          error={task.error}
          context="Asset binding could not be saved"
        />
        {task.isSuccess && (
          <p role="status" className="text-sm">
            Asset binding saved.
          </p>
        )}
        <div className="flex gap-2">
          <Button disabled={task.isPending || !collection} type="submit">
            Bind local asset
          </Button>
          <Button
            variant="outline"
            type="button"
            disabled={task.isPending || !collection}
            onClick={() =>
              task.mutate(() =>
                api.stacUnbindAsset(ws, {
                  collection_id: collection,
                  item_id: itemID || undefined,
                  key,
                }),
              )
            }
          >
            Remove binding
          </Button>
        </div>
      </form>
    </details>
  );
}

function Settings({ ws }: { ws: string }) {
  const settings = useQuery({
    queryKey: ["stac", ws, "settings"],
    queryFn: () => api.getSTACSettings(ws),
  });
  return (
    <div>
      <QueryError error={settings.error} retry={settings.refetch} />
      {settings.isPending && <p role="status">Loading settings…</p>}
      {settings.data && (
        <SettingsForm
          key={pretty(settings.data)}
          ws={ws}
          settings={settings.data}
        />
      )}
    </div>
  );
}
function SettingsForm({
  ws,
  settings,
}: {
  ws: string;
  settings: STACSettings;
}) {
  const [draft, setDraft] = useState(settings);
  const task = useTask(ws);
  const { config } = useAuth();
  const base = `${config?.url_base ?? ""}${config?.base_path ?? ""}/workspaces/${encodeURIComponent(ws)}/stac`;
  return (
    <form
      className={panel}
      onSubmit={(e) => {
        e.preventDefault();
        task.mutate(() => api.putSTACSettings(ws, draft));
      }}
    >
      <h2 className="font-semibold">Workspace STAC settings</h2>
      <label className="flex items-center gap-2 text-sm">
        <input
          type="checkbox"
          checked={draft.enabled}
          onChange={(e) => setDraft({ ...draft, enabled: e.target.checked })}
        />
        Enable this workspace catalog
      </label>
      <label className="flex items-center gap-2 text-sm">
        <input
          type="checkbox"
          checked={draft.public}
          onChange={(e) => setDraft({ ...draft, public: e.target.checked })}
        />
        Allow public access (Collection visibility still applies)
      </label>
      <Field label="Catalog title">
        <Input
          value={draft.title ?? ""}
          onChange={(e) => setDraft({ ...draft, title: e.target.value })}
        />
      </Field>
      <Field label="Catalog description">
        <Textarea
          value={draft.description ?? ""}
          onChange={(e) => setDraft({ ...draft, description: e.target.value })}
        />
      </Field>
      <QueryError error={task.error} context="Settings could not be saved" />
      <Button disabled={task.isPending} type="submit">
        Save settings
      </Button>
      <div className="space-y-2 border-t pt-4">
        <h3 className="text-sm font-medium">Client access</h3>
        <a
          className="break-all text-sm underline"
          href={`${base}/`}
          target="_blank"
          rel="noreferrer"
        >
          {base}/
        </a>
        <pre className="overflow-auto rounded-lg bg-muted p-3 text-xs">{`from pystac_client import Client\ncatalog = Client.open(${JSON.stringify(base + "/")})\nitems = catalog.search(bbox=[-180, -90, 180, 90]).items()`}</pre>
        <p className="text-xs text-muted-foreground">
          Use an API key or bearer token for private catalogs. External asset
          storage applies its own access rules.
        </p>
      </div>
    </form>
  );
}
