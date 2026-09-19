import {
  useCallback,
  useEffect,
  useMemo,
  useState,
  type ReactNode,
} from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import {
  getGetAuthMeQueryKey,
  login as loginRequest,
  logout as logoutRequest,
  useGetAuthMe,
  useGetConsoleConfig,
} from "@/api/generated/authentication/authentication";
import { useSessionKeepalive } from "./use-session-keepalive";
import { coordinatedRefresh } from "./refresh-session";
import {
  AuthContext,
  type AuthContextValue,
  type LoginPayload,
} from "./auth-context";
import type { LoginRequest } from "@/api/generated/models";
import { csrfToken } from "@/api/client";
import {
  assertCurrentSession,
  invalidateSessionRequests,
  sessionGeneration,
} from "@/api/session";

export function AuthProvider({ children }: { children: ReactNode }) {
  const client = useQueryClient();
  const [sessionRevoked, setSessionRevoked] = useState(false);
  const config = useGetConsoleConfig({ query: { staleTime: Infinity } });
  // A browser session always comes with the readable CSRF cookie. Without it
  // (and with authentication on) there is nothing to ask the server about,
  // so the sign-in page doesn't log a failing /auth/me request.
  const sessionPossible = config.data
    ? !config.data.auth?.enabled || Boolean(csrfToken())
    : false;
  const me = useGetAuthMe({
    query: { retry: false, enabled: !sessionRevoked && sessionPossible },
  });
  const isolateSession = useCallback(() => {
    const generation = invalidateSessionRequests();
    setSessionRevoked(true);
    // Keep the observed auth query (with an explicit empty value) to avoid a
    // remove/refetch/401 loop. Everything else private belongs to one session.
    const privateQueries = {
      predicate: (query: { queryKey: readonly unknown[] }) =>
        query.queryKey[0] !== "/console/config",
    };
    void client.cancelQueries(privateQueries);
    client.removeQueries({
      predicate: (query) =>
        privateQueries.predicate(query) &&
        query.queryKey[0] !== getGetAuthMeQueryKey()[0],
    });
    client.setQueryData(getGetAuthMeQueryKey(), null);
    client.getMutationCache().clear();
    return generation;
  }, [client]);
  const loginMutation = useMutation({
    mutationFn: async (payload: LoginPayload) => {
      const generation = isolateSession();
      const identity = await loginRequest(payload as LoginRequest);
      assertCurrentSession(generation);
      return identity;
    },
    onSuccess: (identity) => {
      setSessionRevoked(false);
      client.setQueryData(getGetAuthMeQueryKey(), identity);
    },
  });
  useEffect(() => {
    const unauthorized = () => {
      isolateSession();
    };
    window.addEventListener("neoserver:unauthorized", unauthorized);
    return () =>
      window.removeEventListener("neoserver:unauthorized", unauthorized);
  }, [isolateSession]);

  const refresh = useCallback(async () => {
    const generation = sessionGeneration();
    const identity = await coordinatedRefresh();
    assertCurrentSession(generation);
    client.setQueryData(getGetAuthMeQueryKey(), identity);
    return identity;
  }, [client]);

  // Keep an actively-used session from hitting the server's idle timeout.
  // Only cookie sessions can be refreshed; header credentials have no session
  // to rotate.
  useSessionKeepalive({
    idleExpiresAt: me.data?.session_idle_expires_at,
    enabled: Boolean(me.data?.session_id) && !sessionRevoked,
    refresh,
  });

  const { mutateAsync: login } = loginMutation;
  const value = useMemo<AuthContextValue>(
    () => ({
      config: config.data,
      me: sessionRevoked ? undefined : (me.data ?? undefined),
      loading:
        config.isLoading ||
        (!sessionRevoked && sessionPossible && me.isLoading),
      login,
      logout: async () => {
        await logoutRequest();
        isolateSession();
      },
      refresh,
    }),
    [
      config.data,
      config.isLoading,
      login,
      me.data,
      me.isLoading,
      sessionPossible,
      sessionRevoked,
      isolateSession,
      refresh,
    ],
  );

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
}
