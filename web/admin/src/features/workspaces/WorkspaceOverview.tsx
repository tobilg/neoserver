import { getGetAuthMeQueryKey } from "@/api/generated/authentication/authentication";
import { useState } from "react";
import { flushSync } from "react-dom";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Pencil } from "lucide-react";
import { Link, useNavigate, useParams } from "react-router";
import {
  useGetWorkspace,
  useGetWorkspaceSummary,
  updateWorkspace,
  getGetWorkspaceQueryKey,
  getGetWorkspaceSummaryQueryKey,
  getListWorkspacesQueryKey,
} from "@/api/generated/workspaces/workspaces";
import { Page } from "@/components/Page";
import { QueryError } from "@/components/QueryError";
import { StatusChip } from "@/components/StatusChip";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
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
import { POLL_SUMMARY_MS } from "@/hooks/use-active-polling";
import { useUnsavedChangesGuard } from "@/lib/forms";
import { GettingStarted } from "./GettingStarted";
import { confirmDiscard } from "@/lib/confirm";
import { useAuth } from "@/auth/auth-context";
import { RecentlyPublished } from "./RecentlyPublished";
import { ServiceState } from "./service-state";
import { SERVICES, serviceState } from "./services";

export function WorkspaceOverview() {
  const { ws = "" } = useParams();
  const navigate = useNavigate();
  const client = useQueryClient();
  const [editing, setEditing] = useState(false);
  const [onboardingDirty, setOnboardingDirty] = useState(false);
  const [name, setName] = useState(ws);
  const [description, setDescription] = useState("");
  const workspace = useGetWorkspace(ws);
  const [baseline, setBaseline] = useState({ name: ws, description: "" });
  const dirty =
    editing && (name !== baseline.name || description !== baseline.description);
  useUnsavedChangesGuard(dirty || onboardingDirty);
  const query = useGetWorkspaceSummary(ws, {
    query: { refetchInterval: POLL_SUMMARY_MS },
  });

  const update = useMutation({
    meta: { suppressErrorToast: true },
    mutationFn: () => updateWorkspace(ws, { name, description }),
    onSuccess: async (updated) => {
      flushSync(() => setEditing(false));
      client.setQueryData(getGetWorkspaceQueryKey(ws), updated);
      client.setQueryData(getGetWorkspaceQueryKey(updated.id), updated);
      client.setQueryData(getGetWorkspaceQueryKey(updated.name), updated);
      await Promise.all([
        client.invalidateQueries({ queryKey: getGetAuthMeQueryKey() }),
        client.invalidateQueries({ queryKey: getListWorkspacesQueryKey() }),
        client.invalidateQueries({
          queryKey: getGetWorkspaceSummaryQueryKey(ws),
        }),
        client.invalidateQueries({
          queryKey: getGetWorkspaceQueryKey(updated.name),
        }),
      ]);
      if (updated.name !== ws) {
        await navigate(`/workspaces/${encodeURIComponent(updated.name)}`, {
          replace: true,
        });
        client.removeQueries({
          queryKey: getGetWorkspaceQueryKey(ws),
          exact: true,
          type: "inactive",
        });
      }
    },
  });
  const summary = query.data;
  const { config } = useAuth();
  const publications =
    (summary?.counts?.layers ?? 0) + (summary?.counts?.coverages ?? 0);
  return (
    <Page
      title={ws}
      description={
        workspace.data?.description ||
        "Publication endpoints and active work are the operational center of this workspace."
      }
      action={
        <Button
          size="sm"
          variant="outline"
          disabled={!workspace.data || workspace.isLoading}
          onClick={() => {
            setName(workspace.data?.name ?? ws);
            setDescription(workspace.data?.description ?? "");
            setBaseline({
              name: workspace.data?.name ?? ws,
              description: workspace.data?.description ?? "",
            });
            update.reset();
            setEditing(true);
          }}
        >
          <Pencil /> Edit workspace
        </Button>
      }
    >
      <QueryError
        error={workspace.error || query.error}
        retry={() => {
          void workspace.refetch();
          void query.refetch();
        }}
      />
      {(workspace.isLoading || query.isLoading) && (
        <p role="status">Loading workspace overview…</p>
      )}
      {!query.error && summary && (
        <GettingStarted
          key={ws}
          workspace={ws}
          summary={summary}
          onDirty={setOnboardingDirty}
        />
      )}
      {!query.error && summary && (
        <div className="grid gap-4 lg:grid-cols-12">
          <Card className="lg:col-span-7">
            <CardHeader>
              <CardTitle>Services</CardTitle>
            </CardHeader>
            <CardContent>
              <p className="mb-3 text-sm text-muted-foreground">
                A service answers clients once it is on for this workspace and
                has permitted publications.{" "}
                <Link className="underline" to="endpoints">
                  Check a layer and connect
                </Link>
                .
              </p>
              <ul className="grid gap-px overflow-hidden rounded-md border bg-border sm:grid-cols-2">
                {SERVICES.map(({ key, server, label, env }) => (
                  <li
                    className="flex items-center justify-between gap-2 bg-card p-3"
                    key={key}
                  >
                    <span className="text-sm">{label}</span>
                    <ServiceState
                      state={serviceState(
                        config?.services?.[server] !== false,
                        summary.protocols?.[key] === true,
                        publications,
                      )}
                      env={env}
                    />
                  </li>
                ))}
              </ul>
            </CardContent>
          </Card>
          <RecentlyPublished workspace={ws} className="lg:col-span-5" />
          <nav
            aria-label="Catalog"
            className="grid grid-cols-2 gap-px overflow-hidden rounded-lg border bg-border sm:grid-cols-3 lg:col-span-12 lg:grid-cols-6"
          >
            {(
              [
                ["services", "stores", "Stores", undefined],
                ["imports", "imports", "Imports", "imports"],
                ["layers", "layers", "Layers", undefined],
                ["coverages", "coverages", "Coverages", undefined],
                ["styles", "styles", "Styles", undefined],
                ["api_keys", "api-keys", "API keys", undefined],
              ] as const
            ).map(([name, route, label, job]) => {
              const running = job ? (summary.active_jobs?.[job] ?? 0) : 0;
              return (
                <Link
                  to={route}
                  key={name}
                  className="bg-card p-4 transition-colors hover:bg-muted"
                >
                  <span className="block font-mono text-2xl tabular-nums">
                    {summary.counts?.[name] ?? "—"}
                  </span>
                  <span className="flex items-center gap-2 text-xs text-muted-foreground">
                    {label}
                    {running > 0 && <StatusChip value={`${running} running`} />}
                  </span>
                </Link>
              );
            })}
          </nav>
          {(summary.active_jobs?.tile_cache ?? 0) > 0 && (
            <p className="text-sm lg:col-span-12">
              <Link className="underline" to="caching">
                {summary.active_jobs.tile_cache} tile-cache{" "}
                {summary.active_jobs.tile_cache === 1 ? "job" : "jobs"} running
              </Link>
            </p>
          )}
        </div>
      )}
      <Dialog
        open={editing}
        onOpenChange={async (open) => {
          if (update.isPending) return;
          if (
            !open &&
            dirty &&
            !(await confirmDiscard("Discard unsaved workspace changes?"))
          )
            return;
          setEditing(open);
        }}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Edit workspace</DialogTitle>
            <DialogDescription>
              Workspace administrators can update this catalog container. The
              global workspace list remains super-admin only.
            </DialogDescription>
          </DialogHeader>
          <div className="space-y-4">
            <div>
              <Label htmlFor="overview-workspace-name">Name</Label>
              <Input
                id="overview-workspace-name"
                value={name}
                disabled={update.isPending}
                onChange={(event) => setName(event.target.value)}
              />
            </div>
            <div>
              <Label htmlFor="overview-workspace-description">
                Description
              </Label>
              <Input
                id="overview-workspace-description"
                value={description}
                disabled={update.isPending}
                onChange={(event) => setDescription(event.target.value)}
              />
            </div>
            {name !== ws && (
              <p className="rounded-lg border-l-2 border-warning bg-warning/10 p-3 text-xs">
                Renaming changes every public OGC URL for this workspace.
                Existing client bookmarks will stop working.
              </p>
            )}
          </div>
          <DialogFooter>
            <Button
              disabled={!name.trim() || update.isPending}
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
    </Page>
  );
}
