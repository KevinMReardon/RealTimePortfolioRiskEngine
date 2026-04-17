"use client";

import * as React from "react";
import { Search } from "lucide-react";

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
import { Badge } from "@/components/ui/badge";
import { Skeleton } from "@/components/ui/skeleton";
import { ErrorAlert } from "@/components/feedback/query-states";
import { usePriceSymbolQuery } from "@/hooks/use-price-data";
import { compactUsdLike, formatDailyReturnFraction } from "@/lib/format";
import { ApiError } from "@/lib/api/errors";

function fmtClock(iso: string) {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return iso;
  return new Intl.DateTimeFormat(undefined, {
    dateStyle: "medium",
    timeStyle: "medium",
  }).format(d);
}

export function SymbolLookupPanel() {
  const [draft, setDraft] = React.useState("");
  const [active, setActive] = React.useState<string | null>(null);
  const q = usePriceSymbolQuery(active);

  function submit(e: React.FormEvent) {
    e.preventDefault();
    const sym = draft.trim().toUpperCase();
    setActive(sym.length ? sym : null);
  }

  return (
    <Card>
      <CardHeader className="pb-2">
        <CardTitle className="text-lg">Symbol lookup</CardTitle>
        <CardDescription>
          Query the latest projected mark and a short daily-return history.
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-4">
        <form onSubmit={submit} className="flex flex-col gap-2 sm:flex-row sm:items-end">
          <div className="flex-1 space-y-1.5">
            <Label htmlFor="sym-lookup">Ticker / symbol</Label>
            <Input
              id="sym-lookup"
              placeholder="e.g. AAPL"
              value={draft}
              onChange={(e) => setDraft(e.target.value)}
              autoCapitalize="characters"
              spellCheck={false}
            />
          </div>
          <Button type="submit" className="sm:mb-0.5">
            <Search className="mr-2 h-4 w-4" aria-hidden />
            Lookup
          </Button>
        </form>

        {!active ? (
          <p className="text-sm text-muted-foreground">Enter a symbol to load details.</p>
        ) : null}

        {active && q.isPending ? (
          <div className="space-y-2">
            <Skeleton className="h-6 w-48" />
            <Skeleton className="h-4 w-full" />
            <Skeleton className="h-4 w-3/4" />
          </div>
        ) : null}

        {active && q.isError ? (
          <ErrorAlert
            error={q.error}
            title={
              q.error instanceof ApiError && q.error.status === 404
                ? "No price mark for this symbol"
                : "Lookup failed"
            }
          />
        ) : null}

        {active && q.isSuccess ? (
          <div className="space-y-3 rounded-lg border bg-muted/30 p-4">
            <div className="flex flex-wrap items-center gap-2">
              <span className="text-lg font-semibold tracking-tight">{q.data.symbol}</span>
              <Badge
                variant={
                  q.data.provider_data_status === "fresh"
                    ? "success"
                    : q.data.provider_data_status === "stale"
                      ? "warning"
                      : "outline"
                }
              >
                {q.data.provider_data_status}
              </Badge>
            </div>
            <dl className="grid gap-2 text-sm sm:grid-cols-2">
              <div>
                <dt className="text-muted-foreground">Last price</dt>
                <dd className="font-mono text-base font-medium">
                  {compactUsdLike(q.data.price)}
                </dd>
              </div>
              <div>
                <dt className="text-muted-foreground">As of (event time)</dt>
                <dd className="font-medium">{fmtClock(q.data.as_of)}</dd>
              </div>
              <div>
                <dt className="text-muted-foreground">Projection updated</dt>
                <dd className="font-medium">{fmtClock(q.data.updated_at)}</dd>
              </div>
              <div>
                <dt className="text-muted-foreground">Source</dt>
                <dd className="break-all font-mono text-xs">{q.data.source || "—"}</dd>
              </div>
            </dl>
            <div>
              <div className="text-xs font-medium uppercase tracking-wide text-muted-foreground">
                Mini history (daily returns)
              </div>
              <p className="mt-1 text-sm text-muted-foreground">{q.data.history_summary}</p>
            </div>
            {q.data.history.length > 0 ? (
              <ul className="max-h-40 overflow-auto rounded border bg-background text-xs font-mono">
                {q.data.history.map((h) => (
                  <li
                    key={`${h.return_date}-${h.as_of_event_time}`}
                    className="flex justify-between gap-2 border-b px-2 py-1 last:border-b-0"
                  >
                    <span>{h.return_date}</span>
                    <span className="text-muted-foreground">
                      close {compactUsdLike(h.close_price)}
                    </span>
                    <span>{formatDailyReturnFraction(h.daily_return ?? null)}</span>
                  </li>
                ))}
              </ul>
            ) : null}
          </div>
        ) : null}
      </CardContent>
    </Card>
  );
}
