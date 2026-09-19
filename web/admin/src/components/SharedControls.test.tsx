import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { SwitchRow } from "./SwitchRow";
import { ToggleChips } from "./ToggleChips";

afterEach(cleanup);

it("labels the switch and describes it with the help text", () => {
  const change = vi.fn();
  render(
    <SwitchRow
      label="Cache tiles"
      description="Stores rendered tiles on disk."
      checked={false}
      stateLabel={["Enabled", "Disabled"]}
      onCheckedChange={change}
    />,
  );
  const control = screen.getByRole("switch", { name: "Cache tiles" });
  expect(control).toHaveAccessibleDescription("Stores rendered tiles on disk.");
  expect(screen.getByText("Disabled")).toBeVisible();
  fireEvent.click(control);
  expect(change).toHaveBeenCalledWith(true);
});

it("toggles chips and keeps unknown selected values", () => {
  const change = vi.fn();
  render(
    <ToggleChips
      options={[
        { value: "admin", label: "Admin" },
        { value: "viewer", label: "Viewer" },
      ]}
      value={["admin", "legacy"]}
      onChange={change}
    />,
  );
  expect(screen.getByRole("checkbox", { name: "Admin" })).toBeChecked();
  expect(
    screen.getByRole("checkbox", { name: "legacy (current)" }),
  ).toBeChecked();
  fireEvent.click(screen.getByRole("checkbox", { name: "Viewer" }));
  expect(change).toHaveBeenLastCalledWith(["admin", "legacy", "viewer"]);
  fireEvent.click(screen.getByRole("checkbox", { name: "Admin" }));
  expect(change).toHaveBeenLastCalledWith(["legacy"]);
});

it("enters sizes in MiB and stores bytes", async () => {
  const { ByteSizeInput } = await import("./ByteSizeInput");
  const change = vi.fn();
  render(<ByteSizeInput id="quota" value={2 * 1024 ** 3} onChange={change} />);
  const input = document.getElementById("quota") as HTMLInputElement;
  expect(input.value).toBe("2");
  expect(screen.getByRole("combobox", { name: "Unit" })).toHaveValue(
    String(1024 ** 3),
  );
  fireEvent.change(input, { target: { value: "1.5" } });
  expect(change).toHaveBeenLastCalledWith(Math.round(1.5 * 1024 ** 3));
});
