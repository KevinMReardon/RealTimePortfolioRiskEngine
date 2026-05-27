package agent

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/KevinMReardon/realtime-portfolio-risk/internal/events"
	"github.com/KevinMReardon/realtime-portfolio-risk/internal/portfolio"
)

type stubWatchlist struct {
	symbols []string
}

func (s stubWatchlist) Watchlist() []string { return s.symbols }

func TestBootstrapBriefingContext_includesUnheldWatchlistSymbols(t *testing.T) {
	t.Parallel()
	pid := uuid.New()
	ds := &fakeToolDataSource{
		assemblerFound: true,
		assemblerInput: portfolio.PortfolioAssemblerInput{
			PortfolioID: pid,
			Positions: []portfolio.ProjectionRow{
				{Symbol: "AAPL", Quantity: decimal.NewFromInt(10), AverageCost: decimal.NewFromInt(100)},
			},
			PriceBySymbol: map[string]portfolio.PriceMarkInput{
				"AAPL": {Price: decimal.NewFromInt(200)},
			},
		},
		priceMarks: events.ListPriceMarksResult{
			Items: []events.PriceMarkListRow{
				{Symbol: "AAPL", Price: "200"},
				{Symbol: "NVDA", Price: "900"},
				{Symbol: "TSLA", Price: "250"},
			},
		},
	}
	d := NewToolDispatcher(ds, nil, nil, nil).WithWatchlist(stubWatchlist{symbols: []string{"AAPL", "NVDA", "TSLA"}})

	_, marketRaw, err := d.BootstrapBriefingContext(context.Background(), pid)
	if err != nil {
		t.Fatal(err)
	}
	var market briefingMarketBootstrap
	if err := json.Unmarshal(marketRaw, &market); err != nil {
		t.Fatal(err)
	}
	if len(market.Symbols) < 3 {
		t.Fatalf("symbols len=%d want >=3", len(market.Symbols))
	}
	unheld := 0
	for _, row := range market.Symbols {
		if !row.Held {
			unheld++
		}
	}
	if unheld < 2 {
		t.Fatalf("expected at least 2 unheld watchlist symbols, got %d", unheld)
	}
}
