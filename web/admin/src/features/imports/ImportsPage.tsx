import { useEffect, useRef, useState, type FormEvent } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { FileUp, Globe2, Plus, RefreshCw } from "lucide-react";
import {
  Link,
  Navigate,
  useNavigate,
  useParams,
  useSearchParams,
} from "react-router";
import type { ImportListLocationState } from "./ImportDetailPage";
import { useUnsavedChangesGuard } from "@/lib/forms";
import { QueryError } from "@/components/QueryError";
import { apiBase, csrfToken } from "@/api/client";
import type { CreateImportURI, ImportJob } from "@/api/generated/models";
import {
  getListImportsQueryKey,
  createImport,
  useListImports,
} from "@/api/generated/imports/imports";
import {
  isActiveStatus,
  statusSignature,
  useActivePolling,
} from "@/hooks/use-active-polling";
import { Page } from "@/components/Page";
import { useAuth } from "@/auth/auth-context";
import { cn } from "@/lib/utils";
import { formatBytes, formatDateTime, plural } from "@/lib/format";
import { StatusChip } from "@/components/StatusChip";
import { Button } from "@/components/ui/button";
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
import { Progress } from "@/components/ui/progress";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { tabsRootFix, tabsTriggerFix } from "@/lib/tabsCompat";
import { confirmDiscard } from "@/lib/confirm";
import { NativeSelect } from "@/components/NativeSelect";
import { DataTable } from "@/components/DataTable";
import { CursorPager } from "@/components/CursorPager";
import type { ColumnDef } from "@tanstack/react-table";

const listPolling = {
  isActive: (data: { imports: ImportJob[] } | undefined) =>
    Boolean(data?.imports?.some((item) => isActiveStatus(item.status))),
  signature: (data: { imports: ImportJob[] } | undefined) =>
    statusSignature(data?.imports),
};

export function ImportsPage() {
  const { ws = "" } = useParams();
  const [searchParams] = useSearchParams();
  // Job details moved to their own route; keep older links working.
  const legacy = searchParams.get("import");
  if (legacy)
    return (
      <Navigate
        replace
        to={`/workspaces/${encodeURIComponent(ws)}/imports/${encodeURIComponent(legacy)}`}
      />
    );
  return <WorkspaceImports key={ws} ws={ws} />;
}

function WorkspaceImports({ ws }: { ws: string }) {
  const endpoint = `/workspaces/${encodeURIComponent(ws)}/imports`;
  const client = useQueryClient();
  const navigate = useNavigate();
  const [searchParams, setSearchParams] = useSearchParams();
  const cursor = searchParams.get("cursor") ?? "";
  const status = searchParams.get("status") ?? "";
  const search = searchParams.get("search") ?? "";
  const [previousCursors, setPreviousCursors] = useState<string[]>([]);
  const listSearch = (() => {
    const next = new URLSearchParams(searchParams);
    next.delete("new");
    const value = next.toString();
    return value ? `?${value}` : "";
  })();
  const detailPath = (id: string) =>
    `/workspaces/${encodeURIComponent(ws)}/imports/${encodeURIComponent(id)}`;
  const detailState: ImportListLocationState = { listSearch };
  function filterList(name: string, value: string) {
    setPreviousCursors([]);
    setSearchParams(
      (previous) => {
        const next = new URLSearchParams(previous);
        next.delete("cursor");
        if (value) next.set(name, value);
        else next.delete(name);
        return next;
      },
      { replace: true },
    );
  }
  function showCursor(value: string) {
    setSearchParams((previous) => {
      const next = new URLSearchParams(previous);
      if (value) next.set("cursor", value);
      else next.delete("cursor");
      return next;
    });
  }
  const [createOpen, setCreateOpen] = useState(
    () => searchParams.get("new") === "upload",
  );
  const [progress, setProgress] = useState(0);
  const [uploadError, setUploadError] = useState("");
  const [createDirty, setCreateDirty] = useState(false);
  const [uploading, setUploading] = useState(false);
  const [sourceTab, setSourceTab] = useState("upload");
  const { config } = useAuth();
  const uploadLimit = config?.limits?.upload_bytes ?? 0;
  const [uploadFile, setUploadFile] = useState<File | null>(null);
  const [uploadName, setUploadName] = useState("");
  const [nameTouched, setNameTouched] = useState(false);
  const [dragging, setDragging] = useState(false);
  function chooseFile(file: File | null) {
    setUploadFile(file);
    setUploadError("");
    setCreateDirty(true);
    if (file && !nameTouched) setUploadName(importNameFor(file.name));
  }
  useUnsavedChangesGuard((createOpen && createDirty) || uploading);
  // Opening the new job has to wait until the guard has released, otherwise
  // a successful upload navigates into its own "discard changes?" prompt.
  const [createdJob, setCreatedJob] = useState("");
  useEffect(() => {
    if (!createdJob || uploading) return;
    navigate(
      `/workspaces/${encodeURIComponent(ws)}/imports/${encodeURIComponent(createdJob)}`,
      { state: { listSearch } satisfies ImportListLocationState },
    );
  }, [createdJob, uploading, navigate, ws, listSearch]);
  const xhr = useRef<XMLHttpRequest | null>(null);
  const list = useListImports(
    ws,
    cursor || status || search
      ? {
          cursor: cursor || undefined,
          status: status || undefined,
          search: search || undefined,
        }
      : undefined,
    { query: useActivePolling(listPolling) },
  );
  const createURI = useMutation({
    meta: { suppressErrorToast: true },
    mutationFn: (body: Record<string, unknown>) =>
      createImport(ws, body as unknown as CreateImportURI),
    onSuccess: async (job) => {
      setCreateOpen(false);
      setCreateDirty(false);
      await client.invalidateQueries({ queryKey: getListImportsQueryKey(ws) });
      setCreatedJob(job.id);
    },
  });

  const rows = list.data?.imports ?? [];
  const importColumns: ColumnDef<ImportJob, unknown>[] = [
    {
      id: "name",
      header: () => "Name",
      enableSorting: false,
      cell: ({ row: { original: job } }) => (
        <>
          <Link
            className="font-medium underline-offset-4 hover:underline"
            to={detailPath(job.id)}
            state={detailState}
          >
            {job.name}
          </Link>
          <p className="mt-1 font-mono text-xs text-muted-foreground @xl/table:hidden">
            {job.phase} · {formatDateTime(job.updated_at)}
          </p>
        </>
      ),
    },
    {
      id: "status",
      header: () => "State",
      enableSorting: false,
      cell: ({ row }) => <StatusChip value={row.original.status} />,
    },
    {
      id: "phase",
      header: () => "Phase",
      enableSorting: false,
      cell: ({ row }) => (
        <span className="font-mono text-xs">{row.original.phase}</span>
      ),
    },
    {
      id: "source",
      header: () => "Source",
      enableSorting: false,
      cell: ({ row }) => (
        <span className="block max-w-72 truncate font-mono text-xs">
          {row.original.source_locator || row.original.source_kind}
        </span>
      ),
    },
    {
      id: "updated_at",
      header: () => "Updated",
      enableSorting: false,
      cell: ({ row }) => (
        <span className="whitespace-nowrap">
          {formatDateTime(row.original.updated_at)}
        </span>
      ),
    },
  ];
  async function changeCreateOpen(value: boolean) {
    if (uploading || createURI.isPending) return;
    if (
      !value &&
      createDirty &&
      !(await confirmDiscard("Discard the new import?"))
    )
      return;
    setCreateOpen(value);
    if (!value) {
      setCreateDirty(false);
      setUploadFile(null);
      setUploadName("");
      setNameTouched(false);
      setUploadError("");
      setProgress(0);
      if (searchParams.has("new"))
        setSearchParams(
          (previous) => {
            const next = new URLSearchParams(previous);
            next.delete("new");
            return next;
          },
          { replace: true },
        );
    }
  }
  const closeCreate = () => changeCreateOpen(false);
  function upload(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setUploadError("");
    setProgress(0);
    const problem = !uploadFile
      ? "Choose a dataset or archive to upload."
      : uploadLimit && uploadFile.size > uploadLimit
        ? `This file is ${formatBytes(uploadFile.size)}; the server accepts up to ${formatBytes(uploadLimit)}.`
        : !uploadName.trim()
          ? "Enter an import name."
          : !IMPORT_NAME.test(uploadName.trim())
            ? "Use only letters, digits, “_”, “-”, “:” or “.” in the import name."
            : "";
    if (problem || !uploadFile) {
      setUploadError(problem);
      return;
    }
    const form = new FormData();
    form.set("name", uploadName.trim());
    form.set("file", uploadFile);
    const request = new XMLHttpRequest();
    setUploading(true);
    xhr.current = request;
    request.open("POST", `${apiBase}${endpoint}`);
    const csrf = csrfToken();
    if (csrf) request.setRequestHeader("X-CSRF-Token", csrf);
    request.upload.onprogress = (value) =>
      value.lengthComputable &&
      setProgress(Math.round((value.loaded / value.total) * 100));
    request.onerror = () =>
      setUploadError(
        "The upload did not reach neoserver. Check the connection, then retry.",
      );
    request.onload = () => {
      if (request.status >= 200 && request.status < 300) {
        const job = JSON.parse(request.responseText) as ImportJob;
        setCreateOpen(false);
        setCreateDirty(false);
        void client.invalidateQueries({ queryKey: getListImportsQueryKey(ws) });
        setCreatedJob(job.id);
      } else {
        setUploadError(problemMessage(request.responseText));
      }
    };
    request.onloadend = () => setUploading(false);
    request.onabort = () =>
      setUploadError("Upload cancelled. You can retry with the selected file.");
    request.send(form);
  }
  return (
    <Page
      title="Imports"
      description="Acquire, inspect, transform, preview, publish, and diagnose managed vector datasets."
      action={
        <div className="flex gap-2">
          <Button
            variant="outline"
            size="sm"
            onClick={() => void list.refetch()}
          >
            <RefreshCw /> Refresh
          </Button>
          <Dialog
            open={createOpen}
            onOpenChange={(value) => void changeCreateOpen(value)}
          >
            <DialogTrigger asChild>
              <Button size="sm">
                <Plus /> New import
              </Button>
            </DialogTrigger>
            <DialogContent className="max-h-[90vh] overflow-y-auto sm:max-w-xl">
              <DialogHeader>
                <DialogTitle>New managed import</DialogTitle>
                <DialogDescription>
                  Upload a local dataset or let the server acquire an allowed
                  URL/path.
                </DialogDescription>
              </DialogHeader>
              <Tabs
                className={tabsRootFix}
                value={sourceTab}
                onValueChange={async (value) => {
                  if (
                    uploading ||
                    createURI.isPending ||
                    (createDirty &&
                      !(await confirmDiscard(
                        "Discard the current acquisition form?",
                      )))
                  )
                    return;
                  setCreateDirty(false);
                  setSourceTab(value);
                }}
              >
                <TabsList className="grid w-full grid-cols-2">
                  <TabsTrigger value="upload" className={tabsTriggerFix}>
                    <FileUp /> Upload file
                  </TabsTrigger>
                  <TabsTrigger value="uri" className={tabsTriggerFix}>
                    <Globe2 /> URL or path
                  </TabsTrigger>
                </TabsList>
                <TabsContent value="upload">
                  <form className="space-y-4 pt-4" noValidate onSubmit={upload}>
                    <div className="space-y-1">
                      <label
                        htmlFor="file"
                        onDragOver={(event) => {
                          event.preventDefault();
                          setDragging(true);
                        }}
                        onDragLeave={() => setDragging(false)}
                        onDrop={(event) => {
                          event.preventDefault();
                          setDragging(false);
                          chooseFile(event.dataTransfer.files[0] ?? null);
                        }}
                        className={cn(
                          "flex cursor-pointer flex-col items-center gap-1 rounded-lg border border-dashed p-6 text-center text-sm focus-within:outline-2 focus-within:outline-ring",
                          dragging && "border-brand bg-brand/5",
                        )}
                      >
                        <FileUp className="size-6 text-muted-foreground" />
                        <span className="font-medium">
                          {uploadFile
                            ? uploadFile.name
                            : "Drop a file here or choose one"}
                        </span>
                        <span className="text-xs text-muted-foreground">
                          {uploadFile
                            ? formatBytes(uploadFile.size)
                            : "Dataset or archive"}
                        </span>
                        <input
                          id="file"
                          name="file"
                          type="file"
                          aria-label="Dataset or archive"
                          aria-describedby="file-hint"
                          accept=".gpkg,.geojson,.json,.fgb,.parquet,.shp,.zip"
                          className="sr-only"
                          onChange={(event) =>
                            chooseFile(event.target.files?.[0] ?? null)
                          }
                        />
                      </label>
                      <p
                        id="file-hint"
                        className="text-xs text-muted-foreground"
                      >
                        GeoPackage, GeoJSON, FlatGeobuf, GeoParquet, or a zipped
                        Shapefile
                        {uploadLimit
                          ? ` · up to ${formatBytes(uploadLimit)}`
                          : ""}
                        .
                      </p>
                    </div>
                    <div className="space-y-1">
                      <Label htmlFor="name">Import name</Label>
                      <Input
                        id="name"
                        name="name"
                        value={uploadName}
                        placeholder="Filled in from the file name"
                        onChange={(event) => {
                          setNameTouched(true);
                          setCreateDirty(true);
                          setUploadName(event.target.value);
                        }}
                      />
                    </div>
                    {progress > 0 && (
                      <div>
                        <Progress value={progress} />
                        <p className="mt-1 font-mono text-xs">
                          {progress}% uploaded
                        </p>
                      </div>
                    )}
                    {uploadError && (
                      <p role="alert" className="text-sm text-destructive">
                        {uploadError}
                      </p>
                    )}
                    <DialogFooter>
                      {uploading ? (
                        <Button
                          type="button"
                          variant="outline"
                          onClick={() => xhr.current?.abort()}
                        >
                          Cancel upload
                        </Button>
                      ) : (
                        <Button
                          type="button"
                          variant="outline"
                          onClick={() => closeCreate()}
                        >
                          Cancel
                        </Button>
                      )}
                      <Button disabled={uploading}>
                        {uploading ? "Uploading…" : "Upload and inspect"}
                      </Button>
                    </DialogFooter>
                  </form>
                </TabsContent>
                <TabsContent value="uri">
                  <form
                    className="space-y-4 pt-4"
                    onChange={() => setCreateDirty(true)}
                    onSubmit={(event) => {
                      event.preventDefault();
                      const data = new FormData(event.currentTarget);
                      createURI.mutate({
                        name: data.get("name"),
                        source_uri: data.get("source_uri"),
                      });
                    }}
                  >
                    <Field name="name" label="Import name" required />
                    <Field
                      name="source_uri"
                      label="HTTPS URL or allowed server path"
                      required
                      placeholder="https://… or /srv/data/…"
                    />
                    {createURI.error && (
                      <p className="text-sm text-destructive">
                        {createURI.error.message}
                      </p>
                    )}
                    <DialogFooter>
                      <Button disabled={createURI.isPending}>
                        Acquire and inspect
                      </Button>
                    </DialogFooter>
                  </form>
                </TabsContent>
              </Tabs>
            </DialogContent>
          </Dialog>
        </div>
      }
    >
      <QueryError
        error={list.error}
        retry={() => void list.refetch()}
        context="Imports could not be loaded"
      />
      <DataTable
        data={rows}
        columns={importColumns}
        isLoading={list.isLoading}
        serverMode
        urlKey="imports"
        getRowId={(job) => job.id}
        emptyState={
          cursor || status || search
            ? "No imports match this page or filter."
            : "No imports yet. Upload a dataset to begin."
        }
        columnClassNames={{
          name: "whitespace-normal break-words",
          phase: "hidden @xl/table:table-cell",
          source: "hidden @3xl/table:table-cell",
          updated_at: "hidden @xl/table:table-cell",
        }}
        toolbar={
          <>
            <Input
              className="max-w-64"
              aria-label="Search imports"
              placeholder="Filter imports…"
              value={search}
              onChange={(event) => filterList("search", event.target.value)}
            />
            <NativeSelect
              aria-label="Import status"
              className="h-9 w-auto"
              value={status}
              onChange={(event) => filterList("status", event.target.value)}
            >
              <option value="">All imports</option>
              <option value="actionable">Needs attention / in progress</option>
              <option value="ready_to_publish">Ready to publish</option>
              <option value="awaiting_plan">Awaiting plan</option>
              <option value="failed">Failed</option>
              <option value="published">Published</option>
              <option value="cancelled">Cancelled</option>
            </NativeSelect>
          </>
        }
        footer={
          <CursorPager
            busy={list.isFetching}
            summary={
              list.isFetching
                ? "Loading imports…"
                : `${plural(rows.length, "import")} on this page · newest first`
            }
            onNewer={
              cursor
                ? () => {
                    showCursor(previousCursors.at(-1) ?? "");
                    setPreviousCursors((values) => values.slice(0, -1));
                  }
                : undefined
            }
            onOlder={
              list.data?.next_cursor
                ? () => {
                    setPreviousCursors((values) => [...values, cursor]);
                    showCursor(list.data?.next_cursor ?? "");
                  }
                : undefined
            }
          />
        }
      />
    </Page>
  );
}

function problemMessage(raw: string) {
  try {
    const value = JSON.parse(raw) as { message?: string; detail?: string };
    return (
      [value.message, value.detail].filter(Boolean).join(": ") ||
      "Import failed"
    );
  } catch {
    return raw || "Import failed";
  }
}
function Field({
  name,
  label,
  required,
  placeholder,
}: {
  name: string;
  label: string;
  required?: boolean;
  placeholder?: string;
}) {
  return (
    <div>
      <Label htmlFor={name}>{label}</Label>
      <Input
        id={name}
        name={name}
        required={required}
        placeholder={placeholder}
      />
    </div>
  );
}

/** Mirrors the importer's accepted job-name characters. */
const IMPORT_NAME = /^[\p{L}\p{N}_\-:.]{1,128}$/u;

/** Suggests an import name from a file name: "My Roads.gpkg" → "My_Roads". */
function importNameFor(fileName: string) {
  return fileName
    .replace(/\.[^.]+$/, "")
    .replace(/[^\p{L}\p{N}_\-:.]+/gu, "_")
    .replace(/^_+|_+$/g, "")
    .slice(0, 128);
}
