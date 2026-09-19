import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { MemoryRouter, useLocation, useNavigationType } from "react-router";
import { afterEach, describe, expect, it } from "vitest";
import type { ColumnDef } from "@tanstack/react-table";
import { DataTable } from "./DataTable";
import { useState } from "react";

interface Row {
  id: string;
  name: string;
  status: string;
}

const rows: Row[] = [
  { id: "1", name: "alpha", status: "running" },
  { id: "2", name: "bravo", status: "published" },
  { id: "3", name: "charlie", status: "failed" },
];

const columns: ColumnDef<Row, unknown>[] = [
  { id: "name", accessorFn: (row) => row.name, header: () => "name" },
  { id: "status", accessorFn: (row) => row.status, header: () => "status" },
];

function LocationProbe() {
  const location = useLocation();
  const navigationType = useNavigationType();
  return (
    <>
      <output data-testid="search">{location.search}</output>
      <output data-testid="nav-type">{navigationType}</output>
    </>
  );
}

function renderTable(
  props: Partial<React.ComponentProps<typeof DataTable<Row>>> = {},
  initialEntry = "/workspaces/demo/layers",
) {
  return render(
    <MemoryRouter initialEntries={[initialEntry]}>
      <DataTable
        data={rows}
        columns={columns}
        urlKey="layers"
        getRowId={(row) => row.id}
        {...props}
      />
      <LocationProbe />
    </MemoryRouter>,
  );
}

afterEach(cleanup);

describe("DataTable", () => {
  it("returns to an existing page after the last row on a page is deleted", () => {
    function ShrinkingCatalog() {
      const [data, setData] = useState(rows);
      return (
        <>
          <button onClick={() => setData(rows.slice(0, 1))}>
            Delete last page
          </button>
          <DataTable
            data={data}
            columns={columns}
            urlKey="layers"
            pageSize={2}
          />
          <LocationProbe />
        </>
      );
    }
    render(
      <MemoryRouter initialEntries={["/?layers_page=2"]}>
        <ShrinkingCatalog />
      </MemoryRouter>,
    );
    expect(screen.getByText("charlie")).toBeInTheDocument();
    fireEvent.click(screen.getByText("Delete last page"));
    expect(screen.getByText("alpha")).toBeInTheDocument();
    expect(screen.getByTestId("search").textContent).toBe("");
  });
  it("repairs an out-of-range deep link without hiding existing rows", () => {
    renderTable({}, "/workspaces/demo/layers?layers_page=99&other_page=3");
    expect(screen.getByText("alpha")).toBeInTheDocument();
    expect(screen.getByTestId("search").textContent).toBe("?other_page=3");
  });
  it("distinguishes no matches from an empty catalog and offers recovery", () => {
    renderTable(
      { emptyState: "No layers yet." },
      "/workspaces/demo/layers?layers_q=missing",
    );
    expect(screen.queryByText("No layers yet.")).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Clear filter" }));
    expect(screen.getByText("alpha")).toBeInTheDocument();
  });
  it("renders every row by default", () => {
    renderTable();
    expect(screen.getByText("alpha")).toBeInTheDocument();
    expect(screen.getByText("bravo")).toBeInTheDocument();
    expect(screen.getByText("charlie")).toBeInTheDocument();
  });

  it("writes the text filter into the URL and applies it", () => {
    renderTable();
    fireEvent.change(screen.getByLabelText("Filter…"), {
      target: { value: "brav" },
    });
    expect(screen.getByTestId("search").textContent).toContain("layers_q=brav");
    expect(screen.queryByText("alpha")).not.toBeInTheDocument();
    expect(screen.getByText("bravo")).toBeInTheDocument();
  });

  it("replaces rather than pushes while the filter is typed", () => {
    // Pushing per keystroke makes Back delete the search term one letter at a
    // time instead of leaving the filtered view.
    renderTable();
    fireEvent.change(screen.getByLabelText("Filter\u2026"), {
      target: { value: "brav" },
    });
    expect(screen.getByTestId("nav-type").textContent).toBe("REPLACE");
  });

  it("pushes a history entry when sorting, so Back undoes it", () => {
    renderTable();
    fireEvent.click(screen.getByRole("button", { name: /name/i }));
    expect(screen.getByTestId("nav-type").textContent).toBe("PUSH");
  });

  it("restores filter state from the URL", () => {
    renderTable({}, "/workspaces/demo/layers?layers_q=charlie");
    expect(screen.getByText("charlie")).toBeInTheDocument();
    expect(screen.queryByText("alpha")).not.toBeInTheDocument();
  });

  it("records sort state in the URL and announces it to assistive tech", () => {
    renderTable();
    fireEvent.click(screen.getByRole("button", { name: /name/i }));
    expect(screen.getByTestId("search").textContent).toContain(
      "layers_sort=name",
    );
    const header = screen.getByRole("columnheader", { name: /name/i });
    expect(header).toHaveAttribute("aria-sort", "ascending");
  });

  it("restores descending sort from the URL", () => {
    renderTable({}, "/workspaces/demo/layers?layers_sort=name&layers_dir=desc");
    expect(screen.getByRole("columnheader", { name: /name/i })).toHaveAttribute(
      "aria-sort",
      "descending",
    );
  });

  it("namespaces URL state so two tables do not collide", () => {
    renderTable({ urlKey: "coverages" });
    fireEvent.change(screen.getByLabelText("Filter…"), {
      target: { value: "x" },
    });
    const search = screen.getByTestId("search").textContent ?? "";
    expect(search).toContain("coverages_q=x");
    expect(search).not.toContain("layers_q");
  });

  it("hides columns listed in the URL", () => {
    renderTable({}, "/workspaces/demo/layers?layers_hide=status");
    expect(
      screen.queryByRole("columnheader", { name: /status/i }),
    ).not.toBeInTheDocument();
    expect(
      screen.getByRole("columnheader", { name: /name/i }),
    ).toBeInTheDocument();
  });

  it("shows the empty state rather than a bare table", () => {
    renderTable({ data: [], emptyState: "No layers yet." });
    expect(screen.getByText("No layers yet.")).toBeInTheDocument();
  });

  it("shows skeleton rows while loading and no empty state", () => {
    renderTable({ data: [], isLoading: true, emptyState: "No layers yet." });
    expect(screen.queryByText("No layers yet.")).not.toBeInTheDocument();
    expect(screen.queryByText("alpha")).not.toBeInTheDocument();
  });

  it("exposes bulk actions only once a row is selected", () => {
    renderTable({
      bulkActions: (selected) => <span>{selected.length} chosen</span>,
    });
    expect(screen.queryByText(/chosen/)).not.toBeInTheDocument();
    fireEvent.click(screen.getAllByLabelText("Select row")[0]);
    expect(screen.getByText("1 chosen")).toBeInTheDocument();
    expect(screen.getByText("1 selected")).toBeInTheDocument();
  });
});
