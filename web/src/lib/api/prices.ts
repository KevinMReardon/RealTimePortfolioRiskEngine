import { apiFetch } from "@/lib/api/client";
import type {
  ListPricesResponse,
  PriceDetailResponse,
  PriceFeedStatusResponse,
  PriceFeedWatchlistResponse,
  UpdatePriceFeedWatchlistRequest,
} from "@/lib/api/types";

export type ListPricesParams = {
  q?: string;
  sort?: string;
  order?: "asc" | "desc";
  limit?: number;
  offset?: number;
};

export function listPrices(params: ListPricesParams) {
  const sp = new URLSearchParams();
  if (params.q) sp.set("q", params.q);
  if (params.sort) sp.set("sort", params.sort);
  if (params.order) sp.set("order", params.order);
  if (params.limit != null) sp.set("limit", String(params.limit));
  if (params.offset != null) sp.set("offset", String(params.offset));
  const q = sp.toString();
  return apiFetch<ListPricesResponse>(`/v1/prices${q ? `?${q}` : ""}`);
}

export function getPriceSymbol(symbol: string, history = 10) {
  const path = `/v1/prices/${encodeURIComponent(symbol)}?history=${history}`;
  return apiFetch<PriceDetailResponse>(path);
}

export function getPriceFeedStatus() {
  return apiFetch<PriceFeedStatusResponse>("/v1/price-feed/status");
}

export function getPriceFeedWatchlist() {
  return apiFetch<PriceFeedWatchlistResponse>("/v1/price-feed/watchlist");
}

export function updatePriceFeedWatchlist(body: UpdatePriceFeedWatchlistRequest) {
  return apiFetch<PriceFeedWatchlistResponse>("/v1/price-feed/watchlist", {
    method: "PUT",
    body: JSON.stringify(body),
  });
}
