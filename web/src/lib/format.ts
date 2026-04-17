export function compactUsdLike(raw: string | undefined) {
  if (!raw) return "—";
  const n = Number(raw);
  if (!Number.isFinite(n)) return raw;
  return new Intl.NumberFormat(undefined, {
    maximumFractionDigits: 2,
  }).format(n);
}

export function compactNumber(raw: string | undefined) {
  if (!raw) return "—";
  const n = Number(raw);
  if (!Number.isFinite(n)) return raw;
  return new Intl.NumberFormat(undefined, {
    maximumSignificantDigits: 4,
  }).format(n);
}

/** Formats symbol_returns.daily_return stored as a decimal fraction (e.g. 0.012 → +1.2%). */
export function formatDailyReturnFraction(raw: string | undefined | null) {
  if (raw == null || raw === "") return "—";
  const n = Number(raw);
  if (!Number.isFinite(n)) return raw;
  return new Intl.NumberFormat(undefined, {
    style: "percent",
    maximumFractionDigits: 2,
    signDisplay: "exceptZero",
  }).format(n);
}
