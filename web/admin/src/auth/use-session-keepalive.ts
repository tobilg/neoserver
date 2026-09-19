import { useEffect, useRef } from "react";

/** Refresh once the idle window has less than this left. */
export const KEEPALIVE_THRESHOLD_MS = 10 * 60_000;
/** How often the idle window is re-evaluated. */
export const KEEPALIVE_CHECK_MS = 60_000;
/** An operator counts as present if they interacted within this window. */
export const ACTIVITY_WINDOW_MS = 5 * 60_000;

const ACTIVITY_EVENTS = ["pointerdown", "keydown"] as const;

export interface SessionKeepaliveOptions {
  /** `session_idle_expires_at` from /auth/me; null disables the keepalive. */
  idleExpiresAt?: string | null;
  /** Rotates the session server-side and returns the refreshed identity. */
  refresh: () => Promise<unknown>;
  /** False while signed out, or for credentials that cannot be refreshed. */
  enabled: boolean;
}

/**
 * Keeps an actively-used session alive.
 *
 * The server's idle timeout would otherwise sign out an operator who has a tab
 * open all day, losing whatever they were part-way through. Refreshing extends
 * only the idle window: `/auth/refresh` caps it at the absolute session expiry,
 * so this cannot hold a session open indefinitely, and is not meant to.
 *
 * Deliberately does nothing when the tab is hidden or the operator has not
 * interacted recently -- an abandoned tab should be allowed to expire.
 */
export function useSessionKeepalive({
  idleExpiresAt,
  refresh,
  enabled,
}: SessionKeepaliveOptions) {
  // Zero until mount: reading the clock during render is impure, and the
  // mount effect below seeds it anyway.
  const lastActivity = useRef(0);
  const refreshing = useRef(false);
  // Held in a ref so a new closure each render does not restart the interval.
  const refreshRef = useRef(refresh);
  useEffect(() => {
    refreshRef.current = refresh;
  }, [refresh]);

  useEffect(() => {
    if (!enabled) {
      return;
    }
    const record = () => {
      lastActivity.current = Date.now();
    };
    // Mounting/reloading a passive tab is not operator activity.
    lastActivity.current = 0;
    for (const event of ACTIVITY_EVENTS) {
      window.addEventListener(event, record, { passive: true });
    }
    return () => {
      for (const event of ACTIVITY_EVENTS) {
        window.removeEventListener(event, record);
      }
    };
  }, [enabled]);

  useEffect(() => {
    if (!enabled || !idleExpiresAt) {
      return;
    }
    const expiry = Date.parse(idleExpiresAt);
    if (Number.isNaN(expiry)) {
      return;
    }

    const timer = setInterval(() => {
      if (refreshing.current || document.hidden) {
        return;
      }
      const now = Date.now();
      if (
        !lastActivity.current ||
        now - lastActivity.current > ACTIVITY_WINDOW_MS
      ) {
        return;
      }
      if (expiry - now > KEEPALIVE_THRESHOLD_MS) {
        return;
      }
      refreshing.current = true;
      void Promise.resolve(refreshRef.current())
        .catch(() => {
          // A rejected refresh means the session is already gone; apiFetch has
          // dispatched the unauthorised event and the guard will redirect.
        })
        .finally(() => {
          refreshing.current = false;
        });
    }, KEEPALIVE_CHECK_MS);

    return () => clearInterval(timer);
  }, [enabled, idleExpiresAt]);
}
