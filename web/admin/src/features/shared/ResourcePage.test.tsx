import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { createMemoryRouter, RouterProvider } from "react-router";
import { afterEach, describe, expect, it, vi } from "vitest";
import { ResourcePage } from "./ResourcePage";

vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));

const rows = [
  { id: "1", name: "alpha", enabled: true },
  { id: "2", name: "bravo", enabled: false },
];

function renderPage(
  props: Partial<React.ComponentProps<typeof ResourcePage>> = {},
) {
  // ResourcePage's unsaved-changes guard uses useBlocker, which requires a
  // data router rather than the plain MemoryRouter.
  const router = createMemoryRouter(
    [
      {
        path: "/workspaces/:ws/stores",
        element: (
          <ResourcePage
            title="Stores"
            description="Connected data sources."
            rows={rows}
            columns={["name", "enabled"]}
            urlKey="services"
            onRefresh={vi.fn()}
            {...props}
          />
        ),
      },
    ],
    { initialEntries: ["/workspaces/demo/stores"] },
  );
  return render(<RouterProvider router={router} />);
}

afterEach(cleanup);

describe("ResourcePage", () => {
  it("does not render cached rows or their actions after permission denial", () => {
    renderPage({
      error: Object.assign(new Error("Access denied"), { status: 403 }),
      onUpdate: vi.fn(),
    });
    expect(screen.getByRole("alert")).toHaveTextContent("Access denied");
    expect(screen.queryByText("alpha")).not.toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "Edit alpha" }),
    ).not.toBeInTheDocument();
  });
  it("renders the supplied rows", () => {
    renderPage();
    expect(screen.getByText("alpha")).toBeInTheDocument();
    expect(screen.getByText("bravo")).toBeInTheDocument();
  });

  it("hides create, edit and delete unless a handler is supplied", () => {
    renderPage();
    expect(
      screen.queryByRole("button", { name: /create/i }),
    ).not.toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: /^Edit /i }),
    ).not.toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: /^Delete /i }),
    ).not.toBeInTheDocument();
  });

  it("creates from the JSON editor and closes on success", async () => {
    const onCreate = vi.fn().mockResolvedValue(undefined);
    renderPage({
      createTemplate: { name: "" },
      createLabel: "Create",
      onCreate,
    });

    fireEvent.click(screen.getByRole("button", { name: /^Create$/ }));
    fireEvent.click(screen.getByRole("button", { name: "Advanced JSON" }));
    fireEvent.change(screen.getByLabelText("Create payload"), {
      target: { value: '{"name":"charlie"}' },
    });
    fireEvent.click(
      screen.getAllByRole("button", { name: /^Create$/ }).at(-1)!,
    );

    await waitFor(() =>
      expect(onCreate).toHaveBeenCalledWith({ name: "charlie" }),
    );
  });

  it("reports malformed JSON without calling the server", async () => {
    const onCreate = vi.fn();
    renderPage({ createTemplate: { name: "" }, onCreate });

    fireEvent.click(screen.getByRole("button", { name: /^Create$/ }));
    fireEvent.click(screen.getByRole("button", { name: "Advanced JSON" }));
    fireEvent.change(screen.getByLabelText("Create payload"), {
      target: { value: "{not json" },
    });
    fireEvent.click(
      screen.getAllByRole("button", { name: /^Create$/ }).at(-1)!,
    );

    await waitFor(() =>
      expect(document.querySelector(".text-destructive")).toBeInTheDocument(),
    );
    expect(onCreate).not.toHaveBeenCalled();
  });

  it("loads the full representation before editing", async () => {
    const onLoadItem = vi
      .fn()
      .mockResolvedValue({ id: "1", name: "alpha", secret: "kept" });
    const onUpdate = vi.fn().mockResolvedValue(undefined);
    renderPage({ onLoadItem, onUpdate });

    fireEvent.click(screen.getByRole("button", { name: "Edit alpha" }));
    await waitFor(() => expect(onLoadItem).toHaveBeenCalled());
    fireEvent.click(
      await screen.findByRole("button", { name: "Advanced JSON" }),
    );

    const editor = await screen.findByLabelText("Edit stores payload");
    expect((editor as HTMLTextAreaElement).value).toContain("kept");
  });

  it("confirms before deleting and passes the row through", async () => {
    const onDelete = vi.fn().mockResolvedValue(undefined);
    renderPage({ onDelete });

    fireEvent.click(screen.getByRole("button", { name: "Delete bravo" }));
    await screen.findByText("Delete resource?");
    fireEvent.click(screen.getByRole("button", { name: /^Delete$/ }));

    await waitFor(() => expect(onDelete).toHaveBeenCalledWith(rows[1]));
  });

  it("shows an empty state rather than a bare table", () => {
    renderPage({ rows: [] });
    expect(screen.getByText("No stores yet.")).toBeInTheDocument();
  });
});
