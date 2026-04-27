"use client";

import {
  useMutation,
  useQuery,
  useQueryClient,
  type UseQueryResult,
} from "@tanstack/react-query";
import { errorHttpStatus } from "@/lib/api/errors";
import {
  createBriefing,
  getLatestBriefing,
  getSessionReplay,
  listBriefings,
} from "@/lib/api/briefing";
import type {
  AgentSessionDTO,
  BriefingCreateRequestOptions,
  BriefingCreateResponse,
  ListBriefingsResponse,
  SessionReplayResponse,
} from "@/lib/api/types";

const BRIEFING_POLL_MS = 2_000;

function isInFlightStatus(status: string | null | undefined): boolean {
  const s = (status ?? "").trim().toLowerCase();
  return s === "queued" || s === "running";
}

export const briefingQueryKeys = {
  root: (portfolioId: string) => ["briefings", portfolioId] as const,
  latest: (portfolioId: string) => ["briefings", portfolioId, "latest"] as const,
  list: (portfolioId: string, limit: number, offset: number) =>
    ["briefings", portfolioId, "list", limit, offset] as const,
  replay: (sessionId: string) => ["briefings", "replay", sessionId] as const,
};

export function useCreateBriefingMutation(portfolioId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (options?: BriefingCreateRequestOptions) =>
      createBriefing(portfolioId, options),
    onSuccess: async () => {
      await qc.invalidateQueries({ queryKey: briefingQueryKeys.root(portfolioId) });
    },
  });
}

export function useLatestBriefingQuery(
  portfolioId: string | null,
): UseQueryResult<AgentSessionDTO, Error> {
  return useQuery({
    queryKey: portfolioId
      ? briefingQueryKeys.latest(portfolioId)
      : ["briefings", "latest", "none"],
    queryFn: () => getLatestBriefing(portfolioId!),
    enabled: Boolean(portfolioId),
    refetchInterval: (query) => {
      if (errorHttpStatus(query.state.error) === 401) return false;
      const status = (query.state.data as AgentSessionDTO | undefined)?.Status;
      return isInFlightStatus(status) ? BRIEFING_POLL_MS : false;
    },
  });
}

export function useBriefingsListQuery(
  portfolioId: string | null,
  limit = 20,
  offset = 0,
): UseQueryResult<ListBriefingsResponse, Error> {
  return useQuery({
    queryKey: portfolioId
      ? briefingQueryKeys.list(portfolioId, limit, offset)
      : ["briefings", "list", "none", limit, offset],
    queryFn: () => listBriefings(portfolioId!, limit, offset),
    enabled: Boolean(portfolioId),
    placeholderData: (prev) => prev,
    refetchInterval: (query) => {
      if (errorHttpStatus(query.state.error) === 401) return false;
      const data = query.state.data as ListBriefingsResponse | undefined;
      const hasInFlight = Boolean(
        data?.items?.some((item) => isInFlightStatus(item.Status)),
      );
      return hasInFlight ? BRIEFING_POLL_MS : false;
    },
  });
}

export function useSessionReplayQuery(
  sessionId: string | null,
): UseQueryResult<SessionReplayResponse, Error> {
  return useQuery({
    queryKey: sessionId
      ? briefingQueryKeys.replay(sessionId)
      : ["briefings", "replay", "none"],
    queryFn: () => getSessionReplay(sessionId!),
    enabled: Boolean(sessionId),
  });
}

export type { BriefingCreateResponse };
