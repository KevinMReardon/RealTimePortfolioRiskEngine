import { apiFetch } from "@/lib/api/client";
import type {
  InsightsExplainResponse,
  PortfolioView,
  PostPriceRequest,
  PostTradeRequest,
  RiskHTTPResponse,
  ScenarioHTTPResponse,
  ScenarioRunRequest,
  IngestResponse,
} from "@/lib/api/types";

export function getPortfolio(portfolioId: string) {
  return apiFetch<PortfolioView>(`/v1/portfolios/${encodeURIComponent(portfolioId)}`);
}

export function getRisk(portfolioId: string) {
  return apiFetch<RiskHTTPResponse>(
    `/v1/portfolios/${encodeURIComponent(portfolioId)}/risk`,
  );
}

export function runScenario(portfolioId: string, body: ScenarioRunRequest) {
  return apiFetch<ScenarioHTTPResponse>(
    `/v1/portfolios/${encodeURIComponent(portfolioId)}/scenarios`,
    { method: "POST", body: JSON.stringify(body) },
  );
}

export function explainInsights(portfolioId: string, payload: unknown) {
  return apiFetch<InsightsExplainResponse>(
    `/v1/portfolios/${encodeURIComponent(portfolioId)}/insights/explain`,
    {
      method: "POST",
      body: JSON.stringify(payload ?? {}),
    },
  );
}

export function postTrade(body: PostTradeRequest) {
  return apiFetch<IngestResponse>(`/v1/trades`, {
    method: "POST",
    body: JSON.stringify(body),
  });
}

export function postPrice(body: PostPriceRequest) {
  return apiFetch<IngestResponse>(`/v1/prices`, {
    method: "POST",
    body: JSON.stringify(body),
  });
}
