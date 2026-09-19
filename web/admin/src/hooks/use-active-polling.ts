import { useCallback, useRef, useSyncExternalStore } from "react";

/**
 * Job lifecycle states that mean work is still in flight. Imports, tile-cache
 * jobs, mosaic harvests and resumable deletions all report progress through
 * these, so the console keeps polling while any of them is present.
 */
export const ACTIVE_JOB_STATUSES = new Set([
  "pending",
  "queued",
  "running",
  "publishing",
  "cancelling",
  "rolling_back",
]);

export function isActiveStatus(status: unknown): boolean {
  return ACTIVE_JOB_STATUSES.has(String(status ?? ""));
}

/**
 * Polling ladder. The server is single-active, so cadence backs off once a job
 * stops changing rather than hammering a busy process at a fixed rate.
 */
export const POLL_ACTIVE_MS = 2_000;
export const POLL_SETTLED_MS = 10_000;
export const POLL_STALE_MS = 30_000;

/** Elapsed time without an observed change before each step down. */
export const POLL_SETTLED_AFTER_MS = 60_000;
export const POLL_STALE_AFTER_MS = 300_000;

/**
 * Flat cadences for resources that have no job lifecycle to track. Readiness
 * drives the topbar instrument; the workspace summary refreshes counts.
 */
export const POLL_READINESS_MS = 30_000;
export const POLL_SUMMARY_MS = 15_000;

function subscribeVisibility(onChange: () => void) {
  document.addEventListener("visibilitychange", onChange);
  return () => document.removeEventListener("visibilitychange", onChange);
}

function getVisibility() {
  return !document.hidden;
}

function getVisibilityOnServer() {
  return true;
}

/** Minimal structural view of the query object TanStack passes in. */
type QueryLike<TData> = { state: { data: TData | undefined } };

export interface ActivePollingOptions<TData> {
  /** True while the payload still contains in-flight work. */
  isActive: (data: TData | undefined) => boolean;
  /**
   * Cheap fingerprint of the parts that indicate progress. When it stops
   * changing the cadence steps down. Keep it small: it is recomputed on every
   * poll evaluation, so do not stringify whole collections.
   */
  signature: (data: TData | undefined) => string;
}

export interface ActivePollingResult<TData> {
  refetchInterval: (query: QueryLike<TData>) => number | false;
  refetchIntervalInBackground: false;
}

/**
 * Drives `refetchInterval` for a query that tracks long-running work.
 *
 * Polls every {@link POLL_ACTIVE_MS} while work is active, steps down to
 * {@link POLL_SETTLED_MS} after {@link POLL_SETTLED_AFTER_MS} without an
 * observed change and to {@link POLL_STALE_MS} after
 * {@link POLL_STALE_AFTER_MS}, stops entirely when nothing is active, and
 * pauses while the tab is hidden. Timing state is per hook call, so a component
 * with several tracked queries should call this once per query.
 */
export function useActivePolling<TData>({
  isActive,
  signature,
}: ActivePollingOptions<TData>): ActivePollingResult<TData> {
  const visible = useSyncExternalStore(
    subscribeVisibility,
    getVisibility,
    getVisibilityOnServer,
  );
  const lastSignature = useRef<string | null>(null);
  const lastChangeAt = useRef(0);

  const refetchInterval = useCallback(
    (query: QueryLike<TData>): number | false => {
      const data = query.state.data;
      if (!isActive(data)) {
        lastSignature.current = null;
        lastChangeAt.current = 0;
        return false;
      }
      // Hidden tabs stop polling. Returning to the tab re-renders through the
      // visibility store, which recomputes this from the last known change.
      if (!visible) {
        return false;
      }
      const now = Date.now();
      const current = signature(data);
      if (lastSignature.current !== current || lastChangeAt.current === 0) {
        lastSignature.current = current;
        lastChangeAt.current = now;
      }
      const sinceChange = now - lastChangeAt.current;
      if (sinceChange < POLL_SETTLED_AFTER_MS) {
        return POLL_ACTIVE_MS;
      }
      if (sinceChange < POLL_STALE_AFTER_MS) {
        return POLL_SETTLED_MS;
      }
      return POLL_STALE_MS;
    },
    [isActive, signature, visible],
  );

  return { refetchInterval, refetchIntervalInBackground: false };
}

/**
 * Fingerprint helper for a collection of status-bearing records. Accepts any
 * element shape so declared interfaces (which have no index signature) and
 * loose `Record` rows can both be passed without casting at the call site.
 */
export function statusSignature(
  items: readonly unknown[] | undefined,
  idKey = "id",
): string {
  if (!items?.length) {
    return "";
  }
  return items
    .map((item) => {
      const record = (item ?? {}) as Record<string, unknown>;
      return `${String(record[idKey] ?? "")}:${String(record.status ?? "")}`;
    })
    .join(",");
}
