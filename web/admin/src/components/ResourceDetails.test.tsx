import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, expect, it } from "vitest";
import { DisplayValue } from "./ResourceDetails";
import { ConnectionResult } from "./ConnectionResult";
import { formatBytes } from "@/lib/display";
afterEach(cleanup);
it("uses field-specific boolean meanings and keeps exact dates", () => {
  render(
    <>
      <DisplayValue field="built_in" value={true} />
      <DisplayValue field="public" value={false} />
      <DisplayValue field="created_at" value="2026-09-16T12:00:00Z" />
    </>,
  );
  expect(screen.getByText("Built-in")).toBeVisible();
  expect(screen.getByText("Restricted")).toBeVisible();
  expect(screen.getByTitle("2026-09-16T12:00:00Z")).toHaveAttribute(
    "datetime",
    "2026-09-16T12:00:00Z",
  );
  expect(formatBytes(1048576)).toBe("1 MiB");
});
it("summarizes a failed connection without dropping technical details", () => {
  render(
    <ConnectionResult
      ok={false}
      detail={'failed: database "missing" does not exist; TLS trace'}
    />,
  );
  expect(screen.getByRole("status")).toHaveTextContent("Database not found");
  expect(screen.getByText(/failed: database/)).not.toBeVisible();
  expect(screen.getByText("Technical details")).toBeVisible();
});
