import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { ExtentStrip } from "./ExtentStrip";

describe("ExtentStrip", () => {
  it("projects a WGS84 extent into the fixed overview frame", () => {
    const { container } = render(
      <ExtentStrip
        extent={{ min_x: -180, min_y: -90, max_x: 0, max_y: 0, srid: 4326 }}
      />,
    );
    expect(
      screen.getByRole("img", { name: /-180 to 0 longitude/ }),
    ).toBeInTheDocument();
    const rect = container.querySelector("rect");
    expect(rect).toHaveAttribute("x", "0");
    expect(rect).toHaveAttribute("y", "22");
    expect(rect).toHaveAttribute("width", "44");
    expect(rect).toHaveAttribute("height", "22");
  });

  it("does not pretend a non-geographic extent is projected", () => {
    render(
      <ExtentStrip
        extent={{ min_x: 0, min_y: 0, max_x: 1, max_y: 1, srid: 3857 }}
      />,
    );
    expect(screen.getByText("Not reported")).toBeInTheDocument();
  });
});

describe("ExtentStrip markers", () => {
  it("rings extents too small to see", () => {
    const { container } = render(
      <ExtentStrip
        extent={{ min_x: 0, min_y: 0, max_x: 0.01, max_y: 0.01, srid: 4326 }}
      />,
    );
    expect(container.querySelector("circle[data-marker]")).not.toBeNull();
  });
});
