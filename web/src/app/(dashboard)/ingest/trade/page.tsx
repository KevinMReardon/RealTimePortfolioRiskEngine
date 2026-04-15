import { Suspense } from "react";

import { TradeIngestForm } from "@/components/ingest/trade-form";
import { Skeleton } from "@/components/ui/skeleton";

export default function IngestTradePage() {
  return (
    <div className="space-y-4">
      <div className="mx-auto max-w-2xl space-y-1">
        <h1 className="text-2xl font-semibold tracking-tight">Ingest · Trade</h1>
        <p className="text-sm text-muted-foreground">
          Validations and idempotency behavior are enforced server-side.
        </p>
      </div>
      <Suspense fallback={<Skeleton className="mx-auto h-[520px] max-w-2xl" />}>
        <TradeIngestForm />
      </Suspense>
    </div>
  );
}
