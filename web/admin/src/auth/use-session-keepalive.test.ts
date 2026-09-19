import { renderHook } from "@testing-library/react";
import { act } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  ACTIVITY_WINDOW_MS,
  KEEPALIVE_CHECK_MS,
  KEEPALIVE_THRESHOLD_MS,
  useSessionKeepalive,
} from "./use-session-keepalive";

function inMinutes(minutes: number) {
  return new Date(Date.now() + minutes * 60_000).toISOString();
}

/** Advances past one evaluation tick. */
function tick(times = 1) {
  act(() => {
    vi.advanceTimersByTime(KEEPALIVE_CHECK_MS * times + 10);
  });
}

function interact() {
  act(() => {
    window.dispatchEvent(new Event("pointerdown"));
  });
}

describe("useSessionKeepalive", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date("2026-08-06T09:00:00Z"));
  });

  afterEach(() => {
    vi.useRealTimers();
    vi.restoreAllMocks();
  });

  it("refreshes when an active operator nears the idle timeout", () => {
    const refresh = vi.fn().mockResolvedValue(undefined);
    renderHook(() =>
      useSessionKeepalive({
        idleExpiresAt: inMinutes(5),
        refresh,
        enabled: true,
      }),
    );
    interact();
    tick();
    expect(refresh).toHaveBeenCalledOnce();
  });

  it("leaves a session alone while the idle window is comfortable", () => {
    const refresh = vi.fn().mockResolvedValue(undefined);
    renderHook(() =>
      useSessionKeepalive({
        idleExpiresAt: inMinutes(KEEPALIVE_THRESHOLD_MS / 60_000 + 20),
        refresh,
        enabled: true,
      }),
    );
    interact();
    tick(3);
    expect(refresh).not.toHaveBeenCalled();
  });

  it("does not treat a passive mount or rerender as activity", () => {
    const refresh = vi.fn().mockResolvedValue(undefined);
    const hook = renderHook(() =>
      useSessionKeepalive({
        idleExpiresAt: inMinutes(5),
        refresh,
        enabled: true,
      }),
    );
    tick(2);
    hook.rerender();
    tick(2);
    expect(refresh).not.toHaveBeenCalled();
  });

  it("stops refreshing once the tab is abandoned", () => {
    const refresh = vi.fn().mockResolvedValue(undefined);
    renderHook(() =>
      useSessionKeepalive({
        idleExpiresAt: inMinutes(5),
        refresh,
        enabled: true,
      }),
    );

    // Actual interaction counts as presence, not mounting the tab.
    interact();
    tick();
    const afterInteraction = refresh.mock.calls.length;
    expect(afterInteraction).toBeGreaterThan(0);

    // Nobody touches the tab again; once the activity window lapses the
    // keepalive must let the session expire rather than holding it open.
    act(() => {
      vi.advanceTimersByTime(ACTIVITY_WINDOW_MS + 60_000);
    });
    const afterLapse = refresh.mock.calls.length;
    tick(3);
    expect(refresh.mock.calls.length).toBe(afterLapse);
  });

  it("does not refresh from a hidden tab", () => {
    vi.spyOn(document, "hidden", "get").mockReturnValue(true);
    const refresh = vi.fn().mockResolvedValue(undefined);
    renderHook(() =>
      useSessionKeepalive({
        idleExpiresAt: inMinutes(5),
        refresh,
        enabled: true,
      }),
    );
    interact();
    tick(2);
    expect(refresh).not.toHaveBeenCalled();
  });

  it("does nothing when disabled or without an idle expiry", () => {
    const refresh = vi.fn().mockResolvedValue(undefined);
    const disabled = renderHook(() =>
      useSessionKeepalive({
        idleExpiresAt: inMinutes(1),
        refresh,
        enabled: false,
      }),
    );
    interact();
    tick();
    expect(refresh).not.toHaveBeenCalled();
    disabled.unmount();

    renderHook(() =>
      useSessionKeepalive({ idleExpiresAt: null, refresh, enabled: true }),
    );
    interact();
    tick();
    expect(refresh).not.toHaveBeenCalled();
  });

  it("does not stack refreshes while one is in flight", async () => {
    let release: () => void = () => {};
    const refresh = vi.fn().mockImplementation(
      () =>
        new Promise<void>((resolve) => {
          release = resolve;
        }),
    );
    renderHook(() =>
      useSessionKeepalive({
        idleExpiresAt: inMinutes(5),
        refresh,
        enabled: true,
      }),
    );
    interact();
    tick();
    interact();
    tick();
    expect(refresh).toHaveBeenCalledOnce();

    await act(async () => {
      release();
    });
    interact();
    tick();
    expect(refresh).toHaveBeenCalledTimes(2);
  });

  it("survives a rejected refresh", async () => {
    const refresh = vi.fn().mockRejectedValue(new Error("session expired"));
    renderHook(() =>
      useSessionKeepalive({
        idleExpiresAt: inMinutes(5),
        refresh,
        enabled: true,
      }),
    );
    interact();
    tick();
    await act(async () => {});
    // A later attempt is still possible; the rejection must not wedge the guard.
    interact();
    tick();
    expect(refresh).toHaveBeenCalledTimes(2);
  });
});
