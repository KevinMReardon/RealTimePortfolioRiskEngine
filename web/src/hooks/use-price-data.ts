"use client";

import {
  useMutation,
  useQuery,
  useQueryClient,
  type UseQueryResult,
} from "@tanstack/react-query";
import { errorHttpStatus } from "@/lib/api/errors";
import {
  getPriceFeedWatchlist,
  getPriceFeedStatus,
  getPriceSymbol,
  listPrices,
  updatePriceFeedWatchlist,
  type ListPricesParams,
} from "@/lib/api/prices";
import type {
  PriceDetailResponse,
  PriceFeedStatusResponse,
} from "@/lib/api/types";

export const priceQueryKeys = {
  list: (p: ListPricesParams) => ["prices", "list", p] as const,
  symbol: (symbol: string) => ["prices", "symbol", symbol] as const,
  feedStatus: () => ["prices", "feed-status"] as const,
  watchlist: () => ["prices", "watchlist"] as const,
};

export function usePriceFeedStatusQuery(): UseQueryResult<
  PriceFeedStatusResponse,
  Error
> {
  return useQuery({
    queryKey: priceQueryKeys.feedStatus(),
    queryFn: () => getPriceFeedStatus(),
    refetchInterval: (query) =>
      errorHttpStatus(query.state.error) === 401 ? false : 15_000,
  });
}

export function usePricesListQuery(params: ListPricesParams) {
  return useQuery({
    queryKey: priceQueryKeys.list(params),
    queryFn: () => listPrices(params),
    placeholderData: (prev) => prev,
  });
}

export function usePriceFeedWatchlistQuery() {
  return useQuery({
    queryKey: priceQueryKeys.watchlist(),
    queryFn: () => getPriceFeedWatchlist(),
    refetchInterval: (query) =>
      errorHttpStatus(query.state.error) === 401 ? false : 15_000,
  });
}

export function useUpdatePriceFeedWatchlistMutation() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: updatePriceFeedWatchlist,
    onSuccess: (data) => {
      qc.setQueryData(priceQueryKeys.watchlist(), data);
      qc.invalidateQueries({ queryKey: priceQueryKeys.feedStatus() });
      qc.invalidateQueries({ queryKey: ["prices", "list"] });
    },
  });
}

export function usePriceSymbolQuery(
  symbol: string | null,
): UseQueryResult<PriceDetailResponse, Error> {
  const sym = symbol?.trim() ?? "";
  return useQuery({
    queryKey: priceQueryKeys.symbol(sym.toUpperCase()),
    queryFn: () => getPriceSymbol(sym.toUpperCase(), 10),
    enabled: sym.length > 0,
    retry: 0,
  });
}

export type { PriceDetailResponse, PriceFeedStatusResponse };
