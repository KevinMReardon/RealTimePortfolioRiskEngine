package api

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"go.uber.org/zap"

	"github.com/KevinMReardon/realtime-portfolio-risk/internal/agent"
	"github.com/KevinMReardon/realtime-portfolio-risk/internal/connectors/alpaca"
	"github.com/KevinMReardon/realtime-portfolio-risk/internal/events"
	"github.com/KevinMReardon/realtime-portfolio-risk/internal/observability"
	"github.com/KevinMReardon/realtime-portfolio-risk/internal/policy"
	"github.com/KevinMReardon/realtime-portfolio-risk/internal/proposals"
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
		wantHash := strings.TrimSpace(prop.PayloadHash)
		wantVer := prop.RowVersion
		if body.PayloadHash != nil && strings.TrimSpace(*body.PayloadHash) != "" {
			wantHash = strings.TrimSpace(*body.PayloadHash)
		}
		if body.RowVersion != nil {
			wantVer = *body.RowVersion
		}
		if wantHash != prop.PayloadHash || wantVer != prop.RowVersion {
			log.Info("proposal_submit_rejected", zap.String("reason", "version_mismatch"),
				zap.String("want_payload_hash", wantHash), zap.Int("want_row_version", wantVer),
				zap.String("db_payload_hash", prop.PayloadHash), zap.Int("db_row_version", prop.RowVersion))
			respondAPIError(c, http.StatusConflict, ErrCodeConflict, "stale proposal version or payload mismatch", nil)
			observability.ObserveProposalSubmit("version_mismatch")
			return
		}

		keys, linked, err := alpacaKeys.LoadPortfolioAlpacaKeyMaterial(ctx, pid)
		if err != nil {
			log.Warn("proposal_submit_alpaca_keys_failed", zap.Error(err))
			respondAPIError(c, http.StatusInternalServerError, ErrCodeInternal, "internal error", nil)
			observability.ObserveProposalSubmit("error")
			return
		}
		if !linked {
			log.Warn("proposal_submit_rejected", zap.String("reason", "no_alpaca_keys"), zap.String("portfolio_id", pid.String()))
			respondAPIError(c, http.StatusServiceUnavailable, ErrCodeInsufficientData, "portfolio has no Alpaca API keys; link Alpaca before submitting orders", nil)
			observability.ObserveProposalSubmit("no_keys")
			return
		}

		baseURL := strings.TrimSpace(keys.BaseURL)
		if baseURL == "" {
			if strings.EqualFold(keys.AccountMode, "live") {
				baseURL = alpaca.DefaultRESTBaseURLLive
			} else {
				baseURL = alpaca.DefaultRESTBaseURLPaper
			}
		}
		restCli, err := alpaca.NewREST(alpaca.RESTConfig{
			KeyID:     keys.KeyID,
			SecretKey: keys.SecretKey,
			BaseURL:   baseURL,
		})
		if err != nil {
			log.Warn("proposal_submit_alpaca_client_failed", zap.Error(err))
			respondAPIError(c, http.StatusInternalServerError, ErrCodeInternal, "internal error", nil)
			observability.ObserveProposalSubmit("error")
			return
		}

		acct, err := restCli.GetAccount(ctx)
		if err != nil {
			log.Warn("proposal_submit_alpaca_account_failed", zap.Error(err))
			_ = store.MarkProposalBrokerError(ctx, pid, propID, "alpaca GetAccount: "+err.Error())
			respondAPIError(c, http.StatusBadGateway, ErrCodeInternal, "broker account request failed", map[string]any{
				"detail": err.Error(),
			})
			observability.ObserveProposalSubmit("alpaca_account_error")
			return
		}
		if acct.TradingBlocked || acct.AccountBlocked {
			respondAPIError(c, http.StatusForbidden, ErrCodeForbidden, "Alpaca account is not permitted to trade", map[string]any{
				"trading_blocked": acct.TradingBlocked,
				"account_blocked": acct.AccountBlocked,
			})
			observability.ObserveProposalSubmit("account_blocked")
			return
		}

		inAsm, found, err := readStore.LoadPortfolioAssemblerInput(ctx, pid)
		if err != nil || !found {
			respondAPIError(c, http.StatusInternalServerError, ErrCodeInternal, "internal error", nil)
			observability.ObserveProposalSubmit("error")
			return
		}
		nyLoc, err := time.LoadLocation("America/New_York")
		if err != nil {
			nyLoc = time.FixedZone("America/New_York", -5*3600)
		}
		nowNY := time.Now().In(nyLoc)
		anchorDate := time.Date(nowNY.Year(), nowNY.Month(), nowNY.Day(), 0, 0, 0, 0, time.UTC)
		equityAnchor := decimal.Zero
		if eq, ok, err := store.LoadEquityAnchorForPortfolioDate(ctx, pid, anchorDate); err == nil && ok {
			equityAnchor = eq
		}
		dbKillActive, dbKillPresent, err := store.KillSwitchLatestActive(ctx)
		if err != nil {
			respondAPIError(c, http.StatusInternalServerError, ErrCodeInternal, "internal error", nil)
			observability.ObserveProposalSubmit("error")
			return
		}
		killEnv, killDB := proposals.KillSwitchInputs(tradingHaltEnv, dbKillActive, dbKillPresent)
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
			respondAPIError(c, http.StatusBadRequest, ErrCodeValidation, err.Error(), nil)
			observability.ObserveProposalSubmit("bad_intent")
			return
		}
		dec := policy.EvaluateForBrokerSubmit(intent, snap, pol)
		if dec.EffectiveOutcome != policy.OutcomeAllow {
			respondAPIError(c, http.StatusUnprocessableEntity, ErrCodeValidation, "policy blocks submission for current market snapshot", map[string]any{
				"effective_outcome": string(dec.EffectiveOutcome),
				"violations":        dec.Violations,
			})
			observability.ObserveProposalSubmit("policy_denied")
			return
		}

		if prop.NotionalUSD != nil && prop.NotionalUSD.LessThan(alpaca.MinNotionalStockOrderUSD) {
			log.Info("proposal_submit_rejected", zap.String("reason", "notional_below_broker_minimum"),
				zap.String("notional_usd", prop.NotionalUSD.String()))
			respondAPIError(c, http.StatusBadRequest, ErrCodeValidation, "notional_usd must be at least 1.00 USD (Alpaca minimum); use quantity or increase size", map[string]any{
				"min_notional_usd": alpaca.MinNotionalStockOrderUSD.String(),
			})
			observability.ObserveProposalSubmit("notional_below_minimum")
			return
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
			_ = store.MarkProposalBrokerError(ctx, pid, propID, msg)
			log.Warn("proposal_submit_alpaca_place_failed", zap.Error(err))
			respondAPIError(c, http.StatusBadGateway, ErrCodeInternal, "broker order request failed", map[string]any{
				"detail": msg,
			})
			observability.ObserveProposalSubmit("alpaca_place_error")
			return
		}
		brokerID := strings.TrimSpace(orderSnap.ID)
		if brokerID == "" {
			brokerID = strings.TrimSpace(orderSnap.ClientOrderID)
		}
		if err := store.MarkProposalSubmitted(ctx, proposals.SubmitSuccessParams{
			PortfolioID:   pid,
			ProposalID:    propID,
			PayloadHash:   prop.PayloadHash,
			RowVersion:    prop.RowVersion,
			BrokerOrderID: brokerID,
		}); err != nil {
			if err == proposals.ErrSubmitConflict {
				respondAPIError(c, http.StatusConflict, ErrCodeConflict, "submit conflict (proposal changed during broker call)", nil)
				observability.ObserveProposalSubmit("conflict_after_broker")
				return
			}
			respondAPIError(c, http.StatusInternalServerError, ErrCodeInternal, "internal error", nil)
			observability.ObserveProposalSubmit("error")
			return
		}
		log.Info("proposal_submit_succeeded",
			zap.String("portfolio_id", pid.String()),
			zap.String("proposal_id", propID.String()),
			zap.String("broker_order_id", brokerID),
		)
		observability.ObserveProposalSubmit("success")
		c.JSON(http.StatusOK, gin.H{
			"status":          "submitted",
			"broker_order_id": brokerID,
			"proposal_id":     propID.String(),
		})
	}
}
