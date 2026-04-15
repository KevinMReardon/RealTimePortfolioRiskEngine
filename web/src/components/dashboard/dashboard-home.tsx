"use client";

import Link from "next/link";
import { useMemo } from "react";
import { useRouter, useSearchParams } from "next/navigation";

import { useSavedPortfolios } from "@/hooks/use-saved-portfolios";
import { usePortfolioQuery, useRiskQuery } from "@/hooks/use-portfolio-api";
import { compactNumber, compactUsdLike } from "@/lib/format";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Separator } from "@/components/ui/separator";
import { ExposureBars } from "@/components/charts/exposure-bars";
import { ErrorAlert, LoadingCardGrid } from "@/components/feedback/query-states";
import { PositionsTable } from "@/components/tables/positions-table";

export function DashboardHome() {
  const router = useRouter();
  const params = useSearchParams();
  const { items: saved } = useSavedPortfolios();

  const selected = params.get("portfolio") ?? saved[0]?.id ?? null;

  const portfolioQ = usePortfolioQuery(selected);
  const riskQ = useRiskQuery(selected);

  const headline = useMemo(() => {
    if (!selected) return "Pick a portfolio to begin";
    return saved.find((p) => p.id === selected)?.label ?? "Portfolio";
  }, [saved, selected]);

  return (
    <div className="mx-auto max-w-6xl space-y-6 animate-fade-in">
      <div className="flex flex-col gap-3 sm:flex-row sm:items-end sm:justify-between">
        <div className="space-y-1">
          <h1 className="text-2xl font-semibold tracking-tight">Overview</h1>
          <p className="text-sm text-muted-foreground">
            A calm, high-signal snapshot of exposure and risk for the selected portfolio.
          </p>
        </div>
        <div className="flex flex-col gap-2 sm:items-end">
          <div className="text-xs text-muted-foreground">Active portfolio</div>
          <Select
            value={selected ?? undefined}
            onValueChange={(v) => {
              const url = new URL(window.location.href);
              url.searchParams.set("portfolio", v);
              router.push(`${url.pathname}?${url.searchParams.toString()}`);
            }}
            disabled={!saved.length}
          >
            <SelectTrigger className="w-[min(92vw,380px)]">
              <SelectValue placeholder={saved.length ? "Select…" : "Add a portfolio first"} />
            </SelectTrigger>
            <SelectContent>
              {saved.map((p) => (
                <SelectItem key={p.id} value={p.id}>
                  <span className="font-medium">{p.label}</span>
                  <span className="ml-2 font-mono text-xs text-muted-foreground">
                    {p.id.slice(0, 8)}…
                  </span>
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          <Button asChild variant="outline" size="sm">
            <Link href="/portfolios">Manage portfolios</Link>
          </Button>
        </div>
      </div>

      {!selected ? (
        <Card>
          <CardHeader>
            <CardTitle>No portfolio selected</CardTitle>
            <CardDescription>
              The Go API addresses portfolios by UUID. Save one or more IDs locally to drive the UI.
            </CardDescription>
          </CardHeader>
          <CardContent className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
            <div className="text-sm text-muted-foreground">
              This keeps the console usable even though v1 doesn’t expose a portfolio directory endpoint.
            </div>
            <Button asChild>
              <Link href="/portfolios">Add a portfolio UUID</Link>
            </Button>
          </CardContent>
        </Card>
      ) : null}

      {selected && portfolioQ.isLoading ? <LoadingCardGrid /> : null}
      {selected && portfolioQ.error ? <ErrorAlert error={portfolioQ.error} /> : null}

      {selected && portfolioQ.data ? (
        <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-4">
          <Metric
            label="Market value"
            value={compactUsdLike(portfolioQ.data.totals.market_value)}
            hint="Mark-to-market totals"
          />
          <Metric
            label="Unrealized PnL"
            value={compactUsdLike(portfolioQ.data.totals.unrealized_pnl)}
            hint="Based on last marks"
          />
          <Metric
            label="Unpriced symbols"
            value={String(portfolioQ.data.unpriced_symbols.length)}
            hint={
              portfolioQ.data.unpriced_symbols.length
                ? portfolioQ.data.unpriced_symbols.join(", ")
                : "All open lots have marks"
            }
          />
          <Metric
            label="Positions"
            value={String(portfolioQ.data.positions.length)}
            hint={headline}
          />
        </div>
      ) : null}

      <div className="grid gap-4 lg:grid-cols-5">
        <Card className="lg:col-span-3">
          <CardHeader className="flex flex-row items-start justify-between gap-4 space-y-0">
            <div className="space-y-1">
              <CardTitle>Risk snapshot</CardTitle>
              <CardDescription>
                VaR and volatility are computed server-side (`GET /v1/portfolios/:id/risk`).
              </CardDescription>
            </div>
            {riskQ.isFetching ? <Badge variant="outline">Updating</Badge> : null}
          </CardHeader>
          <CardContent className="space-y-4">
            {selected && riskQ.isLoading ? (
              <LoadingCardGrid />
            ) : null}
            {selected && riskQ.error ? <ErrorAlert error={riskQ.error} /> : null}
            {selected && riskQ.data ? (
              <div className="grid gap-4 md:grid-cols-3">
                <div className="space-y-2">
                  <div className="text-xs text-muted-foreground">VaR (95%, 1d)</div>
                  <div className="text-2xl font-semibold tracking-tight">
                    {compactUsdLike(riskQ.data.var_95_1d)}
                  </div>
                  <div className="text-xs text-muted-foreground">
                    σ window: {riskQ.data.metadata.sigma_window_n}d
                  </div>
                </div>
                <div className="space-y-2">
                  <div className="text-xs text-muted-foreground">Portfolio σ (1d)</div>
                  <div className="text-2xl font-semibold tracking-tight">
                    {compactNumber(riskQ.data.volatility.sigma_1d_portfolio)}
                  </div>
                  <div className="text-xs text-muted-foreground">
                    HHI: {compactNumber(riskQ.data.concentration.hhi)}
                  </div>
                </div>
                <div className="space-y-2">
                  <div className="text-xs text-muted-foreground">Concentration</div>
                  <ExposureBars rows={riskQ.data.concentration.top_n ?? []} />
                </div>
              </div>
            ) : null}

            {!selected ? (
              <div className="text-sm text-muted-foreground">
                Select a portfolio to load risk metrics.
              </div>
            ) : null}
          </CardContent>
        </Card>

        <Card className="lg:col-span-2">
          <CardHeader>
            <CardTitle>Exposure mix</CardTitle>
            <CardDescription>Weights drive intuition; drill into detail for full tables.</CardDescription>
          </CardHeader>
          <CardContent>
            {riskQ.data ? (
              <ExposureBars rows={riskQ.data.exposure} />
            ) : (
              <div className="text-sm text-muted-foreground">
                Risk metrics will populate here once available.
              </div>
            )}
            <Separator className="my-4" />
            <div className="flex items-center justify-between gap-3">
              <div className="text-sm text-muted-foreground">
                Want the full audit trail?
              </div>
              <Button asChild size="sm" variant="secondary">
                <Link href={selected ? `/portfolios/${selected}` : "/portfolios"}>
                  Open portfolio
                </Link>
              </Button>
            </div>
          </CardContent>
        </Card>
      </div>

      <Card>
        <CardHeader>
          <CardTitle>Positions</CardTitle>
          <CardDescription>
            Sorting, filtering, and pagination are client-side (fast for typical portfolio sizes).
          </CardDescription>
        </CardHeader>
        <CardContent>
          {portfolioQ.data?.positions?.length ? (
            <PositionsTable rows={portfolioQ.data.positions} />
          ) : (
            <div className="text-sm text-muted-foreground">
              No positions to show yet — ingest trades to populate the projection.
            </div>
          )}
        </CardContent>
      </Card>
    </div>
  );
}

function Metric({
  label,
  value,
  hint,
}: {
  label: string;
  value: string;
  hint: string;
}) {
  return (
    <Card>
      <CardHeader className="pb-2">
        <CardDescription>{label}</CardDescription>
        <CardTitle className="text-2xl font-semibold tracking-tight">{value}</CardTitle>
      </CardHeader>
      <CardContent className="text-xs text-muted-foreground">{hint}</CardContent>
    </Card>
  );
}
