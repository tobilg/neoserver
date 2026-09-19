import { Navigate } from "react-router";
import { useAuth } from "@/auth/auth-context";

/**
 * Sends a signed-in operator to the one place they can actually work: their
 * only workspace if they have exactly one, the server-wide list if they are a
 * super admin, and the no-access page if the console is not theirs to use.
 *
 * Kept out of `router.tsx` so that module exports only the router, which is
 * what lets Fast Refresh reload this component without rebuilding every route.
 */
export function Landing() {
  const { me } = useAuth();
  if (!me) return null;
  const administrable = me.workspaces.filter(
    (workspace) => workspace.console_access,
  );
  if (administrable.length === 1)
    return (
      <Navigate
        to={`/workspaces/${encodeURIComponent(administrable[0].name)}`}
        replace
      />
    );
  if (me.super_admin) return <Navigate to="/workspaces" replace />;
  if (administrable[0])
    return (
      <Navigate
        to={`/workspaces/${encodeURIComponent(administrable[0].name)}`}
        replace
      />
    );
  return <Navigate to="/no-access" replace />;
}
