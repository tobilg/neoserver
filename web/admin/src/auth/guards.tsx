import { Navigate, Outlet, useLocation, useParams } from "react-router";
import { useAuth } from "./auth-context";
import { Skeleton } from "@/components/ui/skeleton";

export function AuthGuard() {
  const { me, loading } = useAuth();
  const location = useLocation();
  if (loading)
    return (
      <div className="space-y-3 p-8">
        <Skeleton className="h-8 w-64" />
        <Skeleton className="h-48 w-full" />
      </div>
    );
  if (!me)
    return (
      <Navigate
        to={`/login?next=${encodeURIComponent(location.pathname + location.search)}`}
        replace
      />
    );
  return <Outlet />;
}

export function ConsoleGuard() {
  const { me } = useAuth();
  if (!me?.console_access) return <Navigate to="/no-access" replace />;
  return <Outlet />;
}

export function SuperAdminGuard() {
  const { me } = useAuth();
  if (!me?.super_admin) return <Navigate to="/" replace />;
  return <Outlet />;
}

export function WorkspaceGuard({ children }: { children?: React.ReactNode }) {
  const { me } = useAuth();
  const { ws } = useParams();
  const allowed =
    me?.super_admin ||
    me?.workspaces.some(
      (workspace) =>
        (workspace.name === ws || workspace.id === ws) &&
        workspace.console_access,
    );
  if (!allowed) return <Navigate to="/no-access" replace />;
  return children ?? <Outlet />;
}
