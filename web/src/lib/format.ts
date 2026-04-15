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
