import { useMemo, useRef, useState, type FormEvent } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import {
  Ban,
  Check,
  Clipboard,
  Eye,
  KeyRound,
  Plus,
  Trash2,
} from "lucide-react";
import { Link, useParams, useSearchParams } from "react-router";
import {
  getListAPIKeysQueryKey,
  createAPIKey,
  useGetAPIKey,
  useListAPIKeys,
  revokeAPIKey,
  deleteAPIKey,
} from "@/api/generated/api-keys/api-keys";
import { useListWorkspaceRoles } from "@/api/generated/roles/roles";
import type {
  APIKey,
  APIKeyWithSecret,
  CreateAPIKeyBody,
} from "@/api/generated/models";
import { Page } from "@/components/Page";
import { DisplayValue, ResourceDetails } from "@/components/ResourceDetails";
import { QueryError } from "@/components/QueryError";
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
import { Checkbox } from "@/components/ui/checkbox";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { DataTable } from "@/components/DataTable";
import { ConfirmDialog } from "@/components/ConfirmDialog";
import { apiKeyState, expiryISO } from "@/lib/expiry";
import { formatDateTime } from "@/lib/format";
import { useExpiryClock } from "@/lib/use-expiry-clock";
import { isAccessError } from "@/lib/query-access";
import type { ColumnDef } from "@tanstack/react-table";

export function APIKeysPage() {
  const { ws = "" } = useParams();
  const [params] = useSearchParams();
  const connectLayer = params.get("connect");
  const returnPath = `/workspaces/${encodeURIComponent(ws)}/endpoints?${new URLSearchParams({ layer: connectLayer ?? "" })}`;
  const endpoint = `/workspaces/${encodeURIComponent(ws)}/apikeys`;
  const client = useQueryClient();
  const [open, setOpen] = useState(false);
  const [secret, setSecret] = useState<APIKeyWithSecret>();
  const [copied, setCopied] = useState(false);
  const [copyError, setCopyError] = useState("");
  // The secret cannot be shown again, so dismissing it is a deliberate act.
  const [stored, setStored] = useState(false);
  const secretElement = useRef<HTMLElement>(null);
  const [revokeTarget, setRevokeTarget] = useState<APIKey>();
  const [deleteTarget, setDeleteTarget] = useState<APIKey>();
  const [expiryError, setExpiryError] = useState("");
  const keys = useListAPIKeys(ws);
  const now = useExpiryClock(
    (keys.data?.api_keys ?? []).map((key) => key.expires_at),
  );
  const rows = useMemo(
    () =>
      (keys.data?.api_keys ?? []).map((key) => ({
        ...key,
        state: apiKeyState(key, now),
      })),
    [keys.data, now],
  );
  const roles = useListWorkspaceRoles(ws);
  const invalidate = () =>
    client.invalidateQueries({ queryKey: getListAPIKeysQueryKey(ws) });

  const create = useMutation({
    meta: { suppressErrorToast: true },
    mutationFn: (body: Record<string, unknown>) =>
      createAPIKey(ws, body as unknown as CreateAPIKeyBody),
    onSuccess: async (created) => {
      setCopied(false);
      setCopyError("");
      setSecret(created);
      setOpen(false);
      await invalidate();
    },
  });
  const revoke = useMutation({
    meta: { suppressErrorToast: true },
    mutationFn: (id: string) => revokeAPIKey(ws, id),
    onSuccess: async () => {
      setRevokeTarget(undefined);
      await invalidate();
    },
  });
  const remove = useMutation({
    meta: { suppressErrorToast: true },
    mutationFn: (key: APIKey) => deleteAPIKey(ws, key.id),
    onSuccess: async (_result, key) => {
      setDeleteTarget(undefined);
      toast.success(`Deleted ${key.name}`);
      await invalidate();
    },
  });
  const columns = useMemo<ColumnDef<APIKey & { state: string }, unknown>[]>(
    () => [
      { id: "name", accessorFn: (key) => key.name, header: () => "Name" },
      {
        id: "prefix",
        accessorFn: (key) => key.key_prefix,
        header: () => "Prefix",
        cell: ({ row }) => (
          <span className="font-mono">{row.original.key_prefix}</span>
        ),
      },
      {
        id: "owner",
        accessorFn: (key) => `${key.owner_name ?? ""} ${key.owner_email ?? ""}`,
        header: () => "Owner",
        cell: ({ row }) => (
          <>
            <div>{row.original.owner_name || "—"}</div>
            <div className="text-xs text-muted-foreground">
              {row.original.owner_email}
            </div>
          </>
        ),
      },
      {
        id: "role",
        accessorFn: (key) => key.role_id,
        header: () => "Role",
        cell: ({ row }) => (
          <span className="font-mono">{row.original.role_id}</span>
        ),
      },
      {
        id: "expires",
        accessorFn: (key) => key.expires_at ?? "",
        header: () => "Expires",
        cell: ({ row }) => (
          <span className="text-xs">
            {row.original.expires_at ? (
              <DisplayValue
                field="expires_at"
                value={row.original.expires_at}
              />
            ) : (
              "Never"
            )}
          </span>
        ),
      },
      {
        id: "state",
        accessorFn: (key) => key.state,
        header: () => "State",
        cell: ({ row }) => <StatusChip value={row.original.state} />,
      },
      {
        id: "actions",
        header: () => <span className="sr-only">Actions</span>,
        enableSorting: false,
        enableHiding: false,
        cell: ({ row }) => (
          <>
            <APIKeyDetailsButton workspace={ws} item={row.original} />
            {row.original.revoked ? (
              <Button
                variant="ghost"
                size="icon-sm"
                title="Delete permanently"
                disabled={remove.isPending}
                aria-label={`Delete ${row.original.name}`}
                onClick={() => {
                  remove.reset();
                  setDeleteTarget(row.original);
                }}
              >
                <Trash2 />
              </Button>
            ) : (
              <Button
                variant="ghost"
                size="icon-sm"
                title="Revoke"
                disabled={revoke.isPending}
                aria-label={`Revoke ${row.original.name}`}
                onClick={() => {
                  revoke.reset();
                  setRevokeTarget(row.original);
                }}
              >
                <Ban />
              </Button>
            )}
          </>
        ),
      },
    ],
    // The mutation handles are stable; only their pending flags vary.
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [endpoint, revoke.isPending, remove.isPending],
  );
  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const data = new FormData(event.currentTarget);
    let expiresAt: string | undefined;
    try {
      expiresAt = expiryISO(String(data.get("expires_at") || ""));
      setExpiryError("");
    } catch (error) {
      setExpiryError((error as Error).message);
      return;
    }
    create.mutate({
      name: data.get("name"),
      owner_name: data.get("owner_name"),
      owner_email: data.get("owner_email"),
      role_id: data.get("role_id"),
      expires_at: expiresAt,
    });
  }
  return (
    <Page
      title="API keys"
      description="Workspace credentials. Secrets are shown exactly once; store them in a secret manager."
      action={
        <Dialog
          open={open}
          onOpenChange={(value) => {
            if (!create.isPending) setOpen(value);
          }}
        >
          <DialogTrigger asChild>
            <Button size="sm">
              <Plus /> Create key
            </Button>
          </DialogTrigger>
          <DialogContent>
            <form onSubmit={submit}>
              <DialogHeader>
                <DialogTitle>Create workspace API key</DialogTitle>
                <DialogDescription>
                  Choose the least privileged role that supports this client.
                </DialogDescription>
              </DialogHeader>
              <div className="grid gap-4 py-5">
                <Field name="name" label="Key name" required />
                <Field name="owner_name" label="Owner name" />
                <Field name="owner_email" label="Owner email" type="email" />
                <div>
                  <Label htmlFor="role_id">Role</Label>
                  <Select name="role_id" defaultValue="viewer">
                    <SelectTrigger
                      id="role_id"
                      className="w-full"
                      aria-describedby="role_id-hint"
                    >
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      {roles.data?.roles?.map((role) => (
                        <SelectItem value={role.id} key={role.id}>
                          {role.name}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                  <p
                    id="role_id-hint"
                    className="mt-1 text-xs text-muted-foreground"
                  >
                    Viewer and editor keys work with services and the API. Only
                    admin can sign in to this console.
                  </p>
                </div>
                <Field
                  name="expires_at"
                  label="Expires at"
                  type="datetime-local"
                />
                <p className="text-xs text-muted-foreground">
                  Uses your local timezone (
                  {Intl.DateTimeFormat().resolvedOptions().timeZone}). Leave
                  blank for no expiration.
                </p>
                {expiryError && (
                  <p role="alert" className="text-sm text-destructive">
                    {expiryError}
                  </p>
                )}
              </div>
              {create.error && (
                <p role="alert" className="text-sm text-destructive">
                  {create.error.message}
                </p>
              )}
              <DialogFooter>
                <Button
                  type="button"
                  variant="outline"
                  disabled={create.isPending}
                  onClick={() => setOpen(false)}
                >
                  Cancel
                </Button>
                <Button disabled={create.isPending}>Create key</Button>
              </DialogFooter>
            </form>
          </DialogContent>
        </Dialog>
      }
    >
      <QueryError error={keys.error} retry={() => keys.refetch()} />
      {connectLayer !== null && (
        <p className="mb-4 rounded-lg border p-3 text-sm">
          Creating credentials for <code>{connectLayer || "your client"}</code>.{" "}
          <Link className="underline" to={returnPath}>
            Return to connection examples
          </Link>
          . The secret is never placed in the URL.
        </p>
      )}
      {!isAccessError(keys.error) &&
        (!keys.error || (keys.data?.api_keys?.length ?? 0) > 0) && (
          <DataTable
            data={rows}
            columns={columns}
            isLoading={keys.isLoading}
            urlKey="keys"
            filterPlaceholder="Filter keys…"
            getRowId={(key, index) => key.id ?? String(index)}
            emptyState="No API keys yet."
          />
        )}
      <ConfirmDialog
        open={Boolean(revokeTarget)}
        onOpenChange={(value) => {
          if (!value) setRevokeTarget(undefined);
        }}
        title={`Revoke ${revokeTarget?.name ?? "API key"}?`}
        description="Clients using this key will immediately lose access. Revocation cannot be undone; create and distribute a replacement key if needed. The revoked key stays listed until you delete it."
        confirmLabel="Revoke key"
        pending={revoke.isPending}
        error={revoke.error}
        onConfirm={() => {
          if (revokeTarget) revoke.mutate(revokeTarget.id);
        }}
      />
      <ConfirmDialog
        open={Boolean(deleteTarget)}
        onOpenChange={(value) => {
          if (!value) setDeleteTarget(undefined);
        }}
        title={`Delete ${deleteTarget?.name ?? "API key"} permanently?`}
        description={`The revoked key and its record are removed. Its prefix (${deleteTarget?.key_prefix ?? ""}) can no longer be matched to a name or owner in logs.`}
        confirmLabel="Delete key"
        pending={remove.isPending}
        error={remove.error}
        onConfirm={() => {
          if (deleteTarget) remove.mutate(deleteTarget);
        }}
      />
      <Dialog
        open={Boolean(secret)}
        onOpenChange={(value) => {
          if (value) return;
          // Escape, the overlay and the close control all land here; none of
          // them may throw away a secret the operator has not stored yet.
          if (!stored) return;
          setSecret(undefined);
          setCopied(false);
          setStored(false);
        }}
      >
        <DialogContent showCloseButton={false}>
          <DialogHeader>
            <DialogTitle className="flex items-center gap-2">
              <KeyRound /> Copy this key now
            </DialogTitle>
            <DialogDescription>
              neoserver stores only its hash. This secret cannot be displayed
              again.
              {connectLayer !== null && (
                <>
                  {" "}
                  Save this key, close this dialog, then use Return to
                  connection examples for {connectLayer || "your client"}.
                </>
              )}
            </DialogDescription>
          </DialogHeader>
          <div className="min-w-0 space-y-1">
            <code
              ref={secretElement}
              tabIndex={0}
              aria-label="One-time API key"
              className="block w-full rounded-md border bg-muted p-3 font-mono text-xs leading-relaxed break-all focus-visible:outline-2 focus-visible:outline-ring"
            >
              {secret?.key}
            </code>
            {secret?.key_prefix && (
              <p className="text-xs text-muted-foreground">
                Listed as <span className="font-mono">{secret.key_prefix}</span>{" "}
                in the API keys table.
              </p>
            )}
          </div>
          {copyError && (
            <p role="alert" className="text-sm text-destructive">
              {copyError}
            </p>
          )}
          <Label
            htmlFor="api-key-stored"
            className="flex items-start gap-2 rounded-md border p-3 font-normal"
          >
            <Checkbox
              id="api-key-stored"
              checked={stored}
              onCheckedChange={(value) => setStored(value === true)}
            />
            <span className="text-sm">
              I have stored this key
              <span className="block text-muted-foreground">
                Closing this dialog is the last chance to copy it.
              </span>
            </span>
          </Label>
          <DialogFooter>
            <Button
              variant="outline"
              onClick={() => {
                if (!secretElement.current) return;
                secretElement.current.focus();
                const range = document.createRange();
                range.selectNodeContents(secretElement.current);
                const selection = window.getSelection();
                selection?.removeAllRanges();
                selection?.addRange(range);
              }}
            >
              Select key
            </Button>
            <Button
              onClick={async () => {
                setCopied(false);
                setCopyError("");
                try {
                  if (!secret?.key || !navigator.clipboard?.writeText)
                    throw new Error("Clipboard unavailable");
                  await navigator.clipboard.writeText(secret.key);
                  setCopied(true);
                } catch {
                  setCopyError(
                    "Could not copy the key. Choose Select key, then press Ctrl+C (Command+C on Mac) or use your device’s Copy action. Save it before closing; it cannot be shown again.",
                  );
                }
              }}
            >
              {copied ? <Check /> : <Clipboard />}
              {copied ? "Copied" : "Copy key"}
            </Button>
            <Button
              variant="secondary"
              disabled={!stored}
              onClick={() => {
                setSecret(undefined);
                setCopied(false);
                setStored(false);
              }}
            >
              Done
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </Page>
  );
}

function APIKeyDetailsButton({
  workspace,
  item,
}: {
  workspace: string;
  item: APIKey;
}) {
  const [open, setOpen] = useState(false);
  const query = useGetAPIKey(workspace, item.id, {
    query: { enabled: open },
  });
  const key = query.data ?? item;
  return (
    <>
      <Button
        variant="ghost"
        size="icon-sm"
        aria-label={`Inspect ${item.name}`}
        onClick={() => setOpen(true)}
      >
        <Eye />
      </Button>
      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{item.name}</DialogTitle>
            <DialogDescription>
              Stored metadata only. The key secret is never returned again.
            </DialogDescription>
          </DialogHeader>
          <QueryError
            error={query.error}
            retry={() => query.refetch()}
            context="Key metadata could not be refreshed"
          />
          <ResourceDetails value={keyDetails(key, workspace)} raw={key} />
        </DialogContent>
      </Dialog>
    </>
  );
}

/** Readable key metadata: state first, identifiers last. */
function keyDetails(key: APIKey, workspace: string) {
  return {
    state: <StatusChip value={apiKeyState(key)} />,
    role: key.role_id,
    key_prefix: key.key_prefix,
    owner: [key.owner_name, key.owner_email].filter(Boolean).join(" · "),
    expires: key.expires_at ? formatDateTime(key.expires_at) : "Never",
    created_at: key.created_at,
    workspace,
    id: key.id,
  };
}

function Field({
  name,
  label,
  type = "text",
  required = false,
}: {
  name: string;
  label: string;
  type?: string;
  required?: boolean;
}) {
  return (
    <div>
      <Label htmlFor={name}>{label}</Label>
      <Input id={name} name={name} type={type} required={required} />
    </div>
  );
}
