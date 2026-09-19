import type { ReactElement } from "react";
import { render } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { createMemoryRouter, RouterProvider } from "react-router";

/**
 * Renders a screen inside a data router and a fresh query client.
 *
 * A data router is required rather than convenient: ResourcePage's
 * unsaved-changes guard calls `useBlocker`, which throws under the plain
 * MemoryRouter, and several screens embed ResourcePage indirectly.
 */
export function renderScreen(
  element: ReactElement,
  { path = "/", entry = "/" }: { path?: string; entry?: string } = {},
) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const router = createMemoryRouter([{ path, element }], {
    initialEntries: [entry],
  });
  return render(
    <QueryClientProvider client={client}>
      <RouterProvider router={router} />
    </QueryClientProvider>,
  );
}
