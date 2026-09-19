import { Navigate, useLocation } from "react-router";

/** Redirect a retired URL while keeping its query string and hash. */
export function KeepSearchRedirect({ to }: { to: string }) {
  const location = useLocation();
  return (
    <Navigate
      replace
      relative="path"
      to={{ pathname: to, search: location.search, hash: location.hash }}
    />
  );
}
