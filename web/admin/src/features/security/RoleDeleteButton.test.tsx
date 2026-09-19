import { cleanup, fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { renderScreen } from "@/test/render";
import { RoleDeleteButton } from "./RoleDeleteButton";
const { inspect, remove } = vi.hoisted(() => ({
  inspect: vi.fn(),
  remove: vi.fn(),
}));
vi.mock("@/api/generated/roles/roles", () => ({
  getRoleDeletionPlan: inspect,
  deleteRole: remove,
  getListRolesQueryKey: () => ["roles"],
  getListRolePoliciesQueryKey: (id: string) => ["policies", id],
}));
vi.mock("sonner", () => ({ toast: { success: vi.fn() } }));
beforeEach(() => {
  inspect.mockReset();
  remove.mockReset();
  remove.mockResolvedValue(undefined);
});
afterEach(cleanup);
it("shows dependencies and prevents deletion while assigned", async () => {
  inspect.mockResolvedValue({
    role_id: "custom",
    is_system: false,
    dependencies: { sessions: 2, api_keys: 1 },
  });
  renderScreen(<RoleDeleteButton id="custom" system={false} />);
  fireEvent.click(screen.getByRole("button", { name: "Delete" }));
  expect(await screen.findByText("sessions: 2")).toBeInTheDocument();
  expect(screen.getByRole("button", { name: "Delete role" })).toBeDisabled();
  expect(remove).not.toHaveBeenCalled();
});
it("deletes only after a successful empty dependency plan and confirmation", async () => {
  inspect.mockResolvedValue({
    role_id: "custom",
    is_system: false,
    dependencies: { sessions: 0 },
  });
  renderScreen(<RoleDeleteButton id="custom" system={false} />);
  fireEvent.click(screen.getByRole("button", { name: "Delete" }));
  await screen.findByText("No active credentials or publication assignments.");
  expect(remove).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole("button", { name: "Delete role" }));
  await waitFor(() => expect(remove).toHaveBeenCalledWith("custom"));
});
