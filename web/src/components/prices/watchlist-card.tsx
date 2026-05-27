"use client";

import * as React from "react";
import {
  ArrowDown,
  ArrowUp,
  ChevronDown,
  ChevronUp,
  ListOrdered,
  Plus,
  Save,
  X,
} from "lucide-react";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Skeleton } from "@/components/ui/skeleton";
import { ErrorAlert } from "@/components/feedback/query-states";
import {
  usePriceFeedWatchlistQuery,
  useUpdatePriceFeedWatchlistMutation,
} from "@/hooks/use-price-data";

function arraysEqual(a: string[], b: string[]) {
  if (a.length !== b.length) return false;
  for (let i = 0; i < a.length; i++) {
    if (a[i] !== b[i]) return false;
  }
  return true;
}

export function WatchlistCard() {
  const w = usePriceFeedWatchlistQuery();
  const update = useUpdatePriceFeedWatchlistMutation();

  const [symbols, setSymbols] = React.useState<string[]>([]);
  const [newSymbol, setNewSymbol] = React.useState("");
  const [reorderMode, setReorderMode] = React.useState(false);
  const [collapsed, setCollapsed] = React.useState(false);
  const [filter, setFilter] = React.useState("");
  const [msg, setMsg] = React.useState<string | null>(null);

  React.useEffect(() => {
    if (!w.data) return;
    setSymbols(w.data.watchlist);
  }, [w.data]);

  const original = w.data?.watchlist ?? [];
  const isDirty = !arraysEqual(symbols, original);
  const visibleSymbols = React.useMemo(() => {
    const f = filter.trim().toUpperCase();
    if (!f) return symbols.map((sym, idx) => ({ sym, idx }));
    return symbols
      .map((sym, idx) => ({ sym, idx }))
      .filter(({ sym }) => sym.toUpperCase().includes(f));
  }, [symbols, filter]);

  function moveSymbol(from: number, to: number) {
    setSymbols((prev) => {
      if (from < 0 || to < 0 || from >= prev.length || to >= prev.length) return prev;
      const next = [...prev];
      const [item] = next.splice(from, 1);
      next.splice(to, 0, item);
      return next;
    });
    setMsg(null);
  }

  function removeAt(index: number) {
    setSymbols((prev) => prev.filter((_, i) => i !== index));
    setMsg(null);
  }

  function addSymbol() {
    const symbol = newSymbol.trim().toUpperCase();
    if (!symbol) return;
    setSymbols((prev) => (prev.includes(symbol) ? prev : [...prev, symbol]));
    setNewSymbol("");
    setMsg(null);
  }

  function discard() {
    setSymbols(original);
    setMsg(null);
  }

  function save() {
    setMsg(null);
    update.mutate(
      { watchlist: symbols },
      {
        onSuccess: (res) => {
          setSymbols(res.watchlist);
          setMsg(`Saved ${res.watchlist.length} symbols.`);
        },
        onError: (err) => {
          setMsg(err instanceof Error ? err.message : "Failed to save watchlist");
        },
      },
    );
  }

  if (w.isPending) {
    return (
      <Card>
        <CardHeader className="pb-2">
          <Skeleton className="h-5 w-40" />
        </CardHeader>
        <CardContent>
          <Skeleton className="h-16 w-full" />
        </CardContent>
      </Card>
    );
  }

  if (w.isError) {
    return <ErrorAlert error={w.error} title="Could not load watchlist" />;
  }

  return (
    <Card>
      <CardHeader className="pb-3">
        <div className="flex flex-wrap items-start justify-between gap-2">
          <div>
            <CardTitle className="text-base flex items-center gap-2">
              Watchlist
              <Badge variant="outline" className="font-mono">
                {symbols.length}
              </Badge>
              {isDirty ? (
                <Badge variant="warning" className="text-[10px]">
                  unsaved
                </Badge>
              ) : null}
            </CardTitle>
            <CardDescription>
              Symbols tracked by the price feed and surfaced to the briefing agent.
            </CardDescription>
          </div>
          <div className="flex items-center gap-1">
            <Button
              type="button"
              variant="ghost"
              size="sm"
              onClick={() => setCollapsed((c) => !c)}
              aria-expanded={!collapsed}
              aria-label={collapsed ? "Expand watchlist" : "Collapse watchlist"}
            >
              {collapsed ? (
                <ChevronDown className="h-4 w-4" />
              ) : (
                <ChevronUp className="h-4 w-4" />
              )}
            </Button>
          </div>
        </div>
      </CardHeader>

      {collapsed ? null : (
        <CardContent className="space-y-3">
          <div className="flex flex-wrap items-center gap-2">
            <form
              className="flex flex-1 min-w-[220px] items-center gap-2"
              onSubmit={(e) => {
                e.preventDefault();
                addSymbol();
              }}
            >
              <Input
                placeholder="Add symbol (e.g. NVDA)"
                value={newSymbol}
                onChange={(e) => setNewSymbol(e.target.value.toUpperCase())}
                disabled={update.isPending}
                spellCheck={false}
                autoCapitalize="characters"
                className="h-8"
              />
              <Button
                type="submit"
                size="sm"
                variant="outline"
                disabled={!newSymbol.trim() || update.isPending}
                className="h-8 gap-1"
              >
                <Plus className="h-3.5 w-3.5" />
                Add
              </Button>
            </form>

            <Input
              placeholder="Filter…"
              value={filter}
              onChange={(e) => setFilter(e.target.value)}
              spellCheck={false}
              className="h-8 sm:max-w-[160px]"
              aria-label="Filter watchlist symbols"
            />

            <Button
              type="button"
              size="sm"
              variant={reorderMode ? "default" : "outline"}
              onClick={() => setReorderMode((m) => !m)}
              className="h-8 gap-1"
              aria-pressed={reorderMode}
            >
              <ListOrdered className="h-3.5 w-3.5" />
              Reorder
            </Button>
          </div>

          {symbols.length === 0 ? (
            <p className="rounded-md border border-dashed py-6 text-center text-sm text-muted-foreground">
              No symbols. Add one above to start tracking.
            </p>
          ) : (
            <div className="flex flex-wrap gap-1.5">
              {visibleSymbols.map(({ sym, idx }) => (
                <div
                  key={`${sym}-${idx}`}
                  className="group inline-flex items-center gap-1 rounded-md border bg-muted/40 pl-2 pr-1 py-0.5 text-xs font-mono"
                >
                  <span className="text-foreground">{sym}</span>
                  {reorderMode ? (
                    <>
                      <button
                        type="button"
                        onClick={() => moveSymbol(idx, idx - 1)}
                        disabled={idx === 0 || update.isPending}
                        className="rounded p-0.5 text-muted-foreground hover:bg-background hover:text-foreground disabled:opacity-30 disabled:hover:bg-transparent"
                        aria-label={`Move ${sym} up`}
                      >
                        <ArrowUp className="h-3 w-3" />
                      </button>
                      <button
                        type="button"
                        onClick={() => moveSymbol(idx, idx + 1)}
                        disabled={idx === symbols.length - 1 || update.isPending}
                        className="rounded p-0.5 text-muted-foreground hover:bg-background hover:text-foreground disabled:opacity-30 disabled:hover:bg-transparent"
                        aria-label={`Move ${sym} down`}
                      >
                        <ArrowDown className="h-3 w-3" />
                      </button>
                    </>
                  ) : null}
                  <button
                    type="button"
                    onClick={() => removeAt(idx)}
                    disabled={update.isPending}
                    className="rounded p-0.5 text-muted-foreground hover:bg-destructive/10 hover:text-destructive"
                    aria-label={`Remove ${sym}`}
                  >
                    <X className="h-3 w-3" />
                  </button>
                </div>
              ))}
            </div>
          )}

          {filter && visibleSymbols.length === 0 && symbols.length > 0 ? (
            <p className="text-xs text-muted-foreground">
              No symbols match “{filter}”. {symbols.length} hidden.
            </p>
          ) : null}

          <div className="flex flex-wrap items-center justify-between gap-2 border-t pt-3">
            <div className="text-xs text-muted-foreground">
              {msg ? msg : `Showing ${visibleSymbols.length} of ${symbols.length}.`}
            </div>
            <div className="flex items-center gap-2">
              <Button
                type="button"
                variant="outline"
                size="sm"
                onClick={discard}
                disabled={!isDirty || update.isPending}
              >
                Discard
              </Button>
              <Button
                type="button"
                size="sm"
                onClick={save}
                disabled={!isDirty || update.isPending}
                className="gap-1"
              >
                <Save className="h-3.5 w-3.5" />
                {update.isPending ? "Saving…" : "Save"}
              </Button>
            </div>
          </div>
        </CardContent>
      )}
    </Card>
  );
}
