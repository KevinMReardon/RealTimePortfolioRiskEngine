package scenario

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/KevinMReardon/realtime-portfolio-risk/internal/portfolio"
)

func TestClonePriceBySymbol_nilAndIsolation(t *testing.T) {
	t.Parallel()
	if m := ClonePriceBySymbol(nil); len(m) != 0 {
		t.Fatalf("nil clone: %v", m)
	}
	base := map[string]portfolio.PriceMarkInput{
		"AAPL": {Price: decimal.NewFromInt(100)},
	}
	cl := ClonePriceBySymbol(base)
	cl["AAPL"] = portfolio.PriceMarkInput{Price: decimal.NewFromInt(999)}
	if base["AAPL"].Price.IntPart() != 100 {
		t.Fatal("mutated base map")
	}
}

func TestValidateShocks(t *testing.T) {
	t.Parallel()
	if err := ValidateShocks(nil); !errors.Is(err, ErrEmptyShocks) {
		t.Fatalf("nil: %v", err)
	}
	if err := ValidateShocks([]Shock{}); !errors.Is(err, ErrEmptyShocks) {
		t.Fatalf("empty: %v", err)
	}
	if err := ValidateShocks([]Shock{{Symbol: "", Type: ShockTypePCT, Value: decimal.NewFromFloat(0.1)}}); !errors.Is(err, ErrEmptyShockSymbol) {
		t.Fatalf("empty symbol: %v", err)
	}
	if err := ValidateShocks([]Shock{{Symbol: "A", Type: "ABS", Value: decimal.Zero}}); !errors.Is(err, ErrInvalidShockType) {
		t.Fatalf("bad type: %v", err)
	}
	if err := ValidateShocks([]Shock{
		{Symbol: "A", Type: ShockTypePCT, Value: decimal.Zero},
		{Symbol: "A", Type: ShockTypePCT, Value: decimal.Zero},
	}); !errors.Is(err, ErrDuplicateShockSym) {
		t.Fatalf("dup: %v", err)
	}
	if err := ValidateShocks([]Shock{{Symbol: "A", Type: ShockTypePCT, Value: decimal.Zero}}); err != nil {
		t.Fatal(err)
	}
}

func TestShockedPortfolioInput_missingMark(t *testing.T) {
	t.Parallel()
	pid := uuid.New()
	base := portfolio.PortfolioAssemblerInput{
		PortfolioID: pid,
		Positions: []portfolio.ProjectionRow{
			{Symbol: "MSFT", Quantity: decimal.NewFromInt(1), AverageCost: decimal.NewFromInt(50), RealizedPnL: decimal.Zero},
		},
		PriceBySymbol: map[string]portfolio.PriceMarkInput{},
	}
	_, missing, err := ShockedPortfolioInput(base, []Shock{{Symbol: "MSFT", Type: ShockTypePCT, Value: decimal.NewFromFloat(0.1)}})
	if err != nil {
		t.Fatal(err)
	}
	if len(missing) != 1 || missing[0] != "MSFT" {
		t.Fatalf("missing=%v", missing)
	}
}

func TestEval_pctShockTotalsMatchPortfolioRules(t *testing.T) {
	t.Parallel()
	pid := uuid.New()
	t0 := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	base := portfolio.PortfolioAssemblerInput{
		PortfolioID: pid,
		Positions: []portfolio.ProjectionRow{
			{
				Symbol:      "AAPL",
				Quantity:    decimal.NewFromInt(10),
				AverageCost: decimal.NewFromInt(100),
				RealizedPnL: decimal.Zero,
			},
		},
		PriceBySymbol: map[string]portfolio.PriceMarkInput{
			"AAPL": {Price: decimal.NewFromInt(100), AsOfEventID: uuid.New(), AsOfEventTime: t0},
		},
	}
	shocked, missing, err := Eval(base, []Shock{{Symbol: "AAPL", Type: ShockTypePCT, Value: decimal.NewFromFloat(0.1)}})
	if err != nil || len(missing) != 0 {
		t.Fatalf("err=%v missing=%v", err, missing)
	}
	if shocked.Totals.MarketValue != "1100" {
		t.Fatalf("market_value=%s", shocked.Totals.MarketValue)
	}
	if shocked.Totals.UnrealizedPnL != "100" {
		t.Fatalf("unrealized=%s", shocked.Totals.UnrealizedPnL)
	}
	if shocked.Totals.RealizedPnL != "0" {
		t.Fatalf("realized=%s", shocked.Totals.RealizedPnL)
	}
	if len(shocked.Positions) != 1 || shocked.Positions[0].LastPrice != "110" {
		t.Fatalf("position=%+v", shocked.Positions[0])
	}
}
