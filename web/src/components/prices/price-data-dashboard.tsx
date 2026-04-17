"use client";

import Link from "next/link";
import { Wrench } from "lucide-react";

import { FeedStatusCard } from "@/components/prices/feed-status-card";
import { SymbolLookupPanel } from "@/components/prices/symbol-lookup-panel";
import { TrackedPricesTable } from "@/components/prices/tracked-prices-table";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";

export function PriceDataDashboard() {
  return (
    <div className="space-y-8">
      <div className="space-y-1">
        <h1 className="text-2xl font-semibold tracking-tight">Price data</h1>
        <p className="text-sm text-muted-foreground">
          Read-only view of projected marks, daily return history, and automated feed health.
        </p>
      </div>

      <FeedStatusCard />

      <div className="grid gap-6 lg:grid-cols-2">
        <SymbolLookupPanel />
        <Card className="border-dashed">
          <CardHeader className="pb-2">
            <CardTitle className="flex items-center gap-2 text-lg">
              <Wrench className="h-4 w-4" aria-hidden />
              Manual ingestion
            </CardTitle>
            <CardDescription>
              Admin and debug fallback only — not part of routine workflows.
            </CardDescription>
          </CardHeader>
          <CardContent>
            <Link
              href="/ingest/price/manual"
              className="text-sm font-medium text-primary underline-offset-4 hover:underline"
            >
              Open manual price form
            </Link>
          </CardContent>
        </Card>
      </div>

      <Card>
        <CardHeader className="pb-2">
          <CardTitle className="text-lg">All tracked symbols</CardTitle>
          <CardDescription>
            Server-backed pagination and sorting. Filter runs as you type (short debounce).
          </CardDescription>
        </CardHeader>
        <CardContent>
          <TrackedPricesTable />
        </CardContent>
      </Card>
    </div>
  );
}
