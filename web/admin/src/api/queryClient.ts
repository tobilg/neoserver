import { MutationCache, QueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { ApiError } from "./client";

/**
 * Set `meta.suppressErrorToast` on a mutation that already reports failure in
 * place -- a form that maps server errors onto fields, or a dialog that shows
 * the message inline. Without it the operator sees the same failure twice.
 */
declare module "@tanstack/react-query" {
  interface Register {
    mutationMeta: { suppressErrorToast?: boolean };
  }
}

function describe(error: unknown): string {
  if (error instanceof ApiError) {
    return [error.message, error.detail].filter(Boolean).join(": ");
  }
  return error instanceof Error ? error.message : "The request failed.";
}

export const queryClient = new QueryClient({
  // One place to report every mutation failure, rather than an ad-hoc error
  // line per screen. Queries are excluded: a failed read belongs in the panel
  // that was trying to render it, not in a toast.
  mutationCache: new MutationCache({
    onError: (error, _variables, _context, mutation) => {
      if (mutation.meta?.suppressErrorToast) {
        return;
      }
      if (error instanceof Error && error.name === "AbortError") return;
      // A 401 already redirects to login; a toast on the way out is noise.
      if (error instanceof ApiError && error.status === 401) {
        return;
      }
      toast.error(describe(error));
    },
  }),
  defaultOptions: {
    queries: {
      staleTime: 20_000,
      retry: (count, error) =>
        !(
          error instanceof Error &&
          "status" in error &&
          [401, 403].includes(Number(error.status))
        ) &&
        !(error instanceof Error && error.name === "AbortError") &&
        count < 2,
    },
    mutations: { retry: false },
  },
});
