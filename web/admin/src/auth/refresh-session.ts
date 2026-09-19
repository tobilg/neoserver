import { ApiError, apiBase } from "@/api/client";
import {
  getAuthMe,
  refreshSession,
} from "@/api/generated/authentication/authentication";
import type { AuthMe } from "@/api/generated/models";

let pending: Promise<AuthMe> | undefined;

/** One refresh per tab, serialized across same-origin tabs where supported. */
export function coordinatedRefresh(): Promise<AuthMe> {
  if (pending) return pending;
  const refresh = async () => {
    try {
      return await refreshSession();
    } catch (error) {
      // A competing refresh is recoverable, not an authentication failure.
      if (error instanceof ApiError && error.status === 409) return getAuthMe();
      throw error;
    }
  };
  pending = (
    navigator.locks
      ? navigator.locks.request(`${apiBase}:session-refresh`, refresh)
      : refresh()
  ).finally(() => {
    pending = undefined;
  });
  return pending;
}
