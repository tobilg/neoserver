import { useEffect, useRef, useState } from "react";
import { toast } from "sonner";
import { updateLayer } from "@/api/generated/layers/layers";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useParams } from "react-router";
import { ExternalLink, ImagePlus, Plus, Trash2, Upload } from "lucide-react";
import { EditorState } from "@codemirror/state";
import { EditorView, basicSetup } from "codemirror";
import { xml } from "@codemirror/lang-xml";
import { editorTheme } from "./editor-theme";
import { errorLine } from "./style-errors";
import { scrollHorizontally } from "@/lib/keyboard-scroll";
import { apiBase, basePath } from "@/api/client";
import {
  getGetStyleQueryKey,
  getGetStyleAssetUrl,
  getListStyleAssetsQueryKey,
  getListStylesQueryKey,
  putStyleAsset,
  createStyle,
  deleteStyle,
  deleteStyleAsset,
  useGetStyle,
  useListStyleAssets,
  useListStyles,
  useUpdateStyle,
} from "@/api/generated/styles/styles";
import type { CreateStyleBodyFormat } from "@/api/generated/models";
import {
  styleFormats,
  styleGeometries,
  styleTemplate,
  type StyleGeometry,
} from "./style-templates";
import { useAuth } from "@/auth/auth-context";
import { Page } from "@/components/Page";
import { WMSPreview } from "@/components/WMSPreview";
import { QueryError } from "@/components/QueryError";
import { useUnsavedChangesGuard } from "@/lib/forms";
import { previewBounds } from "@/lib/preview-bounds";
import { useCatalogChoices } from "@/features/catalog/use-catalog-choices";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/AppDialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { confirmDiscard } from "@/lib/confirm";
import { NativeSelect } from "@/components/NativeSelect";
import { formatBytes } from "@/lib/format";

export function StyleEditorPage() {
  const { ws = "" } = useParams();
  return <WorkspaceStyleEditor key={ws} ws={ws} />;
}

function WorkspaceStyleEditor({ ws }: { ws: string }) {
  const { config } = useAuth();
  const workspaceHost = useRef<HTMLDivElement>(null);
  const [wide, setWide] = useState(false);
  const [layout, setLayout] = useState("auto");
  const [ratio, setRatio] = useState(50);
  const narrow = !wide || !["auto", "split"].includes(layout);
  useEffect(() => {
    const host = workspaceHost.current;
    if (!host || typeof ResizeObserver === "undefined") return;
    const observer = new ResizeObserver(([entry]) =>
      setWide(entry.contentRect.width >= 900),
    );
    observer.observe(host);
    return () => observer.disconnect();
  }, []);
  const [panel, setPanel] = useState("editor");
  const catalog = useCatalogChoices(ws);
  const client = useQueryClient();
  const editorHost = useRef<HTMLDivElement>(null);
  const editor = useRef<EditorView | null>(null);
  const [selected, setSelected] = useState("");
  const [layer, setLayer] = useState("");
  const [newOpen, setNewOpen] = useState(false);
  const [deleteOpen, setDeleteOpen] = useState(false);
  const [assignOpen, setAssignOpen] = useState(false);
  const [assignLayer, setAssignLayer] = useState("");
  const [newName, setNewName] = useState("");
  const [newFormat, setNewFormat] =
    useState<CreateStyleBodyFormat>("sld_1.0.0");
  const [newGeometry, setNewGeometry] = useState<StyleGeometry>("polygon");
  const [dirty, setDirty] = useState(false);
  const dirtyRef = useRef(false);
  const baseline = useRef("");
  const [revision, setRevision] = useState(0);
  const [appliedSLD, setAppliedSLD] = useState("");
  useUnsavedChangesGuard(dirty || (newOpen && Boolean(newName)));
  async function discardDraft() {
    if (dirty && !(await confirmDiscard("Discard unsaved style changes?")))
      return false;
    if (dirty && editor.current) {
      editor.current.dispatch({
        changes: {
          from: 0,
          to: editor.current.state.doc.length,
          insert: baseline.current,
        },
      });
    }
    dirtyRef.current = false;
    setDirty(false);
    return true;
  }
  const previewGroups = [
    ["Layers", catalog.resources.filter((item) => item.kind === "feature")],
    ["Coverages", catalog.resources.filter((item) => item.kind === "coverage")],
    ["Layer groups", catalog.resources.filter((item) => item.kind === "group")],
  ] as const;
  const styles = useListStyles(ws);
  const activeStyle = selected || styles.data?.styles?.[0]?.name || "";
  const style = useGetStyle(ws, activeStyle, {
    query: { enabled: !!activeStyle },
  });
  const isXML = !style.data?.format || /^(sld_|se_)/.test(style.data.format);
  useEffect(() => {
    if (!editorHost.current || !style.data || dirtyRef.current) return;
    editor.current?.destroy();
    baseline.current = style.data.body ?? style.data.sld_body ?? "";
    editor.current = new EditorView({
      parent: editorHost.current,
      state: EditorState.create({
        doc: baseline.current,
        extensions: [
          basicSetup,
          editorTheme,
          ...(isXML ? [xml()] : []),
          EditorView.updateListener.of((update) => {
            if (update.docChanged) {
              const changed = update.state.doc.toString() !== baseline.current;
              dirtyRef.current = changed;
              setDirty(changed);
            }
          }),
          EditorView.contentAttributes.of({
            tabindex: "0",
            "aria-label": isXML
              ? "SLD XML"
              : `Style source (${style.data.format})`,
          }),
        ],
      }),
    });
  }, [style.data, isXML]);
  useEffect(() => () => editor.current?.destroy(), []);
  const save = useUpdateStyle({
    mutation: {
      meta: { suppressErrorToast: true },
      onSuccess: (updated, variables) => {
        baseline.current = variables.data.body ?? "";
        dirtyRef.current =
          editor.current?.state.doc.toString() !== baseline.current;
        setDirty(dirtyRef.current);
        client.setQueryData(getGetStyleQueryKey(ws, variables.style), updated);
        setAppliedSLD("");
        setRevision((value) => value + 1);
        toast.success(`Saved ${variables.style}`);
      },
    },
  });
  function goToLine(line: number) {
    const view = editor.current;
    if (!view) return;
    const target = view.state.doc.line(
      Math.max(1, Math.min(line, view.state.doc.lines)),
    );
    view.dispatch({
      selection: { anchor: target.from, head: target.to },
      scrollIntoView: true,
    });
    view.focus();
  }
  const saveErrorLine = errorLine(save.error?.detail);
  const storedErrors =
    style.data?.valid === false
      ? (style.data.diagnostics ?? []).map((item) => ({
          message: item.message ?? "",
          line: item.line,
        }))
      : [];
  function saveStyle() {
    save.mutate({
      workspace: ws,
      style: activeStyle,
      data: {
        body: editor.current?.state.doc.toString() ?? "",
      },
    });
  }

  const create = useMutation({
    meta: { suppressErrorToast: true },
    mutationFn: () =>
      createStyle(ws, {
        name: newName.trim(),
        title: newName.trim(),
        format: newFormat,
        body: styleTemplate(newFormat, newGeometry),
      }),
    onSuccess: async (created) => {
      setSelected(created.name);
      setNewName("");
      setNewOpen(false);
      await client.invalidateQueries({ queryKey: getListStylesQueryKey(ws) });
    },
  });

  const remove = useMutation({
    meta: { suppressErrorToast: true },
    mutationFn: () => deleteStyle(ws, activeStyle),
    onSuccess: async () => {
      setSelected("");
      setDeleteOpen(false);
      await client.invalidateQueries({ queryKey: getListStylesQueryKey(ws) });
    },
  });
  const publication = catalog.resources.find(
    (item) => item.public_id === layer,
  );
  const assignment = catalog.publications.find(
    (item) => item.public_id === assignLayer,
  );
  const assign = useMutation({
    meta: { suppressErrorToast: true },
    mutationFn: async () => {
      if (!assignment || !activeStyle || dirty)
        throw new Error("Save the style and choose a layer first.");
      return updateLayer(ws, assignment.service_id, assignment.id, {
        default_style: activeStyle,
      });
    },
    onSuccess: async () => {
      setAssignOpen(false);
      toast.success(`Assigned ${activeStyle} to ${assignment?.public_id}`);
      await catalog.retry();
    },
  });
  const { crs, bbox } = previewBounds(publication?.native_extent);
  const preview = `${basePath}/workspaces/${encodeURIComponent(ws)}/wms?service=WMS&version=1.3.0&request=GetMap&layers=${encodeURIComponent(layer)}&styles=${encodeURIComponent(activeStyle)}&crs=${encodeURIComponent(crs)}&bbox=${bbox}&width=700&height=450&format=image/png`;
  return (
    <Page
      title="Styles"
      description="Edit and validate stored styles, then inspect their WMS portrayal."
      action={
        <div className="flex flex-wrap gap-2">
          {/* The empty state offers templates; otherwise this is the main action. */}
          <Button
            variant={activeStyle ? "default" : "outline"}
            disabled={save.isPending}
            onClick={async () => {
              if (await discardDraft()) setNewOpen(true);
            }}
          >
            <Plus /> New style
          </Button>
        </div>
      }
    >
      {!activeStyle && !styles.isLoading && !styles.error && (
        <div className="mb-4 rounded-lg border p-6">
          <h2 className="font-medium">Create your first style</h2>
          <p className="mt-2 max-w-2xl text-sm text-muted-foreground">
            Start from a geometry template, preview it against a published
            layer, then save and assign it.
          </p>
          <div
            role="group"
            aria-label="Start from a template"
            className="mt-4 flex flex-wrap gap-2"
          >
            {styleGeometries.map((geometry, index) => (
              <Button
                key={geometry}
                variant={index === 0 ? "default" : "outline"}
                disabled={save.isPending}
                onClick={() => {
                  setNewGeometry(geometry);
                  setNewOpen(true);
                }}
              >
                <Plus /> {geometry.charAt(0).toUpperCase() + geometry.slice(1)}{" "}
                style
              </Button>
            ))}
          </div>
        </div>
      )}
      {!activeStyle && (
        <QueryError
          error={styles.error}
          retry={() => styles.refetch()}
          context="Styles could not be loaded"
        />
      )}
      <div ref={workspaceHost} hidden={!activeStyle}>
        <div className="mb-3 space-y-2">
          <div
            role="toolbar"
            aria-label="Style actions"
            className="flex flex-wrap items-center gap-2 rounded-lg border bg-muted/40 p-2"
          >
            <span className="min-w-0 flex-1 truncate font-mono text-sm">
              {activeStyle}
            </span>
            <p role="status" className="text-sm text-muted-foreground">
              {save.isPending
                ? "Saving…"
                : save.error
                  ? "Save failed. Your draft is preserved."
                  : dirty
                    ? "Unsaved changes"
                    : activeStyle
                      ? "Saved"
                      : "No style selected"}
            </p>
            <Button
              variant="ghost"
              size="sm"
              className="text-red-700 dark:text-destructive"
              disabled={!activeStyle || save.isPending}
              onClick={async () => {
                if (await discardDraft()) setDeleteOpen(true);
              }}
            >
              <Trash2 /> Delete
            </Button>
            <Button
              size="sm"
              disabled={!style.data || save.isPending}
              onClick={saveStyle}
            >
              {save.isPending ? "Saving…" : "Save style"}
              {dirty ? " *" : ""}
            </Button>
          </div>
          {save.error && (
            <div
              role="alert"
              className="rounded-lg border border-destructive/40 p-3 text-sm text-red-700 dark:text-destructive"
            >
              <p>
                {[save.error.message, save.error.detail]
                  .filter(Boolean)
                  .join(": ")}
              </p>
              {saveErrorLine && (
                <Button
                  variant="outline"
                  size="sm"
                  className="mt-2"
                  onClick={() => goToLine(saveErrorLine)}
                >
                  Go to line {saveErrorLine}
                </Button>
              )}
            </div>
          )}
          {!save.error && !dirty && storedErrors.length > 0 && (
            <div
              role="alert"
              className="rounded-lg border border-destructive/40 p-3 text-sm text-red-700 dark:text-destructive"
            >
              <p className="font-medium">
                This stored style is invalid and cannot be rendered.
              </p>
              <ul className="mt-1 list-disc pl-5">
                {storedErrors.map((item, index) => (
                  <li key={index}>
                    {item.message}
                    {item.line ? (
                      <Button
                        variant="link"
                        size="sm"
                        className="h-auto px-2 py-0"
                        onClick={() => goToLine(item.line!)}
                      >
                        Go to line {item.line}
                      </Button>
                    ) : null}
                  </li>
                ))}
              </ul>
            </div>
          )}
        </div>
        <div className="mb-3 grid gap-3 md:grid-cols-2">
          <div>
            <Label htmlFor="style-selection">Style</Label>
            <Select
              value={activeStyle}
              disabled={save.isPending}
              onValueChange={async (value) => {
                if (await discardDraft()) {
                  setSelected(value);
                  setAppliedSLD("");
                  save.reset();
                }
              }}
            >
              <SelectTrigger id="style-selection">
                <SelectValue placeholder="Choose a style" />
              </SelectTrigger>
              <SelectContent>
                {styles.data?.styles?.map((item) => (
                  <SelectItem key={item.id} value={item.name}>
                    {item.name}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
          <div>
            <Label htmlFor="preview-layer">Preview layer</Label>
            <NativeSelect
              id="preview-layer"
              className="w-full"
              value={layer}
              onChange={(event) => setLayer(event.target.value)}
            >
              <option value="">
                {catalog.isLoading
                  ? "Loading published layers…"
                  : catalog.resources.length
                    ? "Choose a published layer"
                    : "No published layers yet"}
              </option>
              {previewGroups.map(([label, items]) =>
                items.length ? (
                  <optgroup key={label} label={label}>
                    {items.map((item) => (
                      <option key={item.id} value={item.public_id}>
                        {item.title && item.title !== item.public_id
                          ? `${item.title} (${item.public_id})`
                          : item.public_id}
                      </option>
                    ))}
                  </optgroup>
                ) : null,
              )}
            </NativeSelect>
          </div>
        </div>
        <QueryError
          error={styles.error || style.error}
          retry={() => {
            void styles.refetch();
            void style.refetch();
          }}
        />
        <QueryError
          error={catalog.error}
          retry={catalog.retry}
          context="Preview layer choices could not be loaded"
        />
        <div className="mb-3 flex flex-wrap items-center gap-3">
          <Button
            variant="outline"
            disabled={dirty || !style.data || save.isPending}
            onClick={() => {
              setAssignLayer(
                catalog.publications.find((item) => item.public_id === layer)
                  ?.public_id ?? "",
              );
              assign.reset();
              setAssignOpen(true);
            }}
          >
            Assign to layer
          </Button>
          <Button
            variant="outline"
            disabled={!isXML || !layer || !activeStyle || !config?.services.wms}
            onClick={() => {
              setAppliedSLD(editor.current?.state.doc.toString() ?? "");
              setRevision((value) => value + 1);
              if (narrow) setPanel("preview");
            }}
          >
            Apply preview
          </Button>
          <span className="text-sm text-muted-foreground">
            <strong>
              {appliedSLD
                ? "Preview: applied draft. "
                : "Preview: saved style. "}
            </strong>
            {isXML
              ? "Apply previews the editor draft without saving. Saving refreshes the stored portrayal."
              : `Format: ${style.data?.format}. Save to validate and refresh the preview; draft preview is available for SLD/SE only.`}
          </span>
        </div>
        <div className="mb-3 flex flex-wrap items-center gap-3">
          <Label htmlFor="style-layout">Layout</Label>
          <NativeSelect
            id="style-layout"
            value={layout}
            className="w-full"
            onChange={(event) => {
              setLayout(event.target.value);
              if (
                event.target.value === "editor" ||
                event.target.value === "preview"
              )
                setPanel(event.target.value);
            }}
          >
            <option value="auto">Automatic</option>
            <option value="editor">Editor</option>
            <option value="preview">Preview</option>
            <option value="split" disabled={!wide}>
              Split view
            </option>
          </NativeSelect>
          {!narrow && (
            <label className="flex items-center gap-2 text-sm">
              Editor width
              <input
                type="range"
                min="35"
                max="65"
                value={ratio}
                onChange={(event) => setRatio(Number(event.target.value))}
              />
            </label>
          )}
        </div>
        <Tabs value={panel} onValueChange={setPanel} className="gap-0">
          <TabsList
            aria-label="Style workspace"
            className={narrow ? "mb-2" : "hidden"}
          >
            <TabsTrigger value="editor">
              Editor{dirty ? " (unsaved)" : ""}
            </TabsTrigger>
            <TabsTrigger value="preview">Preview</TabsTrigger>
          </TabsList>
          <div
            className="grid border"
            style={
              narrow
                ? undefined
                : { gridTemplateColumns: `${ratio}% minmax(0,1fr)` }
            }
          >
            <TabsContent
              forceMount
              value="editor"
              tabIndex={-1}
              role={narrow ? "tabpanel" : "region"}
              aria-label={narrow ? undefined : "Style editor"}
              {...(!narrow ? { "aria-labelledby": undefined } : {})}
              className={`min-w-0 overflow-hidden ${narrow ? "data-[state=inactive]:hidden" : "border-r"}`}
            >
              <div ref={editorHost} />
            </TabsContent>
            <TabsContent
              forceMount
              value="preview"
              tabIndex={narrow ? 0 : -1}
              role={narrow ? "tabpanel" : "region"}
              aria-label={narrow ? undefined : "Style preview"}
              {...(!narrow ? { "aria-labelledby": undefined } : {})}
              className={`grid min-h-[360px] min-w-0 place-items-center bg-muted p-4 ${narrow ? "data-[state=inactive]:hidden" : "min-h-[560px]"}`}
            >
              {layer && activeStyle && config?.services.wms ? (
                <WMSPreview
                  url={
                    appliedSLD
                      ? `${preview}&SLD_BODY=${encodeURIComponent(appliedSLD)}`
                      : preview
                  }
                  revision={revision}
                  alt={`WMS preview of ${layer} using ${activeStyle}`}
                />
              ) : (
                <p className="text-sm text-foreground">
                  {!config?.services.wms
                    ? "Live preview requires WMS.Enabled=true; editing and validation remain available."
                    : "Choose a style and enter a layer ID to render a WMS preview."}
                </p>
              )}
            </TabsContent>
          </div>
        </Tabs>
      </div>
      <StyleAssetsPanel
        workspace={ws}
        defaultOpen={Boolean(styles.data?.styles?.length)}
      />
      <Dialog
        open={assignOpen}
        onOpenChange={(value) => {
          if (!assign.isPending) setAssignOpen(value);
        }}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Assign {activeStyle} to a layer</DialogTitle>
            <DialogDescription>
              This changes the layer’s default portrayal. Other publication
              settings remain unchanged.
            </DialogDescription>
          </DialogHeader>
          <div>
            <Label htmlFor="assign-style-layer">Layer</Label>
            <NativeSelect
              id="assign-style-layer"
              className="w-full"
              value={assignLayer}
              onChange={(event) => setAssignLayer(event.target.value)}
            >
              <option value="">Choose a layer</option>
              {catalog.publications.map((item) => (
                <option key={item.id} value={item.public_id}>
                  {item.title || item.public_id} ({item.public_id})
                </option>
              ))}
            </NativeSelect>
            {assignment && (
              <p className="mt-2 text-sm">
                Replace default style{" "}
                <code>{assignment.default_style || "automatic"}</code> on{" "}
                <code>{assignment.public_id}</code> with{" "}
                <code>{activeStyle}</code>?
              </p>
            )}
          </div>
          <QueryError
            error={assign.error}
            context="Style could not be assigned"
          />
          <DialogFooter>
            <Button
              variant="outline"
              disabled={assign.isPending}
              onClick={() => setAssignOpen(false)}
            >
              Cancel
            </Button>
            <Button
              disabled={!assignment || assign.isPending || dirty}
              onClick={() => assign.mutate()}
            >
              Confirm assignment
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
      <Dialog
        open={newOpen}
        onOpenChange={async (value) => {
          if (
            !create.isPending &&
            (value ||
              !newName ||
              (await confirmDiscard("Discard the new style?")))
          )
            setNewOpen(value);
        }}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>New style</DialogTitle>
            <DialogDescription>
              Choose a format and starter, then refine the source in the editor.
            </DialogDescription>
          </DialogHeader>
          <div>
            <Label htmlFor="new-style-name">Style name</Label>
            <Input
              id="new-style-name"
              disabled={create.isPending}
              value={newName}
              onChange={(event) => setNewName(event.target.value)}
              placeholder="blue-polygons"
            />
          </div>
          <div>
            <Label htmlFor="new-style-format">Style format</Label>
            <Select
              value={newFormat}
              disabled={create.isPending}
              onValueChange={(value) => {
                setNewFormat(value as CreateStyleBodyFormat);
                create.reset();
              }}
            >
              <SelectTrigger id="new-style-format">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {styleFormats.map(({ value, label }) => (
                  <SelectItem key={value} value={value}>
                    {label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
          <div>
            <Label htmlFor="new-style-geometry">Starter</Label>
            <Select
              value={newGeometry}
              disabled={create.isPending}
              onValueChange={(value) => {
                setNewGeometry(value as StyleGeometry);
                create.reset();
              }}
            >
              <SelectTrigger id="new-style-geometry">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {styleGeometries.map((value) => (
                  <SelectItem key={value} value={value}>
                    {value}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
          {!/^(sld_|se_)/.test(newFormat) && (
            <p role="note" className="text-sm text-muted-foreground">
              CSS, YSLD and Mapbox styles can be saved here. Rendering requires
              the dynamic-style extension in both server WMS.Extensions and
              workspace WMS settings. Preview these formats after saving;
              unsaved preview is SLD/SE only.
            </p>
          )}
          <DialogFooter>
            <Button
              disabled={!newName.trim() || create.isPending}
              onClick={() => create.mutate()}
            >
              Create style
            </Button>
          </DialogFooter>
          {create.error && (
            <QueryError
              error={create.error}
              context="Style could not be created"
            />
          )}
        </DialogContent>
      </Dialog>
      <Dialog open={deleteOpen} onOpenChange={setDeleteOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Delete {activeStyle}?</DialogTitle>
            <DialogDescription>
              The server refuses deletion while a layer, group, or coverage
              still references this style.
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setDeleteOpen(false)}>
              Cancel
            </Button>
            <Button
              variant="destructive"
              disabled={remove.isPending}
              onClick={() => remove.mutate()}
            >
              Delete style
            </Button>
          </DialogFooter>
          {remove.error && (
            <p className="text-sm text-destructive">{remove.error.message}</p>
          )}
        </DialogContent>
      </Dialog>
    </Page>
  );
}

function StyleAssetsPanel({
  workspace,
  defaultOpen,
}: {
  workspace: string;
  /** Collapsed until the workspace has styles (or assets) to use them in. */
  defaultOpen: boolean;
}) {
  const client = useQueryClient();
  const [file, setFile] = useState<File>();
  const [name, setName] = useState("");
  const assets = useListStyleAssets(workspace);
  const invalidate = () =>
    client.invalidateQueries({
      queryKey: getListStyleAssetsQueryKey(workspace),
    });

  const upload = useMutation({
    meta: { suppressErrorToast: true },
    mutationFn: () => {
      if (!file || !name.trim())
        throw new Error("Choose a file and asset name");
      // The spec declares a single asset content type, so send the file's own
      // type explicitly; the generated client merges request headers last.
      return putStyleAsset(workspace, name.trim(), file, {
        headers: { "Content-Type": file.type || "application/octet-stream" },
      });
    },
    onSuccess: async () => {
      setFile(undefined);
      setName("");
      await invalidate();
    },
  });
  const remove = useMutation({
    meta: { suppressErrorToast: true },
    mutationFn: (asset: string) => deleteStyleAsset(workspace, asset),
    onSuccess: invalidate,
  });
  const count = assets.data?.assets?.length ?? 0;
  return (
    <details
      key={String(defaultOpen || count > 0)}
      open={defaultOpen || count > 0 || undefined}
      className="mt-4 min-w-0 rounded-xl border bg-card"
    >
      <summary className="cursor-pointer px-6 py-4 font-medium">
        Style assets{count ? ` (${count})` : ""}
        <span className="ml-2 text-sm font-normal text-muted-foreground">
          Marker images referenced as asset://name in styles
        </span>
      </summary>
      <div className="min-w-0 px-6 pb-6">
        <div className="mb-4 grid grid-cols-[minmax(0,1fr)] items-end gap-3 md:grid-cols-[1fr_1fr_auto]">
          <div>
            <Label htmlFor="style-asset-name">Asset name</Label>
            <Input
              id="style-asset-name"
              value={name}
              onChange={(event) => setName(event.target.value)}
              placeholder="marker.png"
            />
          </div>
          <div>
            <Label htmlFor="style-asset-file">Image</Label>
            <label
              className="mt-1.5 flex h-9 cursor-pointer items-center gap-2 rounded-md border border-dashed px-3 text-sm text-muted-foreground has-focus-visible:outline-2 has-focus-visible:outline-ring"
              onDragOver={(event) => event.preventDefault()}
              onDrop={(event) => {
                event.preventDefault();
                const dropped = event.dataTransfer.files[0];
                if (!dropped) return;
                setFile(dropped);
                if (!name) setName(dropped.name);
              }}
            >
              <ImagePlus className="size-4 shrink-0" />
              <span className="truncate">
                {file ? file.name : "Choose or drop a PNG, JPEG, GIF or SVG"}
              </span>
              <input
                id="style-asset-file"
                type="file"
                className="sr-only"
                accept="image/png,image/jpeg,image/gif,image/svg+xml"
                onChange={(event) => {
                  const selected = event.target.files?.[0];
                  setFile(selected);
                  if (selected && !name) setName(selected.name);
                }}
              />
            </label>
          </div>
          <Button
            disabled={!file || !name.trim() || upload.isPending}
            onClick={() => upload.mutate()}
          >
            <Upload /> Upload asset
          </Button>
        </div>
        <div className="overflow-x-auto rounded-lg border">
          <Table
            tabIndex={0}
            aria-label="Style assets, scroll to see all columns"
            onKeyDown={(event) =>
              scrollHorizontally(event, event.currentTarget.parentElement)
            }
            className="focus-visible:outline-2 focus-visible:outline-ring focus-visible:-outline-offset-2"
          >
            <TableHeader>
              <TableRow>
                <TableHead>Name</TableHead>
                <TableHead>Type</TableHead>
                <TableHead className="text-right">Bytes</TableHead>
                <TableHead>SHA-256</TableHead>
                <TableHead className="w-20">
                  <span className="sr-only">Actions</span>
                </TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {assets.data?.assets?.map((asset) => (
                <TableRow key={asset.name}>
                  <TableCell className="font-mono">{asset.name}</TableCell>
                  <TableCell>{asset.content_type}</TableCell>
                  <TableCell className="text-right font-mono">
                    {formatBytes(asset.size_bytes)}
                  </TableCell>
                  <TableCell className="max-w-52 truncate font-mono text-xs">
                    {asset.sha256}
                  </TableCell>
                  <TableCell className="text-right">
                    <Button variant="ghost" size="icon-sm" asChild>
                      <a
                        href={`${apiBase}${getGetStyleAssetUrl(workspace, asset.name)}`}
                        target="_blank"
                        rel="noreferrer"
                        aria-label={`Open ${asset.name}`}
                      >
                        <ExternalLink />
                      </a>
                    </Button>
                    <Button
                      variant="ghost"
                      size="icon-sm"
                      aria-label={`Delete ${asset.name}`}
                      disabled={remove.isPending}
                      onClick={() => remove.mutate(asset.name)}
                    >
                      <Trash2 />
                    </Button>
                  </TableCell>
                </TableRow>
              ))}
              {!assets.isLoading &&
                !assets.error &&
                !assets.data?.assets?.length && (
                  <TableRow>
                    <TableCell
                      colSpan={5}
                      className="h-20 text-center text-muted-foreground"
                    >
                      No style assets. Upload an image and reference it as
                      asset://name in SLD.
                    </TableCell>
                  </TableRow>
                )}
            </TableBody>
          </Table>
        </div>
        <QueryError error={assets.error} retry={() => assets.refetch()} />
        {(upload.error || remove.error) && (
          <p className="mt-2 text-sm text-destructive">
            {(upload.error || remove.error)?.message}
          </p>
        )}
      </div>
    </details>
  );
}
