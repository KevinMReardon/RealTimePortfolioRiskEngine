package policy

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/KevinMReardon/realtime-portfolio-risk/internal/portfolio"
)

func TestBuildSnapshot_IncludesMarksForNonPositionSymbols(t *testing.T) {
	t.Parallel()
	in := portfolio.PortfolioAssemblerInput{
		PortfolioID: uuid.New(),
		Positions: []portfolio.ProjectionRow{
			{
				Symbol:   "AMZN",
				Quantity: decimal.RequireFromString("10"),
			},
		},
		PriceBySymbol: map[string]portfolio.PriceMarkInput{
			"AMZN": {Price: decimal.RequireFromString("100")},
			"AMD":  {Price: decimal.RequireFromString("50")},
		},
	}

	snap := BuildSnapshot(in, decimal.Zero, time.Now(), false, false)
	if _, ok := snap.MarkPriceBySymbol["AMD"]; !ok {
		t.Fatal("expected mark for non-position symbol AMD to be present")
	}
}

func TestBuildSnapshot_PortfolioEquityUsesHeldPositionsOnly(t *testing.T) {
	t.Parallel()
	in := portfolio.PortfolioAssemblerInput{
		PortfolioID: uuid.New(),
		Positions: []portfolio.ProjectionRow{
			{
				Symbol:   "AMZN",
				Quantity: decimal.RequireFromString("2"),
			},
		},
		PriceBySymbol: map[string]portfolio.PriceMarkInput{
			"AMZN": {Price: decimal.RequireFromString("100")},
			"AMD":  {Price: decimal.RequireFromString("50")},
		},
	}

	snap := BuildSnapshot(in, decimal.Zero, time.Now(), false, false)
	want := decimal.RequireFromString("200")
	if !snap.PortfolioEquity.Equal(want) {
		t.Fatalf("portfolio equity=%s want=%s", snap.PortfolioEquity.String(), want.String())
	}
}
