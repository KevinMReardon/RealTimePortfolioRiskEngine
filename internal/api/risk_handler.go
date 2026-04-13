package api

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"go.uber.org/zap"

	"github.com/KevinMReardon/realtime-portfolio-risk/internal/domain"
	"github.com/KevinMReardon/realtime-portfolio-risk/internal/portfolio"
	"github.com/KevinMReardon/realtime-portfolio-risk/internal/risk"
)

// MinDailyReturnsForVolatility is the minimum count of non-null daily returns per priced symbol
// required before parametric VaR inputs are considered reliable (STDDEV_SAMP needs >= 2 samples).
const MinDailyReturnsForVolatility = 2

// RiskReadStore loads portfolio projections and symbol return statistics for GET /risk.
type RiskReadStore interface {
	PortfolioReadStore
	LoadSymbolSigma1D(ctx context.Context, symbols []string, windowN int) (map[string]decimal.Decimal, error)
	LoadSymbolReturnSampleCounts(ctx context.Context, symbols []string, windowN int) (map[string]int, error)
	// ListRecentEventsForPortfolio returns the last limit events in chronological order (oldest first).
	ListRecentEventsForPortfolio(ctx context.Context, portfolioID uuid.UUID, limit int) ([]domain.EventEnvelope, error)
}

// RiskHTTPResponse is the LLD §10.4 risk query shape (fields + lineage aligned with §10.3 where applicable).
type RiskHTTPResponse struct {
	PortfolioID   string               `json:"portfolio_id"`
	Exposure      []riskExposureDTO    `json:"exposure"`
	Concentration riskConcentrationDTO `json:"concentration"`
	Volatility    riskVolatilityDTO    `json:"volatility"`
	Var95_1d      string               `json:"var_95_1d"`
	Assumptions   risk.Assumptions     `json:"assumptions"`
	Metadata      riskMetadataDTO      `json:"metadata"`

	DrivingEventIDs    []uuid.UUID               `json:"driving_event_ids,omitempty"`
	AsOfPositions      *portfolio.ReadAsOfRef    `json:"as_of_positions,omitempty"`
	AsOfPrices         []portfolio.PriceMarkAsOf `json:"as_of_prices,omitempty"`
	AsOfEventID        *uuid.UUID                `json:"as_of_event_id,omitempty"`
	AsOfEventTime      *time.Time                `json:"as_of_event_time,omitempty"`
	AsOfProcessingTime *time.Time                `json:"as_of_processing_time,omitempty"`
}

type riskExposureDTO struct {
	Symbol   string `json:"symbol"`
	Exposure string `json:"exposure"`
	Weight   string `json:"weight"`
}

type riskConcentrationDTO struct {
	TopN []riskExposureDTO `json:"top_n,omitempty"`
	HHI  string            `json:"hhi"`
}

type riskVolatilityDTO struct {
	Sigma1DPortfolio string          `json:"sigma_1d_portfolio"`
	BySymbol         []riskVolSymDTO `json:"by_symbol,omitempty"`
}

type riskVolSymDTO struct {
	Symbol  string `json:"symbol"`
	Sigma1D string `json:"sigma_1d"`
}

type riskMetadataDTO struct {
	SigmaWindowN            int `json:"sigma_window_n"`
	MinDailyReturnsRequired int `json:"min_daily_returns_required"`
}

func getRiskHandler(store RiskReadStore, log *zap.Logger, priceStreamPartitions []uuid.UUID, sigmaWindowN int) gin.HandlerFunc {
	if sigmaWindowN < 2 {
		sigmaWindowN = 60
	}
	return func(c *gin.Context) {
		base, ok := loadPortfolioBaseState(c, store, log, priceStreamPartitions, "risk_portfolio")
		if !ok {
			return
		}
		pid := base.PortfolioID
		ctx := c.Request.Context()

		resp, err := BuildRiskHTTPResponse(ctx, store, base, sigmaWindowN)
		if err != nil {
			var unpriced *ErrRiskUnpricedPositions
			var insuff *ErrRiskInsufficientReturnHistory
			switch {
			case errors.As(err, &unpriced):
				respondAPIError(c, http.StatusConflict, ErrCodeUnpricedPositionsPresent,
					"one or more open positions lack a price mark",
					map[string]any{"unpriced_symbols": unpriced.Symbols})
				return
			case errors.As(err, &insuff):
				respondAPIError(c, http.StatusUnprocessableEntity, ErrCodeInsufficientData,
					"insufficient daily return history for volatility estimate",
					map[string]any{
						"symbols":                    insuff.Symbols,
						"min_daily_returns_required": insuff.MinDailyReturnsRequired,
						"sigma_window_n":             insuff.SigmaWindowN,
					})
				return
			}
			log.Warn("risk_build_failed", zap.String("portfolio_id", pid.String()), zap.Error(err))
			respondAPIError(c, http.StatusInternalServerError, ErrCodeInternal, "internal error", nil)
			return
		}

		c.JSON(http.StatusOK, resp)
	}
}
