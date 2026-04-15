import { PriceIngestForm } from "@/components/ingest/price-form";

export default function IngestPricePage() {
  return (
    <div className="space-y-4">
      <div className="mx-auto max-w-2xl space-y-1">
        <h1 className="text-2xl font-semibold tracking-tight">Ingest · Price</h1>
        <p className="text-sm text-muted-foreground">
          Use monotonically increasing <span className="font-mono">source_sequence</span> per symbol in real feeds.
        </p>
      </div>
      <PriceIngestForm />
    </div>
  );
}
