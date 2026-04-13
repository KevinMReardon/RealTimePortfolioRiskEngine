package portfolio

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

func testPortfolioID() uuid.UUID {
	return uuid.MustParse("550e8400-e29b-41d4-a716-446655440001")
}

func TestAssemblePortfolioView_duplicateSymbol(t *testing.T) {
	t.Parallel()
	_, err := AssemblePortfolioView(PortfolioAssemblerInput{
		PortfolioID: testPortfolioID(),
		Positions: []ProjectionRow{
			{Symbol: "AAPL", Quantity: decimal.NewFromInt(1)},
			{Symbol: "AAPL", Quantity: decimal.NewFromInt(2)},
		},
	})
	if err == nil {
		t.Fatal("want error on duplicate symbol")
	}
	if !errors.Is(err, ErrAssemblerDuplicatePositionSymbol) {
		t.Fatalf("want ErrAssemblerDuplicatePositionSymbol, got %v", err)
	}
}

func TestAssemblePortfolioView_nilPortfolioID(t *testing.T) {
	t.Parallel()
	_, err := AssemblePortfolioView(PortfolioAssemblerInput{
		PortfolioID: uuid.Nil,
		Positions:   []ProjectionRow{{Symbol: "X", Quantity: decimal.NewFromInt(1)}},
	})
	if err == nil {
		t.Fatal("want error on nil portfolio id")
	}
	if !errors.Is(err, ErrAssemblerPortfolioIDRequired) {
		t.Fatalf("want ErrAssemblerPortfolioIDRequired, got %v", err)
	}
}

func TestAssemblePortfolioView_pricedAndUnpriced(t *testing.T) {
	t.Parallel()
	tradeID := uuid.MustParse("10000000-0000-0000-0000-000000000001")
	priceID := uuid.MustParse("20000000-0000-0000-0000-000000000002")
	tTrade := time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC)
	tPrice := time.Date(2026, 4, 1, 12, 0, 1, 0, time.UTC)
	procTrade := tTrade.Add(time.Millisecond)
	procPrice := tPrice.Add(2 * time.Millisecond)

	got, err := AssemblePortfolioView(PortfolioAssemblerInput{
		PortfolioID: testPortfolioID(),
		Positions: []ProjectionRow{
			{Symbol: "MSFT", Quantity: decimal.NewFromInt(2), AverageCost: decimal.NewFromInt(100), RealizedPnL: decimal.Zero},
			{Symbol: "AAPL", Quantity: decimal.NewFromInt(10), AverageCost: decimal.NewFromInt(50), RealizedPnL: decimal.NewFromInt(5)},
		},
		PriceBySymbol: map[string]PriceMarkInput{
			"AAPL": {Price: decimal.NewFromInt(60), AsOfEventID: priceID, AsOfEventTime: tPrice, ProcessingTime: procPrice},
		},
		TradeApply: &ApplyCursorMeta{EventID: tradeID, EventTime: tTrade, ProcessingTime: procTrade},
	})
	if err != nil {
		t.Fatal(err)
	}

	if got.PortfolioID != testPortfolioID().String() {
		t.Fatalf("portfolio_id %q", got.PortfolioID)
	}
	if len(got.UnpricedSymbols) != 1 || got.UnpricedSymbols[0] != "MSFT" {
		t.Fatalf("unpriced_symbols %+v", got.UnpricedSymbols)
	}
	if len(got.Positions) != 2 {
		t.Fatalf("positions len %d", len(got.Positions))
	}
	aapl := got.Positions[0]
	if aapl.Symbol != "AAPL" || aapl.LastPrice != "60" || aapl.MarketValue == "" {
		t.Fatalf("AAPL position %+v", aapl)
	}
	msft := got.Positions[1]
	if msft.Symbol != "MSFT" || msft.LastPrice != "" || msft.MarketValue != "" {
		t.Fatalf("MSFT position %+v", msft)
	}

	if got.Totals.MarketValue != "600" || got.Totals.UnrealizedPnL != "100" || got.Totals.RealizedPnL != "5" {
		t.Fatalf("totals %+v", got.Totals)
	}

	if len(got.DrivingEventIDs) != 2 {
		t.Fatalf("driving_event_ids %+v", got.DrivingEventIDs)
	}
	if got.AsOfPositions == nil || got.AsOfPositions.EventID != tradeID {
		t.Fatalf("as_of_positions %+v", got.AsOfPositions)
	}
	if len(got.AsOfPrices) != 1 || got.AsOfPrices[0].Symbol != "AAPL" || got.AsOfPrices[0].EventID != priceID {
		t.Fatalf("as_of_prices %+v", got.AsOfPrices)
	}
	// Cross-partition: no single fused as_of tuple (LLD §14.1).
	if got.AsOfEventID != nil || got.AsOfEventTime != nil || got.AsOfProcessingTime != nil {
		t.Fatalf("expected omitted merged as_of fields, got id=%v time=%v proc=%v", got.AsOfEventID, got.AsOfEventTime, got.AsOfProcessingTime)
	}
}

func TestAssemblePortfolioView_flatLotWithRealized(t *testing.T) {
	t.Parallel()
	got, err := AssemblePortfolioView(PortfolioAssemblerInput{
		PortfolioID: testPortfolioID(),
		Positions: []ProjectionRow{
			{Symbol: "AAPL", Quantity: decimal.Zero, AverageCost: decimal.Zero, RealizedPnL: decimal.NewFromInt(20)},
		},
		PriceBySymbol: map[string]PriceMarkInput{
			"AAPL": {Price: decimal.NewFromInt(100)},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Positions) != 1 {
		t.Fatal(len(got.Positions))
	}
	p := got.Positions[0]
	if p.Quantity != "0" || p.RealizedPnL != "20" || p.MarketValue != "" {
		t.Fatalf("position %+v", p)
	}
	if got.Totals.RealizedPnL != "20" || got.Totals.MarketValue != "0" || got.Totals.UnrealizedPnL != "0" {
		t.Fatalf("totals %+v", got.Totals)
	}
	if len(got.UnpricedSymbols) != 0 {
		t.Fatalf("unpriced %+v", got.UnpricedSymbols)
	}
}

func TestAssemblePortfolioView_mergedAsOf_singlePartition_tradeOnly(t *testing.T) {
	t.Parallel()
	tradeID := uuid.MustParse("40000000-0000-0000-0000-000000000004")
	t0 := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)
	proc := t0.Add(500 * time.Millisecond)
	got, err := AssemblePortfolioView(PortfolioAssemblerInput{
		PortfolioID: testPortfolioID(),
		Positions: []ProjectionRow{
			{Symbol: "AAPL", Quantity: decimal.NewFromInt(1), AverageCost: decimal.NewFromInt(10), RealizedPnL: decimal.Zero},
		},
		PriceBySymbol: map[string]PriceMarkInput{
			"AAPL": {Price: decimal.NewFromInt(12), AsOfEventTime: t0}, // no event id → no price lineage
		},
		TradeApply: &ApplyCursorMeta{EventID: tradeID, EventTime: t0, ProcessingTime: proc},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.AsOfPositions == nil || got.AsOfPositions.EventID != tradeID {
		t.Fatalf("as_of_positions %+v", got.AsOfPositions)
	}
	if len(got.AsOfPrices) != 0 {
		t.Fatalf("as_of_prices %+v", got.AsOfPrices)
	}
	if got.AsOfEventID == nil || *got.AsOfEventID != tradeID {
		t.Fatalf("merged as_of_event_id %+v", got.AsOfEventID)
	}
	if got.AsOfEventTime == nil || !got.AsOfEventTime.Equal(t0.UTC()) {
		t.Fatalf("merged as_of_event_time %+v", got.AsOfEventTime)
	}
	if got.AsOfProcessingTime == nil || !got.AsOfProcessingTime.Equal(proc.UTC()) {
		t.Fatalf("merged as_of_processing_time %+v", got.AsOfProcessingTime)
	}
}

func TestAssemblePortfolioView_nilPriceMap(t *testing.T) {
	t.Parallel()
	got, err := AssemblePortfolioView(PortfolioAssemblerInput{
		PortfolioID: testPortfolioID(),
		Positions: []ProjectionRow{
			{Symbol: "AAPL", Quantity: decimal.NewFromInt(1), AverageCost: decimal.NewFromInt(10)},
		},
		PriceBySymbol: nil,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.UnpricedSymbols) != 1 {
		t.Fatalf("unpriced %+v", got.UnpricedSymbols)
	}
}

func TestAssemblePortfolioView_JSONShape(t *testing.T) {
	t.Parallel()
	tradeID := uuid.MustParse("30000000-0000-0000-0000-000000000003")
	priceID := uuid.MustParse("31000000-0000-0000-0000-000000000031")
	t0 := time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)
	t1 := t0.Add(time.Minute)
	got, err := AssemblePortfolioView(PortfolioAssemblerInput{
		PortfolioID: testPortfolioID(),
		Positions: []ProjectionRow{
			{Symbol: "AAPL", Quantity: decimal.NewFromInt(1), AverageCost: decimal.NewFromInt(10), RealizedPnL: decimal.Zero},
		},
		PriceBySymbol: map[string]PriceMarkInput{
			"AAPL": {Price: decimal.NewFromInt(12), AsOfEventID: priceID, AsOfEventTime: t1},
		},
		TradeApply: &ApplyCursorMeta{EventID: tradeID, EventTime: t0, ProcessingTime: t0.Add(time.Second)},
	})
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"portfolio_id", "positions", "unpriced_symbols", "totals", "driving_event_ids", "as_of_positions", "as_of_prices"} {
		if _, ok := raw[key]; !ok {
			t.Fatalf("JSON missing key %q in %s", key, string(b))
		}
	}
	for _, key := range []string{"as_of_event_id", "as_of_event_time", "as_of_processing_time"} {
		if _, ok := raw[key]; ok {
			t.Fatalf("JSON should omit cross-partition merged key %q in %s", key, string(b))
		}
	}
}

func TestMarkToMarket_SumTotals(t *testing.T) {
	t.Parallel()
	avg := decimal.NewFromInt(32).Div(decimal.NewFromInt(3))
	realized := decimal.NewFromInt(11).Sub(avg).Mul(decimal.NewFromInt(30))
	row := ProjectionRow{
		Symbol:      "AAPL",
		Quantity:    decimal.NewFromInt(120),
		AverageCost: avg,
		RealizedPnL: realized,
	}
	last := decimal.NewFromInt(13)
	m := MarkToMarket(row, last, true)
	wantMV := last.Mul(row.Quantity)
	wantUnreal := last.Sub(avg).Mul(row.Quantity)
	if !m.MarketValue.Equal(wantMV) {
		t.Fatalf("market value got %s want %s", m.MarketValue, wantMV)
	}
	if !m.UnrealizedPnL.Equal(wantUnreal) {
		t.Fatalf("unrealized got %s want %s", m.UnrealizedPnL, wantUnreal)
	}

	tot := SumTotals([]MarkToMarketRow{m})
	if !tot.TotalMarketValue.Equal(wantMV) {
		t.Fatalf("total MV %s", tot.TotalMarketValue)
	}
	if !tot.TotalUnrealizedPnL.Equal(wantUnreal) {
		t.Fatalf("total unreal %s", tot.TotalUnrealizedPnL)
	}
	if !tot.TotalRealizedPnL.Equal(realized) {
		t.Fatalf("total real %s", tot.TotalRealizedPnL)
	}
}

func TestMarkToMarket_unpricedOpenLot(t *testing.T) {
	t.Parallel()
	row := ProjectionRow{
		Symbol:      "X",
		Quantity:    decimal.NewFromInt(10),
		AverageCost: decimal.NewFromInt(5),
		RealizedPnL: decimal.Zero,
	}
	m := MarkToMarket(row, decimal.Zero, false)
	if !m.MarketValue.IsZero() || !m.UnrealizedPnL.IsZero() {
		t.Fatalf("unpriced open lot should have zero MV/unreal, got mv=%s unreal=%s", m.MarketValue, m.UnrealizedPnL)
	}
	tot := SumTotals([]MarkToMarketRow{m})
	if !tot.TotalMarketValue.IsZero() || !tot.TotalUnrealizedPnL.IsZero() {
		t.Fatalf("totals should exclude unpriced from MV/unreal")
	}
}
