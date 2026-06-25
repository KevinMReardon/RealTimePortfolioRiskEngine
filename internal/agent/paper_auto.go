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

// PaperAutoProposalStore supports auto-approve, session-scoped listing, and retry accounting.
type PaperAutoProposalStore interface {
	ProposalReader
	CriticVerdictWriter
	ApproveProposalAuto(ctx context.Context, p proposals.AutoApproveParams) error
	ListByPortfolio(ctx context.Context, portfolioID uuid.UUID, filter proposals.ListFilter) ([]proposals.Proposal, error)
	ListByAgentSession(ctx context.Context, portfolioID, sessionID uuid.UUID, filter proposals.ListByAgentSessionFilter) ([]proposals.Proposal, error)
	RecordPaperAutoRetryFailure(ctx context.Context, portfolioID, proposalID uuid.UUID, maxAttempts int, lastError string) (proposals.Proposal, error)
}

// PaperAutoConfig controls autonomous paper execution after briefing materialization.
type PaperAutoConfig struct {
	Enabled              bool
	Timeout              time.Duration
	MaxSubmitsPerSession int
	MaxAutoRetries       int
}

type paperAutoPassKind int

const (
	paperAutoPassFresh paperAutoPassKind = iota
	paperAutoPassRetry
)

const defaultPaperAutoMaxRetries = 3

// PaperAutoRunner executes critic → auto-approve → broker submit for eligible proposals (paper only).
type PaperAutoRunner struct {
	Config PaperAutoConfig
	Critic *Critic
	Submit submit.Deps
	Keys   PaperAutoAlpacaKeys
	Store  PaperAutoProposalStore
	Log    *zap.Logger
}

func (r *PaperAutoRunner) maxAutoRetries() int {
	if r == nil || r.Config.MaxAutoRetries <= 0 {
		return defaultPaperAutoMaxRetries
	}
	return r.Config.MaxAutoRetries
}

// RunAfterMaterialize processes newly inserted proposal IDs for one agent session.
// Failures do not increment paper_auto_retry_count (first pass after briefing).
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

	if !r.paperKeysReady(ctx, log, portfolioID) {
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
		if r.processOne(ctx, log, portfolioID, propID, true, paperAutoPassFresh) {
			submitted++
		}
	}
}

func (r *PaperAutoRunner) paperKeysReady(ctx context.Context, log *zap.Logger, portfolioID uuid.UUID) bool {
	keys, linked, err := r.Keys.LoadPortfolioAlpacaKeyMaterial(ctx, portfolioID)
	if err != nil {
		log.Warn("paper_auto_keys_failed", zap.Error(err), zap.String("portfolio_id", portfolioID.String()))
		return false
	}
	if !linked {
		log.Warn("paper_auto_skipped", zap.String("reason", "no_alpaca_keys"), zap.String("portfolio_id", portfolioID.String()))
		return false
	}
	if !submit.IsPaperLinked(keys) {
		log.Warn("paper_auto_skipped", zap.String("reason", "not_paper_account"), zap.String("portfolio_id", portfolioID.String()))
		return false
	}
	return true
}

// processOneRetry is a scheduled retry pass; failures increment paper_auto_retry_count.
func (r *PaperAutoRunner) processOneRetry(ctx context.Context, log *zap.Logger, portfolioID, propID uuid.UUID) bool {
	return r.processOne(ctx, log, portfolioID, propID, true, paperAutoPassRetry)
}

// retrySubmitApproved attempts broker submit for an already-approved row.
func (r *PaperAutoRunner) retrySubmitApproved(ctx context.Context, log *zap.Logger, portfolioID uuid.UUID, prop proposals.Proposal) bool {
	if isPaperAutoTerminalStatus(prop.Status) {
		return false
	}
	if prop.PaperAutoRetryCount >= r.maxAutoRetries() {
		return false
	}
	if prop.Status != proposals.StatusApproved {
		return false
	}
	res := submit.FromProposal(ctx, r.Submit, prop, submit.Options{})
	if res.Outcome == submit.OutcomeSuccess {
		log.Info("paper_auto_retry_submitted",
			zap.String("portfolio_id", portfolioID.String()),
			zap.String("proposal_id", prop.ProposalID.String()),
			zap.String("broker_order_id", res.BrokerOrderID),
		)
		return true
	}
	r.recordRetryFailure(ctx, log, portfolioID, prop.ProposalID, "submit: "+string(res.Outcome))
	return false
}

func (r *PaperAutoRunner) processOne(ctx context.Context, log *zap.Logger, portfolioID, propID uuid.UUID, allowRetryDenied bool, pass paperAutoPassKind) bool {
	prop, err := r.Store.GetByIDForPortfolio(ctx, portfolioID, propID)
	if err != nil {
		log.Warn("paper_auto_load_proposal", zap.Error(err), zap.String("proposal_id", propID.String()))
		return false
	}
	if isPaperAutoTerminalStatus(prop.Status) {
		return false
	}
	if pass == paperAutoPassRetry && prop.PaperAutoRetryCount >= r.maxAutoRetries() {
		log.Debug("paper_auto_retry_skipped",
			zap.String("reason", "max_attempts"),
			zap.String("proposal_id", propID.String()),
			zap.Int("paper_auto_retry_count", prop.PaperAutoRetryCount),
		)
		return false
	}
	if prop.Status != proposals.StatusProposed {
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
	skipCritic := pass == paperAutoPassRetry && len(prop.CriticVerdict) > 0
	if !skipCritic {
		verdict, err := r.Critic.Review(ctx, portfolioID, propID)
		if err != nil {
			log.Warn("paper_auto_critic_failed", zap.Error(err), zap.String("proposal_id", propID.String()))
			if pass == paperAutoPassRetry {
				r.recordRetryFailure(ctx, log, portfolioID, propID, err.Error())
			}
			return false
		}
		if !verdict.Allow {
			log.Info("paper_auto_critic_veto", zap.String("reason_code", verdict.ReasonCode), zap.String("proposal_id", propID.String()))
			if pass == paperAutoPassRetry {
				r.recordRetryFailure(ctx, log, portfolioID, propID, "critic_veto: "+verdict.ReasonCode)
			}
			return false
		}
	}
	if err := r.Store.ApproveProposalAuto(ctx, proposals.AutoApproveParams{
		PortfolioID: portfolioID,
		ProposalID:  propID,
		PayloadHash: prop.PayloadHash,
		RowVersion:  prop.RowVersion,
	}); err != nil {
		log.Warn("paper_auto_approve_failed", zap.Error(err), zap.String("proposal_id", propID.String()))
		if pass == paperAutoPassRetry {
			r.recordRetryFailure(ctx, log, portfolioID, propID, err.Error())
		}
		return false
	}
	approved, err := r.Store.GetByIDForPortfolio(ctx, portfolioID, propID)
	if err != nil {
		log.Warn("paper_auto_reload_after_approve", zap.Error(err), zap.String("proposal_id", propID.String()))
		if pass == paperAutoPassRetry {
			r.recordRetryFailure(ctx, log, portfolioID, propID, err.Error())
		}
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
		if pass == paperAutoPassRetry {
			r.recordRetryFailure(ctx, log, portfolioID, propID, "submit: "+string(res.Outcome))
		}
		return false
	}
}

func (r *PaperAutoRunner) recordRetryFailure(ctx context.Context, log *zap.Logger, portfolioID, propID uuid.UUID, msg string) {
	if r == nil || r.Store == nil {
		return
	}
	prop, err := r.Store.RecordPaperAutoRetryFailure(ctx, portfolioID, propID, r.maxAutoRetries(), msg)
	if err != nil {
		log.Warn("paper_auto_record_retry_failure", zap.Error(err), zap.String("proposal_id", propID.String()))
		return
	}
	log.Info("paper_auto_retry_attempt_recorded",
		zap.String("proposal_id", propID.String()),
		zap.Int("paper_auto_retry_count", prop.PaperAutoRetryCount),
		zap.String("status", prop.Status),
	)
}

func isPaperAutoTerminalStatus(status string) bool {
	switch strings.TrimSpace(status) {
	case proposals.StatusSubmitted, proposals.StatusFilled, proposals.StatusRejected,
		proposals.StatusCancelled, proposals.StatusAutoAbandoned:
		return true
	default:
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
