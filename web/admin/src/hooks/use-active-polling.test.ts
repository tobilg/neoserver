import { renderHook } from "@testing-library/react";
import { act } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  isActiveStatus,
  POLL_ACTIVE_MS,
  POLL_SETTLED_AFTER_MS,
  POLL_SETTLED_MS,
  POLL_STALE_AFTER_MS,
  POLL_STALE_MS,
  statusSignature,
  useActivePolling,
} from "./use-active-polling";

interface Payload {
  status: string;
  processed?: number;
}

function query(data: Payload | undefined) {
  return { state: { data } };
}

function renderPolling() {
  return renderHook(() =>
    useActivePolling<Payload>({
      isActive: (data) => isActiveStatus(data?.status),
      signature: (data) => `${data?.status ?? ""}:${data?.processed ?? 0}`,
    }),
  );
}

describe("isActiveStatus", () => {
  it("recognises in-flight job states", () => {
    for (const status of [
      "queued",
      "running",
      "publishing",
      "cancelling",
      "rolling_back",
    ]) {
      expect(isActiveStatus(status)).toBe(true);
    }
  });

  it("treats terminal and unknown states as inactive", () => {
    for (const status of [
      "published",
      "failed",
      "cancelled",
      "rolled_back",
      "",
      undefined,
      null,
    ]) {
      expect(isActiveStatus(status)).toBe(false);
    }
  });
});

describe("statusSignature", () => {
  it("changes when any row's status changes", () => {
    const before = statusSignature([{ id: "a", status: "running" }]);
    const after = statusSignature([{ id: "a", status: "published" }]);
    expect(before).not.toBe(after);
  });

  it("is stable for identical input and empty for none", () => {
    const rows = [
      { id: "a", status: "running" },
      { id: "b", status: "queued" },
    ];
    expect(statusSignature(rows)).toBe(statusSignature(rows));
    expect(statusSignature([])).toBe("");
    expect(statusSignature(undefined)).toBe("");
  });
});

describe("useActivePolling", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date("2026-08-04T00:00:00Z"));
  });

  afterEach(() => {
    vi.useRealTimers();
    vi.unstubAllGlobals();
  });

  it("does not poll when nothing is active", () => {
    const { result } = renderPolling();
    expect(result.current.refetchInterval(query({ status: "published" }))).toBe(
      false,
    );
    expect(result.current.refetchInterval(query(undefined))).toBe(false);
  });

  it("polls at the active cadence while work is in flight", () => {
    const { result } = renderPolling();
    expect(result.current.refetchInterval(query({ status: "running" }))).toBe(
      POLL_ACTIVE_MS,
    );
  });

  it("steps down only after the payload stops changing", () => {
    const { result } = renderPolling();
    const poll = result.current.refetchInterval;

    expect(poll(query({ status: "running", processed: 1 }))).toBe(
      POLL_ACTIVE_MS,
    );

    // Progress keeps changing, so the fast cadence is retained past the
    // settle threshold.
    vi.advanceTimersByTime(POLL_SETTLED_AFTER_MS + 1_000);
    expect(poll(query({ status: "running", processed: 2 }))).toBe(
      POLL_ACTIVE_MS,
    );

    // Now the payload is unchanged across the settle window.
    vi.advanceTimersByTime(POLL_SETTLED_AFTER_MS + 1_000);
    expect(poll(query({ status: "running", processed: 2 }))).toBe(
      POLL_SETTLED_MS,
    );

    // And unchanged past the stale window.
    vi.advanceTimersByTime(POLL_STALE_AFTER_MS);
    expect(poll(query({ status: "running", processed: 2 }))).toBe(
      POLL_STALE_MS,
    );
  });

  it("returns to the fast cadence when progress resumes", () => {
    const { result } = renderPolling();
    const poll = result.current.refetchInterval;

    poll(query({ status: "running", processed: 1 }));
    vi.advanceTimersByTime(POLL_STALE_AFTER_MS + 1_000);
    expect(poll(query({ status: "running", processed: 1 }))).toBe(
      POLL_STALE_MS,
    );

    expect(poll(query({ status: "running", processed: 2 }))).toBe(
      POLL_ACTIVE_MS,
    );
  });

  it("resets its timer once work finishes", () => {
    const { result } = renderPolling();
    const poll = result.current.refetchInterval;

    poll(query({ status: "running", processed: 1 }));
    vi.advanceTimersByTime(POLL_STALE_AFTER_MS + 1_000);
    expect(poll(query({ status: "running", processed: 1 }))).toBe(
      POLL_STALE_MS,
    );

    expect(poll(query({ status: "published" }))).toBe(false);

    // A new job starts from the fast cadence rather than inheriting the
    // previous run's backoff.
    expect(poll(query({ status: "running", processed: 1 }))).toBe(
      POLL_ACTIVE_MS,
    );
  });

  it("pauses while the tab is hidden and resumes when it returns", () => {
    let hidden = false;
    vi.spyOn(document, "hidden", "get").mockImplementation(() => hidden);

    const { result } = renderPolling();
    expect(result.current.refetchInterval(query({ status: "running" }))).toBe(
      POLL_ACTIVE_MS,
    );

    act(() => {
      hidden = true;
      document.dispatchEvent(new Event("visibilitychange"));
    });
    expect(result.current.refetchInterval(query({ status: "running" }))).toBe(
      false,
    );

    act(() => {
      hidden = false;
      document.dispatchEvent(new Event("visibilitychange"));
    });
    expect(result.current.refetchInterval(query({ status: "running" }))).toBe(
      POLL_ACTIVE_MS,
    );
  });

  it("never refetches in the background", () => {
    const { result } = renderPolling();
    expect(result.current.refetchIntervalInBackground).toBe(false);
  });
});
