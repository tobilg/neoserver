import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { PhaseStrip } from "./PhaseStrip";

describe("PhaseStrip", () => {
  it("uses the importer's diagnostic vocabulary", () => {
    render(<PhaseStrip phase="transform" />);
    expect(
      screen.getByLabelText("Import phase: transform"),
    ).toBeInTheDocument();
    for (const phase of [
      "acquire",
      "discover",
      "validate",
      "transform",
      "preview",
      "publish",
    ]) {
      expect(screen.getByText(phase)).toBeInTheDocument();
    }
  });

  it("marks every phase done once the import completed", () => {
    const { container } = render(<PhaseStrip phase="publish" complete />);
    expect(screen.getByLabelText("Import phases complete")).toBeInTheDocument();
    expect(container.querySelectorAll(".bg-success")).toHaveLength(6);
    expect(container.querySelector(".bg-brand")).toBeNull();
  });
});
