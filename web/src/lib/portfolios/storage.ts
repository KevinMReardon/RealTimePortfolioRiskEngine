export const STORAGE_KEY = "rpre.saved_portfolios.v1";

export type SavedPortfolio = {
  id: string;
  label: string;
  createdAt: string;
};

function safeParse(raw: string | null): SavedPortfolio[] {
  if (!raw) return [];
  try {
    const v = JSON.parse(raw) as unknown;
    if (!Array.isArray(v)) return [];
    const out: SavedPortfolio[] = [];
    for (const item of v) {
      if (!item || typeof item !== "object") continue;
      const id = (item as { id?: unknown }).id;
      const label = (item as { label?: unknown }).label;
      const createdAt = (item as { createdAt?: unknown }).createdAt;
      if (typeof id !== "string" || !id) continue;
      out.push({
        id,
        label: typeof label === "string" && label ? label : "Portfolio",
        createdAt:
          typeof createdAt === "string" && createdAt
            ? createdAt
            : new Date().toISOString(),
      });
    }
    return out;
  } catch {
    return [];
  }
}

export function loadSavedPortfolios(): SavedPortfolio[] {
  if (typeof window === "undefined") return [];
  return safeParse(window.localStorage.getItem(STORAGE_KEY));
}

export function saveSavedPortfolios(items: SavedPortfolio[]) {
  if (typeof window === "undefined") return;
  window.localStorage.setItem(STORAGE_KEY, JSON.stringify(items));
}

export function upsertSavedPortfolio(input: { id: string; label?: string }) {
  const items = loadSavedPortfolios();
  const idx = items.findIndex((p) => p.id === input.id);
  const next: SavedPortfolio = {
    id: input.id,
    label: input.label?.trim() ? input.label.trim() : "Portfolio",
    createdAt: idx >= 0 ? items[idx]!.createdAt : new Date().toISOString(),
  };
  const merged =
    idx >= 0
      ? items.map((p, i) => (i === idx ? next : p))
      : [next, ...items];
  saveSavedPortfolios(merged);
  return merged;
}

export function removeSavedPortfolio(id: string) {
  const items = loadSavedPortfolios().filter((p) => p.id !== id);
  saveSavedPortfolios(items);
  return items;
}
