import { apiFetch } from "@/lib/api/client";
import type { ListProposalsResponse } from "@/lib/api/types";

type ProposalVersionedInput = {
  payload_hash: string;
  row_version: number;
};

export function listProposals(portfolioId: string) {
  return apiFetch<ListProposalsResponse>(
    `/v1/portfolios/${encodeURIComponent(portfolioId)}/proposals`,
  );
}

export function approveProposal(
  portfolioId: string,
  proposalId: string,
  body: ProposalVersionedInput,
) {
  return apiFetch<{ status: string }>(
    `/v1/portfolios/${encodeURIComponent(portfolioId)}/proposals/${encodeURIComponent(proposalId)}/approve`,
    {
      method: "POST",
      body: JSON.stringify(body),
    },
  );
}

export function denyProposal(
  portfolioId: string,
  proposalId: string,
  body: ProposalVersionedInput & { deny_reason: string },
) {
  return apiFetch<{ status: string }>(
    `/v1/portfolios/${encodeURIComponent(portfolioId)}/proposals/${encodeURIComponent(proposalId)}/deny`,
    {
      method: "POST",
      body: JSON.stringify(body),
    },
  );
}

export function submitProposal(portfolioId: string, proposalId: string) {
  return apiFetch<{ status: string }>(
    `/v1/portfolios/${encodeURIComponent(portfolioId)}/proposals/${encodeURIComponent(proposalId)}/submit`,
    {
      method: "POST",
    },
  );
}
