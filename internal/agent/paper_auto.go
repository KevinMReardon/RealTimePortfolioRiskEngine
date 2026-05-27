package agent

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/KevinMReardon/realtime-portfolio-risk/internal/events"
	"github.com/KevinMReardon/realtime-portfolio-risk/internal/policy"
	"github.com/KevinMReardon/realtime-portfolio-risk/internal/proposals"
	"github.com/KevinMReardon/realtime-portfolio-risk/internal/proposals/submit"
)

// PaperAutoAlpacaKeys loads portfolio Alpaca credentials for paper-auto gating and submit.
type PaperAutoAlpacaKeys interface {
	LoadPortfolioAlpacaKeyMaterial(ctx context.Context, portfolioID uuid.UUID) (events.PortfolioAlpacaKeyMaterial, bool, error)
}

// PaperAutoProposalStore supports auto-approve and reload after materialize.
type PaperAutoProposalStore interface {
	ProposalReader
	CriticVerdictWriter
	ApproveProposalAuto(ctx context.Context, p proposals.AutoApproveParams) error
	ListByPortfolio(ctx context.Context, portfolioID uuid.UUID, filter proposals.ListFilter) ([]proposals.Proposal, error)
}

// PaperAutoConfig controls autonomous paper execution after briefing materialization.
type PaperAutoConfig struct {
	Enabled              bool
	Timeout              time.Duration
	MaxSubmitsPerSession int
}

// PaperAutoRunner executes critic → auto-approve → broker submit for eligible proposals (paper only).
type PaperAutoRunner struct {
	Config PaperAutoConfig
	Critic *Critic
	Submit submit.Deps
	Keys   PaperAutoAlpacaKeys
	Store  PaperAutoProposalStore
	Log    *zap.Logger
}

// RunAfterMaterialize processes newly inserted proposal IDs for one agent session.
func (r *PaperAutoRunner) RunAfterMaterialize(parent context.Context, portfolioID uuid.UUID, proposalIDs []uuid.UUID) {
	if r == nil || !r.Config.Enabled || len(proposalIDs) == 0 {
		return
	}
	log := r.Log
	if log == nil {
		log = zap.NewNop()
	}
	timeout := r.Config.Timeout
	if timeout <= 0 {
		timeout = 5 * time.Minute
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()

	keys, linked, err := r.Keys.LoadPortfolioAlpacaKeyMaterial(ctx, portfolioID)
	if err != nil {
		log.Warn("paper_auto_keys_failed", zap.Error(err), zap.String("portfolio_id", portfolioID.String()))
		return
	}
	if !linked {
		log.Warn("paper_auto_skipped", zap.String("reason", "no_alpaca_keys"), zap.String("portfolio_id", portfolioID.String()))
		return
	}
	if !submit.IsPaperLinked(keys) {
		log.Warn("paper_auto_skipped", zap.String("reason", "not_paper_account"), zap.String("portfolio_id", portfolioID.String()))
		return
	}

	max := r.Config.MaxSubmitsPerSession
	if max <= 0 {
		max = 5
	}
	submitted := 0
	for _, propID := range proposalIDs {
		if ctx.Err() != nil {
			return
		}
		if submitted >= max {
			log.Info("paper_auto_cap_reached", zap.Int("max", max), zap.String("portfolio_id", portfolioID.String()))
			return
		}
		if r.processOneWithOptions(ctx, log, portfolioID, propID, false) {
			submitted++
		}
	}
}

func (r *PaperAutoRunner) processOne(ctx context.Context, log *zap.Logger, portfolioID, propID uuid.UUID) bool {
	return r.processOneWithOptions(ctx, log, portfolioID, propID, false)
}

func (r *PaperAutoRunner) processOneWithOptions(ctx context.Context, log *zap.Logger, portfolioID, propID uuid.UUID, allowRetryDenied bool) bool {
	prop, err := r.Store.GetByIDForPortfolio(ctx, portfolioID, propID)
	if err != nil {
		log.Warn("paper_auto_load_proposal", zap.Error(err), zap.String("proposal_id", propID.String()))
		return false
	}
	if prop.Status != "proposed" {
		return false
	}
	allowByInitialPolicy := proposals.PolicyResultAllowsAutoSubmit(prop.PolicyResult)
	if !allowByInitialPolicy && !(allowRetryDenied && policyResultRetryable(prop.PolicyResult)) {
		log.Info("paper_auto_skipped_proposal", zap.String("reason", "policy_not_allow"), zap.String("proposal_id", propID.String()))
		return false
	}
	if r.Critic == nil {
		log.Warn("paper_auto_skipped", zap.String("reason", "no_critic"), zap.String("proposal_id", propID.String()))
		return false
	}
	verdict, err := r.Critic.Review(ctx, portfolioID, propID)
	if err != nil {
		log.Warn("paper_auto_critic_failed", zap.Error(err), zap.String("proposal_id", propID.String()))
		return false
	}
	if !verdict.Allow {
		log.Info("paper_auto_critic_veto", zap.String("reason_code", verdict.ReasonCode), zap.String("proposal_id", propID.String()))
		return false
	}
	if err := r.Store.ApproveProposalAuto(ctx, proposals.AutoApproveParams{
		PortfolioID: portfolioID,
		ProposalID:  propID,
		PayloadHash: prop.PayloadHash,
		RowVersion:  prop.RowVersion,
	}); err != nil {
		log.Warn("paper_auto_approve_failed", zap.Error(err), zap.String("proposal_id", propID.String()))
		return false
	}
	approved, err := r.Store.GetByIDForPortfolio(ctx, portfolioID, propID)
	if err != nil {
		log.Warn("paper_auto_reload_after_approve", zap.Error(err), zap.String("proposal_id", propID.String()))
		return false
	}
	res := submit.FromProposal(ctx, r.Submit, approved, submit.Options{})
	switch res.Outcome {
	case submit.OutcomeSuccess:
		log.Info("paper_auto_submitted",
			zap.String("proposal_id", propID.String()),
			zap.String("broker_order_id", res.BrokerOrderID),
		)
		return true
	default:
		log.Warn("paper_auto_submit_outcome",
			zap.String("outcome", string(res.Outcome)),
			zap.String("proposal_id", propID.String()),
		)
		return false
	}
}

func policyResultRetryable(raw json.RawMessage) bool {
	if proposals.PolicyResultAllowsAutoSubmit(raw) {
		return true
	}
	var rec proposals.PolicyResultRecord
	if err := json.Unmarshal(raw, &rec); err != nil {
		return false
	}
	for _, v := range rec.Violations {
		if strings.EqualFold(strings.TrimSpace(v.Code), policy.RuleMarketHours) {
			return true
		}
	}
	return false
}
