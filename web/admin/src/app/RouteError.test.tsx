import { cleanup, render, screen } from "@testing-library/react";
import { createMemoryRouter, RouterProvider } from "react-router";
import { afterEach, describe, expect, it } from "vitest";
import { ApiError } from "@/api/client";
import { RouteError } from "./RouteError";

function Boom({ error }: { error: unknown }): never {
  throw error;
}

function renderWithError(error: unknown, scope?: "screen" | "app") {
  const router = createMemoryRouter(
    [
      {
        path: "/",
        element: <Boom error={error} />,
        errorElement: <RouteError scope={scope} />,
      },
    ],
    { initialEntries: ["/"] },
  );
  return render(<RouterProvider router={router} />);
}

afterEach(cleanup);

describe("RouteError", () => {
  it("reports an ApiError with its status, message and detail", () => {
    renderWithError(
      new ApiError(409, "Conflict", "a layer with that public ID exists"),
    );
    expect(screen.getByText(/409 · Conflict/)).toBeInTheDocument();
    expect(
      screen.getByText("a layer with that public ID exists"),
    ).toBeInTheDocument();
  });

  it("surfaces the error code as a reference operators can quote", () => {
    renderWithError(new ApiError(500, "Internal Error", undefined, "req-42"));
    expect(screen.getByText("req-42")).toBeInTheDocument();
  });

  it("shows a generic title for an unexpected exception", () => {
    renderWithError(new Error("cannot read properties of undefined"));
    expect(
      screen.getByText("This screen stopped unexpectedly"),
    ).toBeInTheDocument();
    expect(
      screen.getByText("cannot read properties of undefined"),
    ).toBeInTheDocument();
  });

  it("offers a way back to the console from a screen-level failure", () => {
    renderWithError(new Error("boom"), "screen");
    expect(
      screen.getByRole("button", { name: /back to the console/i }),
    ).toBeInTheDocument();
  });

  it("omits the back action when the whole app failed", () => {
    renderWithError(new Error("boom"), "app");
    expect(
      screen.queryByRole("button", { name: /back to the console/i }),
    ).not.toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: /reload this screen/i }),
    ).toBeInTheDocument();
  });

  it("handles a thrown non-Error value without crashing", () => {
    renderWithError("just a string");
    expect(
      screen.getByText("This screen stopped unexpectedly"),
    ).toBeInTheDocument();
  });
});
