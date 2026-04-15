"use client";

import {
  useMutation,
  useQuery,
  useQueryClient,
  type UseQueryResult,
} from "@tanstack/react-query";
import {
  explainInsights,
  getPortfolio,
  getRisk,
  postPrice,
  postTrade,
  runScenario,
} from "@/lib/api/portfolio";
import type {
  InsightsExplainResponse,
  IngestResponse,
  PortfolioView,
  PostPriceRequest,
  PostTradeRequest,
  RiskHTTPResponse,
  ScenarioHTTPResponse,
  ScenarioRunRequest,
} from "@/lib/api/types";

export const qk = {
  portfolio: (id: string) => ["portfolio", id] as const,
  risk: (id: string) => ["risk", id] as const,
};

export function usePortfolioQuery(
  portfolioId: string | null,
): UseQueryResult<PortfolioView, Error> {
  return useQuery({
    queryKey: portfolioId ? qk.portfolio(portfolioId) : ["portfolio", "none"],
    queryFn: () => getPortfolio(portfolioId!),
    enabled: Boolean(portfolioId),
  });
}

export function useRiskQuery(
  portfolioId: string | null,
): UseQueryResult<RiskHTTPResponse, Error> {
  return useQuery({
    queryKey: portfolioId ? qk.risk(portfolioId) : ["risk", "none"],
    queryFn: () => getRisk(portfolioId!),
    enabled: Boolean(portfolioId),
    retry: (failureCount, error) => {
      const msg = error?.message ?? "";
      // Common “data not ready” cases for this API — don’t hammer retries.
      if (msg.toLowerCase().includes("unpriced")) return false;
      if (msg.toLowerCase().includes("insufficient")) return false;
      return failureCount < 1;
    },
  });
}

export function useRunScenarioMutation(portfolioId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: ScenarioRunRequest) => runScenario(portfolioId, body),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: qk.portfolio(portfolioId) });
    },
  });
}

export function useExplainInsightsMutation(portfolioId: string) {
  return useMutation({
    mutationFn: (payload: unknown) => explainInsights(portfolioId, payload),
  });
}

export function usePostTradeMutation() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: PostTradeRequest) => postTrade(body),
    onSuccess: async (_data, vars) => {
      await qc.invalidateQueries({ queryKey: qk.portfolio(vars.portfolio_id) });
      await qc.invalidateQueries({ queryKey: qk.risk(vars.portfolio_id) });
    },
  });
}

export function usePostPriceMutation() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: PostPriceRequest) => postPrice(body),
    // Price updates can affect many portfolios; simplest safe refresh: invalidate all watched queries.
    onSuccess: async () => {
      await qc.invalidateQueries();
    },
  });
}

export type { ScenarioHTTPResponse, InsightsExplainResponse, IngestResponse };
