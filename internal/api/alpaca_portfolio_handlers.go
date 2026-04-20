package api

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/KevinMReardon/realtime-portfolio-risk/internal/connectors/alpaca"
)

type alpacaPublicAccount struct {
	ID               string    `json:"id,omitempty"`
	Status           string    `json:"status"`
	Currency         string    `json:"currency,omitempty"`
	PatternDayTrader bool      `json:"pattern_day_trader"`
	TradingBlocked   bool      `json:"trading_blocked"`
	AccountBlocked   bool      `json:"account_blocked"`
	CreatedAt        time.Time `json:"created_at,omitempty"`
}

type alpacaSyncStateSnapshot struct {
	LastSuccessAt  *time.Time `json:"last_success_at,omitempty"`
	LastError      *string    `json:"last_error,omitempty"`
	StateUpdatedAt *time.Time `json:"updated_at,omitempty"`
}

type alpacaStatusResponse struct {
	Configured bool `json:"configured"`

	LastSyncAt     *time.Time `json:"last_sync_at,omitempty"`
	LastError      *string    `json:"last_error,omitempty"`
	StateUpdatedAt *time.Time `json:"sync_state_updated_at,omitempty"`

	Sync *alpacaSyncStateSnapshot `json:"sync,omitempty"`

	Account           *alpacaPublicAccount `json:"account,omitempty"`
	AccountError      string               `json:"account_error,omitempty"`
	BrokerUnreachable bool                 `json:"broker_unreachable,omitempty"`
}

type alpacaReconciliationHTTPResponse struct {
	Configured bool `json:"configured"`

	Mismatches          []AlpacaQtyMismatchRow `json:"mismatches"`
	InternalOnlySymbols []string               `json:"internal_only_symbols"`
	BrokerOnlySymbols   []string               `json:"broker_only_symbols"`
	AggregateHash       string               `json:"aggregate_hash"`

	BrokerError       string `json:"broker_error,omitempty"`
	BrokerUnreachable bool   `json:"broker_unreachable,omitempty"`
}

func getAlpacaPortfolioStatusHandler(
	readStore PortfolioReadStore,
	syncStore *alpaca.SyncStateStore,
	rest alpaca.REST,
	alpacaConfigured bool,
	log *zap.Logger,
	priceStreamPartitions []uuid.UUID,
) gin.HandlerFunc {
	return func(c *gin.Context) {
		pid, _, ok := loadPortfolioAssemblerInputOnly(c, readStore, log, priceStreamPartitions, "alpaca_status")
		if !ok {
			return
		}

		resp := alpacaStatusResponse{
			Configured: alpacaConfigured && rest != nil,
		}

		ctx := c.Request.Context()
		if syncStore != nil {
			st, err := syncStore.Get(ctx, pid)
			if err != nil {
				log.Warn("alpaca_status_sync_state", zap.String("portfolio_id", pid.String()), zap.Error(err))
				respondAPIError(c, http.StatusInternalServerError, ErrCodeInternal, "internal error", nil)
				return
			}
			if st != nil {
				ut := st.UpdatedAt.UTC()
				resp.StateUpdatedAt = &ut
				resp.Sync = &alpacaSyncStateSnapshot{
					LastSuccessAt:  cloneTimePtr(st.LastSuccessAt),
					LastError:      cloneStrPtr(st.LastError),
					StateUpdatedAt: &ut,
				}
				resp.LastSyncAt = cloneTimePtr(st.LastSuccessAt)
				resp.LastError = cloneStrPtr(st.LastError)
			}
		}

		if alpacaConfigured && rest != nil {
			acct, err := rest.GetAccount(ctx)
			if err != nil {
				resp.AccountError = sanitizeBrokerErr(err)
				resp.BrokerUnreachable = true
				log.Warn("alpaca_status_get_account", zap.String("portfolio_id", pid.String()), zap.Error(err))
			} else {
				resp.Account = publicAccountFromSummary(acct)
			}
		}

		c.JSON(http.StatusOK, resp)
	}
}

func getAlpacaPortfolioReconciliationHandler(
	readStore PortfolioReadStore,
	rest alpaca.REST,
	alpacaConfigured bool,
	log *zap.Logger,
	priceStreamPartitions []uuid.UUID,
) gin.HandlerFunc {
	return func(c *gin.Context) {
		pid, input, ok := loadPortfolioAssemblerInputOnly(c, readStore, log, priceStreamPartitions, "alpaca_reconcile")
		if !ok {
			return
		}

		body := alpacaReconciliationHTTPResponse{
			Configured:          alpacaConfigured && rest != nil,
			Mismatches:          []AlpacaQtyMismatchRow{},
			InternalOnlySymbols: []string{},
			BrokerOnlySymbols:   []string{},
		}

		if !alpacaConfigured || rest == nil {
			c.JSON(http.StatusOK, body)
			return
		}

		ctx := c.Request.Context()
		brokerPos, err := rest.ListPositions(ctx)
		if err != nil {
			body.BrokerError = sanitizeBrokerErr(err)
			body.BrokerUnreachable = true
			log.Warn("alpaca_reconcile_positions", zap.String("portfolio_id", pid.String()), zap.Error(err))
			c.JSON(http.StatusOK, body)
			return
		}

		payload := ReconcileQtyDrift(input.Positions, brokerPos)
		body.Mismatches = payload.Mismatches
		body.InternalOnlySymbols = payload.InternalOnlySymbols
		body.BrokerOnlySymbols = payload.BrokerOnlySymbols
		body.AggregateHash = payload.AggregateHash

		c.JSON(http.StatusOK, body)
	}
}

func publicAccountFromSummary(a alpaca.AccountSummary) *alpacaPublicAccount {
	return &alpacaPublicAccount{
		ID:               strings.TrimSpace(a.ID),
		Status:           strings.TrimSpace(a.Status),
		Currency:         strings.TrimSpace(a.Currency),
		PatternDayTrader: a.PatternDayTrader,
		TradingBlocked:   a.TradingBlocked,
		AccountBlocked:   a.AccountBlocked,
		CreatedAt:        a.CreatedAt.UTC(),
	}
}

func cloneTimePtr(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	x := t.UTC()
	return &x
}

func cloneStrPtr(p *string) *string {
	if p == nil {
		return nil
	}
	x := strings.TrimSpace(*p)
	return &x
}

func sanitizeBrokerErr(err error) string {
	s := strings.TrimSpace(err.Error())
	const max = 512
	if len(s) <= max {
		return s
	}
	return s[:max]
}
