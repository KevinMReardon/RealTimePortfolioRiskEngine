package portfolio

import (
	"encoding/json"
	"fmt"

	"github.com/shopspring/decimal"

	"github.com/KevinMReardon/realtime-portfolio-risk/internal/domain"
)

// Aggregate holds in-memory position state per portfolio partition. All trade
// quantity changes go through ApplyEvent → domain.Positions.ApplyTrade.
type Aggregate struct {
	byPortfolio map[string]*domain.Positions
}

// NewAggregate returns an empty aggregate.
func NewAggregate() *Aggregate {
	return &Aggregate{byPortfolio: make(map[string]*domain.Positions)}
}

// Reset clears all position state (used before full replay).
func (a *Aggregate) Reset() {
	a.byPortfolio = make(map[string]*domain.Positions)
}

// SetPortfolioPositions replaces the in-memory snapshot for a partition (DB hydrate).
// Quantity returns the applied quantity for symbol after events applied in this aggregate.
func (a *Aggregate) Quantity(portfolioID, symbol string) decimal.Decimal {
	return a.positionsFor(portfolioID).Quantity(symbol)
}

// Lot returns §7 position state for symbol in the partition (zeros if none).
func (a *Aggregate) Lot(portfolioID, symbol string) domain.PositionLot {
	return a.positionsFor(portfolioID).Lot(symbol)
}

func (a *Aggregate) SetPortfolioPositions(portfolioID string, p *domain.Positions) {
	if p == nil {
		delete(a.byPortfolio, portfolioID)
		return
	}
	a.byPortfolio[portfolioID] = p
}

func (a *Aggregate) positionsFor(portfolioID string) *domain.Positions {
	p, ok := a.byPortfolio[portfolioID]
	if !ok {
		p = domain.NewPositions()
		a.byPortfolio[portfolioID] = p
	}
	return p
}

// ApplyEvent applies one canonical event to the given portfolio partition.
// PriceUpdated does not change quantities (pricing/projections handled separately later).
func (a *Aggregate) ApplyEvent(portfolioID string, ev domain.EventEnvelope) error {
	switch ev.EventType {
	case domain.EventTypeTradeExecuted:
		var payload domain.TradePayload
		if err := json.Unmarshal(ev.Payload, &payload); err != nil {
			return fmt.Errorf("%w: trade payload: %v", domain.ErrInvalidPayload, err)
		}
		return a.positionsFor(portfolioID).ApplyTrade(payload)
	case domain.EventTypePriceUpdated:
		return nil
	default:
		return nil
	}
}
