import {
  useCallback,
  useEffect,
  useId,
  useRef,
  useState,
  type CSSProperties,
  type ReactNode,
} from "react";
import { MoreHorizontal } from "lucide-react";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";

/**
 * Row actions for list tables. Wide tables show them inline; narrow tables
 * (cards) collapse them behind one ⋯ button that opens them as a menu. The
 * actions render once in both layouts, so dialogs they own never remount.
 */
export function RowActions({
  label,
  children,
}: {
  label: string;
  children: ReactNode;
}) {
  const [open, setOpen] = useState(false);
  const [position, setPosition] = useState<CSSProperties>();
  const id = useId();
  const trigger = useRef<HTMLButtonElement>(null);
  const panel = useRef<HTMLDivElement>(null);
  const close = useCallback((restoreFocus = false) => {
    setOpen(false);
    if (restoreFocus) trigger.current?.focus();
  }, []);
  function toggle() {
    if (open) return close();
    const button = trigger.current;
    // The table root is a size container, so it is the containing block of
    // this fixed panel; offsets are relative to it and escape row clipping.
    const root = button?.closest<HTMLElement>("[data-table-root]");
    if (button && root) {
      const b = button.getBoundingClientRect();
      const r = root.getBoundingClientRect();
      setPosition({ top: b.bottom - r.top + 4, right: r.right - b.right });
    }
    setOpen(true);
  }
  useEffect(() => {
    if (!open) return;
    panel.current
      ?.querySelector<HTMLElement>("button:not(:disabled), a[href]")
      ?.focus();
    const outside = (event: PointerEvent) => {
      const target = event.target as Node;
      if (
        !panel.current?.contains(target) &&
        !trigger.current?.contains(target)
      )
        close();
    };
    const key = (event: KeyboardEvent) => {
      if (event.key === "Escape") close(true);
    };
    const move = () => close();
    document.addEventListener("pointerdown", outside);
    document.addEventListener("keydown", key);
    window.addEventListener("resize", move);
    window.addEventListener("scroll", move, true);
    return () => {
      document.removeEventListener("pointerdown", outside);
      document.removeEventListener("keydown", key);
      window.removeEventListener("resize", move);
      window.removeEventListener("scroll", move, true);
    };
  }, [open, close]);
  return (
    <>
      <Button
        ref={trigger}
        className="size-11 @2xl/table:hidden"
        variant="outline"
        size="icon"
        aria-label={`Actions for ${label}`}
        aria-haspopup="true"
        aria-expanded={open}
        aria-controls={id}
        onClick={toggle}
      >
        <MoreHorizontal />
      </Button>
      <div
        id={id}
        ref={panel}
        role="group"
        aria-label={`Actions for ${label}`}
        style={position}
        onClick={(event) => {
          // Choosing an action closes the menu; dialogs keep their own state.
          if ((event.target as HTMLElement).closest("button, a")) close();
        }}
        className={cn(
          open ? "flex" : "hidden",
          // Narrow: a floating menu of full-width items.
          "fixed z-30 w-64 flex-col gap-0.5 rounded-lg border bg-popover p-1 text-popover-foreground shadow-lg",
          "@max-2xl/table:[&_div]:flex @max-2xl/table:[&_div]:w-full @max-2xl/table:[&_div]:flex-col @max-2xl/table:[&_div]:items-stretch @max-2xl/table:[&_div]:gap-0.5",
          "@max-2xl/table:[&_:is(button,a)]:h-auto @max-2xl/table:[&_:is(button,a)]:min-h-11 @max-2xl/table:[&_:is(button,a)]:w-full @max-2xl/table:[&_:is(button,a)]:justify-start @max-2xl/table:[&_:is(button,a)]:border-0 @max-2xl/table:[&_:is(button,a)]:bg-transparent @max-2xl/table:[&_:is(button,a)]:px-3 @max-2xl/table:[&_:is(button,a)]:text-foreground @max-2xl/table:[&_:is(button,a)]:whitespace-normal @max-2xl/table:[&_:is(button,a)]:shadow-none",
          "@max-2xl/table:[&_:is(button,a):hover]:bg-muted",
          // Icon-only actions show their accessible name as the item text.
          "@max-2xl/table:[&_button[data-size^=icon]]:gap-2 @max-2xl/table:[&_button[data-size^=icon]]:after:content-[attr(aria-label)]",
          // Wide: plain inline actions.
          "@2xl/table:static @2xl/table:flex @2xl/table:w-auto @2xl/table:flex-row @2xl/table:items-center @2xl/table:justify-end @2xl/table:gap-1 @2xl/table:rounded-none @2xl/table:border-0 @2xl/table:bg-transparent @2xl/table:p-0 @2xl/table:shadow-none",
        )}
      >
        {children}
      </div>
    </>
  );
}

export function RowDetails({ children }: { children: ReactNode }) {
  const [open, setOpen] = useState(false);
  return (
    <details
      className="mt-2 @2xl/table:hidden"
      onToggle={(event) => setOpen(event.currentTarget.open)}
    >
      <summary className="min-h-11 cursor-pointer py-3 text-xs text-muted-foreground">
        Details and status
      </summary>
      {open && <dl className="space-y-2 text-xs">{children}</dl>}
    </details>
  );
}
