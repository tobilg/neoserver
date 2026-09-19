import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { useState } from "react";
import { afterEach, describe, expect, it } from "vitest";
import { ObjectEditor } from "./SchemaFields";

afterEach(cleanup);
const initial = {
  title: "Roads",
  crs_default: 4326,
  dimensions: [{ name: "time" }],
  server_extension: "keep",
};
function Editor({ revealAdvanced = false }: { revealAdvanced?: boolean }) {
  const [draft, setDraft] = useState(JSON.stringify(initial));
  return (
    <ObjectEditor
      schema={{
        type: "object",
        properties: {
          title: { type: "string" },
          crs_default: { type: "integer" },
          dimensions: {
            type: "array",
            items: { type: "object", properties: { name: { type: "string" } } },
          },
        },
      }}
      draft={draft}
      onChange={setDraft}
      label="Publication"
      primaryFields={["title"]}
      revealAdvanced={revealAdvanced}
    />
  );
}
describe("publication field disclosure", () => {
  it("makes optional objects explicit, omits disabled overrides and restores their draft", () => {
    function OptionalEditor() {
      const [draft, setDraft] = useState(JSON.stringify({ extension: "keep" }));
      return (
        <ObjectEditor
          label="Optional settings"
          draft={draft}
          onChange={setDraft}
          schema={{
            type: "object",
            properties: {
              extent: {
                type: "object",
                required: ["minx"],
                properties: { minx: { type: "number" } },
              },
            },
          }}
        />
      );
    }
    render(<OptionalEditor />);
    expect(screen.queryByLabelText("Minx *")).not.toBeInTheDocument();
    const override = screen.getByRole("switch", { name: "Override extent" });
    fireEvent.click(override);
    fireEvent.change(screen.getByLabelText("Minx *"), {
      target: { value: "42" },
    });
    fireEvent.click(override);
    expect(screen.queryByLabelText("Minx *")).not.toBeInTheDocument();
    fireEvent.click(override);
    expect(screen.getByLabelText("Minx *")).toHaveValue(42);
    fireEvent.click(override);
    fireEvent.click(screen.getByRole("button", { name: "Advanced JSON" }));
    expect(
      JSON.parse(
        (
          screen.getByLabelText(
            "Optional settings payload",
          ) as HTMLTextAreaElement
        ).value,
      ),
    ).toEqual({ extension: "keep" });
  });
  it("keeps hidden and unknown values when editing common fields and roundtripping JSON", () => {
    render(<Editor />);
    expect(screen.getByLabelText("Default CRS (EPSG code)")).not.toBeVisible();
    fireEvent.change(screen.getByLabelText("Title"), {
      target: { value: "Renamed" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Advanced JSON" }));
    expect(
      JSON.parse(
        (screen.getByLabelText("Publication payload") as HTMLTextAreaElement)
          .value,
      ),
    ).toEqual({ ...initial, title: "Renamed" });
    fireEvent.click(screen.getByRole("button", { name: "Use form" }));
    expect(screen.getByLabelText("Title")).toHaveValue("Renamed");
  });
  it("opens advanced fields after a save error without resetting the draft", () => {
    const view = render(<Editor />);
    fireEvent.change(screen.getByLabelText("Title"), {
      target: { value: "Draft" },
    });
    view.rerender(<Editor revealAdvanced />);
    expect(screen.getByLabelText("Default CRS (EPSG code)")).toBeVisible();
    expect(screen.getByLabelText("Title")).toHaveValue("Draft");
  });
});
