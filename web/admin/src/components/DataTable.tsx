import {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from "react";
import { useSearchParams } from "react-router";
import {
  flexRender,
  getCoreRowModel,
  getFilteredRowModel,
  getPaginationRowModel,
  getSortedRowModel,
  useReactTable,
  type ColumnDef,
  type SortingState,
  type VisibilityState,
} from "@tanstack/react-table";
import { ArrowDown, ArrowUp, ChevronsUpDown, Columns3 } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import {
  DropdownMenu,
  DropdownMenuCheckboxItem,
  DropdownMenuContent,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Input } from "@/components/ui/input";
import { RowActions, RowDetails } from "@/components/RowActions";
import { cn } from "@/lib/utils";
import { columnLabel } from "@/lib/schema-fields";
import { Skeleton } from "@/components/ui/skeleton";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";

export const DEFAULT_PAGE_SIZE = 25;

/**
 * Search-parameter names, namespaced by `urlKey` so two tables can live on one
 * route without fighting over the query string.
 */
function paramNames(urlKey: string) {
  const prefix = urlKey ? `${urlKey}_` : "";
  return {
    query: `${prefix}q`,
    sort: `${prefix}sort`,
    direction: `${prefix}dir`,
    page: `${prefix}page`,
    hidden: `${prefix}hide`,
  };
}

export interface DataTableProps<TRow> {
  data: TRow[];
  columns: ColumnDef<TRow, unknown>[];
  isLoading?: boolean;
  /** Rendered in place of rows when there is no data and nothing is loading. */
  emptyState?: ReactNode;
  /** Namespace for this table's URL state. Required when a route has two. */
  urlKey?: string;
  /** Placeholder for the text filter. Omit the filter entirely with `false`. */
  filterPlaceholder?: string | false;
  pageSize?: number;
  /** Stable row identity, used for selection keys. */
  getRowId?: (row: TRow, index: number) => string;
  /** Rendered above the table while at least one row is selected. */
  bulkActions?: (selected: TRow[], clear: () => void) => ReactNode;
  /** Extra controls rendered in the toolbar, left of column visibility. */
  toolbar?: ReactNode;
  /** Rows are a server page: never silently filter, sort or page its subset. */
  serverMode?: boolean;
  footer?: ReactNode;
  /** Optional responsive presentation; does not remount row actions. */
  columnClassNames?: Record<string, string>;
  tableClassName?: string;
  columnPickerClassName?: string;
  /** Columns hidden until the viewer picks them (stable, rarely needed). */
  defaultHiddenColumns?: string[];
}

/**
 * Shared list table. Sort, filter and page state live in the URL so a view can
 * be linked and the browser's back button moves through it.
 */
export function DataTable<TRow>({
  data,
  columns,
  isLoading = false,
  emptyState,
  urlKey = "",
  filterPlaceholder = "Filter…",
  pageSize = DEFAULT_PAGE_SIZE,
  getRowId,
  bulkActions,
  toolbar,
  serverMode = false,
  footer,
  columnClassNames,
  tableClassName,
  columnPickerClassName,
  defaultHiddenColumns,
}: DataTableProps<TRow>) {
  // Catalog publications supply their own compact layout. Other lists share a
  // compact identity/actions view without remounting dialogs on resize.
  const compact = !columnClassNames;
  const ids = columns.map(
    (c) => c.id ?? ("accessorKey" in c ? String(c.accessorKey) : ""),
  );
  const identity =
    [
      "name",
      "public_id",
      "target_name",
      "operation",
      "principal",
      "resource",
      "id",
    ].find((id) => ids.includes(id)) ?? ids[0];
  const compactClass = (id: string) =>
    compact
      ? id === identity
        ? "whitespace-normal break-words"
        : id === "actions"
          ? "w-16 whitespace-normal @2xl/table:w-auto @2xl/table:whitespace-nowrap"
          : id === "__select"
            ? "w-11"
            : ["state", "status", "enabled"].includes(id)
              ? "w-20 whitespace-normal"
              : "hidden @2xl/table:table-cell"
      : undefined;
  // The actions column stays visible while the rest of the table scrolls.
  const pinned = (id: string) =>
    id === "actions" &&
    "sticky right-0 z-[1] bg-background shadow-[inset_1px_0_0_var(--border)] group-data-[overflowing=true]/table:shadow-[-8px_0_8px_-8px_rgb(0_0_0/0.25),inset_1px_0_0_var(--border)]";
  const scroller = useRef<HTMLDivElement>(null);
  const [overflowing, setOverflowing] = useState(false);
  useEffect(() => {
    const container = scroller.current?.querySelector<HTMLElement>(
      '[data-slot="table-container"]',
    );
    if (!container) return;
    const update = () =>
      setOverflowing(
        container.scrollLeft + container.clientWidth <
          container.scrollWidth - 1,
      );
    update();
    container.addEventListener("scroll", update, { passive: true });
    const observer =
      typeof ResizeObserver === "undefined"
        ? undefined
        : new ResizeObserver(update);
    observer?.observe(container);
    observer?.observe(container.firstElementChild ?? container);
    return () => {
      container.removeEventListener("scroll", update);
      observer?.disconnect();
    };
  }, []);
  const [searchParams, setSearchParams] = useSearchParams();
  const names = useMemo(() => paramNames(urlKey), [urlKey]);

  const globalFilter = searchParams.get(names.query) ?? "";
  const sortColumn = searchParams.get(names.sort) ?? "";
  const sortDescending = searchParams.get(names.direction) === "desc";
  const pageIndex = Math.max(
    0,
    Number.parseInt(searchParams.get(names.page) ?? "1", 10) - 1 || 0,
  );
  // Without a URL choice, the table's default-hidden columns apply; "-"
  // records an explicit "show everything".
  const hiddenParam = searchParams.get(names.hidden);
  const hiddenColumns = useMemo(
    () =>
      hiddenParam === null
        ? (defaultHiddenColumns ?? [])
        : hiddenParam.split(",").filter((id) => id && id !== "-"),
    [hiddenParam, defaultHiddenColumns],
  );

  const patchParams = useCallback(
    (patch: Record<string, string | null>, replace = false) => {
      setSearchParams(
        (previous) => {
          const next = new URLSearchParams(previous);
          for (const [key, value] of Object.entries(patch)) {
            if (value === null || value === "") {
              next.delete(key);
            } else {
              next.set(key, value);
            }
          }
          return next;
        },
        { replace },
      );
    },
    [setSearchParams],
  );

  const sorting: SortingState = useMemo(
    () => (sortColumn ? [{ id: sortColumn, desc: sortDescending }] : []),
    [sortColumn, sortDescending],
  );

  const columnVisibility: VisibilityState = useMemo(() => {
    const state: VisibilityState = {};
    for (const column of hiddenColumns) {
      if (
        column !== "__select" &&
        !columns.some(
          (item) => item.id === column && item.enableHiding === false,
        )
      )
        state[column] = false;
    }
    return state;
  }, [hiddenColumns, columns]);

  const selectionColumn: ColumnDef<TRow, unknown> | null = bulkActions
    ? {
        id: "__select",
        enableSorting: false,
        enableHiding: false,
        size: 36,
        header: ({ table }) => (
          <Checkbox
            aria-label="Select all rows"
            checked={
              table.getIsAllPageRowsSelected() ||
              (table.getIsSomePageRowsSelected() && "indeterminate")
            }
            onCheckedChange={(value) =>
              table.toggleAllPageRowsSelected(Boolean(value))
            }
          />
        ),
        cell: ({ row }) => (
          <Checkbox
            aria-label="Select row"
            checked={row.getIsSelected()}
            onCheckedChange={(value) => row.toggleSelected(Boolean(value))}
          />
        ),
      }
    : null;

  const resolvedColumns = useMemo(
    () => (selectionColumn ? [selectionColumn, ...columns] : columns),
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [columns, Boolean(bulkActions)],
  );

  // TanStack Table returns fresh function identities each render, so the React
  // Compiler declines to memoize this component. That is expected and harmless
  // here: the compiler is not enabled for this build, and table state is driven
  // by URL search params rather than by memoized props.
  // eslint-disable-next-line react-hooks/incompatible-library
  const table = useReactTable({
    data,
    columns: resolvedColumns,
    getRowId,
    state: {
      sorting,
      globalFilter: serverMode ? "" : globalFilter,
      columnVisibility,
      pagination: { pageIndex, pageSize },
    },
    onSortingChange: (updater) => {
      const next = typeof updater === "function" ? updater(sorting) : updater;
      const first = next[0];
      patchParams({
        [names.sort]: first?.id ?? null,
        [names.direction]: first?.desc ? "desc" : null,
        [names.page]: null,
      });
    },
    onGlobalFilterChange: (updater) => {
      const next =
        typeof updater === "function"
          ? (updater(globalFilter) as string)
          : (updater as string);
      // Replace rather than push: typing a filter would otherwise leave one
      // history entry per keystroke, so Back would walk the word backwards a
      // letter at a time instead of leaving the filtered view.
      patchParams({ [names.query]: next || null, [names.page]: null }, true);
    },
    onColumnVisibilityChange: (updater) => {
      const next =
        typeof updater === "function" ? updater(columnVisibility) : updater;
      const hidden = Object.entries(next)
        .filter(([, visible]) => visible === false)
        .map(([id]) => id);
      const same =
        hidden.length === (defaultHiddenColumns?.length ?? 0) &&
        hidden.every((id) => defaultHiddenColumns?.includes(id));
      patchParams({
        [names.hidden]: same ? null : hidden.join(",") || "-",
      });
    },
    onPaginationChange: (updater) => {
      const current = { pageIndex, pageSize };
      const next = typeof updater === "function" ? updater(current) : updater;
      patchParams({
        [names.page]: next.pageIndex > 0 ? String(next.pageIndex + 1) : null,
      });
    },
    getCoreRowModel: getCoreRowModel(),
    getSortedRowModel: getSortedRowModel(),
    getFilteredRowModel: getFilteredRowModel(),
    getPaginationRowModel: getPaginationRowModel(),
    manualPagination: serverMode,
    manualFiltering: serverMode,
    manualSorting: serverMode,
    enableSorting: !serverMode,
    // URL filters already reset the page. Automatic resets on fresh query data
    // would navigate during rendering and can continually remount action cells.
    autoResetPageIndex: false,
  });

  const visibleColumnCount = table.getVisibleFlatColumns().length;
  const selectedRows = table
    .getSelectedRowModel()
    .rows.map((row) => row.original);
  const pageCount = table.getPageCount();
  const safePage = Math.min(pageIndex, Math.max(0, pageCount - 1));
  useEffect(() => {
    if (
      !serverMode &&
      !isLoading &&
      (pageIndex !== safePage ||
        (searchParams.has(names.page) &&
          !/^[1-9]\d*$/.test(searchParams.get(names.page)!)))
    ) {
      patchParams(
        { [names.page]: safePage > 0 ? String(safePage + 1) : null },
        true,
      );
    }
  }, [
    serverMode,
    isLoading,
    pageIndex,
    safePage,
    names.page,
    searchParams,
    patchParams,
  ]);
  // Render the valid page immediately, then repair the URL in the effect.
  const pageRows = serverMode
    ? table.getCoreRowModel().rows
    : table
        .getPrePaginationRowModel()
        .rows.slice(safePage * pageSize, (safePage + 1) * pageSize);

  return (
    <div data-table-root className="@container/table min-w-0 space-y-2">
      <div className="flex flex-wrap items-center gap-2">
        {!serverMode && filterPlaceholder !== false && (
          <Input
            className="max-w-64"
            placeholder={filterPlaceholder}
            aria-label={filterPlaceholder}
            value={globalFilter}
            onChange={(event) => table.setGlobalFilter(event.target.value)}
          />
        )}
        {toolbar}
        <div
          className={cn(
            "ml-auto flex items-center gap-2",
            // Cards show only identity, state and actions; nothing to pick.
            compact && "hidden @2xl/table:flex",
            columnPickerClassName,
          )}
        >
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button variant="outline" size="sm">
                <Columns3 /> Columns
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end">
              {table
                .getAllLeafColumns()
                .filter((column) => column.getCanHide())
                .map((column) => (
                  <DropdownMenuCheckboxItem
                    key={column.id}
                    checked={column.getIsVisible()}
                    onCheckedChange={(value) =>
                      column.toggleVisibility(Boolean(value))
                    }
                  >
                    {columnLabel(column.id)}
                  </DropdownMenuCheckboxItem>
                ))}
            </DropdownMenuContent>
          </DropdownMenu>
        </div>
      </div>

      {bulkActions && selectedRows.length > 0 && (
        <div className="flex items-center gap-2 rounded-lg border bg-muted/40 px-3 py-2 text-sm">
          <span className="font-medium">{selectedRows.length} selected</span>
          {bulkActions(selectedRows, () => table.resetRowSelection())}
        </div>
      )}

      <div
        ref={scroller}
        data-overflowing={overflowing}
        className="group/table overflow-hidden rounded-lg border"
      >
        <Table
          tabIndex={0}
          aria-label="Scrollable data table"
          className={cn(
            "focus-visible:outline-2 focus-visible:outline-ring focus-visible:-outline-offset-2",
            compact && "table-fixed @2xl/table:table-auto",
            tableClassName,
          )}
        >
          <TableHeader>
            {table.getHeaderGroups().map((headerGroup) => (
              <TableRow key={headerGroup.id}>
                {headerGroup.headers.map((header) => {
                  const sortable = header.column.getCanSort();
                  const sorted = header.column.getIsSorted();
                  return (
                    <TableHead
                      key={header.id}
                      aria-sort={
                        sorted === "asc"
                          ? "ascending"
                          : sorted === "desc"
                            ? "descending"
                            : undefined
                      }
                      className={cn(
                        "text-xs font-medium text-muted-foreground",
                        columnClassNames?.[header.column.id],
                        compactClass(header.column.id),
                        pinned(header.column.id),
                      )}
                    >
                      {header.isPlaceholder ? null : sortable ? (
                        <button
                          type="button"
                          className="inline-flex items-center gap-1"
                          onClick={header.column.getToggleSortingHandler()}
                        >
                          {flexRender(
                            header.column.columnDef.header,
                            header.getContext(),
                          )}
                          {sorted === "asc" ? (
                            <ArrowUp className="size-3" />
                          ) : sorted === "desc" ? (
                            <ArrowDown className="size-3" />
                          ) : (
                            <ChevronsUpDown className="size-3 opacity-40" />
                          )}
                        </button>
                      ) : header.column.columnDef.header ? (
                        flexRender(
                          header.column.columnDef.header,
                          header.getContext(),
                        )
                      ) : (
                        // Unlabelled columns (typically row actions) still need
                        // a header name for screen readers.
                        <span className="sr-only">
                          {header.column.id === "actions"
                            ? "Actions"
                            : columnLabel(header.column.id)}
                        </span>
                      )}
                    </TableHead>
                  );
                })}
              </TableRow>
            ))}
          </TableHeader>
          <TableBody>
            {isLoading &&
              Array.from({ length: 5 }).map((_, rowIndex) => (
                <TableRow key={`skeleton-${rowIndex}`}>
                  {table.getVisibleLeafColumns().map((column) => (
                    <TableCell
                      key={column.id}
                      className={columnClassNames?.[column.id]}
                    >
                      <Skeleton className="h-3 w-full" />
                    </TableCell>
                  ))}
                </TableRow>
              ))}
            {!isLoading && pageRows.length === 0 && (
              <TableRow>
                <TableCell
                  colSpan={visibleColumnCount}
                  className="h-24 text-center text-muted-foreground"
                >
                  {data.length > 0 && globalFilter ? (
                    <>
                      <p>No results match this filter.</p>
                      <Button
                        variant="outline"
                        size="sm"
                        onClick={() => table.setGlobalFilter("")}
                      >
                        Clear filter
                      </Button>
                    </>
                  ) : (
                    (emptyState ?? "Nothing to show yet.")
                  )}
                </TableCell>
              </TableRow>
            )}
            {!isLoading &&
              pageRows.map((row) => (
                <TableRow
                  key={row.id}
                  data-state={row.getIsSelected() ? "selected" : undefined}
                >
                  {row.getVisibleCells().map((cell) => (
                    <TableCell
                      key={cell.id}
                      className={cn(
                        columnClassNames?.[cell.column.id],
                        compactClass(cell.column.id),
                        pinned(cell.column.id),
                      )}
                    >
                      {compact && cell.column.id === "actions" ? (
                        <RowActions
                          label={String(
                            row.getValue(identity ?? "") ?? "resource",
                          )}
                        >
                          {flexRender(
                            cell.column.columnDef.cell,
                            cell.getContext(),
                          )}
                        </RowActions>
                      ) : (
                        flexRender(
                          cell.column.columnDef.cell,
                          cell.getContext(),
                        )
                      )}
                      {compact && cell.column.id === identity && (
                        <RowDetails>
                          {row
                            .getAllCells()
                            .filter(
                              (other) =>
                                ![identity, "actions", "__select"].includes(
                                  other.column.id,
                                ),
                            )
                            .map((other) => (
                              <div key={other.id}>
                                <dt className="font-medium">
                                  {columnLabel(other.column.id)}
                                </dt>
                                <dd className="whitespace-normal break-all">
                                  {flexRender(
                                    other.column.columnDef.cell,
                                    other.getContext(),
                                  )}
                                </dd>
                              </div>
                            ))}
                        </RowDetails>
                      )}
                    </TableCell>
                  ))}
                </TableRow>
              ))}
          </TableBody>
        </Table>
      </div>

      {footer}
      {!serverMode && pageCount > 1 && (
        <div className="flex items-center justify-end gap-2 text-xs text-muted-foreground">
          <span className="font-mono">
            Page {safePage + 1} of {pageCount}
          </span>
          <Button
            variant="outline"
            size="sm"
            disabled={!table.getCanPreviousPage()}
            onClick={() => table.previousPage()}
          >
            Previous
          </Button>
          <Button
            variant="outline"
            size="sm"
            disabled={!table.getCanNextPage()}
            onClick={() => table.nextPage()}
          >
            Next
          </Button>
        </div>
      )}
    </div>
  );
}
