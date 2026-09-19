import { lazy, Suspense, useEffect, useRef, useState } from "react";
import { Search } from "lucide-react";
import { Button } from "@/components/ui/button";

const loadPalette = () => import("./CommandPalette");
const CommandPalette = lazy(loadPalette);

/** Text typed while the palette is still opening belongs in its search. */
function isTypingKey(event: KeyboardEvent) {
  if (event.metaKey || event.ctrlKey || event.altKey) return false;
  return event.key.length === 1 || event.key === "Backspace";
}

export interface CommandDestination {
  label: string;
  to: string;
}

/**
 * ⌘K / Ctrl+K palette for jumping to console pages, workspaces and
 * publications. The catalog is only queried while the palette is open.
 */
export function CommandMenu({
  workspace,
  pages,
}: {
  workspace?: string;
  pages: CommandDestination[];
}) {
  const [open, setOpenState] = useState(false);
  const [used, setUsed] = useState(false);
  const [query, setQuery] = useState("");
  const openRef = useRef(false);
  if (open && !used) setUsed(true);
  const setOpen = (value: boolean | ((current: boolean) => boolean)) => {
    setOpenState((current) => {
      const next = typeof value === "function" ? value(current) : value;
      openRef.current = next;
      if (!next) setQuery("");
      return next;
    });
  };
  useEffect(() => {
    // Fetch the palette ahead of the first ⌘K so it opens without delay.
    const idle = window.requestIdleCallback
      ? window.requestIdleCallback(() => void loadPalette())
      : window.setTimeout(() => void loadPalette(), 1500);
    return () =>
      window.cancelIdleCallback
        ? window.cancelIdleCallback(idle)
        : window.clearTimeout(idle);
  }, []);
  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key.toLowerCase() === "k" && (event.metaKey || event.ctrlKey)) {
        event.preventDefault();
        setOpen((value) => !value);
        return;
      }
      // Until the search field has focus, collect what is typed for it.
      const target = event.target as HTMLElement | null;
      if (
        openRef.current &&
        isTypingKey(event) &&
        !target?.closest("input, textarea, [contenteditable]")
      ) {
        event.preventDefault();
        setQuery((current) =>
          event.key === "Backspace"
            ? current.slice(0, -1)
            : current + event.key,
        );
      }
    };
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, []);
  return (
    <>
      <Button
        variant="ghost"
        size="icon"
        className="md:hidden"
        aria-label="Search pages and publications"
        onClick={() => setOpen(true)}
      >
        <Search />
      </Button>
      <Button
        variant="outline"
        size="sm"
        className="hidden gap-2 text-muted-foreground md:inline-flex"
        onClick={() => setOpen(true)}
      >
        <Search /> Go to…
        <kbd className="rounded border px-1 font-mono text-[11px]">⌘K</kbd>
      </Button>
      {/* Stay mounted after first use so the close animation can run. */}
      {(open || used) && (
        <Suspense fallback={null}>
          <CommandPalette
            open={open}
            onOpenChange={setOpen}
            workspace={workspace}
            pages={pages}
            query={query}
            onQueryChange={setQuery}
          />
        </Suspense>
      )}
    </>
  );
}
