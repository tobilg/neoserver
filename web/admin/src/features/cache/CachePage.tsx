import { toast } from "sonner";
import { useClearWorkspaceCache } from "@/api/generated/cache/cache";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";

/** The workspace's in-memory response cache, shown on the Caching page. */
export function ResponseCacheCard({ workspace }: { workspace: string }) {
  const clear = useClearWorkspaceCache({
    mutation: { onSuccess: () => toast.success("Response cache cleared") },
  });
  return (
    <Card>
      <CardHeader>
        <CardTitle>Response cache</CardTitle>
      </CardHeader>
      <CardContent className="space-y-3">
        <p className="text-sm text-muted-foreground">
          In-memory capabilities, collections, features, counts and tiles for
          this workspace. Clearing it makes the next client request rebuild each
          entry; durable cached tiles are kept.
        </p>
        <Button
          variant="outline"
          onClick={() => clear.mutate({ workspace })}
          disabled={clear.isPending}
        >
          {clear.isPending ? "Clearing…" : "Clear response cache"}
        </Button>
        {clear.isSuccess && (
          <p className="text-sm text-success" role="status">
            Response cache cleared.
          </p>
        )}
        {clear.error && (
          <p className="text-sm text-destructive" role="alert">
            {clear.error.message}
          </p>
        )}
      </CardContent>
    </Card>
  );
}
