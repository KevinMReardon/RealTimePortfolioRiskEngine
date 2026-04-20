import { Suspense } from "react";

import { AlpacaIntegrations } from "@/components/integrations/alpaca-integrations";
import { Skeleton } from "@/components/ui/skeleton";

export default function AlpacaIntegrationsPage() {
  return (
    <Suspense fallback={<Skeleton className="mx-auto h-[520px] max-w-5xl" />}>
      <AlpacaIntegrations />
    </Suspense>
  );
}
