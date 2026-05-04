"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { errorHttpStatus } from "@/lib/api/errors";
import {
  approveProposal,
  denyProposal,
  listProposals,
  submitProposal,
} from "@/lib/api/proposals";
import type { ListProposalsResponse } from "@/lib/api/types";

export const proposalsQueryKeys = {
  list: (portfolioId: string) => ["proposals", portfolioId, "list"] as const,
};

export function useProposalsQuery(portfolioId: string | null) {
  return useQuery({
    queryKey: portfolioId
      ? proposalsQueryKeys.list(portfolioId)
      : ["proposals", "none", "list"],
    queryFn: () => listProposals(portfolioId!),
    enabled: Boolean(portfolioId),
    placeholderData: (prev) => prev,
    refetchInterval: (query) =>
      errorHttpStatus(query.state.error) === 401 ? false : 15_000,
  });
}

function invalidateProposalReads(
  qc: ReturnType<typeof useQueryClient>,
  portfolioId: string,
) {
  return Promise.all([
    qc.invalidateQueries({ queryKey: proposalsQueryKeys.list(portfolioId) }),
    qc.invalidateQueries({ queryKey: ["briefings", portfolioId] }),
    qc.invalidateQueries({ queryKey: ["portfolio", portfolioId] }),
    qc.invalidateQueries({ queryKey: ["risk", portfolioId] }),
  ]);
}

export function useApproveProposalMutation(portfolioId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: {
      proposalId: string;
      payloadHash: string;
      rowVersion: number;
    }) =>
      approveProposal(portfolioId, input.proposalId, {
        payload_hash: input.payloadHash,
        row_version: input.rowVersion,
      }),
    onSuccess: async () => {
      await invalidateProposalReads(qc, portfolioId);
    },
  });
}

export function useDenyProposalMutation(portfolioId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: {
      proposalId: string;
      payloadHash: string;
      rowVersion: number;
      denyReason: string;
    }) =>
      denyProposal(portfolioId, input.proposalId, {
        payload_hash: input.payloadHash,
        row_version: input.rowVersion,
        deny_reason: input.denyReason,
      }),
    onSuccess: async () => {
      await invalidateProposalReads(qc, portfolioId);
    },
  });
}

export function useSubmitProposalMutation(portfolioId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: {
      proposalId: string;
      payloadHash: string;
      rowVersion: number;
    }) =>
      submitProposal(portfolioId, input.proposalId, {
        payload_hash: input.payloadHash,
        row_version: input.rowVersion,
      }),
    onSuccess: async () => {
      await invalidateProposalReads(qc, portfolioId);
    },
  });
}

export type { ListProposalsResponse };
