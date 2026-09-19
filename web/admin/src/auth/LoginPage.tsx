import { useState, type FormEvent } from "react";
import { useNavigate, useSearchParams } from "react-router";
import { AlertCircle, KeyRound, LockKeyhole, Waypoints } from "lucide-react";
import { useAuth } from "./auth-context";
import { beginOIDCLogin } from "./oidc";
import { Alert, AlertTitle } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { tabsRootFix, tabsTriggerFix } from "@/lib/tabsCompat";

/** Explains a rejected sign-in instead of repeating the server's status. */
function signInError(type: "password" | "token", message: string) {
  if (
    !/authentication failed|invalid|unauthori[sz]ed|credential|expired|revoked/i.test(
      message,
    )
  )
    return message;
  return type === "password"
    ? "The username or password is incorrect."
    : "That token or API key wasn't accepted. It may be expired or revoked, or issued by another server. Create a new one with neoserver create-token.";
}

function safeNext(value: string | null) {
  return value?.startsWith("/") && !value.startsWith("//") ? value : "/";
}

export function LoginPage() {
  const { config, login } = useAuth();
  const [params] = useSearchParams();
  const navigate = useNavigate();
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  async function submit(
    event: FormEvent<HTMLFormElement>,
    type: "password" | "token",
  ) {
    event.preventDefault();
    setBusy(true);
    setError("");
    const data = new FormData(event.currentTarget);
    try {
      const me = await login(
        type === "password"
          ? {
              method: "password",
              username: String(data.get("username")),
              password: String(data.get("password")),
            }
          : { method: "token", token: String(data.get("credential")) },
      );
      const administrable = me.workspaces.filter(
        (workspace) => workspace.console_access,
      );
      const fallback =
        me.super_admin && administrable.length !== 1
          ? "/workspaces"
          : administrable[0]
            ? `/workspaces/${encodeURIComponent(administrable[0].name)}`
            : "/no-access";
      navigate(
        safeNext(params.get("next")) === "/"
          ? fallback
          : safeNext(params.get("next")),
        { replace: true },
      );
    } catch (reason) {
      setError(
        signInError(
          type,
          reason instanceof Error ? reason.message : "Authentication failed",
        ),
      );
    } finally {
      setBusy(false);
    }
  }
  const passwordLogin = config?.auth.password_login === true;
  // Token sign-in stays available until the config says otherwise.
  const tokenLogin = config?.auth.token_login !== false;
  const showTabs = passwordLogin && tokenLogin;
  const passwordForm = (
    <form
      className="space-y-4 pt-3"
      onSubmit={(event) => void submit(event, "password")}
    >
      <div>
        <Label htmlFor="username">Username</Label>
        <Input id="username" name="username" autoComplete="username" required />
      </div>
      <div>
        <Label htmlFor="password">Password</Label>
        <Input
          id="password"
          name="password"
          type="password"
          autoComplete="current-password"
          required
        />
      </div>
      <Button className="w-full" disabled={busy}>
        Start session
      </Button>
    </form>
  );
  const tokenForm = (
    <form
      className="space-y-4 pt-3"
      onSubmit={(event) => void submit(event, "token")}
    >
      <div className="space-y-1">
        <Label htmlFor="credential">API key or JWT</Label>
        <Input
          id="credential"
          name="credential"
          type="password"
          autoComplete="off"
          required
        />
        <p className="text-xs text-muted-foreground">
          Exchanged once for a session; never stored in the browser.
        </p>
        <details className="text-xs text-muted-foreground">
          <summary className="cursor-pointer">Where do I get a token?</summary>
          <p className="mt-1">
            Use the bootstrap JWT printed by <code>neoserver init</code>, issue
            one with <code>neoserver create-token --role super_admin</code>, or
            use a workspace API key. <code>NEOSRV_STORE_KEY</code> is the
            database encryption key, not a sign-in credential.
          </p>
        </details>
      </div>
      <Button className="w-full" disabled={busy}>
        Start session
      </Button>
    </form>
  );
  return (
    <main className="grid min-h-screen place-items-center bg-background p-6">
      <div className="w-full max-w-md">
        <div className="mb-6 flex items-center gap-3">
          <div className="grid size-11 place-items-center border border-primary text-primary">
            <Waypoints />
          </div>
          <div>
            <p className="m-0 text-xs font-semibold text-muted-foreground">
              Spatial infrastructure
            </p>
            <h1 className="m-0 text-2xl font-semibold">neoserver Console</h1>
          </div>
        </div>
        <Card>
          <CardHeader>
            <CardTitle>Operator sign in</CardTitle>
            <CardDescription>
              Exchange an existing neoserver credential for a protected browser
              session.
            </CardDescription>
          </CardHeader>
          <CardContent>
            {error && (
              <Alert variant="destructive" className="mb-4">
                <AlertCircle />
                <AlertTitle>{error}</AlertTitle>
              </Alert>
            )}
            {showTabs ? (
              <Tabs
                // defaultValue is read once, but `config` arrives from a fetch,
                // so remount once it is known to preselect password sign-in.
                key={config ? "configured" : "loading"}
                className={tabsRootFix}
                defaultValue="password"
              >
                <TabsList className="grid w-full grid-cols-2">
                  <TabsTrigger value="password" className={tabsTriggerFix}>
                    <LockKeyhole /> Password
                  </TabsTrigger>
                  <TabsTrigger value="token" className={tabsTriggerFix}>
                    <KeyRound /> Token
                  </TabsTrigger>
                </TabsList>
                <TabsContent value="password">{passwordForm}</TabsContent>
                <TabsContent value="token">{tokenForm}</TabsContent>
              </Tabs>
            ) : passwordLogin ? (
              passwordForm
            ) : tokenLogin ? (
              tokenForm
            ) : null}
            {config?.auth.oidc.enabled && (
              <>
                <div className="my-5 flex items-center gap-3 text-xs text-muted-foreground">
                  <span className="h-px flex-1 bg-border" />
                  or
                  <span className="h-px flex-1 bg-border" />
                </div>
                <Button
                  variant="outline"
                  className="w-full"
                  onClick={() =>
                    void beginOIDCLogin(config, safeNext(params.get("next")))
                  }
                >
                  Continue with identity provider
                </Button>
              </>
            )}
          </CardContent>
        </Card>
      </div>
    </main>
  );
}
