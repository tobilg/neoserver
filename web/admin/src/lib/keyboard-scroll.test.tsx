import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, expect, it } from "vitest";
import { scrollHorizontally } from "./keyboard-scroll";

afterEach(cleanup);
function dimensions(element: HTMLElement) {
  Object.defineProperties(element, {
    scrollWidth: { value: 600 },
    clientWidth: { value: 200 },
  });
  element.scrollLeft = 100;
}
it("scrolls a focused region horizontally using arrow keys", () => {
  render(
    <div role="region" tabIndex={0} onKeyDown={scrollHorizontally}>
      Wide content
    </div>,
  );
  const region = screen.getByRole("region");
  dimensions(region);
  fireEvent.keyDown(region, { key: "ArrowRight" });
  expect(region.scrollLeft).toBe(164);
  fireEvent.keyDown(region, { key: "ArrowLeft" });
  expect(region.scrollLeft).toBe(100);
});
it("preserves input cursor keys, modified keys and vertical scrolling", () => {
  render(
    <div role="region" tabIndex={0} onKeyDown={scrollHorizontally}>
      <input aria-label="Target name" />
    </div>,
  );
  const region = screen.getByRole("region");
  dimensions(region);
  fireEvent.keyDown(screen.getByRole("textbox"), { key: "ArrowRight" });
  fireEvent.keyDown(region, { key: "ArrowRight", shiftKey: true });
  fireEvent.keyDown(region, { key: "ArrowRight", metaKey: true });
  fireEvent.keyDown(region, { key: "ArrowDown" });
  expect(region.scrollLeft).toBe(100);
});
it("can scroll a stock table's wrapper without changing the UI primitive", () => {
  render(
    <div role="region">
      <table
        tabIndex={0}
        onKeyDown={(event) =>
          scrollHorizontally(event, event.currentTarget.parentElement)
        }
      >
        <tbody>
          <tr>
            <td>Wide content</td>
          </tr>
        </tbody>
      </table>
    </div>,
  );
  const region = screen.getByRole("region");
  dimensions(region);
  fireEvent.keyDown(screen.getByRole("table"), { key: "ArrowRight" });
  expect(region.scrollLeft).toBe(164);
});
