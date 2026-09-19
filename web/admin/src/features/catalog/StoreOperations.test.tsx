import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, expect, it, vi } from "vitest";
import { EditorView } from "@codemirror/view";
import { SQLViewDialog } from "./StoreOperations";

function typeSQL(value: string) {
  const editor = EditorView.findFromDOM(screen.getByLabelText("SQL query"))!;
  editor.dispatch({
    changes: { from: 0, to: editor.state.doc.length, insert: value },
  });
}

const { validateSQLView } = vi.hoisted(() => ({ validateSQLView: vi.fn() }));
vi.mock("@/api/generated/services/services", async (original) => ({
  ...(await original<object>()),
  validateSQLView,
}));
afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

it("cancels SQL validation without losing the draft or permitting unvalidated publication", async () => {
  let signal: AbortSignal | undefined;
  validateSQLView.mockImplementation(
    (_ws, _service, _body, options) =>
      new Promise((_resolve, reject) => {
        signal = options.signal;
        signal!.addEventListener("abort", () =>
          reject(new DOMException("Aborted", "AbortError")),
        );
      }),
  );
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  render(
    <QueryClientProvider client={client}>
      <SQLViewDialog workspace="Test" service="managed" />
    </QueryClientProvider>,
  );
  fireEvent.click(screen.getByRole("button", { name: "SQL view" }));
  typeSQL("SELECT 1 AS id, ST_Point(0,0) AS geom");
  fireEvent.change(screen.getByLabelText("Public layer ID"), {
    target: { value: "points" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Validate SQL" }));
  fireEvent.click(
    await screen.findByRole("button", { name: "Cancel validation" }),
  );
  await waitFor(() => expect(signal?.aborted).toBe(true));
  expect(screen.getByLabelText("SQL query")).toHaveTextContent(
    "SELECT 1 AS id, ST_Point(0,0) AS geom",
  );
  expect(screen.getByLabelText("Public layer ID")).toHaveValue("points");
  expect(
    screen.getByRole("button", { name: "Publish SQL view" }),
  ).toBeDisabled();
  expect(screen.getByRole("button", { name: "Validate SQL" })).toBeEnabled();
  expect(screen.getByRole("status")).toHaveTextContent("draft is preserved");
});
