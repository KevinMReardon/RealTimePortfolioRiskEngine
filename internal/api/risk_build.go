package api

import (
	"context"
	"fmt"

	"github.com/shopspring/decimal"

	"github.com/KevinMReardon/realtime-portfolio-risk/internal/risk"
)

// ErrRiskUnpricedPositions is returned by BuildRiskHTTPResponse when an open position has no price mark.
type ErrRiskUnpricedPositions struct {
	Symbols []string
}

func (e *ErrRiskUnpricedPositions) Error() string {
	return "risk: unpriced open positions"
}

// ErrRiskInsufficientReturnHistory is returned when return samples are below MinDailyReturnsForVolatility.
type ErrRiskInsufficientReturnHistory struct {
	Symbols                 []string
	MinDailyReturnsRequired int
	SigmaWindowN            int
}

func (e *ErrRiskInsufficientReturnHistory) Error() string {
	return "risk: insufficient return history"
}

// BuildRiskHTTPResponse runs the same risk grounding and snapshot path as GET /v1/portfolios/:id/risk.
// On success it returns the HTTP DTO; grounding failures return *ErrRiskUnpricedPositions or
// *ErrRiskInsufficientReturnHistory; other errors wrap store/engine failures.
func BuildRiskHTTPResponse(ctx context.Context, store RiskReadStore, base portfolioBaseState, sigmaWindowN int) (RiskHTTPResponse, error) {
	if sigmaWindowN < 2 {
		sigmaWindowN = 60
	}
	pid := base.PortfolioID
	input := base.Input
	pview := base.View

	riskIn := risk.Input{
		Positions: make([]risk.PositionInput, 0, len(input.Positions)),
		Prices:    make(map[string]decimal.Decimal, len(input.PriceBySymbol)),
		Sigma1D:   make(map[string]decimal.Decimal),
	}
	var unpricedOpen []string
	for _, p := range input.Positions {
		if p.Quantity.IsZero() {
			continue
		}
		riskIn.Positions = append(riskIn.Positions, risk.PositionInput{
			Symbol:   p.Symbol,
			Quantity: p.Quantity,
		})
		mark, ok := input.PriceBySymbol[p.Symbol]
		if !ok || mark.Price.IsZero() {
			unpricedOpen = append(unpricedOpen, p.Symbol)
			continue
		}
		riskIn.Prices[p.Symbol] = mark.Price
	}

	if len(unpricedOpen) > 0 {
		return RiskHTTPResponse{}, &ErrRiskUnpricedPositions{Symbols: append([]string(nil), unpricedOpen...)}
	}

	pricedSyms := make([]string, 0, len(riskIn.Prices))
	for s := range riskIn.Prices {
		pricedSyms = append(pricedSyms, s)
	}

	if len(pricedSyms) > 0 {
		counts, err := store.LoadSymbolReturnSampleCounts(ctx, pricedSyms, sigmaWindowN)
		if err != nil {
			return RiskHTTPResponse{}, fmt.Errorf("risk return counts: %w", err)
		}
		var insufficient []string
		for _, sym := range pricedSyms {
			if counts[sym] < MinDailyReturnsForVolatility {
				insufficient = append(insufficient, sym)
			}
		}
		if len(insufficient) > 0 {
			return RiskHTTPResponse{}, &ErrRiskInsufficientReturnHistory{
				Symbols:                 append([]string(nil), insufficient...),
				MinDailyReturnsRequired: MinDailyReturnsForVolatility,
				SigmaWindowN:            sigmaWindowN,
			}
		}

		sigmas, err := store.LoadSymbolSigma1D(ctx, pricedSyms, sigmaWindowN)
		if err != nil {
			return RiskHTTPResponse{}, fmt.Errorf("risk sigma: %w", err)
		}
		for sym := range riskIn.Prices {
			if s, ok := sigmas[sym]; ok {
				riskIn.Sigma1D[sym] = s
			} else {
				riskIn.Sigma1D[sym] = decimal.Zero
			}
		}
	}

	snap, err := risk.NewEngine().BuildSnapshot(riskIn)
	if err != nil {
		return RiskHTTPResponse{}, fmt.Errorf("risk build: %w", err)
	}

	resp := RiskHTTPResponse{
		PortfolioID:     pid.String(),
		Exposure:        make([]riskExposureDTO, 0, len(snap.ExposureBySymbol)),
		Assumptions:     snap.Assumptions,
		Var95_1d:        snap.VaR95_1d.String(),
		Metadata:        riskMetadataDTO{SigmaWindowN: sigmaWindowN, MinDailyReturnsRequired: MinDailyReturnsForVolatility},
		DrivingEventIDs: pview.DrivingEventIDs,
		AsOfPositions:   pview.AsOfPositions,
		AsOfPrices:      pview.AsOfPrices,
		AsOfEventID:     pview.AsOfEventID,
	}
	resp.AsOfEventTime = pview.AsOfEventTime
	resp.AsOfProcessingTime = pview.AsOfProcessingTime

	for _, row := range snap.ExposureBySymbol {
		resp.Exposure = append(resp.Exposure, riskExposureDTO{
			Symbol:   row.Symbol,
			Exposure: row.Exposure.String(),
			Weight:   row.Weight.String(),
		})
	}
	resp.Concentration.HHI = snap.Concentration.HHI.String()
	for _, row := range snap.Concentration.TopN {
		resp.Concentration.TopN = append(resp.Concentration.TopN, riskExposureDTO{
			Symbol:   row.Symbol,
			Exposure: row.Exposure.String(),
			Weight:   row.Weight.String(),
		})
	}
	resp.Volatility.Sigma1DPortfolio = snap.Sigma1DPortfolio.String()
	for _, row := range snap.ExposureBySymbol {
		resp.Volatility.BySymbol = append(resp.Volatility.BySymbol, riskVolSymDTO{
			Symbol:  row.Symbol,
			Sigma1D: row.Sigma1D.String(),
		})
	}

	return resp, nil
}
