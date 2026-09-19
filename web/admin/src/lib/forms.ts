import { useEffect } from "react";
import { useBlocker } from "react-router";
import type { FieldValues, Path, UseFormReturn } from "react-hook-form";
import { ApiError } from "@/api/client";
import { confirmDiscard } from "@/lib/confirm";

/**
 * Root error key used for server failures that cannot be attributed to a single
 * field. Render it as a form-level alert.
 */
export const SERVER_ERROR_KEY = "root.server" as const;

/**
 * Maps a failed mutation onto the form.
 *
 * The management API reports errors as `{code, message, detail}` with no
 * per-field structure, so attribution is necessarily heuristic: if the message
 * or detail names one of the form's own fields, the error is attached there;
 * otherwise it becomes a form-level error. Never invent a field match — a
 * misattributed error is worse than a general one.
 */
export function applyServerError<TValues extends FieldValues>(
  form: UseFormReturn<TValues>,
  error: unknown,
  fields: readonly Path<TValues>[] = [],
): void {
  const apiError = error instanceof ApiError ? error : null;
  const message = apiError
    ? [apiError.message, apiError.detail].filter(Boolean).join(": ")
    : error instanceof Error
      ? error.message
      : "The request failed.";

  const haystack = message.toLowerCase();
  const matched = fields.find((field) => {
    const leaf = String(field).split(".").pop() ?? String(field);
    // Require a word boundary so `id` does not match inside `invalid`.
    return new RegExp(`\\b${escapeRegExp(leaf.toLowerCase())}\\b`).test(
      haystack,
    );
  });

  if (matched) {
    form.setError(matched, { type: "server", message });
    return;
  }
  form.setError(SERVER_ERROR_KEY as Path<TValues>, {
    type: "server",
    message,
  });
}

function escapeRegExp(value: string): string {
  return value.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}

/**
 * Blocks in-app navigation and browser unload while a form holds unsaved edits.
 * Returns the router blocker so a caller can render its own confirmation UI;
 * passing `confirmMessage` uses a native confirm instead.
 */
export function useUnsavedChangesGuard(
  when: boolean,
  confirmMessage = "Discard unsaved changes?",
) {
  const blocker = useBlocker(
    ({ currentLocation, nextLocation }) =>
      when && currentLocation.pathname !== nextLocation.pathname,
  );

  useEffect(() => {
    if (blocker.state !== "blocked") {
      return;
    }
    let active = true;
    void confirmDiscard(confirmMessage).then((confirmed) => {
      if (!active) return;
      if (confirmed) blocker.proceed();
      else blocker.reset();
    });
    return () => {
      active = false;
    };
  }, [blocker, confirmMessage]);

  useEffect(() => {
    if (!when) {
      return;
    }
    function onBeforeUnload(event: BeforeUnloadEvent) {
      event.preventDefault();
      // Browsers ignore custom text but require the assignment to prompt.
      event.returnValue = "";
    }
    window.addEventListener("beforeunload", onBeforeUnload);
    return () => window.removeEventListener("beforeunload", onBeforeUnload);
  }, [when]);

  return blocker;
}
