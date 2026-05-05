package api

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/KevinMReardon/realtime-portfolio-risk/internal/connectors/alpaca"
	"github.com/KevinMReardon/realtime-portfolio-risk/internal/events"
	"github.com/KevinMReardon/realtime-portfolio-risk/internal/observability"
	"github.com/KevinMReardon/realtime-portfolio-risk/internal/policy"
	"github.com/KevinMReardon/realtime-portfolio-risk/internal/proposals"
	"github.com/KevinMReardon/realtime-portfolio-risk/internal/proposals/submit"
)

// ProposalAlpacaKeyLoader returns stored per-portfolio Alpaca keys for broker submit.
type ProposalAlpacaKeyLoader interface {
	LoadPortfolioAlpacaKeyMaterial(ctx context.Context, portfolioID uuid.UUID) (events.PortfolioAlpacaKeyMaterial, bool, error)
}

// proposalSubmitRequest uses optional fields so older clients and empty bodies still work:
// omitted payload_hash / row_version default to the approved row from the database.
type proposalSubmitRequest struct {
	PayloadHash *string `json:"payload_hash"`
	RowVersion  *int    `json:"row_version"`
}

func postProposalSubmitHandler(
	store *proposals.Store,
	readStore PortfolioReadStore,
	priceStreamPartitions []uuid.UUID,
	alpacaKeys ProposalAlpacaKeyLoader,
	pol policy.Config,
	tradingHaltEnv bool,
	log *zap.Logger,
) gin.HandlerFunc {
	restFactory := func(cfg alpaca.RESTConfig) (alpaca.REST, error) {
		return alpaca.NewREST(cfg)
	}
	return func(c *gin.Context) {
		pid, ok := ensurePortfolioAccess(c, readStore, priceStreamPartitions)
		if !ok {
			return
		}
		if store == nil {
			respondAPIError(c, http.StatusServiceUnavailable, ErrCodeInsufficientData, "proposals store not configured", nil)
			return
		}
		if alpacaKeys == nil {
			respondAPIError(c, http.StatusServiceUnavailable, ErrCodeInsufficientData, "Alpaca credentials loader not configured", nil)
			return
		}
		if _, authOk := authUserFromContext(c); !authOk {
			respondAPIError(c, http.StatusUnauthorized, ErrCodeUnauthorized, "authentication required", nil)
			return
		}
		propID, err := uuid.Parse(c.Param("proposal_id"))
		if err != nil {
			respondAPIError(c, http.StatusBadRequest, ErrCodeValidation, "proposal_id must be a UUID", nil)
			return
		}
		if log == nil {
			log = zap.NewNop()
		}

		ctx := c.Request.Context()
		prop, err := store.GetByIDForPortfolio(ctx, pid, propID)
		if err != nil {
			if err == proposals.ErrProposalNotFound {
				respondAPIError(c, http.StatusNotFound, ErrCodeNotFound, "proposal not found", nil)
				observability.ObserveProposalSubmit("not_found")
				return
			}
			respondAPIError(c, http.StatusInternalServerError, ErrCodeInternal, "internal error", nil)
			observability.ObserveProposalSubmit("error")
			return
		}
		if prop.Status != "approved" {
			log.Info("proposal_submit_rejected", zap.String("reason", "bad_status"), zap.String("status", prop.Status))
			respondAPIError(c, http.StatusBadRequest, ErrCodeValidation, "proposal must be in approved status to submit", map[string]any{
				"status": prop.Status,
			})
			observability.ObserveProposalSubmit("bad_status")
			return
		}

		var body proposalSubmitRequest
		if err := c.ShouldBindJSON(&body); err != nil {
			log.Info("proposal_submit_rejected", zap.String("reason", "invalid_json"), zap.Error(err))
			respondAPIError(c, http.StatusBadRequest, ErrCodeValidation, "invalid request body including JSON shape", nil)
			return
		}

		deps := submit.Deps{
			Store:          store,
			Read:           readStore,
			Keys:           alpacaKeys,
			Policy:         pol,
			TradingHaltEnv: tradingHaltEnv,
			Log:            log,
			NewREST:        restFactory,
		}
		res := submit.FromProposal(ctx, deps, prop, submit.Options{
			PayloadHash: body.PayloadHash,
			RowVersion:  body.RowVersion,
		})
		mapProposalSubmitResult(c, log, res)
	}
}

func mapProposalSubmitResult(c *gin.Context, log *zap.Logger, res submit.Result) {
	outcome := string(res.Outcome)
	observability.ObserveProposalSubmit(outcome)

	switch res.Outcome {
	case submit.OutcomeSuccess:
		c.JSON(http.StatusOK, gin.H{
			"status":          "submitted",
			"broker_order_id": res.BrokerOrderID,
			"proposal_id":     res.ProposalID.String(),
		})
	case submit.OutcomeNotFound:
		respondAPIError(c, http.StatusNotFound, ErrCodeNotFound, "proposal not found", nil)
	case submit.OutcomeError:
		respondAPIError(c, http.StatusInternalServerError, ErrCodeInternal, "internal error", nil)
	case submit.OutcomeBadStatus:
		respondAPIError(c, http.StatusBadRequest, ErrCodeValidation, "proposal must be in approved status to submit", map[string]any{
			"status": res.ProposalStatus,
		})
	case submit.OutcomeVersionMismatch:
		respondAPIError(c, http.StatusConflict, ErrCodeConflict, "stale proposal version or payload mismatch", nil)
	case submit.OutcomeNoKeys:
		respondAPIError(c, http.StatusServiceUnavailable, ErrCodeInsufficientData, "portfolio has no Alpaca API keys; link Alpaca before submitting orders", nil)
	case submit.OutcomeAlpacaAccountError:
		respondAPIError(c, http.StatusBadGateway, ErrCodeInternal, "broker account request failed", map[string]any{
			"detail": res.BrokerDetail,
		})
	case submit.OutcomeAccountBlocked:
		respondAPIError(c, http.StatusForbidden, ErrCodeForbidden, "Alpaca account is not permitted to trade", map[string]any{
			"trading_blocked": res.TradingBlocked,
			"account_blocked": res.AccountBlocked,
		})
	case submit.OutcomeBadIntent:
		respondAPIError(c, http.StatusBadRequest, ErrCodeValidation, res.ValidationMsg, nil)
	case submit.OutcomePolicyDenied:
		dec := res.PolicyDecision
		respondAPIError(c, http.StatusUnprocessableEntity, ErrCodeValidation, "policy blocks submission for current market snapshot", map[string]any{
			"effective_outcome": string(dec.EffectiveOutcome),
			"violations":        dec.Violations,
		})
	case submit.OutcomeNotionalBelowMinimum:
		respondAPIError(c, http.StatusBadRequest, ErrCodeValidation, "notional_usd must be at least 1.00 USD (Alpaca minimum); use quantity or increase size", map[string]any{
			"min_notional_usd": alpaca.MinNotionalStockOrderUSD.String(),
		})
	case submit.OutcomeAlpacaPlaceError:
		respondAPIError(c, http.StatusBadGateway, ErrCodeInternal, "broker order request failed", map[string]any{
			"detail": res.BrokerDetail,
		})
	case submit.OutcomeConflictAfterBroker:
		respondAPIError(c, http.StatusConflict, ErrCodeConflict, "submit conflict (proposal changed during broker call)", nil)
	default:
		if log != nil {
			log.Warn("proposal_submit_unknown_outcome", zap.String("outcome", outcome))
		}
		respondAPIError(c, http.StatusInternalServerError, ErrCodeInternal, "internal error", nil)
	}
}
