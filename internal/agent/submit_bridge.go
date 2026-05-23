package agent

import (
	"context"

	"github.com/google/uuid"

	"github.com/KevinMReardon/realtime-portfolio-risk/internal/proposals/submit"
)

// ProposalSubmitter submits an already-approved proposal via the shared broker pipeline.
type ProposalSubmitter interface {
	SubmitApproved(ctx context.Context, portfolioID, proposalID uuid.UUID) submit.Result
}

// ProposalSubmitBridge wraps submit.Deps for tool and automation callers.
type ProposalSubmitBridge struct {
	Deps submit.Deps
}

func NewProposalSubmitBridge(deps submit.Deps) *ProposalSubmitBridge {
	return &ProposalSubmitBridge{Deps: deps}
}

func (b *ProposalSubmitBridge) SubmitApproved(ctx context.Context, portfolioID, proposalID uuid.UUID) submit.Result {
	if b == nil {
		return submit.Result{Outcome: submit.OutcomeError, ProposalID: proposalID}
	}
	return submit.FromStore(ctx, b.Deps, portfolioID, proposalID, submit.Options{})
}
