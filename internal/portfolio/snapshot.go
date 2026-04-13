package portfolio

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/shopspring/decimal"

	"github.com/KevinMReardon/realtime-portfolio-risk/internal/domain"
)

// ErrUnsupportedPortfolioSnapshotFormat is returned when snapshot JSON format is unknown.
var ErrUnsupportedPortfolioSnapshotFormat = errors.New("unsupported portfolio snapshot format")

const tradePositionsSnapshotFormatV1 = "rtpre_trade_positions_v1"

type tradePositionsSnapshotV1 struct {
	Format    string                    `json:"format"`
	Positions []tradePositionSnapshotV1 `json:"positions"`
}

type tradePositionSnapshotV1 struct {
	Symbol      string `json:"symbol"`
	Quantity    string `json:"quantity"`
	AverageCost string `json:"average_cost"`
	RealizedPnL string `json:"realized_pnl"`
}

// MarshalTradePositionsSnapshotV1 builds LLD `rtpre_trade_positions_v1` JSON for one
// portfolio key in agg (sorted symbols, decimal strings fixed to 8 places).
func MarshalTradePositionsSnapshotV1(agg *Aggregate, portfolioID string) ([]byte, error) {
	if agg == nil {
		return nil, fmt.Errorf("aggregate required")
	}
	p := agg.byPortfolio[portfolioID]
	entries := domain.SortedSymbolLots(p)
	rows := make([]tradePositionSnapshotV1, 0, len(entries))
	for _, e := range entries {
		rows = append(rows, tradePositionSnapshotV1{
			Symbol:      e.Symbol,
			Quantity:    e.Lot.Quantity.StringFixed(8),
			AverageCost: e.Lot.AverageCost.StringFixed(8),
			RealizedPnL: e.Lot.RealizedPnL.StringFixed(8),
		})
	}
	out := tradePositionsSnapshotV1{
		Format:    tradePositionsSnapshotFormatV1,
		Positions: rows,
	}
	return json.Marshal(out)
}

// ParseTradePositionsSnapshotV1 decodes LLD `rtpre_trade_positions_v1` JSON into domain positions.
func ParseTradePositionsSnapshotV1(data []byte) (*domain.Positions, error) {
	if len(data) == 0 {
		return domain.NewPositions(), nil
	}
	var snap tradePositionsSnapshotV1
	if err := json.Unmarshal(data, &snap); err != nil {
		return nil, fmt.Errorf("snapshot json: %w", err)
	}
	if snap.Format != tradePositionsSnapshotFormatV1 {
		return nil, fmt.Errorf("%w: %q", ErrUnsupportedPortfolioSnapshotFormat, snap.Format)
	}
	lots := make(map[string]domain.PositionLot, len(snap.Positions))
	for _, row := range snap.Positions {
		if row.Symbol == "" {
			return nil, fmt.Errorf("%w: empty symbol", domain.ErrInvalidPayload)
		}
		qty, err := decimal.NewFromString(row.Quantity)
		if err != nil {
			return nil, fmt.Errorf("snapshot quantity %q: %w", row.Symbol, err)
		}
		avg, err := decimal.NewFromString(row.AverageCost)
		if err != nil {
			return nil, fmt.Errorf("snapshot average_cost %q: %w", row.Symbol, err)
		}
		real, err := decimal.NewFromString(row.RealizedPnL)
		if err != nil {
			return nil, fmt.Errorf("snapshot realized_pnl %q: %w", row.Symbol, err)
		}
		lots[row.Symbol] = domain.PositionLot{
			Quantity:    qty,
			AverageCost: avg,
			RealizedPnL: real,
		}
	}
	return domain.PositionsFromLots(lots), nil
}
