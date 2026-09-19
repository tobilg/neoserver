import { useState } from "react";
import { Link } from "react-router";
import { Check, Copy } from "lucide-react";
import { useAuth } from "./auth-context";
import { basePath } from "@/api/client";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";

const roleHelp: Record<string, string> = {
  editor:
    "Your editor access works with the management API and WFS transactions, not with this console.",
  viewer:
    "Your viewer access works with published OGC services and the management API, not with this console.",
};

/** Flattens presented claims into "name = value" rows an admin can map. */
function claimRows(claims: Record<string, unknown> | undefined) {
  return Object.entries(claims ?? {}).flatMap(([name, value]) =>
    (Array.isArray(value) ? value : [value])
      .filter((item) => typeof item === "string" || typeof item === "number")
      .map((item) => ({ name, value: String(item) })),
  );
}

export function NoAccessPage() {
  const { me, logout } = useAuth();
  const [copied, setCopied] = useState(false);
  const [manualCopy, setManualCopy] = useState(false);
  const roles = [...new Set(me?.workspaces.map((item) => item.role) ?? [])];
  const presented = me?.presented_claims;
  const rows = claimRows(presented?.claims as Record<string, unknown>);
  const who = presented?.email || me?.email || me?.subject || me?.principal;
  const request = [
    `Please grant neoserver console access to ${who ?? "my account"}.`,
    presented?.iss && `Identity provider: ${presented.iss}`,
    presented?.sub && `Subject: ${presented.sub}`,
    rows.length > 0 &&
      `Presented claims: ${rows.map((row) => `${row.name}=${row.value}`).join(", ")}`,
    me?.workspaces.length
      ? `Current access: ${me.workspaces.map((item) => `${item.name} (${item.role})`).join(", ")}`
      : "Current access: none",
  ]
    .filter(Boolean)
    .join("\n");
  return (
    <main className="grid min-h-screen place-items-center p-6">
      <Card className="w-full max-w-xl">
        <CardHeader>
          <CardTitle>This account can't use the console</CardTitle>
          <CardDescription>
            You signed in successfully. The console needs the admin role for a
            workspace, or super_admin.
          </CardDescription>
        </CardHeader>
        <CardContent className="space-y-4">
          {roles
            .filter((role) => roleHelp[role])
            .map((role) => (
              <p key={role} className="text-sm">
                {roleHelp[role]}{" "}
                <a
                  className="underline"
                  href={`${basePath}/api/v1/api.html`}
                  target="_blank"
                  rel="noreferrer"
                >
                  API reference
                </a>
              </p>
            ))}
          {me?.console_access && (
            <Button asChild variant="outline">
              <Link to="/">Choose an accessible workspace</Link>
            </Button>
          )}
          <div>
            <p className="text-sm font-medium">Your workspace access</p>
            <p className="font-mono text-sm text-muted-foreground">
              {me?.workspaces
                .map((workspace) => `${workspace.name} (${workspace.role})`)
                .join(", ") || "None"}
            </p>
          </div>
          {presented && (
            <div className="space-y-2">
              <p className="text-sm font-medium">
                Claims your identity provider sent
              </p>
              {rows.length ? (
                <ul className="flex flex-wrap gap-1.5">
                  {rows.map((row) => (
                    <li
                      key={`${row.name}=${row.value}`}
                      className="rounded-full border px-2 py-0.5 font-mono text-xs"
                    >
                      {row.name} = {row.value}
                    </li>
                  ))}
                </ul>
              ) : (
                <p className="text-sm text-muted-foreground">
                  No group or role claims were sent.
                </p>
              )}
              <p className="text-xs text-muted-foreground">
                An administrator can map one of these claims to a role on the
                Identity page.
              </p>
              <details>
                <summary className="cursor-pointer text-xs">Raw claims</summary>
                <pre className="mt-2 max-h-64 overflow-auto rounded-lg border bg-muted p-3 text-xs">
                  {JSON.stringify(presented, null, 2)}
                </pre>
              </details>
            </div>
          )}
          {manualCopy && (
            <div className="space-y-1">
              <p role="alert" className="text-sm">
                Copying failed. Select the request below and copy it manually.
              </p>
              <textarea
                readOnly
                aria-label="Access request"
                className="h-32 w-full rounded-md border bg-muted p-2 font-mono text-xs"
                value={request}
                onFocus={(event) => event.currentTarget.select()}
              />
            </div>
          )}
          <div className="flex flex-wrap gap-2">
            <Button
              variant="outline"
              onClick={async () => {
                try {
                  await navigator.clipboard.writeText(request);
                  setCopied(true);
                } catch {
                  setCopied(false);
                  setManualCopy(true);
                }
              }}
            >
              {copied ? <Check /> : <Copy />}
              {copied ? "Copied" : "Copy access request"}
            </Button>
            <Button
              variant="outline"
              onClick={() =>
                void logout().then(() =>
                  location.assign(`${basePath}/admin/login`),
                )
              }
            >
              End session
            </Button>
          </div>
        </CardContent>
      </Card>
    </main>
  );
}
