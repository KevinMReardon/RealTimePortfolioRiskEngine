"use client";

import * as React from "react";
import {
  loadSavedPortfolios,
  removeSavedPortfolio,
  upsertSavedPortfolio,
  type SavedPortfolio,
} from "@/lib/portfolios/storage";

export function useSavedPortfolios() {
  const [items, setItems] = React.useState<SavedPortfolio[]>([]);

  React.useEffect(() => {
    setItems(loadSavedPortfolios());
  }, []);

  const upsert = React.useCallback((input: { id: string; label?: string }) => {
    const merged = upsertSavedPortfolio(input);
    setItems(merged);
    return merged;
  }, []);

  const remove = React.useCallback((id: string) => {
    const merged = removeSavedPortfolio(id);
    setItems(merged);
    return merged;
  }, []);

  const refresh = React.useCallback(() => {
    setItems(loadSavedPortfolios());
  }, []);

  return { items, upsert, remove, refresh };
}
