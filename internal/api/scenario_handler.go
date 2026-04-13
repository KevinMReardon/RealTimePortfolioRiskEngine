package api

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"go.uber.org/zap"

	"github.com/KevinMReardon/realtime-portfolio-risk/internal/portfolio"
	"github.com/KevinMReardon/realtime-portfolio-risk/internal/scenario"
)

type scenarioShockRequest struct {
	Symbol string          `json:"symbol" binding:"required"`
	Type   string          `json:"type"`
	// Kind is accepted as a backward-compatible alias for Type in external requests.
	Kind  string          `json:"kind"`
	Value  decimal.Decimal `json:"value" binding:"required"`
}

type runScenarioRequest struct {
	Shocks []scenarioShockRequest `json:"shocks" binding:"required"`
}

// ScenarioBaseMetadata is LLD §10.5 “base snapshot metadata”: as-of and lineage for the read
// the scenario was anchored to (same fields as portfolio.PortfolioView top-level lineage).
type ScenarioBaseMetadata struct {
	DrivingEventIDs    []uuid.UUID               `json:"driving_event_ids,omitempty"`
	AsOfPositions      *portfolio.ReadAsOfRef    `json:"as_of_positions,omitempty"`
	AsOfPrices         []portfolio.PriceMarkAsOf `json:"as_of_prices,omitempty"`
	AsOfEventID        *uuid.UUID                `json:"as_of_event_id,omitempty"`
	AsOfEventTime      *time.Time                `json:"as_of_event_time,omitempty"`
	AsOfProcessingTime *time.Time                `json:"as_of_processing_time,omitempty"`
}

// scenarioHTTPResponse is LLD §10.5: base_metadata + full base snapshot, shocked outputs,
// delta vs base, echo of shocks. Read-only: no DB writes and no audit persistence (v1).
type scenarioHTTPResponse struct {
	PortfolioID string `json:"portfolio_id"`

	BaseMetadata ScenarioBaseMetadata    `json:"base_metadata"`
	Base         portfolio.PortfolioView `json:"base"`
	Shocked      portfolio.PortfolioView `json:"shocked"`

	Delta  scenarioDeltaDTO `json:"delta"`
	Shocks []scenarioShock  `json:"shocks"`

	// Flat as-of echoes LLD §9 scenario return shape (same underlying read as base_metadata / base).
	BaseAsOfEventID        *uuid.UUID `json:"base_as_of_event_id,omitempty"`
	BaseAsOfEventTime      *string    `json:"base_as_of_event_time,omitempty"`
	BaseAsOfProcessingTime *string    `json:"base_as_of_processing_time,omitempty"`
}

type scenarioDeltaDTO struct {
	MarketValue   string `json:"market_value"`
	UnrealizedPnL string `json:"unrealized_pnl"`
	RealizedPnL   string `json:"realized_pnl"`
}

type scenarioShock struct {
	Symbol string `json:"symbol"`
	Type   string `json:"type"`
	Value  string `json:"value"`
}

func scenarioBaseMetadataFromView(v portfolio.PortfolioView) ScenarioBaseMetadata {
	var driving []uuid.UUID
	if len(v.DrivingEventIDs) > 0 {
		driving = append([]uuid.UUID(nil), v.DrivingEventIDs...)
	}
	var asOfPrices []portfolio.PriceMarkAsOf
	if len(v.AsOfPrices) > 0 {
		asOfPrices = append([]portfolio.PriceMarkAsOf(nil), v.AsOfPrices...)
	}
	return ScenarioBaseMetadata{
		DrivingEventIDs:    driving,
		AsOfPositions:      v.AsOfPositions,
		AsOfPrices:         asOfPrices,
		AsOfEventID:        v.AsOfEventID,
		AsOfEventTime:      v.AsOfEventTime,
		AsOfProcessingTime: v.AsOfProcessingTime,
	}
}

func postScenarioHandler(readStore PortfolioReadStore, log *zap.Logger, priceStreamPartitions []uuid.UUID) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req runScenarioRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			respondAPIError(c, http.StatusBadRequest, ErrCodeValidation, "invalid request body including JSON shape", nil)
			return
		}

		shocks := make([]scenario.Shock, len(req.Shocks))
		for i, sh := range req.Shocks {
			shockType := sh.Type
			if shockType == "" {
				shockType = sh.Kind
			}
			shocks[i] = scenario.Shock{Symbol: sh.Symbol, Type: shockType, Value: sh.Value}
		}
		if err := scenario.ValidateShocks(shocks); err != nil {
			st, code, msg := MapScenarioRunError(err)
			respondAPIError(c, st, code, msg, nil)
			return
		}

		base, ok := loadPortfolioBaseState(c, readStore, log, priceStreamPartitions, "scenario")
		if !ok {
			return
		}
		pid := base.PortfolioID
		input := base.Input
		baseView := base.View

		shockedView, missingSymbols, err := scenario.Eval(input, shocks)
		if err != nil {
			log.Warn("scenario_eval_failed", zap.String("portfolio_id", pid.String()), zap.Error(err))
			st, code, msg := MapScenarioRunError(err)
			respondAPIError(c, st, code, msg, nil)
			return
		}
		if len(missingSymbols) > 0 {
			respondAPIError(c, http.StatusUnprocessableEntity, ErrCodeInsufficientData,
				"one or more shocked symbols do not have a base price mark",
				map[string]any{"symbols": missingSymbols})
			return
		}

		baseMV, _ := decimal.NewFromString(baseView.Totals.MarketValue)
		baseUPnL, _ := decimal.NewFromString(baseView.Totals.UnrealizedPnL)
		baseRPnL, _ := decimal.NewFromString(baseView.Totals.RealizedPnL)
		shockMV, _ := decimal.NewFromString(shockedView.Totals.MarketValue)
		shockUPnL, _ := decimal.NewFromString(shockedView.Totals.UnrealizedPnL)
		shockRPnL, _ := decimal.NewFromString(shockedView.Totals.RealizedPnL)

		resp := scenarioHTTPResponse{
			PortfolioID:  pid.String(),
			BaseMetadata: scenarioBaseMetadataFromView(baseView),
			Base:         baseView,
			Shocked:      shockedView,
			Delta: scenarioDeltaDTO{
				MarketValue:   shockMV.Sub(baseMV).String(),
				UnrealizedPnL: shockUPnL.Sub(baseUPnL).String(),
				RealizedPnL:   shockRPnL.Sub(baseRPnL).String(),
			},
			Shocks: make([]scenarioShock, 0, len(req.Shocks)),
		}
		resp.BaseAsOfEventID = baseView.AsOfEventID
		if baseView.AsOfEventTime != nil {
			t := baseView.AsOfEventTime.UTC().Format(time.RFC3339Nano)
			resp.BaseAsOfEventTime = &t
		}
		if baseView.AsOfProcessingTime != nil {
			t := baseView.AsOfProcessingTime.UTC().Format(time.RFC3339Nano)
			resp.BaseAsOfProcessingTime = &t
		}
		for _, sh := range req.Shocks {
			shockType := sh.Type
			if shockType == "" {
				shockType = sh.Kind
			}
			resp.Shocks = append(resp.Shocks, scenarioShock{
				Symbol: sh.Symbol,
				Type:   shockType,
				Value:  sh.Value.String(),
			})
		}

		c.JSON(http.StatusOK, resp)
	}
}
