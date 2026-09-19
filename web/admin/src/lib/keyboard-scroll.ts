import type { KeyboardEvent } from "react";

/** Explicit horizontal navigation also works in WebKit's focusable regions. */
export function scrollHorizontally(
  event: KeyboardEvent<HTMLElement>,
  region: HTMLElement | null = event.currentTarget,
) {
  // Inputs and other descendants keep their native cursor/selection controls.
  if (
    event.target !== event.currentTarget ||
    event.altKey ||
    event.ctrlKey ||
    event.metaKey ||
    event.shiftKey
  )
    return;
  if (!region || region.scrollWidth <= region.clientWidth) return;
  if (event.key !== "ArrowLeft" && event.key !== "ArrowRight") return;
  event.preventDefault();
  region.scrollLeft += event.key === "ArrowRight" ? 64 : -64;
}
