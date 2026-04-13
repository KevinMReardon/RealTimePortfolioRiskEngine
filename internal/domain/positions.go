package domain

import (
	"fmt"

	"github.com/shopspring/decimal"
)

// PositionLot is §7 state for one symbol in a portfolio: quantity, weighted average cost,
// and cumulative realized PnL. A flat position with non-zero realized remains in memory/DB
// until the next trade or until realized returns to zero (row removed).
type PositionLot struct {
	Quantity    decimal.Decimal
	AverageCost decimal.Decimal
	RealizedPnL decimal.Decimal
}

// Positions holds long-only position lots per symbol. Mutations go through ApplyTrade.
type Positions struct {
	bySymbol map[string]PositionLot
}

// NewPositions returns empty position state.
func NewPositions() *Positions {
	return &Positions{bySymbol: make(map[string]PositionLot)}
}

// PositionsFromQuantities builds lots with quantity only (average cost and realized zero).
// Prefer PositionsFromLots when hydrating from positions_projection.
func PositionsFromQuantities(qtyBySymbol map[string]decimal.Decimal) *Positions {
	p := &Positions{bySymbol: make(map[string]PositionLot)}
	for k, v := range qtyBySymbol {
		p.bySymbol[k] = PositionLot{Quantity: v}
	}
	return p
}

// PositionsFromLots hydrates from persisted projection rows (including flat lots with realized).
func PositionsFromLots(lots map[string]PositionLot) *Positions {
	p := &Positions{bySymbol: make(map[string]PositionLot)}
	for k, v := range lots {
		p.bySymbol[k] = v
	}
	return p
}

// Quantity returns held quantity for symbol (0 if none).
func (p *Positions) Quantity(symbol string) decimal.Decimal {
	return p.bySymbol[symbol].Quantity
}

// Lot returns §7 fields for symbol; missing symbol is zero-valued.
func (p *Positions) Lot(symbol string) PositionLot {
	return p.bySymbol[symbol]
}

// ApplyTrade applies TradeExecuted per LLD §7 (weighted average cost, realized on sells).
// SELL cannot reduce quantity below zero (ErrPositionUnderflow).
func (p *Positions) ApplyTrade(t TradePayload) error {
	switch t.Side {
	case SideBuy:
		cur := p.bySymbol[t.Symbol]
		oldQty := cur.Quantity
		oldAvg := cur.AverageCost
		oldReal := cur.RealizedPnL
		newQty := oldQty.Add(t.Quantity)
		numer := oldQty.Mul(oldAvg).Add(t.Quantity.Mul(t.Price))
		newAvg := numer.Div(newQty)
		p.bySymbol[t.Symbol] = PositionLot{
			Quantity:    newQty,
			AverageCost: newAvg,
			RealizedPnL: oldReal,
		}
		return nil
	case SideSell:
		cur := p.bySymbol[t.Symbol]
		if cur.Quantity.LessThan(t.Quantity) {
			return ErrPositionUnderflow
		}
		pnl := t.Price.Sub(cur.AverageCost).Mul(t.Quantity)
		newReal := cur.RealizedPnL.Add(pnl)
		newQty := cur.Quantity.Sub(t.Quantity)
		if newQty.IsZero() {
			if newReal.IsZero() {
				delete(p.bySymbol, t.Symbol)
			} else {
				p.bySymbol[t.Symbol] = PositionLot{
					Quantity:    decimal.Zero,
					AverageCost: decimal.Zero,
					RealizedPnL: newReal,
				}
			}
			return nil
		}
		p.bySymbol[t.Symbol] = PositionLot{
			Quantity:    newQty,
			AverageCost: cur.AverageCost,
			RealizedPnL: newReal,
		}
		return nil
	default:
		return fmt.Errorf("%w: invalid side", ErrValidation)
	}
}
