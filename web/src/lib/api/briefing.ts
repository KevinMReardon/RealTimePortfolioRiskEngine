import { apiFetch } from "@/lib/api/client";
import type {
  AgentSessionDTO,
  BriefingCreateRequestOptions,
  BriefingCreateResponse,
  ListBriefingsResponse,
  SessionReplayResponse,
} from "@/lib/api/types";

export function createBriefing(
  portfolioId: string,
  options?: BriefingCreateRequestOptions,
) {
  return apiFetch<BriefingCreateResponse>(
    `/v1/portfolios/${encodeURIComponent(portfolioId)}/briefings`,
    {
      method: "POST",
      body: JSON.stringify(options ?? {}),
    },
  );
}

export function getLatestBriefing(portfolioId: string) {
  return apiFetch<AgentSessionDTO>(
    `/v1/portfolios/${encodeURIComponent(portfolioId)}/briefings/latest`,
  );
}

export function listBriefings(portfolioId: string, limit: number, offset: number) {
  const q = new URLSearchParams({
    limit: String(limit),
    offset: String(offset),
  });
  return apiFetch<ListBriefingsResponse>(
    `/v1/portfolios/${encodeURIComponent(portfolioId)}/briefings?${q.toString()}`,
  );
}

export function getSessionReplay(sessionId: string) {
  return apiFetch<SessionReplayResponse>(
    `/v1/agent-sessions/${encodeURIComponent(sessionId)}/replay`,
  );
}
