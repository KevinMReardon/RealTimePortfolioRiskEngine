import type { RiskExposure } from "@/lib/api/types";
import { cn } from "@/lib/utils";

export function ExposureBars({
  rows,
  className,
}: {
  rows: RiskExposure[];
  className?: string;
}) {
  const sorted = [...rows].sort((a, b) => Number(b.weight) - Number(a.weight));
  const top = sorted.slice(0, 6);
  const maxW = Math.max(1e-9, ...top.map((r) => Number(r.weight)));

  if (!top.length) {
    return (
      <div className={cn("text-sm text-muted-foreground", className)}>
        No exposure rows yet.
      </div>
    );
  }

  return (
    <div className={cn("space-y-3", className)}>
      {top.map((r) => {
        const w = Number(r.weight);
        const pct = Math.max(0, Math.min(1, w / maxW));
        return (
          <div key={r.symbol} className="space-y-1">
            <div className="flex items-center justify-between gap-3 text-xs">
              <div className="truncate font-medium">{r.symbol}</div>
              <div className="tabular-nums text-muted-foreground">
                {(w * 100).toFixed(1)}%
              </div>
            </div>
            <div className="h-2 w-full overflow-hidden rounded-full bg-muted">
              <div
                className="h-full rounded-full bg-primary/80 transition-[width] duration-300"
                style={{ width: `${pct * 100}%` }}
              />
            </div>
          </div>
        );
      })}
    </div>
  );
}
