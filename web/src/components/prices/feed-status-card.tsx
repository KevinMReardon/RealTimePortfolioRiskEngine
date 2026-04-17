"use client";

import * as React from "react";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
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
import { Label } from "@/components/ui/label";
import { Skeleton } from "@/components/ui/skeleton";
import { ErrorAlert } from "@/components/feedback/query-states";
import {
  usePriceFeedStatusQuery,
  usePriceFeedWatchlistQuery,
  useUpdatePriceFeedWatchlistMutation,
} from "@/hooks/use-price-data";

function fmtTime(iso: string | undefined) {
  if (!iso) return "—";
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return iso;
  return new Intl.DateTimeFormat(undefined, {
    dateStyle: "medium",
    timeStyle: "medium",
  }).format(d);
}

export function FeedStatusCard() {
  const q = usePriceFeedStatusQuery();
  const w = usePriceFeedWatchlistQuery();
  const update = useUpdatePriceFeedWatchlistMutation();
  const [symbols, setSymbols] = React.useState<string[]>([]);
  const [newSymbol, setNewSymbol] = React.useState("");
  const [msg, setMsg] = React.useState<string | null>(null);

  React.useEffect(() => {
    if (!w.data) return;
    setSymbols(w.data.watchlist);
  }, [w.data]);

  function moveSymbol(from: number, to: number) {
    setSymbols((prev) => {
      if (from < 0 || to < 0 || from >= prev.length || to >= prev.length) return prev;
      const next = [...prev];
      const [item] = next.splice(from, 1);
      next.splice(to, 0, item);
      return next;
    });
  }

  function removeSymbol(index: number) {
    setSymbols((prev) => prev.filter((_, i) => i !== index));
  }

  function addSymbol() {
    const symbol = newSymbol.trim().toUpperCase();
    if (!symbol) return;
    setSymbols((prev) => (prev.includes(symbol) ? prev : [...prev, symbol]));
    setNewSymbol("");
  }

  if (q.isPending) {
    return (
      <Card>
        <CardHeader className="pb-2">
          <Skeleton className="h-5 w-40" />
          <Skeleton className="h-4 w-full max-w-md" />
        </CardHeader>
        <CardContent className="space-y-2">
          <Skeleton className="h-10 w-full" />
        </CardContent>
      </Card>
    );
  }

  if (q.isError) {
    return <ErrorAlert error={q.error} title="Could not load feed status" />;
  }

  const s = q.data;
  const active = s.active_provider ?? s.configured_provider;
  const fallbackBadge = s.last_tick_used_failover ? (
    <Badge variant="warning">Failover used on last tick</Badge>
  ) : (
    <Badge variant="success">Primary path</Badge>
  );

  return (
    <Card>
      <CardHeader className="pb-2">
        <CardTitle className="text-lg">Feed status</CardTitle>
        <CardDescription>
          Automated provider loop and projection freshness hints (not a substitute
          for metrics or logs).
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-4">
        <div className="flex flex-wrap items-center gap-2">
          <Badge variant={s.feed_enabled ? "success" : "outline"}>
            {s.feed_enabled ? "Feed enabled" : "Feed disabled"}
          </Badge>
          <Badge variant="outline">Provider: {s.configured_provider}</Badge>
          {fallbackBadge}
        </div>
        <dl className="grid gap-3 text-sm sm:grid-cols-2">
          <div>
            <dt className="text-muted-foreground">Active provider (last success)</dt>
            <dd className="font-medium">{active || "—"}</dd>
          </div>
          <div>
            <dt className="text-muted-foreground">Poll interval</dt>
            <dd className="font-medium">
              {s.poll_interval_ms > 0
                ? `${Math.round(s.poll_interval_ms / 1000)}s`
                : "—"}
            </dd>
          </div>
          <div>
            <dt className="text-muted-foreground">Watchlist</dt>
            <dd className="font-medium">
              {s.watchlist_count} symbol{s.watchlist_count === 1 ? "" : "s"}
              {s.watchlist_preview && s.watchlist_preview.length > 0
                ? ` (${s.watchlist_preview.join(", ")})`
                : ""}
            </dd>
          </div>
          <div>
            <dt className="text-muted-foreground">Last successful fetch</dt>
            <dd className="font-medium">{fmtTime(s.last_successful_fetch_at)}</dd>
          </div>
          <div>
            <dt className="text-muted-foreground">Last tick started</dt>
            <dd className="font-medium">{fmtTime(s.last_tick_started_at)}</dd>
          </div>
          <div>
            <dt className="text-muted-foreground">Last tick finished</dt>
            <dd className="font-medium">{fmtTime(s.last_tick_finished_at)}</dd>
          </div>
          {typeof s.last_tick_ingested_count === "number" ? (
            <div>
              <dt className="text-muted-foreground">Quotes ingested (last tick)</dt>
              <dd className="font-medium">{s.last_tick_ingested_count}</dd>
            </div>
          ) : null}
        </dl>
        {s.last_error ? (
          <Alert variant="destructive">
            <AlertTitle>Last feed error</AlertTitle>
            <AlertDescription className="font-mono text-xs whitespace-pre-wrap">
              {s.last_error}
            </AlertDescription>
          </Alert>
        ) : null}
        <form
          className="space-y-2 rounded-md border p-3"
          onSubmit={(e) => {
            e.preventDefault();
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
          }}
        >
          <div className="space-y-1.5">
            <Label>Watchlist order</Label>
            <div className="space-y-2">
              {symbols.map((sym, index) => (
                <div
                  key={`${sym}-${index}`}
                  className="flex items-center justify-between gap-2 rounded border bg-muted/30 px-2 py-1.5"
                >
                  <span className="font-mono text-sm">{sym}</span>
                  <div className="flex items-center gap-1">
                    <Button
                      type="button"
                      variant="outline"
                      size="sm"
                      onClick={() => moveSymbol(index, index - 1)}
                      disabled={index === 0 || update.isPending}
                    >
                      Up
                    </Button>
                    <Button
                      type="button"
                      variant="outline"
                      size="sm"
                      onClick={() => moveSymbol(index, index + 1)}
                      disabled={index === symbols.length - 1 || update.isPending}
                    >
                      Down
                    </Button>
                    <Button
                      type="button"
                      variant="outline"
                      size="sm"
                      onClick={() => removeSymbol(index)}
                      disabled={update.isPending}
                    >
                      Remove
                    </Button>
                  </div>
                </div>
              ))}
            </div>
          </div>
          <div className="flex flex-col gap-2 sm:flex-row">
            <Input
              placeholder="Add symbol (e.g. NVDA or BTC-USD)"
              value={newSymbol}
              onChange={(e) => setNewSymbol(e.target.value)}
              disabled={w.isPending || update.isPending}
              spellCheck={false}
              onKeyDown={(e) => {
                if (e.key === "Enter") {
                  e.preventDefault();
                  addSymbol();
                }
              }}
            />
            <Button type="button" variant="outline" onClick={addSymbol} disabled={update.isPending}>
              Add
            </Button>
          </div>
          <div className="flex items-center gap-2">
            <Button type="submit" size="sm" disabled={update.isPending}>
              {update.isPending ? "Saving..." : "Save watchlist"}
            </Button>
            {msg ? <p className="text-xs text-muted-foreground">{msg}</p> : null}
          </div>
        </form>
      </CardContent>
    </Card>
  );
}
