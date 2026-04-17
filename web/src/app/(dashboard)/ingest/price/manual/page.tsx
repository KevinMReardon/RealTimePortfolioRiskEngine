import Link from "next/link";
import { ArrowLeft } from "lucide-react";

import { PriceIngestForm } from "@/components/ingest/price-form";
import { Button } from "@/components/ui/button";

export default function ManualPriceIngestPage() {
  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-center gap-3">
        <Button variant="ghost" size="sm" asChild>
          <Link href="/ingest/price" className="gap-2">
            <ArrowLeft className="h-4 w-4" aria-hidden />
            Back to Price data
          </Link>
        </Button>
      </div>
      <div className="mx-auto max-w-2xl space-y-1">
        <h1 className="text-2xl font-semibold tracking-tight">Manual price ingest</h1>
        <p className="text-sm text-muted-foreground">
          Debug and backfill path. Use monotonically increasing{" "}
          <span className="font-mono">source_sequence</span> per symbol in real feeds.
        </p>
      </div>
      <PriceIngestForm />
    </div>
  );
}
