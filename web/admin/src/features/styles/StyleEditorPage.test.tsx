import {
  act,
  cleanup,
  fireEvent,
  screen,
  waitFor,
} from "@testing-library/react";
import { EditorView } from "@codemirror/view";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { StyleEditorPage } from "./StyleEditorPage";
import { renderScreen } from "@/test/render";
import { styleFormats, styleTemplate } from "./style-templates";

const { apiFetchMock } = vi.hoisted(() => ({ apiFetchMock: vi.fn() }));
vi.mock("@/api/client", () => ({
  apiFetch: apiFetchMock,
  apiBase: "/api/v1",
  basePath: "",
  jsonBody: (value: unknown) => JSON.stringify(value),
}));
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));
vi.mock("@/auth/auth-context", () => ({
  useAuth: () => ({ config: { services: { wms: true } } }),
}));

function renderPage() {
  return renderScreen(<StyleEditorPage />, {
    path: "/workspaces/:ws/styles",
    entry: "/workspaces/demo/styles",
  });
}

beforeEach(() => {
  vi.stubGlobal(
    "matchMedia",
    vi.fn((query: string) => ({
      matches: false,
      media: query,
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
    })),
  );
  Object.defineProperty(Element.prototype, "scrollIntoView", {
    configurable: true,
    value: vi.fn(),
  });
  apiFetchMock.mockReset();
  apiFetchMock.mockImplementation(async (path: string) => {
    if (path.endsWith("/style-assets")) return { assets: [] };
    if (path.endsWith("/styles")) {
      return {
        styles: [
          { id: "s1", name: "roads", title: "Roads", format: "sld_1.0.0" },
        ],
      };
    }
    return {
      id: "s1",
      name: "roads",
      format: "sld_1.0.0",
      body: "<sld/>",
      sld_body: "<sld/>",
    };
  });
});

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

describe("StyleEditorPage", () => {
  it.each(styleFormats)(
    "creates $label using the selected starter and canonical body",
    async ({ value, label }) => {
      apiFetchMock.mockImplementation(
        async (path: string, options?: RequestInit) => {
          if (path.endsWith("/styles") && options?.method === "POST") {
            expect(JSON.parse(options.body as string)).toEqual({
              name: "new-style",
              title: "new-style",
              format: value,
              body: styleTemplate(value, "line"),
            });
            // Failure retains all draft choices and displays actionable details.
            throw Object.assign(new Error("Creation failed"), {
              detail: "Please check the style configuration",
            });
          }
          if (path.endsWith("/styles")) return { styles: [] };
          return {};
        },
      );
      renderPage();
      fireEvent.click(await screen.findByRole("button", { name: "New style" }));
      fireEvent.change(await screen.findByLabelText("Style name"), {
        target: { value: "new-style" },
      });
      fireEvent.click(screen.getByLabelText("Style format"));
      fireEvent.click(await screen.findByRole("option", { name: label }));
      fireEvent.click(screen.getByLabelText("Starter"));
      fireEvent.click(await screen.findByRole("option", { name: "line" }));
      fireEvent.click(screen.getByRole("button", { name: "Create style" }));
      expect(
        await screen.findByText(/Please check the style configuration/),
      ).toBeInTheDocument();
      expect(screen.getByLabelText("Style name")).toHaveValue("new-style");
      expect(screen.getByLabelText("Style format")).toHaveTextContent(label);
      expect(screen.getByLabelText("Starter")).toHaveTextContent("line");
      if (["css", "ysld", "mapbox"].includes(value))
        expect(screen.getByRole("note")).toHaveTextContent("dynamic-style");
    },
  );
  it.each([
    ["sld_1.0.0", "<sld/>", "<sld>edited</sld>", "SLD XML"],
    [
      "css",
      "* { fill: #000000; }",
      "* { fill: #ff0000; }",
      "Style source (css)",
    ],
  ])(
    "saves canonical content and reloads %s without legacy aliases",
    async (format, original, edited, label) => {
      let persisted = original;
      apiFetchMock.mockImplementation(
        async (path: string, options?: RequestInit) => {
          if (path.endsWith("/styles/roads")) {
            if (options?.method === "PUT") {
              const payload = JSON.parse(options.body as string);
              expect(payload).toEqual({ body: edited });
              persisted = payload.body;
            }
            return {
              id: "s1",
              name: "roads",
              format,
              body: persisted,
              ...(format.startsWith("sld_") ? { sld_body: persisted } : {}),
            };
          }
          if (path.endsWith("/styles"))
            return { styles: [{ id: "s1", name: "roads", format }] };
          return {};
        },
      );
      const view = renderPage();
      const input = await screen.findByLabelText(label);
      await act(async () => {
        const editor = EditorView.findFromDOM(input)!;
        editor.dispatch({
          changes: { from: 0, to: editor.state.doc.length, insert: edited },
        });
      });
      fireEvent.click(screen.getByRole("button", { name: /save style/i }));
      await waitFor(() => expect(persisted).toBe(edited));
      await waitFor(() =>
        expect(
          screen.getByRole("button", { name: /^save style$/i }),
        ).toBeEnabled(),
      );
      view.unmount();
      renderPage();
      expect(await screen.findByLabelText(label)).toHaveTextContent(edited);
    },
  );

  it("lists the workspace styles", async () => {
    renderPage();
    expect(
      await screen.findByRole("heading", { name: /styles/i }),
    ).toBeInTheDocument();
  });

  it("offers style creation", async () => {
    renderPage();
    expect(
      await screen.findByRole("button", { name: /new style/i }),
    ).toBeInTheDocument();
  });

  it("shows the style assets panel", async () => {
    renderPage();
    expect(await screen.findByText(/style assets/i)).toBeInTheDocument();
  });
});

describe("style save feedback", () => {
  it("confirms a successful save", async () => {
    const { toast } = await import("sonner");
    apiFetchMock.mockImplementation(
      async (path: string, options?: RequestInit) => {
        if (path.endsWith("/style-assets")) return { assets: [] };
        if (path.endsWith("/styles"))
          return { styles: [{ id: "s1", name: "roads", format: "sld_1.0.0" }] };
        const body =
          options?.method === "PUT"
            ? JSON.parse(options.body as string).body
            : "<sld/>";
        return {
          id: "s1",
          name: "roads",
          format: "sld_1.0.0",
          body,
          valid: true,
        };
      },
    );
    renderPage();
    const input = await screen.findByLabelText("SLD XML");
    await act(async () => {
      const editor = EditorView.findFromDOM(input)!;
      editor.dispatch({ changes: { from: 0, insert: "<!-- edited -->" } });
    });
    fireEvent.click(screen.getByRole("button", { name: /save style/i }));
    await waitFor(() =>
      expect(toast.success).toHaveBeenCalledWith("Saved roads"),
    );
  });

  it("points to the line of a rejected save", async () => {
    const { ApiError } =
      await vi.importActual<typeof import("@/api/client")>("@/api/client");
    const original = "<sld/>\n<ok/>\n";
    apiFetchMock.mockImplementation(
      async (path: string, options?: RequestInit) => {
        if (path.endsWith("/style-assets")) return { assets: [] };
        if (path.endsWith("/styles"))
          return { styles: [{ id: "s1", name: "roads", format: "sld_1.0.0" }] };
        if (options?.method === "PUT")
          throw new ApiError(
            400,
            "Bad Request",
            "invalid style: failed to parse SLD: line 2, column 1: unexpected content after the root element",
          );
        return {
          id: "s1",
          name: "roads",
          format: "sld_1.0.0",
          body: original,
          valid: true,
        };
      },
    );
    renderPage();
    const input = await screen.findByLabelText("SLD XML");
    await act(async () => {
      const editor = EditorView.findFromDOM(input)!;
      editor.dispatch({ changes: { from: 0, insert: " " } });
    });
    fireEvent.click(screen.getByRole("button", { name: /save style/i }));
    const jump = await screen.findByRole("button", { name: "Go to line 2" });
    expect(screen.getByRole("alert")).toHaveTextContent(
      "unexpected content after the root element",
    );
    fireEvent.click(jump);
    const editor = EditorView.findFromDOM(input)!;
    expect(
      editor.state.doc.lineAt(editor.state.selection.main.head).number,
    ).toBe(2);
  });

  it("warns about a stored style the server reports as invalid", async () => {
    apiFetchMock.mockImplementation(async (path: string) => {
      if (path.endsWith("/style-assets")) return { assets: [] };
      if (path.endsWith("/styles"))
        return { styles: [{ id: "s1", name: "roads", format: "sld_1.0.0" }] };
      return {
        id: "s1",
        name: "roads",
        format: "sld_1.0.0",
        body: "<sld/>\n<broken",
        valid: false,
        diagnostics: [
          {
            code: "parse-error",
            line: 2,
            message: "unexpected content after the root element",
          },
        ],
      };
    });
    renderPage();
    expect(
      await screen.findByText(/stored style is invalid/i),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "Go to line 2" }),
    ).toBeInTheDocument();
  });
});
