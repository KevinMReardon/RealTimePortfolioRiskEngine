"use client";

import Link from "next/link";
import { Wrench } from "lucide-react";

import { FeedStatusCard } from "@/components/prices/feed-status-card";
import { SymbolLookupPanel } from "@/components/prices/symbol-lookup-panel";
import { TrackedPricesTable } from "@/components/prices/tracked-prices-table";
import { WatchlistCard } from "@/components/prices/watchlist-card";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";

export function PriceDataDashboard() {
  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-end justify-between gap-2">
        <div className="space-y-1">
          <h1 className="text-2xl font-semibold tracking-tight">Price data</h1>
          <p className="text-sm text-muted-foreground">
            Projected marks, daily-return history, and automated feed health.
          </p>
        </div>
        <Link
          href="/ingest/price/manual"
          className="inline-flex items-center gap-1.5 text-xs text-muted-foreground underline-offset-4 hover:text-foreground hover:underline"
        >
          <Wrench className="h-3.5 w-3.5" aria-hidden />
          Manual ingestion
        </Link>
      </div>

      <div className="grid gap-4 lg:grid-cols-2">
        <FeedStatusCard />
        <SymbolLookupPanel />
      </div>

      <WatchlistCard />

      <Card>
        <CardHeader className="pb-2">
          <CardTitle className="text-base">All tracked symbols</CardTitle>
          <CardDescription>
            Server-backed pagination and sorting. Filter runs as you type.
          </CardDescription>
        </CardHeader>
        <CardContent>
          <TrackedPricesTable />
        </CardContent>
      </Card>
    </div>
  );
}
