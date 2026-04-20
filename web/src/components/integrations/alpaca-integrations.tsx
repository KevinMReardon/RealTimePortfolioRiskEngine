"use client";

import Link from "next/link";
import { useCallback, useEffect, useMemo, useState } from "react";
import { useRouter, useSearchParams } from "next/navigation";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { usePortfoliosQuery } from "@/hooks/use-portfolio-api";
import { getAlpacaReconciliation, getAlpacaStatus } from "@/lib/api/alpaca";
import type {
  AlpacaReconciliationResponse,
  AlpacaStatusResponse,
} from "@/lib/api/types";

const POLL_MS = 10_000;

function formatWhen(iso?: string | null): string {
  if (!iso) return "—";
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return iso;
  return d.toLocaleString(undefined, {
    dateStyle: "medium",
    timeStyle: "short",
  });
}

function qtyDelta(internal: string, broker: string): string {
  const a = Number(internal);
  const b = Number(broker);
  if (!Number.isFinite(a) || !Number.isFinite(b)) return "—";
  const d = a - b;
  if (Object.is(d, -0)) return "0";
  const rounded = Number.isInteger(d)
    ? String(d)
    : String(Math.round(d * 1e8) / 1e8);
  return rounded.replace(/\.?0+$/, "") === "-0" ? "0" : rounded;
}

export function AlpacaIntegrations() {
  const router = useRouter();
  const params = useSearchParams();
  const portfoliosQ = usePortfoliosQuery();
  const saved = useMemo(() => portfoliosQ.data ?? [], [portfoliosQ.data]);

  const selected =
    params.get("portfolio") ?? saved[0]?.portfolio_id ?? null;

  const [status, setStatus] = useState<AlpacaStatusResponse | null>(null);
  const [recon, setRecon] = useState<AlpacaReconciliationResponse | null>(null);
  const [pollErr, setPollErr] = useState<string | null>(null);
  const mismatches = recon?.mismatches ?? [];
  const internalOnlySymbols = recon?.internal_only_symbols ?? [];
  const brokerOnlySymbols = recon?.broker_only_symbols ?? [];

  const fetchBoth = useCallback(async () => {
    if (!selected) return;
    setPollErr(null);
    try {
      const [s, r] = await Promise.all([
        getAlpacaStatus(selected),
        getAlpacaReconciliation(selected),
      ]);
      setStatus(s);
      setRecon(r);
    } catch (e) {
      const msg =
        e instanceof Error ? e.message : "Unable to refresh Alpaca data.";
      setPollErr(msg);
    }
  }, [selected]);

  useEffect(() => {
    void fetchBoth();
  }, [fetchBoth]);

  useEffect(() => {
    if (!selected) return;
    const id = window.setInterval(() => void fetchBoth(), POLL_MS);
    return () => window.clearInterval(id);
  }, [selected, fetchBoth]);

  const headlinePortfolio = saved.find((p) => p.portfolio_id === selected);

  return (
    <div className="mx-auto max-w-5xl space-y-6 animate-fade-in">
      <div className="flex flex-col gap-3 sm:flex-row sm:items-end sm:justify-between">
        <div className="space-y-1">
          <h1 className="text-2xl font-semibold tracking-tight">
            Connect Alpaca
          </h1>
          <p className="text-sm text-muted-foreground">
            Live sync health and position drift for the selected portfolio&apos;s Alpaca mapping.
          </p>
        </div>
        <div className="flex flex-col gap-2 sm:items-end">
          <div className="text-xs text-muted-foreground">
            {saved.length > 1 ? "Portfolio" : "Your portfolio"}
          </div>
          {saved.length > 1 ? (
            <Select
              value={selected ?? undefined}
              onValueChange={(v) => {
                const url = new URL(window.location.href);
                url.searchParams.set("portfolio", v);
                router.push(`${url.pathname}?${url.searchParams.toString()}`);
              }}
              disabled={portfoliosQ.isLoading}
            >
              <SelectTrigger className="w-[min(92vw,380px)]">
                <SelectValue placeholder="Select…" />
              </SelectTrigger>
              <SelectContent>
                {saved.map((p) => (
                  <SelectItem key={p.portfolio_id} value={p.portfolio_id}>
                    <span className="font-medium">{p.name}</span>
                    <span className="ml-2 font-mono text-xs text-muted-foreground">
                      {p.portfolio_id.slice(0, 8)}…
                    </span>
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          ) : saved.length === 1 ? (
            <div className="w-[min(92vw,380px)] rounded-md border bg-muted/30 px-3 py-2 text-left text-sm">
              <div className="font-medium">{saved[0].name}</div>
              <div className="break-all font-mono text-[11px] text-muted-foreground">
                {saved[0].portfolio_id}
              </div>
            </div>
          ) : null}
          <Button asChild variant="outline" size="sm">
            <Link href="/portfolios">Portfolio settings</Link>
          </Button>
        </div>
      </div>

      <Card className="border-dashed bg-muted/20">
        <CardHeader className="pb-2">
          <CardTitle className="text-base">How credentials work</CardTitle>
          <CardDescription>
            Alpaca API keys are not entered in this browser UI. For self-hosted
            or local development, configure{" "}
            <code className="rounded bg-muted px-1 py-0.5 font-mono text-[11px]">
              ALPACA_PAPER_KEY_ID
            </code>{" "}
            and{" "}
            <code className="rounded bg-muted px-1 py-0.5 font-mono text-[11px]">
              ALPACA_PAPER_SECRET_KEY
            </code>{" "}
            (or `ALPACA_LIVE_*`) on the Go API server only. This page reads
            status through your authenticated API proxy.
          </CardDescription>
        </CardHeader>
      </Card>

      {!selected ? (
        <Card>
          <CardHeader>
            <CardTitle>Select a portfolio</CardTitle>
            <CardDescription>
              Choose which book to compare against your Alpaca account.
            </CardDescription>
          </CardHeader>
        </Card>
      ) : null}

      {selected && headlinePortfolio ? (
        <p className="text-xs text-muted-foreground">
          Viewing{" "}
          <span className="font-medium text-foreground">
            {headlinePortfolio.name}
          </span>
          <span className="ml-2 font-mono">{selected}</span>
        </p>
      ) : null}

      {pollErr ? (
        <Card className="border-destructive/40 bg-destructive/5">
          <CardHeader>
            <CardTitle className="text-base text-destructive">
              Refresh failed
            </CardTitle>
            <CardDescription>{pollErr}</CardDescription>
          </CardHeader>
          <CardContent>
            <Button variant="outline" size="sm" onClick={() => void fetchBoth()}>
              Retry now
            </Button>
          </CardContent>
        </Card>
      ) : null}

      {selected ? (
        <>
          <Card>
            <CardHeader className="flex flex-row flex-wrap items-start justify-between gap-2 space-y-0">
              <div>
                <CardTitle className="text-lg">Sync status</CardTitle>
                <CardDescription>
                  Updated every {POLL_MS / 1000}s · Last server poll window
                </CardDescription>
              </div>
              <div className="flex items-center gap-2">
                <Badge
                  variant={status?.configured ? "success" : "outline"}
                >
                  {status?.configured
                    ? "Alpaca API configured"
                    : "Not configured"}
                </Badge>
                {status?.broker_unreachable ? (
                  <Badge variant="destructive">Broker unreachable</Badge>
                ) : null}
              </div>
            </CardHeader>
            <CardContent className="space-y-4 text-sm">
              <div className="grid gap-3 sm:grid-cols-2">
                <div>
                  <div className="text-xs text-muted-foreground">
                    Last successful sync
                  </div>
                  <div className="font-medium tabular-nums">
                    {formatWhen(status?.last_sync_at)}
                  </div>
                </div>
                <div>
                  <div className="text-xs text-muted-foreground">
                    Sync state updated
                  </div>
                  <div className="font-medium tabular-nums">
                    {formatWhen(status?.sync_state_updated_at)}
                  </div>
                </div>
              </div>
              {status?.last_error ? (
                <div className="rounded-md border border-amber-500/40 bg-amber-500/10 px-3 py-2 text-xs">
                  <span className="font-medium text-amber-900 dark:text-amber-100">
                    Last sync error:{" "}
                  </span>
                  {status.last_error}
                </div>
              ) : null}
              {status?.account_error ? (
                <div className="rounded-md border border-destructive/40 bg-destructive/10 px-3 py-2 text-xs">
                  <span className="font-medium">Account lookup: </span>
                  {status.account_error}
                </div>
              ) : null}
              {status?.account ? (
                <div className="rounded-lg border bg-card/60 px-3 py-3">
                  <div className="text-xs font-medium text-muted-foreground">
                    Broker account (non-sensitive)
                  </div>
                  <dl className="mt-2 grid gap-2 text-sm sm:grid-cols-2">
                    <div>
                      <dt className="text-xs text-muted-foreground">Status</dt>
                      <dd className="font-medium">{status.account.status}</dd>
                    </div>
                    <div>
                      <dt className="text-xs text-muted-foreground">
                        Currency
                      </dt>
                      <dd>{status.account.currency ?? "—"}</dd>
                    </div>
                    <div>
                      <dt className="text-xs text-muted-foreground">
                        Trading blocked
                      </dt>
                      <dd>{status.account.trading_blocked ? "Yes" : "No"}</dd>
                    </div>
                    <div>
                      <dt className="text-xs text-muted-foreground">
                        Account blocked
                      </dt>
                      <dd>{status.account.account_blocked ? "Yes" : "No"}</dd>
                    </div>
                  </dl>
                </div>
              ) : null}
            </CardContent>
          </Card>

          <Card>
            <CardHeader>
              <CardTitle className="text-lg">Position drift</CardTitle>
              <CardDescription>
                Internal projection vs Alpaca open positions (same API keys).
                Aggregate hash:{" "}
                <code className="rounded bg-muted px-1 py-0.5 font-mono text-[11px]">
                  {recon?.aggregate_hash?.slice(0, 16) ?? "—"}
                  …
                </code>
              </CardDescription>
            </CardHeader>
            <CardContent className="space-y-4">
              {recon?.broker_error ? (
                <div className="rounded-md border border-destructive/40 bg-destructive/10 px-3 py-2 text-sm">
                  {recon.broker_error}
                </div>
              ) : null}
              {!recon?.configured ? (
                <p className="text-sm text-muted-foreground">
                  Alpaca REST is not configured on the server — drift checks are
                  unavailable.
                </p>
              ) : mismatches.length === 0 &&
                internalOnlySymbols.length === 0 &&
                brokerOnlySymbols.length === 0 ? (
                <p className="text-sm text-muted-foreground">
                  No quantity drift detected for symbols with open exposure on
                  either side.
                </p>
              ) : (
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead>Symbol</TableHead>
                      <TableHead className="text-right">Internal qty</TableHead>
                      <TableHead className="text-right">Alpaca qty</TableHead>
                      <TableHead className="text-right">Delta</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {mismatches.map((row) => (
                      <TableRow key={row.symbol}>
                        <TableCell className="font-mono font-medium">
                          {row.symbol}
                        </TableCell>
                        <TableCell className="text-right tabular-nums">
                          {row.internal_quantity}
                        </TableCell>
                        <TableCell className="text-right tabular-nums">
                          {row.broker_quantity}
                        </TableCell>
                        <TableCell className="text-right tabular-nums">
                          {qtyDelta(
                            row.internal_quantity,
                            row.broker_quantity,
                          )}
                        </TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              )}
              {internalOnlySymbols.length > 0 || brokerOnlySymbols.length > 0 ? (
                <div className="flex flex-wrap gap-4 text-xs">
                  {internalOnlySymbols.length > 0 ? (
                    <div>
                      <span className="text-muted-foreground">
                        Internal-only (non-zero here, flat at broker):{" "}
                      </span>
                      <span className="font-mono">{internalOnlySymbols.join(", ")}</span>
                    </div>
                  ) : null}
                  {brokerOnlySymbols.length > 0 ? (
                    <div>
                      <span className="text-muted-foreground">
                        Broker-only (position at Alpaca, flat internally):{" "}
                      </span>
                      <span className="font-mono">{brokerOnlySymbols.join(", ")}</span>
                    </div>
                  ) : null}
                </div>
              ) : null}
            </CardContent>
          </Card>
        </>
      ) : null}
    </div>
  );
}
