import { toast } from "sonner";
import { useState } from "react";
import { RoleDeleteButton } from "./RoleDeleteButton";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Trash2 } from "lucide-react";
import { useAuth } from "@/auth/auth-context";
import { useListWorkspaces } from "@/api/generated/workspaces/workspaces";
import {
  getListRolePoliciesQueryKey,
  addRolePolicy,
  useListRolePolicies,
  removeRolePolicy,
} from "@/api/generated/roles/roles";
import {
  getListBrowserSessionsQueryKey,
  useListBrowserSessions,
  revokeBrowserSession,
} from "@/api/generated/authentication/authentication";
import {
  getListGlobalClaimMappingsQueryKey,
  createClaimMapping,
  createGlobalClaimMapping,
  deleteGlobalClaimMapping,
  useListGlobalClaimMappings,
} from "@/api/generated/claim-mappings/claim-mappings";
import type {
  CreateClaimMappingBody,
  RolePolicy,
} from "@/api/generated/models";
import { Page } from "@/components/Page";
import { StatusChip } from "@/components/StatusChip";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { ObjectEditor } from "@/components/SchemaFields";
import { validateFields } from "@/lib/schema-fields";
import { formSchemas } from "@/lib/resource-schemas";
import { QueryError } from "@/components/QueryError";
import { RowDetails } from "@/components/RowActions";
import { DisplayValue } from "@/components/ResourceDetails";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { ResourcePage } from "@/features/shared/ResourcePage";
import {
  getListRolesQueryKey,
  useCreateRole,
  useListRoles,
} from "@/api/generated/roles/roles";
import type { CreateRoleBody } from "@/api/generated/models";
import { NativeSelect } from "@/components/NativeSelect";
import { formatDateTime } from "@/lib/format";

export function RolesPage() {
  const client = useQueryClient();
  const roles = useListRoles();
  const create = useCreateRole();
  const invalidate = () =>
    client.invalidateQueries({ queryKey: getListRolesQueryKey() });
  return (
    <ResourcePage
      title="Roles & policies"
      description="Roles aggregate additive allow policies. There is no deny rule."
      rows={roles.data?.roles ?? []}
      isLoading={roles.isLoading}
      error={roles.error}
      urlKey="roles"
      columns={["id", "name", "description", "is_system", "created_at"]}
      onRefresh={invalidate}
      createLabel="Create role"
      createTemplate={{ id: "", name: "", description: "" }}
      onCreate={async (body) => {
        await create.mutateAsync({ data: body as unknown as CreateRoleBody });
        await invalidate();
      }}
      // Built-in roles can't be deleted, so they get no action at all.
      renderActions={(row) =>
        row.is_system ? null : (
          <RoleDeleteButton id={String(row.id)} system={false} />
        )
      }
      extraContent={<RolePoliciesPanel />}
    />
  );
}

function policyRequest(policy: string[]) {
  const resource = policy[2] || "";
  if (resource.startsWith("service:")) {
    return {
      workspace: policy[1],
      service: resource.slice("service:".length),
      action: policy[3],
    };
  }
  if (resource.startsWith("operation:")) {
    const [, service, operation] = resource.split(":");
    return { workspace: policy[1], service, operation, action: policy[3] };
  }
  return null;
}

function RolePoliciesPanel() {
  const client = useQueryClient();
  const roles = useListRoles();
  const workspaces = useListWorkspaces();
  const [role, setRole] = useState("");
  const selected = role || roles.data?.roles?.[0]?.id || "";
  const policies = useListRolePolicies(selected, {
    query: { enabled: Boolean(selected) },
  });
  const invalidatePolicies = () =>
    client.invalidateQueries({
      queryKey: getListRolePoliciesQueryKey(selected),
    });
  const [draft, setDraft] = useState(
    JSON.stringify(
      { workspace: "", service: "ogcapi", operation: "", action: "read" },
      null,
      2,
    ),
  );

  const operationCatalog = formSchemas.RolePolicy["x-service-operations"] ?? {};
  let draftPolicy: Partial<RolePolicy> = {};
  try {
    const parsed = JSON.parse(draft);
    if (parsed && typeof parsed === "object" && !Array.isArray(parsed))
      draftPolicy = parsed;
  } catch {
    /* Advanced JSON can be incomplete. */
  }
  const operations = operationCatalog[draftPolicy.service ?? ""] ?? {};
  const actions =
    draftPolicy.operation && draftPolicy.operation !== "*"
      ? [operations[draftPolicy.operation]].filter(Boolean)
      : [...new Set(Object.values(operations))];
  function changeDraft(next: string) {
    try {
      const body = JSON.parse(next) as RolePolicy;
      const supported = operationCatalog[body.service] ?? {};
      if (body.service !== draftPolicy.service) {
        body.operation = "";
        body.action = "read";
      }
      if (body.operation && supported[body.operation])
        body.action = supported[body.operation];
      next = JSON.stringify(body, null, 2);
    } catch {
      /* Preserve unfinished advanced JSON. */
    }
    setDraft(next);
  }

  const add = useMutation({
    meta: { suppressErrorToast: true },
    mutationFn: () => {
      const body = JSON.parse(draft) as RolePolicy;
      const errors = validateFields(formSchemas.RolePolicy, body);
      if (errors.length) throw new Error(errors.join(" "));
      return addRolePolicy(selected, body);
    },
    onSuccess: invalidatePolicies,
  });
  const remove = useMutation({
    meta: { suppressErrorToast: true },
    mutationFn: (policy: string[]) => {
      const body = policyRequest(policy);
      if (!body)
        throw new Error("Built-in wildcard policies are immutable here");
      return removeRolePolicy(selected, body as RolePolicy);
    },
    onSuccess: invalidatePolicies,
  });
  return (
    <Card className="mt-5">
      <CardHeader>
        <CardTitle>Service and operation policies</CardTitle>
      </CardHeader>
      <CardContent className="grid gap-4 lg:grid-cols-2">
        <div>
          <label
            className="mb-1 block text-xs font-medium"
            htmlFor="policy-role"
          >
            Role
          </label>
          <NativeSelect
            id="policy-role"
            className="mb-3 w-full"
            value={selected}
            onChange={(event) => setRole(event.target.value)}
          >
            {roles.data?.roles?.map((item) => (
              <option value={item.id} key={item.id}>
                {item.name} ({item.id})
              </option>
            ))}
          </NativeSelect>
          <PolicyTable
            policies={policies.data?.policies ?? []}
            workspaceName={(id) =>
              workspaces.data?.workspaces?.find((ws) => ws.id === id)?.name
            }
            workspacesKnown={workspaces.isSuccess}
            removing={remove.isPending}
            onRemove={(policy) => remove.mutate(policy)}
          />
        </div>
        <div>
          <p className="mb-2 text-sm text-muted-foreground">
            Add an allow grant for a workspace or explicitly select all
            workspaces. Write enables WFS transactions and locks; manage enables
            stored-query administration. Grants are additive and do not change
            layer visibility.
          </p>
          <QueryError
            error={roles.error || policies.error || workspaces.error}
            retry={() => {
              void roles.refetch();
              void policies.refetch();
              void workspaces.refetch();
            }}
          />
          <ObjectEditor
            schema={formSchemas.RolePolicy}
            draft={draft}
            label="Policy"
            onChange={changeDraft}
            choices={{
              workspace: [
                { value: "*", label: "All workspaces (*)" },
                ...(workspaces.data?.workspaces ?? []).map((ws) => ({
                  value: ws.id,
                  label: ws.name,
                })),
              ],
              service: ["ogcapi", "wms", "wfs", "wcs", "wmts", "ogc-tiles"].map(
                (value) => ({ value, label: value }),
              ),
              operation: [
                { value: "", label: "All operations with selected action" },
                ...Object.keys(operations).map((value) => ({
                  value,
                  label: value,
                })),
              ],
              action: actions.map((value) => ({
                value,
                label: value,
              })),
            }}
          />
          <Button
            className="mt-3"
            disabled={!selected || add.isPending}
            onClick={() => add.mutate()}
          >
            Add policy
          </Button>
          {(add.error || remove.error) && (
            <p className="text-sm text-destructive">
              {(add.error || remove.error)?.message}
            </p>
          )}
        </div>
      </CardContent>
    </Card>
  );
}
function describeResource(resource: string) {
  if (!resource || resource === "*") return "All services";
  if (resource.startsWith("service:"))
    return `${resource.slice("service:".length)} · all operations`;
  if (resource.startsWith("operation:")) {
    const [, service, operation] = resource.split(":");
    return `${service} · ${operation}`;
  }
  return resource;
}

/** Grants as a table; read-only system policies are collapsed underneath. */
function PolicyTable({
  policies,
  workspaceName,
  workspacesKnown,
  removing,
  onRemove,
}: {
  policies: string[][];
  workspaceName: (id: string) => string | undefined;
  workspacesKnown: boolean;
  removing: boolean;
  onRemove: (policy: string[]) => void;
}) {
  const editable = policies.filter((policy) => policyRequest(policy));
  const system = policies.filter((policy) => !policyRequest(policy));
  const scope = (id: string) =>
    id === "*"
      ? "All workspaces"
      : id
        ? (workspaceName(id) ?? id)
        : "No workspace (legacy)";
  const rows = (items: string[][], removable: boolean) =>
    items.map((policy) => {
      const unmatched =
        policy[1] !== "*" &&
        Boolean(policy[1]) &&
        workspacesKnown &&
        !workspaceName(policy[1]);
      return (
        <TableRow key={policy.join("|")}>
          <TableCell className="whitespace-normal">
            {scope(policy[1])}
            {unmatched && (
              <span className="block text-xs text-destructive">
                Unmatched workspace scope
                {removable ? "; remove and re-add it." : "."}
              </span>
            )}
          </TableCell>
          <TableCell className="font-mono text-xs whitespace-normal">
            {describeResource(policy[2])}
          </TableCell>
          <TableCell>{policy[3]}</TableCell>
          <TableCell className="w-12 text-right">
            {removable && (
              <Button
                variant="ghost"
                size="icon-sm"
                aria-label="Remove policy"
                disabled={removing}
                onClick={() => onRemove(policy)}
              >
                <Trash2 />
              </Button>
            )}
          </TableCell>
        </TableRow>
      );
    });
  const head = (
    <TableHeader>
      <TableRow>
        <TableHead>Scope</TableHead>
        <TableHead>Service · operation</TableHead>
        <TableHead>Action</TableHead>
        <TableHead className="w-12">
          <span className="sr-only">Actions</span>
        </TableHead>
      </TableRow>
    </TableHeader>
  );
  return (
    <div className="space-y-3">
      <div className="overflow-x-auto rounded-lg border">
        <Table>
          {head}
          <TableBody>
            {rows(editable, true)}
            {editable.length === 0 && (
              <TableRow>
                <TableCell
                  colSpan={4}
                  className="h-16 text-center text-muted-foreground"
                >
                  No custom grants for this role.
                </TableCell>
              </TableRow>
            )}
          </TableBody>
        </Table>
      </div>
      {system.length > 0 && (
        <details className="rounded-lg border">
          <summary className="cursor-pointer px-3 py-2 text-sm">
            {system.length} read-only system{" "}
            {system.length === 1 ? "policy" : "policies"}
            <span className="ml-2 text-xs text-muted-foreground">
              apply within each principal's assigned workspaces
            </span>
          </summary>
          <div className="overflow-x-auto border-t">
            <Table>
              {head}
              <TableBody>{rows(system, false)}</TableBody>
            </Table>
          </div>
        </details>
      )}
    </div>
  );
}

export function IdentityPage() {
  const { config, me } = useAuth();
  const client = useQueryClient();
  const sessions = useListBrowserSessions();

  const revoke = useMutation({
    mutationFn: (id: string) => revokeBrowserSession(id),
    onSuccess: () =>
      client.invalidateQueries({ queryKey: getListBrowserSessionsQueryKey() }),
  });
  return (
    <Page
      title="Identity"
      description="Single sign-on setup, presented claims and active operator sessions."
    >
      {config?.auth.oidc.enabled ? (
        <>
          <div className="grid gap-4 lg:grid-cols-2">
            <Card>
              <CardHeader>
                <CardTitle>OIDC registration</CardTitle>
              </CardHeader>
              <CardContent className="space-y-3 text-sm">
                <Field label="Issuer" value={config.auth.oidc.issuer} />
                <Field label="Client ID" value={config.auth.oidc.client_id} />
                <Field
                  label="Redirect URI"
                  value={config.auth.oidc.redirect_uri}
                />
                <Field
                  label="Group claims"
                  value={config.auth.oidc.group_claims?.join(", ")}
                />
              </CardContent>
            </Card>
            <Card>
              <CardHeader>
                <CardTitle>Current presented claims</CardTitle>
              </CardHeader>
              <CardContent>
                {me?.presented_claims ? (
                  <pre className="max-h-96 overflow-auto rounded-lg border bg-muted p-3 text-xs">
                    {JSON.stringify(me.presented_claims, null, 2)}
                  </pre>
                ) : (
                  <p className="text-sm text-muted-foreground">
                    This session didn't sign in through OIDC, so it presented no
                    claims.
                  </p>
                )}
                <p className="mt-2 text-xs text-muted-foreground">
                  Matching is exact. Local mapping and policy changes are
                  re-evaluated from the claims captured for this session.
                  Identity provider membership changes require a new login.
                </p>
              </CardContent>
            </Card>
          </div>
          <ClaimMappingBuilder />
        </>
      ) : (
        <Card>
          <CardHeader>
            <CardTitle>OIDC sign-in is not configured</CardTitle>
          </CardHeader>
          <CardContent className="space-y-2 text-sm">
            <p className="text-muted-foreground">
              Operators sign in with{" "}
              {[
                config?.auth.password_login && "a password",
                config?.auth.token_login && "an API key or JWT",
              ]
                .filter(Boolean)
                .join(" or ") || "a token"}
              . To add single sign-on, set <code>Auth.OIDC.IssuerURL</code>,{" "}
              <code>ClientID</code> and <code>BrowserLoginEnabled</code> in the
              server configuration and restart. Claim mappings then turn
              identity-provider groups into workspace roles.
            </p>
            <p className="font-mono text-xs break-all text-muted-foreground">
              Redirect URI to register: {config?.auth.oidc.redirect_uri}
            </p>
          </CardContent>
        </Card>
      )}
      <Card className="mt-4">
        <CardHeader>
          <CardTitle>Browser sessions</CardTitle>
        </CardHeader>
        <CardContent>
          <div className="@container/table overflow-x-auto rounded-lg border">
            <Table className="table-fixed @3xl/table:table-auto">
              <TableHeader>
                <TableRow>
                  {[
                    "Principal",
                    "Method",
                    "Client",
                    "Last seen",
                    "Expires",
                    "State",
                    "",
                  ].map((heading, index) => (
                    <TableHead
                      key={heading}
                      className={
                        index === 0
                          ? "whitespace-normal"
                          : index === 6
                            ? "w-14"
                            : "hidden @3xl/table:table-cell"
                      }
                    >
                      {heading || "Actions"}
                    </TableHead>
                  ))}
                </TableRow>
              </TableHeader>
              <TableBody>
                {sessions.data?.sessions.map((session) => (
                  <TableRow key={session.id}>
                    <TableCell className="whitespace-normal break-words">
                      <div>{session.display_name || session.subject}</div>
                      <div className="text-xs text-muted-foreground">
                        {session.email}
                      </div>
                      <RowDetails>
                        {Object.entries({
                          method: session.auth_method,
                          client: session.remote_addr,
                          user_agent: session.user_agent,
                          last_seen_at: session.last_seen_at,
                          expires_at: session.expires_at,
                          state: session.revoked_at
                            ? "revoked"
                            : session.id === me?.session_id
                              ? "current"
                              : "active",
                        }).map(([field, value]) => (
                          <div key={field}>
                            <dt>{field.replaceAll("_", " ")}</dt>
                            <dd>
                              <DisplayValue field={field} value={value} />
                            </dd>
                          </div>
                        ))}
                      </RowDetails>
                    </TableCell>
                    <TableCell className="hidden font-mono @3xl/table:table-cell">
                      {session.auth_method}
                    </TableCell>
                    <TableCell className="hidden @3xl/table:table-cell">
                      <div className="font-mono text-xs">
                        {session.remote_addr || "—"}
                      </div>
                      <div
                        className="max-w-64 truncate text-xs text-muted-foreground"
                        title={session.user_agent}
                      >
                        {session.user_agent}
                      </div>
                    </TableCell>
                    <TableCell className="hidden text-xs @3xl/table:table-cell">
                      {formatDateTime(session.last_seen_at)}
                    </TableCell>
                    <TableCell className="hidden text-xs @3xl/table:table-cell">
                      {formatDateTime(session.expires_at)}
                    </TableCell>
                    <TableCell className="hidden @3xl/table:table-cell">
                      <StatusChip
                        value={
                          session.revoked_at
                            ? "revoked"
                            : session.id === me?.session_id
                              ? "current"
                              : "active"
                        }
                      />
                    </TableCell>
                    <TableCell>
                      <Button
                        size="icon-sm"
                        variant="ghost"
                        aria-label={`Revoke session for ${session.subject}`}
                        className="size-11"
                        disabled={
                          revoke.isPending || Boolean(session.revoked_at)
                        }
                        onClick={() => revoke.mutate(session.id)}
                      >
                        <Trash2 />
                      </Button>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </div>
        </CardContent>
      </Card>
    </Page>
  );
}

function ClaimMappingBuilder() {
  const { me } = useAuth();
  const client = useQueryClient();
  const [scope, setScope] = useState("*");
  const [draft, setDraft] = useState(
    JSON.stringify(
      { claim_name: "groups", claim_value: "", role_id: "admin", priority: 0 },
      null,
      2,
    ),
  );
  const globals = useListGlobalClaimMappings();
  const roles = useListRoles();
  const sessions = useListBrowserSessions();
  const invalidateGlobals = () =>
    client.invalidateQueries({
      queryKey: getListGlobalClaimMappingsQueryKey(),
    });

  const create = useMutation({
    meta: { suppressErrorToast: true },
    mutationFn: () => {
      const body = JSON.parse(draft) as CreateClaimMappingBody;
      const errors = validateFields(formSchemas.ClaimMapping, body);
      if (errors.length) throw new Error(errors.join(" "));
      // "*" is the server's global scope; anything else is a workspace.
      return scope === "*"
        ? createGlobalClaimMapping(body)
        : createClaimMapping(scope, body);
    },
    onSuccess: async (created) => {
      toast.success(
        `Mapped ${created.claim_name} = ${created.claim_value} to ${created.role_id}. Affected users get the role on their next sign-in.`,
      );
      await invalidateGlobals();
    },
  });
  const remove = useMutation({
    meta: { suppressErrorToast: true },
    mutationFn: (id: string) => deleteGlobalClaimMapping(id),
    onSuccess: invalidateGlobals,
  });
  const presented = me?.presented_claims as
    { claims?: Record<string, unknown> } | undefined;
  const candidates = claimPairs(presented?.claims);
  // Recent identity-provider sign-ins that did not reach the console: the
  // claims an administrator most likely needs to map.
  const denied = (sessions.data?.sessions ?? [])
    .filter((session) => !session.console_access && session.presented_claims)
    .map((session) => ({
      who: session.email || session.display_name || session.subject,
      pairs: claimPairs(
        session.presented_claims?.claims as Record<string, unknown>,
      ),
    }))
    .filter(
      (entry, index, all) =>
        entry.pairs.length > 0 &&
        all.findIndex((other) => other.who === entry.who) === index,
    )
    .slice(0, 8);
  const presentedBySessions = (sessions.data?.sessions ?? []).flatMap(
    (session) =>
      claimPairs(session.presented_claims?.claims as Record<string, unknown>),
  );
  const choose = (candidate: { name: string; value: string }) =>
    setDraft(
      JSON.stringify(
        {
          claim_name: candidate.name,
          claim_value: candidate.value,
          role_id: "admin",
          priority: 0,
        },
        null,
        2,
      ),
    );
  return (
    <Card className="mt-4">
      <CardHeader>
        <CardTitle>Create mapping from a presented claim</CardTitle>
      </CardHeader>
      <CardContent className="grid gap-4 lg:grid-cols-2">
        <div>
          <p className="mb-2 text-sm text-muted-foreground">
            Matching is exact. Choose an observed value to avoid copying group
            IDs by hand.
          </p>
          {denied.length > 0 && (
            <section
              aria-label="Recently denied sign-ins"
              className="mb-4 space-y-2 rounded-lg border border-warning/40 bg-warning/5 p-3"
            >
              <p className="text-sm font-medium">
                Recent sign-ins without console access
              </p>
              {denied.map((entry) => (
                <div key={entry.who} className="space-y-1">
                  <p className="text-xs text-muted-foreground">{entry.who}</p>
                  <div className="flex flex-wrap gap-1.5">
                    {entry.pairs.map((candidate) => (
                      <Button
                        variant="outline"
                        size="sm"
                        key={`${candidate.name}:${candidate.value}`}
                        onClick={() => choose(candidate)}
                      >
                        {candidate.name} = {candidate.value}
                      </Button>
                    ))}
                  </div>
                </div>
              ))}
            </section>
          )}
          <p className="mb-1 text-xs font-medium">Your session</p>
          <div className="flex flex-wrap gap-2">
            {candidates.map((candidate) => (
              <Button
                variant="outline"
                size="sm"
                key={`${candidate.name}:${candidate.value}`}
                onClick={() => choose(candidate)}
              >
                {candidate.name} = {candidate.value}
              </Button>
            ))}
            {candidates.length === 0 && (
              <span className="text-sm text-muted-foreground">
                No string group claims were presented by this session.
              </span>
            )}
          </div>
          <div className="mt-4 space-y-2">
            {globals.data?.claim_mappings?.map((mapping) => (
              <div
                className="flex items-center gap-2 border px-3 py-2"
                key={mapping.id}
              >
                <span className="min-w-0 flex-1 text-xs">
                  Members with <code>{mapping.claim_name}</code> ={" "}
                  <code>{mapping.claim_value}</code> get{" "}
                  <strong>{mapping.role_id}</strong> globally.
                  {sessions.isSuccess && (
                    <span
                      className={
                        presentedBySessions.some(
                          (pair) =>
                            pair.name === mapping.claim_name &&
                            pair.value === mapping.claim_value,
                        )
                          ? "block text-muted-foreground"
                          : "block text-warning"
                      }
                    >
                      {presentedBySessions.some(
                        (pair) =>
                          pair.name === mapping.claim_name &&
                          pair.value === mapping.claim_value,
                      )
                        ? "Presented by a recent sign-in."
                        : "No recent sign-in presented this claim. Check the claim name."}
                    </span>
                  )}
                </span>
                <Button
                  variant="ghost"
                  size="icon-sm"
                  aria-label="Delete global claim mapping"
                  disabled={remove.isPending}
                  onClick={() => remove.mutate(mapping.id)}
                >
                  <Trash2 />
                </Button>
              </div>
            ))}
          </div>
        </div>
        <div>
          <label
            className="mb-1 block text-xs font-medium"
            htmlFor="mapping-scope"
          >
            Mapping scope
          </label>
          <p className="mb-2 text-xs text-muted-foreground">
            Console sign-in needs admin (or super_admin). Viewer and editor
            mappings grant service and API access only.
          </p>
          <NativeSelect
            id="mapping-scope"
            className="mb-3 w-full"
            value={scope}
            onChange={(event) => setScope(event.target.value)}
          >
            <option value="*">Global</option>
            {me?.workspaces.map((workspace) => (
              <option value={workspace.name} key={workspace.id}>
                {workspace.name}
              </option>
            ))}
          </NativeSelect>
          <QueryError
            error={globals.error || roles.error}
            retry={() => {
              void globals.refetch();
              void roles.refetch();
            }}
          />
          <ObjectEditor
            schema={formSchemas.ClaimMapping}
            draft={draft}
            label="Claim mapping"
            onChange={setDraft}
            choices={{
              role_id: (roles.data?.roles ?? []).map((role) => ({
                value: role.id,
                label: role.name || role.id,
              })),
            }}
          />
          <Button
            className="mt-3"
            disabled={create.isPending}
            onClick={() => create.mutate()}
          >
            Create mapping
          </Button>
          {(create.error || remove.error) && (
            <p className="text-sm text-destructive">
              {(create.error || remove.error)?.message}
            </p>
          )}
        </div>
      </CardContent>
    </Card>
  );
}

function Field({ label, value }: { label: string; value?: string }) {
  return (
    <div>
      <span className="block text-xs text-muted-foreground">{label}</span>
      <code className="block break-all whitespace-pre-wrap border bg-muted p-2 text-xs">
        {value || "Not configured"}
      </code>
    </div>
  );
}

/** String claim values as name/value pairs, one per array element. */
function claimPairs(claims: Record<string, unknown> | undefined) {
  return Object.entries(claims ?? {}).flatMap(([name, value]) =>
    (Array.isArray(value) ? value : [value])
      .filter((item): item is string => typeof item === "string")
      .map((item) => ({ name, value: item })),
  );
}
