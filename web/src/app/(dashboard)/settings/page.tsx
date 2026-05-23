import { Suspense } from "react";

import { SettingsPage } from "@/components/settings/settings-page";
import { Skeleton } from "@/components/ui/skeleton";

export default function SettingsRoutePage() {
  return (
    <Suspense fallback={<Skeleton className="mx-auto h-[520px] max-w-4xl" />}>
      <SettingsPage />
    </Suspense>
  );
}
