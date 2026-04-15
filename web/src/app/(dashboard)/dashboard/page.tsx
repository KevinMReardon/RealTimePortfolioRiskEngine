import { Suspense } from "react";

import { DashboardHome } from "@/components/dashboard/dashboard-home";
import { Skeleton } from "@/components/ui/skeleton";

export default function DashboardPage() {
  return (
    <Suspense fallback={<Skeleton className="mx-auto h-[520px] max-w-6xl" />}>
      <DashboardHome />
    </Suspense>
  );
}
