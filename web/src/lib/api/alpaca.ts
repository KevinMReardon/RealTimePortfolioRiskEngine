import { apiFetch } from "@/lib/api/client";
import type {
  AlpacaReconciliationResponse,
  AlpacaStatusResponse,
} from "@/lib/api/types";

export function getAlpacaStatus(portfolioId: string) {
  return apiFetch<AlpacaStatusResponse>(
    `/v1/portfolios/${encodeURIComponent(portfolioId)}/alpaca/status`,
  );
}

export function getAlpacaReconciliation(portfolioId: string) {
  return apiFetch<AlpacaReconciliationResponse>(
    `/v1/portfolios/${encodeURIComponent(portfolioId)}/alpaca/reconciliation`,
  );
}
