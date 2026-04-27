import { Suspense } from "react";

import { BriefingPage } from "@/components/briefing/briefing-page";
import { Skeleton } from "@/components/ui/skeleton";

export default function BriefingRoutePage() {
  return (
    <Suspense fallback={<Skeleton className="mx-auto h-[520px] max-w-6xl" />}>
      <BriefingPage />
    </Suspense>
  );
}
