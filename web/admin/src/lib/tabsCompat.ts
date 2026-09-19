/**
 * Compatibility shims for `components/ui/tabs.tsx`, which `check:ui-provenance`
 * verifies against the shadcn CLI output and so cannot be edited in place.
 *
 * Two of its selectors never match what Radix renders:
 *
 *   - The root sets `data-orientation="horizontal"` but styles
 *     `data-horizontal:flex-col`, which compiles to `[data-horizontal]`. Without
 *     an override the tab list and the panel lay out side by side.
 *   - The triggers style `data-active:*`, but Radix sets `data-state="active"`,
 *     so the selected tab gets no treatment and the two tabs look identical.
 *
 * Both constants exist only to restore the appearance `tabs.tsx` already
 * intends. Once `npx shadcn@latest add tabs` ships selectors that match Radix,
 * delete this file and drop the classNames at the call sites.
 */

/** Restores the vertical stack that a horizontal tab set is meant to have. */
export const tabsRootFix = "flex-col";

/** Restores the stock active treatment, in both light and dark. */
export const tabsTriggerFix = [
  "data-[state=active]:bg-background data-[state=active]:text-foreground data-[state=active]:shadow-sm",
  "dark:data-[state=active]:border-input dark:data-[state=active]:bg-input/30",
].join(" ");
