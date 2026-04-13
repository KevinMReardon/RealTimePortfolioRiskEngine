package portfolio

import (
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// Sentinel errors from AssemblePortfolioView for HTTP mapping.
var (
	ErrAssemblerPortfolioIDRequired     = errors.New("portfolio: portfolio_id is required")
	ErrAssemblerDuplicatePositionSymbol = errors.New("portfolio: duplicate position symbol")
)

// --- API DTOs and assembler input (§10.3) --- //
//
// PortfolioView is the §10.3 GET /v1/portfolios/{id} response body (JSON field names).
type PortfolioView struct {
	PortfolioID     string              `json:"portfolio_id"`
	Positions       []PortfolioPosition `json:"positions"`
	UnpricedSymbols []string            `json:"unpriced_symbols"`
	Totals          PortfolioTotalsView `json:"totals"`
	DrivingEventIDs []uuid.UUID         `json:"driving_event_ids,omitempty"`
	// AsOfPositions is the trade-partition projection_cursor (positions), if known.
	AsOfPositions *ReadAsOfRef `json:"as_of_positions,omitempty"`
	// AsOfPrices lists each price mark’s driving event actually read (per symbol). Empty slice omits in JSON.
	AsOfPrices []PriceMarkAsOf `json:"as_of_prices,omitempty"`
	// AsOfEventID / AsOfEventTime / AsOfProcessingTime are set only when a single partition supplies
	// lineage (no cross-projection “max” tuple that could imply one atomic snapshot — see LLD §14.1).
	AsOfEventID        *uuid.UUID `json:"as_of_event_id,omitempty"`
	AsOfEventTime      *time.Time `json:"as_of_event_time,omitempty"`
	AsOfProcessingTime *time.Time `json:"as_of_processing_time,omitempty"`
}

// ReadAsOfRef is lineage for one projection partition read (event id/time + optional ingest time).
type ReadAsOfRef struct {
	EventID        uuid.UUID  `json:"event_id"`
	EventTime      time.Time  `json:"event_time"`
	ProcessingTime *time.Time `json:"processing_time,omitempty"`
}

// PriceMarkAsOf is per-symbol price projection lineage (same read as prices_projection row).
type PriceMarkAsOf struct {
	Symbol         string     `json:"symbol"`
	EventID        uuid.UUID  `json:"event_id"`
	EventTime      time.Time  `json:"event_time"`
	ProcessingTime *time.Time `json:"processing_time,omitempty"`
}

// PortfolioPosition is one row in the positions list (§10.3 + §7-derived marks).
type PortfolioPosition struct {
	Symbol        string `json:"symbol"`
	Quantity      string `json:"quantity"`
	AverageCost   string `json:"average_cost"`
	RealizedPnL   string `json:"realized_pnl"`
	LastPrice     string `json:"last_price,omitempty"`
	MarketValue   string `json:"market_value,omitempty"`
	UnrealizedPnL string `json:"unrealized_pnl,omitempty"`
}

// PortfolioTotalsView is §10.3 totals block.
type PortfolioTotalsView struct {
	MarketValue   string `json:"market_value"`
	RealizedPnL   string `json:"realized_pnl"`
	UnrealizedPnL string `json:"unrealized_pnl"`
}

// ApplyCursorMeta is optional lineage from the trade portfolio projection_cursor and event envelope.
type ApplyCursorMeta struct {
	EventID        uuid.UUID
	EventTime      time.Time
	ProcessingTime time.Time
}

// PriceMarkInput is one symbol’s last mark from prices_projection (plus optional event lineage).
type PriceMarkInput struct {
	Price          decimal.Decimal
	AsOfEventID    uuid.UUID
	AsOfEventTime  time.Time
	ProcessingTime time.Time
}

// PortfolioAssemblerInput is the pure read-model input: projection rows, price marks, optional cursors.
type PortfolioAssemblerInput struct {
	PortfolioID   uuid.UUID
	Positions     []ProjectionRow
	PriceBySymbol map[string]PriceMarkInput
	TradeApply    *ApplyCursorMeta
}

// --- Projection + mark-to-market (§7) --- //

// ProjectionRow is one row from positions_projection. cost_basis stores weighted average cost (§7).
type ProjectionRow struct {
	Symbol      string
	Quantity    decimal.Decimal
	AverageCost decimal.Decimal
	RealizedPnL decimal.Decimal
}

// MarkToMarketRow is §7 per-symbol state with optional last price from prices_projection.
type MarkToMarketRow struct {
	ProjectionRow
	LastPrice     decimal.Decimal
	HasPrice      bool
	MarketValue   decimal.Decimal
	UnrealizedPnL decimal.Decimal
}

// MarkToMarket applies §7 price rules: last_price, unrealized_pnl, market_value when a mark exists
// and quantity > 0. Otherwise market value and unrealized are zero (unpriced or flat lot).
func MarkToMarket(row ProjectionRow, lastPrice decimal.Decimal, hasPrice bool) MarkToMarketRow {
	out := MarkToMarketRow{
		ProjectionRow: row,
		LastPrice:     lastPrice,
		HasPrice:      hasPrice,
	}
	if !hasPrice || row.Quantity.IsZero() {
		return out
	}
	out.MarketValue = lastPrice.Mul(row.Quantity)
	out.UnrealizedPnL = lastPrice.Sub(row.AverageCost).Mul(row.Quantity)
	return out
}

// PortfolioTotals is §7 portfolio-level aggregates.
type PortfolioTotals struct {
	TotalMarketValue   decimal.Decimal
	TotalUnrealizedPnL decimal.Decimal
	TotalRealizedPnL   decimal.Decimal
}

// SumTotals sums realized across all projection rows; market value and unrealized only for
// priced symbols with open quantity (§7 missing-price behavior).
func SumTotals(rows []MarkToMarketRow) PortfolioTotals {
	var t PortfolioTotals
	for _, r := range rows {
		t.TotalRealizedPnL = t.TotalRealizedPnL.Add(r.RealizedPnL)
		if r.HasPrice && !r.Quantity.IsZero() {
			t.TotalMarketValue = t.TotalMarketValue.Add(r.MarketValue)
			t.TotalUnrealizedPnL = t.TotalUnrealizedPnL.Add(r.UnrealizedPnL)
		}
	}
	return t
}

// AssemblePortfolioView builds the §10.3 portfolio DTO from projection rows and price marks.
// It is pure logic (no DB, no HTTP). Duplicate symbols in Positions returns an error.
//
// Lineage: DrivingEventIDs lists event ids from the trade cursor and each priced symbol’s mark.
// AsOfPositions / AsOfPrices mirror what was actually joined from the DB. Top-level AsOfEvent* is
// omitted when both trade and price partitions contributed ids (honest §14.1 — no fused snapshot tuple).
func AssemblePortfolioView(in PortfolioAssemblerInput) (PortfolioView, error) {
	if in.PortfolioID == uuid.Nil {
		return PortfolioView{}, ErrAssemblerPortfolioIDRequired
	}
	seen := make(map[string]struct{}, len(in.Positions))
	for _, p := range in.Positions {
		if _, dup := seen[p.Symbol]; dup {
			return PortfolioView{}, fmt.Errorf("%w: %q", ErrAssemblerDuplicatePositionSymbol, p.Symbol)
		}
		seen[p.Symbol] = struct{}{}
	}

	if in.PriceBySymbol == nil {
		in.PriceBySymbol = map[string]PriceMarkInput{}
	}

	positions := append([]ProjectionRow(nil), in.Positions...)
	sort.Slice(positions, func(i, j int) bool { return positions[i].Symbol < positions[j].Symbol })

	var mtm []MarkToMarketRow
	unpriced := make([]string, 0)
	outPos := make([]PortfolioPosition, 0, len(positions))

	for _, row := range positions {
		pm, ok := in.PriceBySymbol[row.Symbol]
		hasPrice := ok && !pm.Price.IsZero()
		mr := MarkToMarket(row, pm.Price, hasPrice)
		mtm = append(mtm, mr)

		pp := PortfolioPosition{
			Symbol:      row.Symbol,
			Quantity:    row.Quantity.String(),
			AverageCost: row.AverageCost.String(),
			RealizedPnL: row.RealizedPnL.String(),
		}
		if hasPrice {
			pp.LastPrice = pm.Price.String()
		}
		if mr.HasPrice && !row.Quantity.IsZero() {
			pp.MarketValue = mr.MarketValue.String()
			pp.UnrealizedPnL = mr.UnrealizedPnL.String()
		}
		outPos = append(outPos, pp)

		if !row.Quantity.IsZero() && !hasPrice {
			unpriced = append(unpriced, row.Symbol)
		}
	}

	tot := SumTotals(mtm)
	driving := collectDrivingEventIDs(in.TradeApply, in.PriceBySymbol)

	var asOfPos *ReadAsOfRef
	if in.TradeApply != nil && in.TradeApply.EventID != uuid.Nil {
		ref := ReadAsOfRef{EventID: in.TradeApply.EventID, EventTime: in.TradeApply.EventTime.UTC()}
		if !in.TradeApply.ProcessingTime.IsZero() {
			t := in.TradeApply.ProcessingTime.UTC()
			ref.ProcessingTime = &t
		}
		asOfPos = &ref
	}

	priceSyms := make([]string, 0, len(in.PriceBySymbol))
	for s := range in.PriceBySymbol {
		priceSyms = append(priceSyms, s)
	}
	sort.Strings(priceSyms)
	var asOfPrices []PriceMarkAsOf
	for _, s := range priceSyms {
		pm := in.PriceBySymbol[s]
		if pm.AsOfEventID == uuid.Nil {
			continue
		}
		item := PriceMarkAsOf{Symbol: s, EventID: pm.AsOfEventID, EventTime: pm.AsOfEventTime.UTC()}
		if !pm.ProcessingTime.IsZero() {
			t := pm.ProcessingTime.UTC()
			item.ProcessingTime = &t
		}
		asOfPrices = append(asOfPrices, item)
	}
	if len(asOfPrices) == 0 {
		asOfPrices = nil
	}

	hasPos := asOfPos != nil
	hasPriceMeta := len(asOfPrices) > 0
	var asOfIDPtr *uuid.UUID
	var asOfTimePtr *time.Time
	var procPtr *time.Time
	if hasPos && hasPriceMeta {
		// Cross-partition read: do not emit a single fused as_of tuple.
	} else if hasPos {
		id := asOfPos.EventID
		asOfIDPtr = &id
		t := asOfPos.EventTime
		asOfTimePtr = &t
		procPtr = asOfPos.ProcessingTime
	} else if hasPriceMeta {
		asOfID, asOfTime := mergeAsOfCursor(nil, in.PriceBySymbol)
		if asOfID != uuid.Nil {
			id := asOfID
			asOfIDPtr = &id
		}
		if !asOfTime.IsZero() {
			t := asOfTime.UTC()
			asOfTimePtr = &t
		}
		procPtr = mergeLatestProcessingTime(nil, in.PriceBySymbol)
	}

	return PortfolioView{
		PortfolioID:        in.PortfolioID.String(),
		Positions:          outPos,
		UnpricedSymbols:    unpriced,
		Totals:             PortfolioTotalsView{MarketValue: tot.TotalMarketValue.String(), RealizedPnL: tot.TotalRealizedPnL.String(), UnrealizedPnL: tot.TotalUnrealizedPnL.String()},
		DrivingEventIDs:    driving,
		AsOfPositions:      asOfPos,
		AsOfPrices:         asOfPrices,
		AsOfEventID:        asOfIDPtr,
		AsOfEventTime:      asOfTimePtr,
		AsOfProcessingTime: procPtr,
	}, nil
}

func applyMetaLess(a, b ApplyCursorMeta) bool {
	if !a.EventTime.Equal(b.EventTime) {
		return a.EventTime.Before(b.EventTime)
	}
	return a.EventID.String() < b.EventID.String()
}

func mergeAsOfCursor(trade *ApplyCursorMeta, prices map[string]PriceMarkInput) (uuid.UUID, time.Time) {
	var best ApplyCursorMeta
	var has bool
	if trade != nil && trade.EventID != uuid.Nil {
		best = *trade
		has = true
	}
	for _, p := range prices {
		if p.AsOfEventID == uuid.Nil {
			continue
		}
		cand := ApplyCursorMeta{EventID: p.AsOfEventID, EventTime: p.AsOfEventTime, ProcessingTime: p.ProcessingTime}
		if !has {
			best, has = cand, true
			continue
		}
		if applyMetaLess(best, cand) {
			best = cand
		}
	}
	if !has {
		return uuid.Nil, time.Time{}
	}
	return best.EventID, best.EventTime
}

func collectDrivingEventIDs(trade *ApplyCursorMeta, prices map[string]PriceMarkInput) []uuid.UUID {
	set := make(map[uuid.UUID]struct{})
	if trade != nil && trade.EventID != uuid.Nil {
		set[trade.EventID] = struct{}{}
	}
	for _, p := range prices {
		if p.AsOfEventID != uuid.Nil {
			set[p.AsOfEventID] = struct{}{}
		}
	}
	out := make([]uuid.UUID, 0, len(set))
	for id := range set {
		out = append(out, id)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].String() < out[j].String() })
	return out
}

func mergeLatestProcessingTime(trade *ApplyCursorMeta, prices map[string]PriceMarkInput) *time.Time {
	var latest time.Time
	var has bool
	if trade != nil && !trade.ProcessingTime.IsZero() {
		latest, has = trade.ProcessingTime.UTC(), true
	}
	for _, p := range prices {
		if p.ProcessingTime.IsZero() {
			continue
		}
		pt := p.ProcessingTime.UTC()
		if !has || pt.After(latest) {
			latest, has = pt, true
		}
	}
	if !has {
		return nil
	}
	return &latest
}
