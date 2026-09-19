import { getGetAuthMeQueryKey } from "@/api/generated/authentication/authentication";
import { useEffect, useState } from "react";
import { fieldLabel } from "@/lib/schema-fields";
import { toast } from "sonner";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate } from "react-router";
import { FolderTree, Pencil, Plus, Trash2 } from "lucide-react";
import { useAuth } from "@/auth/auth-context";
import { basePath } from "@/api/client";
import { EmptyState } from "@/components/EmptyState";
import {
  suggestWorkspaceName,
  workspaceNameProblem,
} from "@/lib/workspace-name";
import {
  getListWorkspacesQueryKey,
  createWorkspace,
  deleteWorkspace,
  useListWorkspaces,
  updateWorkspace,
} from "@/api/generated/workspaces/workspaces";
import { useGetWorkspaceDeletionPlan } from "@/api/generated/catalog-lifecycle/catalog-lifecycle";
import type { Workspace } from "@/api/generated/models";
import { Page } from "@/components/Page";
import { QueryError } from "@/components/QueryError";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogDescription,
  DialogTitle,
  DialogTrigger,
} from "@/components/AppDialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { useUnsavedChangesGuard } from "@/lib/forms";
import { confirmDiscard } from "@/lib/confirm";

/** The name field shared by the create and rename dialogs. */
function WorkspaceNameField({
  id,
  value,
  onChange,
}: {
  id: string;
  value: string;
  onChange: (value: string) => void;
}) {
  const { config } = useAuth();
  const problem = workspaceNameProblem(value);
  const suggestion = problem ? suggestWorkspaceName(value) : "";
  const origin = (config?.url_base || location.origin).replace(/\/$/, "");
  const preview = `${origin}${config?.base_path ?? ""}/workspaces/${value || "<name>"}/ogc`;
  return (
    <div className="space-y-1">
      <Label htmlFor={id}>Name</Label>
      <Input
        id={id}
        value={value}
        placeholder="e.g. city-data"
        autoComplete="off"
        aria-invalid={Boolean(problem)}
        aria-describedby={`${id}-hint`}
        onChange={(event) => onChange(event.target.value)}
      />
      <div id={`${id}-hint`} className="space-y-1 text-xs">
        {problem ? (
          <p className="text-red-700 dark:text-destructive">
            {problem}
            {suggestion && suggestion !== value && (
              <>
                {" "}
                <Button
                  type="button"
                  variant="link"
                  size="sm"
                  className="h-auto p-0 text-xs"
                  onClick={() => onChange(suggestion)}
                >
                  Use “{suggestion}”
                </Button>
              </>
            )}
          </p>
        ) : (
          <p className="text-muted-foreground">
            Lowercase letters, digits and hyphens work best.
          </p>
        )}
        <p className="break-all font-mono text-muted-foreground">
          Public URL: {preview}
        </p>
      </div>
    </div>
  );
}

export function WorkspacesPage() {
  const navigate = useNavigate();
  const client = useQueryClient();
  const [open, setOpen] = useState(false);
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [editing, setEditing] = useState<Workspace | null>(null);
  const [deleting, setDeleting] = useState<Workspace | null>(null);
  const [deleteConfirmation, setDeleteConfirmation] = useState("");
  const query = useListWorkspaces();
  const deletionPlan = useGetWorkspaceDeletionPlan(deleting?.name ?? "", {
    query: { enabled: Boolean(deleting) },
  });

  // Creating, renaming or removing a workspace changes what the signed-in
  // principal can reach, so the identity has to be refreshed alongside the list.
  const invalidateCatalog = () =>
    Promise.all([
      client.invalidateQueries({ queryKey: getListWorkspacesQueryKey() }),
      client.invalidateQueries({ queryKey: getGetAuthMeQueryKey() }),
    ]);

  const create = useMutation({
    mutationFn: () => createWorkspace({ name, description }),
    onSuccess: async (created) => {
      setOpen(false);
      await invalidateCatalog();
      toast.success(`Created workspace ${created.name}`);
      setCreatedName(created.name);
    },
  });
  const [createdName, setCreatedName] = useState("");
  const update = useMutation({
    meta: { suppressErrorToast: true },
    mutationFn: () =>
      updateWorkspace(editing?.name ?? "", { name, description }),
    onSuccess: async () => {
      setEditing(null);
      await invalidateCatalog();
    },
  });
  const remove = useMutation({
    meta: { suppressErrorToast: true },
    mutationFn: () => deleteWorkspace(deleting?.name ?? "", { recurse: true }),
    onSuccess: async (operation) => {
      toast.success(
        operation
          ? `Deletion accepted for workspace ${deleting?.name}`
          : `Deleted workspace ${deleting?.name}`,
      );
      setDeleting(null);
      if (operation?.id)
        navigate(`/deletions?operation=${encodeURIComponent(operation.id)}`);
      await invalidateCatalog();
    },
  });
  const dirty = open
    ? Boolean(name || description)
    : Boolean(
        editing &&
        (name !== editing.name || description !== (editing.description ?? "")),
      );
  useUnsavedChangesGuard(dirty || create.isPending || update.isPending);
  // Navigate once the mutation has settled. This effect must follow the guard
  // so the guard has already released its block when it runs.
  useEffect(() => {
    if (!createdName || create.isPending) return;
    navigate(`/workspaces/${encodeURIComponent(createdName)}`);
  }, [createdName, create.isPending, navigate]);
  const closeEditor = async () => {
    if (create.isPending || update.isPending) return;
    if (dirty && !(await confirmDiscard("Discard unsaved workspace changes?")))
      return;
    setOpen(false);
    setEditing(null);
  };
  return (
    <Page
      title="Workspaces"
      description="Server-scope catalog containers. Names are part of public OGC URLs and should be treated as stable identifiers."
      action={
        <Dialog
          open={open}
          onOpenChange={(value) => (value ? setOpen(true) : closeEditor())}
        >
          <DialogTrigger asChild>
            <Button
              onClick={() => {
                setName("");
                setDescription("");
                create.reset();
              }}
            >
              <Plus /> New workspace
            </Button>
          </DialogTrigger>
          <DialogContent>
            <DialogHeader>
              <DialogTitle>New workspace</DialogTitle>
              <DialogDescription>
                The name becomes part of every public OGC URL for this
                workspace, so treat it as a stable identifier.
              </DialogDescription>
            </DialogHeader>
            <div className="space-y-4">
              <WorkspaceNameField
                id="workspace-name"
                value={name}
                onChange={setName}
              />
              <div>
                <Label htmlFor="workspace-description">Description</Label>
                <Input
                  id="workspace-description"
                  value={description}
                  onChange={(event) => setDescription(event.target.value)}
                />
              </div>
            </div>
            <DialogFooter>
              <Button
                variant="outline"
                disabled={create.isPending}
                onClick={closeEditor}
              >
                Cancel
              </Button>
              <Button
                disabled={
                  !name ||
                  Boolean(workspaceNameProblem(name)) ||
                  create.isPending
                }
                onClick={() => create.mutate()}
              >
                Create workspace
              </Button>
            </DialogFooter>
            {create.error && (
              <p role="alert" className="text-sm text-destructive">
                {create.error.message}
              </p>
            )}
          </DialogContent>
        </Dialog>
      }
    >
      <QueryError error={query.error} retry={() => query.refetch()} />
      {query.data && (query.data.workspaces ?? []).length === 0 && (
        <EmptyState
          icon={FolderTree}
          title="Create your first workspace"
          action={
            <>
              <Button
                onClick={() => {
                  setName("");
                  setDescription("");
                  create.reset();
                  setOpen(true);
                }}
              >
                <Plus /> New workspace
              </Button>
              <Button variant="outline" asChild>
                <a
                  href={`${basePath}/api/v1/api.html`}
                  target="_blank"
                  rel="noreferrer"
                >
                  API reference
                </a>
              </Button>
            </>
          }
        >
          <p>
            A workspace groups data stores, published layers, styles and API
            keys. Its name becomes part of every public service URL, for example{" "}
            <code>/workspaces/city-data/ogc</code>.
          </p>
          <p>
            Prefer the API? <code>POST /api/v1/workspaces</code> with{" "}
            <code>{`{"name":"city-data"}`}</code>.
          </p>
        </EmptyState>
      )}
      <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-3">
        {query.data?.workspaces?.map((workspace) => (
          <Card
            className="relative h-full hover:border-primary"
            key={workspace.id}
          >
            <CardContent className="p-5">
              <div className="mb-4 flex items-start justify-between">
                <div>
                  <Link
                    // The stretched link makes the whole card open the workspace.
                    className="text-lg font-semibold after:absolute after:inset-0 hover:text-primary"
                    to={`/workspaces/${encodeURIComponent(workspace.name)}`}
                  >
                    {workspace.name}
                  </Link>
                  <p className="mt-1 text-sm text-muted-foreground">
                    {workspace.description || "No description"}
                  </p>
                </div>
                <div className="relative z-[1] flex items-center gap-1">
                  <Button
                    variant="ghost"
                    size="icon-sm"
                    aria-label={`Edit ${workspace.name}`}
                    onClick={() => {
                      setName(workspace.name);
                      setDescription(workspace.description ?? "");
                      setEditing(workspace);
                      update.reset();
                    }}
                  >
                    <Pencil />
                  </Button>
                  <Button
                    variant="ghost"
                    size="icon-sm"
                    aria-label={`Delete ${workspace.name}`}
                    onClick={() => {
                      setDeleteConfirmation("");
                      setDeleting(workspace);
                    }}
                  >
                    <Trash2 />
                  </Button>
                </div>
              </div>
              <div className="grid grid-cols-3 gap-2 border-t pt-3">
                {Object.entries(workspace.counts ?? {})
                  .slice(0, 6)
                  .map(([name, count]) => (
                    <div key={name}>
                      <span className="block font-mono text-lg">{count}</span>
                      <span className="text-xs text-muted-foreground">
                        {name === "styles" ? "Styles" : fieldLabel(name)}
                      </span>
                    </div>
                  ))}
              </div>
            </CardContent>
          </Card>
        ))}
      </div>
      <Dialog
        open={Boolean(editing)}
        onOpenChange={(value) => !value && closeEditor()}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Edit workspace</DialogTitle>
            <DialogDescription>
              Renaming changes the workspace&apos;s public OGC URLs and breaks
              existing client bookmarks.
            </DialogDescription>
          </DialogHeader>
          <div className="space-y-4">
            <WorkspaceNameField
              id="edit-workspace-name"
              value={name}
              onChange={setName}
            />
            <p className="rounded-lg border-l-2 border-warning bg-warning/10 p-3 text-xs">
              Renaming changes every public OGC URL for this workspace. Existing
              client bookmarks will stop working.
            </p>
            <div>
              <Label htmlFor="edit-workspace-description">Description</Label>
              <Input
                id="edit-workspace-description"
                value={description}
                onChange={(event) => setDescription(event.target.value)}
              />
            </div>
          </div>
          <DialogFooter>
            <Button
              variant="outline"
              disabled={update.isPending}
              onClick={closeEditor}
            >
              Cancel
            </Button>
            <Button
              disabled={
                !name || Boolean(workspaceNameProblem(name)) || update.isPending
              }
              onClick={() => update.mutate()}
            >
              Save workspace
            </Button>
          </DialogFooter>
          {update.error && (
            <p className="text-sm text-destructive">{update.error.message}</p>
          )}
        </DialogContent>
      </Dialog>
      <Dialog
        open={Boolean(deleting)}
        onOpenChange={(value) =>
          !value && !remove.isPending && setDeleting(null)
        }
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Delete {deleting?.name}?</DialogTitle>
            <DialogDescription>
              Review the deletion plan below. Everything it lists is removed,
              and the operation cannot be undone.
            </DialogDescription>
          </DialogHeader>
          <p className="text-sm text-muted-foreground">
            This starts the durable recursive deletion workflow for all owned
            catalog and operational state. Source databases and source files are
            not deleted.
          </p>
          <pre className="max-h-60 overflow-auto rounded-lg border bg-muted p-3 text-xs">
            {JSON.stringify(
              deletionPlan.data ?? { status: "Loading dependency plan…" },
              null,
              2,
            )}
          </pre>
          <div>
            <Label htmlFor="delete-workspace-confirmation">
              Type {deleting?.name} to confirm
            </Label>
            <Input
              id="delete-workspace-confirmation"
              disabled={remove.isPending}
              value={deleteConfirmation}
              onChange={(event) => setDeleteConfirmation(event.target.value)}
              autoComplete="off"
            />
          </div>
          <DialogFooter>
            <Button
              variant="outline"
              disabled={remove.isPending}
              onClick={() => setDeleting(null)}
            >
              Cancel
            </Button>
            <Button
              variant="destructive"
              disabled={
                remove.isPending ||
                deletionPlan.isLoading ||
                Boolean(deletionPlan.error) ||
                deleteConfirmation !== deleting?.name
              }
              onClick={() => remove.mutate()}
            >
              {remove.isPending ? "Accepting deletion…" : "Delete workspace"}
            </Button>
          </DialogFooter>
          {remove.error && (
            <p className="text-sm text-destructive">{remove.error.message}</p>
          )}
          {deletionPlan.error && (
            <p className="text-sm text-destructive">
              {deletionPlan.error.message}
            </p>
          )}
        </DialogContent>
      </Dialog>
    </Page>
  );
}
