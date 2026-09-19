import type { ReactNode } from "react";
import { Button } from "@/components/ui/button";

/**
 * The one pager for server-paged lists: a summary plus Newer/Older. It stays
 * out of the way when everything fits on one page.
 */
export function CursorPager({
  summary,
  onNewer,
  onOlder,
  busy = false,
  newerLabel = "Newer",
  olderLabel = "Older",
}: {
  summary: ReactNode;
  /** Undefined when already on the newest page. */
  onNewer?: () => void;
  /** Undefined when there is nothing older. */
  onOlder?: () => void;
  busy?: boolean;
  newerLabel?: string;
  olderLabel?: string;
}) {
  const paged = Boolean(onNewer || onOlder);
  return (
    <div
      className="flex flex-wrap items-center justify-between gap-2 text-xs text-muted-foreground"
      aria-live="polite"
    >
      <span>{summary}</span>
      {paged && (
        <div className="flex gap-2">
          <Button
            variant="outline"
            size="sm"
            disabled={!onNewer || busy}
            onClick={onNewer}
          >
            {newerLabel}
          </Button>
          <Button
            variant="outline"
            size="sm"
            disabled={!onOlder || busy}
            onClick={onOlder}
          >
            {olderLabel}
          </Button>
        </div>
      )}
    </div>
  );
}
