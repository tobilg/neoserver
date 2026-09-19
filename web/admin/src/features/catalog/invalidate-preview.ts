import type { QueryClient } from "@tanstack/react-query";
import { basePath } from "@/api/client";
import { getGetWorkspaceSummaryQueryKey } from "@/api/generated/workspaces/workspaces";

export function invalidatePreview(client: QueryClient, workspace: string) {
  return Promise.all([
    client.invalidateQueries({
      queryKey: [
        basePath +
          "/workspaces/" +
          encodeURIComponent(workspace) +
          "/ogc/collections",
      ],
    }),
    client.invalidateQueries({
      queryKey: getGetWorkspaceSummaryQueryKey(workspace),
    }),
  ]);
}
