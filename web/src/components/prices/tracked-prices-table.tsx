"use client";

import * as React from "react";
import {
  flexRender,
  getCoreRowModel,
  useReactTable,
  type ColumnDef,
  type PaginationState,
  type SortingState,
} from "@tanstack/react-table";

import type { PriceListItem } from "@/lib/api/types";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { Badge } from "@/components/ui/badge";
import { ErrorAlert } from "@/components/feedback/query-states";
import { Skeleton } from "@/components/ui/skeleton";
import { usePricesListQuery } from "@/hooks/use-price-data";
import { compactUsdLike, formatDailyReturnFraction } from "@/lib/format";

const columns: ColumnDef<PriceListItem>[] = [
  {
    accessorKey: "symbol",
    header: "Symbol",
    cell: (info) => (
      <span className="font-medium">{String(info.getValue())}</span>
    ),
  },
  {
    accessorKey: "price",
    header: "Last price",
    cell: (info) => (
      <span className="font-mono text-sm">{compactUsdLike(String(info.getValue()))}</span>
    ),
  },
  {
    accessorKey: "change_pct",
    header: "Change %",
    enableSorting: true,
    cell: (info) => {
      const row = info.row.original;
      return (
        <span className="font-mono text-sm">
          {formatDailyReturnFraction(row.change_pct ?? null)}
        </span>
      );
    },
  },
  {
    accessorKey: "as_of",
    header: "As of",
    cell: (info) => {
      const v = String(info.getValue());
      const d = new Date(v);
      return Number.isNaN(d.getTime()) ? v : d.toLocaleString();
    },
  },
  {
    accessorKey: "updated_at",
    header: "Updated",
    cell: (info) => {
      const v = String(info.getValue());
      const d = new Date(v);
      return Number.isNaN(d.getTime()) ? v : d.toLocaleString();
    },
  },
  {
    accessorKey: "source",
    header: "Source",
    enableSorting: false,
    cell: (info) => {
      const s = String(info.getValue() ?? "");
      const short = s.length > 32 ? `${s.slice(0, 30)}…` : s;
      return (
        <span className="font-mono text-[11px] text-muted-foreground" title={s || undefined}>
          {short || "—"}
        </span>
      );
    },
  },
  {
    accessorKey: "provider_data_status",
    header: "Data status",
    enableSorting: false,
    cell: (info) => {
      const v = String(info.getValue());
      const variant =
        v === "fresh" ? "success" : v === "stale" ? "warning" : "outline";
      return <Badge variant={variant}>{v}</Badge>;
    },
  },
];

function sortToApi(sorting: SortingState): { sort: string; order: "asc" | "desc" } {
  const col = sorting[0]?.id ?? "symbol";
  const desc = sorting[0]?.desc ?? false;
  const map: Record<string, string> = {
    symbol: "symbol",
    price: "price",
    change_pct: "change_pct",
    as_of: "as_of",
    updated_at: "updated_at",
  };
  return { sort: map[col] ?? "symbol", order: desc ? "desc" : "asc" };
}

export function TrackedPricesTable() {
  const [filter, setFilter] = React.useState("");
  const [debouncedQ, setDebouncedQ] = React.useState("");
  React.useEffect(() => {
    const t = window.setTimeout(() => setDebouncedQ(filter.trim()), 400);
    return () => window.clearTimeout(t);
  }, [filter]);

  const [sorting, setSorting] = React.useState<SortingState>([
    { id: "symbol", desc: false },
  ]);
  const [pagination, setPagination] = React.useState<PaginationState>({
    pageIndex: 0,
    pageSize: 50,
  });

  React.useEffect(() => {
    setPagination((p) => ({ ...p, pageIndex: 0 }));
  }, [debouncedQ]);

  const { sort, order } = sortToApi(sorting);
  const { pageIndex, pageSize } = pagination;

  const listQ = usePricesListQuery({
    q: debouncedQ || undefined,
    sort,
    order,
    limit: pageSize,
    offset: pageIndex * pageSize,
  });

  const total = listQ.data?.total ?? 0;
  const pageCount = Math.max(1, Math.ceil(total / pageSize));

  const table = useReactTable({
    data: listQ.data?.items ?? [],
    columns,
    state: { sorting, pagination },
    manualPagination: true,
    manualSorting: true,
    pageCount,
    onSortingChange: (updater) => {
      setSorting(updater);
      setPagination((p) => ({ ...p, pageIndex: 0 }));
    },
    onPaginationChange: setPagination,
    getCoreRowModel: getCoreRowModel(),
  });

  return (
    <div className="space-y-3">
      <div className="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
        <div className="text-sm text-muted-foreground">
          {listQ.isSuccess ? (
            <>
              {total} tracked symbol{total === 1 ? "" : "s"}
            </>
          ) : (
            "Loading directory…"
          )}
        </div>
        <Input
          className="sm:max-w-xs"
          placeholder="Filter symbols…"
          value={filter}
          onChange={(e) => setFilter(e.target.value)}
          aria-label="Filter symbols"
        />
      </div>

      {listQ.isPending && !listQ.data ? (
        <div className="space-y-2">
          <Skeleton className="h-10 w-full" />
          <Skeleton className="h-10 w-full" />
          <Skeleton className="h-10 w-full" />
        </div>
      ) : null}

      {listQ.isError ? <ErrorAlert error={listQ.error} title="Could not load prices" /> : null}

      {listQ.isSuccess ? (
        <>
          <div className="overflow-x-auto rounded-md border">
            <Table>
              <TableHeader>
                {table.getHeaderGroups().map((hg) => (
                  <TableRow key={hg.id}>
                    {hg.headers.map((header) => (
                      <TableHead key={header.id}>
                        {header.isPlaceholder ? null : header.column.getCanSort() ? (
                          <button
                            type="button"
                            className="inline-flex items-center gap-1"
                            onClick={header.column.getToggleSortingHandler()}
                          >
                            {flexRender(
                              header.column.columnDef.header,
                              header.getContext(),
                            )}
                            {{
                              asc: "↑",
                              desc: "↓",
                            }[header.column.getIsSorted() as string] ?? null}
                          </button>
                        ) : (
                          flexRender(
                            header.column.columnDef.header,
                            header.getContext(),
                          )
                        )}
                      </TableHead>
                    ))}
                  </TableRow>
                ))}
              </TableHeader>
              <TableBody>
                {table.getRowModel().rows.length ? (
                  table.getRowModel().rows.map((row) => (
                    <TableRow key={row.id}>
                      {row.getVisibleCells().map((cell) => (
                        <TableCell key={cell.id}>
                          {flexRender(cell.column.columnDef.cell, cell.getContext())}
                        </TableCell>
                      ))}
                    </TableRow>
                  ))
                ) : (
                  <TableRow>
                    <TableCell colSpan={columns.length} className="h-24 text-center">
                      No symbols match your filter.
                    </TableCell>
                  </TableRow>
                )}
              </TableBody>
            </Table>
          </div>

          <div className="flex items-center justify-between gap-2">
            <div className="text-xs text-muted-foreground">
              Page {pageIndex + 1} of {pageCount} · {pageSize} per page
            </div>
            <div className="flex gap-2">
              <Button
                type="button"
                variant="outline"
                size="sm"
                onClick={() => table.previousPage()}
                disabled={!table.getCanPreviousPage()}
              >
                Prev
              </Button>
              <Button
                type="button"
                variant="outline"
                size="sm"
                onClick={() => table.nextPage()}
                disabled={!table.getCanNextPage()}
              >
                Next
              </Button>
            </div>
          </div>
        </>
      ) : null}
    </div>
  );
}
