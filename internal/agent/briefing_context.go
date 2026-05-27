package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/KevinMReardon/realtime-portfolio-risk/internal/events"
	"github.com/KevinMReardon/realtime-portfolio-risk/internal/portfolio"
)

const briefingMarketSnapshotLimit = 200

// BriefingBootstrapper supplies preloaded portfolio + watchlist market data for the first model turn.
type BriefingBootstrapper interface {
	BootstrapBriefingContext(ctx context.Context, portfolioID uuid.UUID) (portfolioCtx, marketCtx json.RawMessage, err error)
}

type briefingPositionRow struct {
	Symbol      string `json:"symbol"`
	Quantity    string `json:"quantity"`
	AverageCost string `json:"average_cost,omitempty"`
	MarketValue string `json:"market_value,omitempty"`
}

type briefingPortfolioBootstrap struct {
	PortfolioID string                `json:"portfolio_id"`
	Positions   []briefingPositionRow `json:"positions"`
}

type briefingMarketRow struct {
	Symbol      string  `json:"symbol"`
	Price       string  `json:"price,omitempty"`
	ChangePct   *string `json:"change_pct,omitempty"`
	Held        bool    `json:"held"`
	Quantity    string  `json:"quantity,omitempty"`
	PriceStatus string  `json:"price_status"` // ok | missing
}

type briefingMarketBootstrap struct {
	WatchlistCount   int                   `json:"watchlist_count"`
	PricedCount      int                   `json:"priced_count"`
	HoldingsCount    int                   `json:"holdings_count"`
	ScanInstruction  string                `json:"scan_instruction"`
	Symbols          []briefingMarketRow   `json:"symbols"`
}

// BootstrapBriefingContext loads holdings and a watchlist-wide price snapshot for the opening prompt.
func (d *ToolDispatcher) BootstrapBriefingContext(ctx context.Context, portfolioID uuid.UUID) (json.RawMessage, json.RawMessage, error) {
	if d == nil {
		return nil, nil, fmt.Errorf("tool dispatcher: nil receiver")
	}
	portfolioCtx, err := d.buildPortfolioBootstrap(ctx, portfolioID)
	if err != nil {
		return nil, nil, err
	}
	marketCtx, err := d.buildMarketBootstrap(ctx, portfolioID)
	if err != nil {
		return portfolioCtx, nil, err
	}
	return portfolioCtx, marketCtx, nil
}

func (d *ToolDispatcher) buildPortfolioBootstrap(ctx context.Context, portfolioID uuid.UUID) (json.RawMessage, error) {
	out := briefingPortfolioBootstrap{
		PortfolioID: portfolioID.String(),
		Positions:   []briefingPositionRow{},
	}
	if d.dataSource == nil {
		b, err := json.Marshal(out)
		return b, err
	}
	in, found, err := d.dataSource.LoadPortfolioAssemblerInput(ctx, portfolioID)
	if err != nil {
		return nil, err
	}
	if !found {
		b, err := json.Marshal(out)
		return b, err
	}
	view, err := portfolio.AssemblePortfolioView(in)
	if err != nil {
		return nil, err
	}
	for _, p := range view.Positions {
		qty, err := decimal.NewFromString(strings.TrimSpace(p.Quantity))
		if err != nil || qty.IsZero() {
			continue
		}
		row := briefingPositionRow{
			Symbol:   strings.ToUpper(strings.TrimSpace(p.Symbol)),
			Quantity: p.Quantity,
		}
		if strings.TrimSpace(p.AverageCost) != "" {
			row.AverageCost = p.AverageCost
		}
		if strings.TrimSpace(p.MarketValue) != "" {
			row.MarketValue = p.MarketValue
		}
		out.Positions = append(out.Positions, row)
	}
	return json.Marshal(out)
}

func (d *ToolDispatcher) buildMarketBootstrap(ctx context.Context, portfolioID uuid.UUID) (json.RawMessage, error) {
	watchlist := []string{}
	if d.watchlistReader != nil {
		watchlist = d.watchlistReader.Watchlist()
	}
	held := map[string]decimal.Decimal{}
	if d.dataSource != nil {
		if in, found, err := d.dataSource.LoadPortfolioAssemblerInput(ctx, portfolioID); err == nil && found {
			for _, p := range in.Positions {
				if p.Quantity.IsZero() {
					continue
				}
				sym := strings.ToUpper(strings.TrimSpace(p.Symbol))
				held[sym] = p.Quantity
			}
		}
	}
	priceBySymbol := map[string]events.PriceMarkListRow{}
	if d.dataSource != nil {
		marks, err := d.dataSource.ListPriceMarks(ctx, events.ListPriceMarksParams{
			Limit: briefingMarketSnapshotLimit,
			Sort:  "symbol",
			Order: "asc",
		})
		if err != nil {
			return nil, err
		}
		for _, row := range marks.Items {
			priceBySymbol[strings.ToUpper(strings.TrimSpace(row.Symbol))] = row
		}
	}
	seen := map[string]struct{}{}
	rows := make([]briefingMarketRow, 0, len(watchlist))
	for _, sym := range watchlist {
		sym = strings.ToUpper(strings.TrimSpace(sym))
		if sym == "" {
			continue
		}
		if _, ok := seen[sym]; ok {
			continue
		}
		seen[sym] = struct{}{}
		rows = append(rows, d.marketRowForSymbol(sym, held, priceBySymbol))
	}
	// Include held symbols missing from watchlist so exits are visible.
	for sym, qty := range held {
		if _, ok := seen[sym]; ok {
			continue
		}
		seen[sym] = struct{}{}
		rows = append(rows, d.marketRowForSymbol(sym, held, priceBySymbol))
		if qty.IsZero() {
			_ = qty
		}
	}
	priced := 0
	for _, r := range rows {
		if r.PriceStatus == "ok" {
			priced++
		}
	}
	out := briefingMarketBootstrap{
		WatchlistCount:  len(watchlist),
		PricedCount:     priced,
		HoldingsCount:   len(held),
		ScanInstruction: "Evaluate every symbol in symbols[] for trade ideas. You MUST include actionable ideas for symbols not currently held when momentum and risk support a new position. Do not limit trade_ideas to held symbols only.",
		Symbols:         rows,
	}
	return json.Marshal(out)
}

func (d *ToolDispatcher) marketRowForSymbol(sym string, held map[string]decimal.Decimal, prices map[string]events.PriceMarkListRow) briefingMarketRow {
	row := briefingMarketRow{Symbol: sym, PriceStatus: "missing"}
	if qty, ok := held[sym]; ok && !qty.IsZero() {
		row.Held = true
		row.Quantity = qty.String()
	}
	if mark, ok := prices[sym]; ok {
		row.Price = mark.Price
		row.ChangePct = mark.ChangePct
		row.PriceStatus = "ok"
	}
	return row
}

// MarketSnapshotJSON is shared by bootstrap and the get_watchlist_market_snapshot tool.
func (d *ToolDispatcher) MarketSnapshotJSON(ctx context.Context, portfolioID uuid.UUID) (json.RawMessage, error) {
	return d.buildMarketBootstrap(ctx, portfolioID)
}
