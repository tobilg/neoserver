import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { PyramidMeter } from "./PyramidMeter";

describe("PyramidMeter", () => {
  it("aggregates progress across zoom levels", () => {
    render(
      <PyramidMeter
        levels={[
          { zoom: 0, total_tiles: 1, processed_tiles: 1 },
          { zoom: 1, total_tiles: 3, processed_tiles: 1 },
        ]}
      />,
    );
    expect(screen.getByRole("progressbar")).toHaveAttribute(
      "aria-valuenow",
      "50",
    );
    expect(screen.getByText("z0")).toBeInTheDocument();
    expect(screen.getByText("z1")).toBeInTheDocument();
  });
});
