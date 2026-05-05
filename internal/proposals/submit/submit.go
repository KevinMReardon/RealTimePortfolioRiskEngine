// Package submit implements submitting an approved proposal to Alpaca (shared by HTTP and other callers).
package submit

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"go.uber.org/zap"

	"github.com/KevinMReardon/realtime-portfolio-risk/internal/agent"
	"github.com/KevinMReardon/realtime-portfolio-risk/internal/connectors/alpaca"
	"github.com/KevinMReardon/realtime-portfolio-risk/internal/events"
	"github.com/KevinMReardon/realtime-portfolio-risk/internal/policy"
	"github.com/KevinMReardon/realtime-portfolio-risk/internal/proposals"
)

// Outcome matches observability.ObserveProposalSubmit label strings (do not rename).
type Outcome string

const (
	OutcomeNotFound             Outcome = "not_found"
	OutcomeError                Outcome = "error"
	OutcomeBadStatus            Outcome = "bad_status"
	OutcomeVersionMismatch      Outcome = "version_mismatch"
	OutcomeNoKeys               Outcome = "no_keys"
	OutcomeAlpacaAccountError   Outcome = "alpaca_account_error"
	OutcomeAccountBlocked       Outcome = "account_blocked"
	OutcomeBadIntent            Outcome = "bad_intent"
	OutcomePolicyDenied         Outcome = "policy_denied"
	OutcomeNotionalBelowMinimum Outcome = "notional_below_minimum"
	OutcomeAlpacaPlaceError     Outcome = "alpaca_place_error"
	OutcomeConflictAfterBroker  Outcome = "conflict_after_broker"
	OutcomeSuccess              Outcome = "success"
)

// Result is the typed outcome of a broker submit attempt for HTTP or other callers to map.
type Result struct {
	Outcome Outcome

	BrokerOrderID string
	ProposalID    uuid.UUID

	PolicyDecision policy.Decision

	BrokerDetail string

	TradingBlocked bool
	AccountBlocked bool

	ValidationMsg string

	ProposalStatus string
}

// Options carries optional payload_hash / row_version overrides (same semantics as the HTTP body).
type Options struct {
	PayloadHash *string
	RowVersion  *int
}

// FromStore loads the proposal, requires approved status, then runs FromProposal.
func FromStore(ctx context.Context, deps Deps, portfolioID, proposalID uuid.UUID, opts Options) Result {
	log := deps.Log
	if log == nil {
		log = zap.NewNop()
	}
	if deps.Store == nil {
		return Result{Outcome: OutcomeError, ProposalID: proposalID}
	}
	prop, err := deps.Store.GetByIDForPortfolio(ctx, portfolioID, proposalID)
	if err != nil {
		if errors.Is(err, proposals.ErrProposalNotFound) {
			return Result{Outcome: OutcomeNotFound, ProposalID: proposalID}
		}
		return Result{Outcome: OutcomeError, ProposalID: proposalID}
	}
	if prop.Status != "approved" {
		log.Info("proposal_submit_rejected", zap.String("reason", "bad_status"), zap.String("status", prop.Status))
		return Result{Outcome: OutcomeBadStatus, ProposalStatus: prop.Status, ProposalID: proposalID}
	}
	return FromProposal(ctx, deps, prop, opts)
}

// FromProposal continues broker submit for an already-loaded approved row (HTTP path after JSON bind).
func FromProposal(ctx context.Context, deps Deps, prop proposals.Proposal, opts Options) Result {
	log := deps.Log
	if log == nil {
		log = zap.NewNop()
	}
	if deps.Store == nil || deps.Read == nil || deps.Keys == nil || deps.NewREST == nil {
		return Result{Outcome: OutcomeError, ProposalID: prop.ProposalID}
	}

	wantHash := strings.TrimSpace(prop.PayloadHash)
	wantVer := prop.RowVersion
	if opts.PayloadHash != nil && strings.TrimSpace(*opts.PayloadHash) != "" {
		wantHash = strings.TrimSpace(*opts.PayloadHash)
	}
	if opts.RowVersion != nil {
		wantVer = *opts.RowVersion
	}
	if wantHash != prop.PayloadHash || wantVer != prop.RowVersion {
		log.Info("proposal_submit_rejected", zap.String("reason", "version_mismatch"),
			zap.String("want_payload_hash", wantHash), zap.Int("want_row_version", wantVer),
			zap.String("db_payload_hash", prop.PayloadHash), zap.Int("db_row_version", prop.RowVersion))
		return Result{Outcome: OutcomeVersionMismatch, ProposalID: prop.ProposalID}
	}

	pid := prop.PortfolioID
	propID := prop.ProposalID

	keys, linked, err := deps.Keys.LoadPortfolioAlpacaKeyMaterial(ctx, pid)
	if err != nil {
		log.Warn("proposal_submit_alpaca_keys_failed", zap.Error(err))
		return Result{Outcome: OutcomeError, ProposalID: propID}
	}
	if !linked {
		log.Warn("proposal_submit_rejected", zap.String("reason", "no_alpaca_keys"), zap.String("portfolio_id", pid.String()))
		return Result{Outcome: OutcomeNoKeys, ProposalID: propID}
	}

	restCli, err := restClientFromKeys(keys, deps.NewREST)
	if err != nil {
		log.Warn("proposal_submit_alpaca_client_failed", zap.Error(err))
		return Result{Outcome: OutcomeError, ProposalID: propID}
	}

	acct, err := restCli.GetAccount(ctx)
	if err != nil {
		log.Warn("proposal_submit_alpaca_account_failed", zap.Error(err))
		_ = deps.Store.MarkProposalBrokerError(ctx, pid, propID, "alpaca GetAccount: "+err.Error())
		return Result{Outcome: OutcomeAlpacaAccountError, ProposalID: propID, BrokerDetail: err.Error()}
	}
	if acct.TradingBlocked || acct.AccountBlocked {
		return Result{
			Outcome:        OutcomeAccountBlocked,
			ProposalID:     propID,
			TradingBlocked: acct.TradingBlocked,
			AccountBlocked: acct.AccountBlocked,
		}
	}

	inAsm, found, err := deps.Read.LoadPortfolioAssemblerInput(ctx, pid)
	if err != nil || !found {
		return Result{Outcome: OutcomeError, ProposalID: propID}
	}
	nyLoc, err := time.LoadLocation("America/New_York")
	if err != nil {
		nyLoc = time.FixedZone("America/New_York", -5*3600)
	}
	nowFn := deps.Now
	if nowFn == nil {
		nowFn = time.Now
	}
	nowNY := nowFn().In(nyLoc)
	anchorDate := time.Date(nowNY.Year(), nowNY.Month(), nowNY.Day(), 0, 0, 0, 0, time.UTC)
	equityAnchor := decimal.Zero
	if eq, ok, err := deps.Store.LoadEquityAnchorForPortfolioDate(ctx, pid, anchorDate); err == nil && ok {
		equityAnchor = eq
	}
	dbKillActive, dbKillPresent, err := deps.Store.KillSwitchLatestActive(ctx)
	if err != nil {
		return Result{Outcome: OutcomeError, ProposalID: propID}
	}
	killEnv, killDB := proposals.KillSwitchInputs(deps.TradingHaltEnv, dbKillActive, dbKillPresent)
	snap := agent.BuildPolicySnapshot(inAsm, equityAnchor, nowNY, killEnv, killDB)
	snap.OptionalBroker = &policy.BrokerAccountSnapshot{
		PatternDayTrader: acct.PatternDayTrader,
		TradingBlocked:   acct.TradingBlocked,
		AccountBlocked:   acct.AccountBlocked,
		Equity:           acct.Equity,
	}

	intent, err := proposals.IntentFromProposal(prop)
	if err != nil {
		log.Info("proposal_submit_rejected", zap.String("reason", "bad_intent"), zap.Error(err))
		return Result{Outcome: OutcomeBadIntent, ProposalID: propID, ValidationMsg: err.Error()}
	}
	dec := policy.EvaluateForBrokerSubmit(intent, snap, deps.Policy)
	if dec.EffectiveOutcome != policy.OutcomeAllow {
		return Result{Outcome: OutcomePolicyDenied, ProposalID: propID, PolicyDecision: dec}
	}

	if prop.NotionalUSD != nil && prop.NotionalUSD.LessThan(alpaca.MinNotionalStockOrderUSD) {
		log.Info("proposal_submit_rejected", zap.String("reason", "notional_below_broker_minimum"),
			zap.String("notional_usd", prop.NotionalUSD.String()))
		return Result{Outcome: OutcomeNotionalBelowMinimum, ProposalID: propID}
	}

	coid := ""
	if prop.ClientOrderID != nil {
		coid = strings.TrimSpace(*prop.ClientOrderID)
	}
	if coid == "" {
		coid = "rtp-" + strings.ReplaceAll(propID.String(), "-", "")
	}
	ot := ""
	if prop.OrderType != nil {
		ot = strings.TrimSpace(*prop.OrderType)
	}
	tif := ""
	if prop.TimeInForce != nil {
		tif = strings.TrimSpace(*prop.TimeInForce)
	}
	placeIn := alpaca.PlaceOrderInput{
		Symbol:        strings.TrimSpace(prop.Symbol),
		Side:          strings.TrimSpace(strings.ToUpper(prop.Side)),
		Qty:           prop.Quantity,
		NotionalUSD:   prop.NotionalUSD,
		OrderType:     ot,
		TimeInForce:   tif,
		LimitPrice:    prop.LimitPrice,
		ClientOrderID: coid,
	}

	orderSnap, err := restCli.PlaceOrder(ctx, placeIn)
	if err != nil {
		msg := err.Error()
		_ = deps.Store.MarkProposalBrokerError(ctx, pid, propID, msg)
		log.Warn("proposal_submit_alpaca_place_failed", zap.Error(err))
		return Result{Outcome: OutcomeAlpacaPlaceError, ProposalID: propID, BrokerDetail: msg}
	}
	brokerID := strings.TrimSpace(orderSnap.ID)
	if brokerID == "" {
		brokerID = strings.TrimSpace(orderSnap.ClientOrderID)
	}
	if err := deps.Store.MarkProposalSubmitted(ctx, proposals.SubmitSuccessParams{
		PortfolioID:   pid,
		ProposalID:    propID,
		PayloadHash:   prop.PayloadHash,
		RowVersion:    prop.RowVersion,
		BrokerOrderID: brokerID,
	}); err != nil {
		if errors.Is(err, proposals.ErrSubmitConflict) {
			return Result{Outcome: OutcomeConflictAfterBroker, ProposalID: propID}
		}
		return Result{Outcome: OutcomeError, ProposalID: propID}
	}
	log.Info("proposal_submit_succeeded",
		zap.String("portfolio_id", pid.String()),
		zap.String("proposal_id", propID.String()),
		zap.String("broker_order_id", brokerID),
	)
	return Result{
		Outcome:       OutcomeSuccess,
		BrokerOrderID: brokerID,
		ProposalID:    propID,
	}
}

func restClientFromKeys(keys events.PortfolioAlpacaKeyMaterial, newREST RESTFactory) (alpaca.REST, error) {
	baseURL := strings.TrimSpace(keys.BaseURL)
	if baseURL == "" {
		if strings.EqualFold(keys.AccountMode, "live") {
			baseURL = alpaca.DefaultRESTBaseURLLive
		} else {
			baseURL = alpaca.DefaultRESTBaseURLPaper
		}
	}
	return newREST(alpaca.RESTConfig{
		KeyID:     keys.KeyID,
		SecretKey: keys.SecretKey,
		BaseURL:   baseURL,
	})
}
