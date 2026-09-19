import { useEffect, useState } from "react";
import { useNavigate, useSearchParams } from "react-router";
import { useAuth } from "./auth-context";
import { finishOIDCLogin } from "./oidc";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";

export function OIDCCallbackPage() {
  const { config, login } = useAuth();
  const [params] = useSearchParams();
  const navigate = useNavigate();
  const [error, setError] = useState("");
  useEffect(() => {
    if (!config) return;
    void finishOIDCLogin(
      config,
      params.get("code") ?? "",
      params.get("state") ?? "",
    )
      .then(async ({ idToken, next }) => {
        await login({ method: "oidc", id_token: idToken });
        navigate(next, { replace: true });
      })
      .catch((reason: unknown) =>
        setError(
          reason instanceof Error ? reason.message : "OIDC sign-in failed",
        ),
      );
  }, [config, login, navigate, params]);
  return (
    <main className="grid min-h-screen place-items-center p-6">
      {error ? (
        <Alert variant="destructive" className="max-w-lg">
          <AlertTitle>Identity-provider sign-in failed</AlertTitle>
          <AlertDescription>{error}</AlertDescription>
        </Alert>
      ) : (
        <p className="font-mono text-sm text-muted-foreground">
          Completing secure sign-in…
        </p>
      )}
    </main>
  );
}
