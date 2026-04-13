package risk

import (
	"testing"

	"github.com/shopspring/decimal"
)

func TestBuildSnapshot_FormulaAndAssumptions(t *testing.T) {
	t.Parallel()

	engine := NewEngine()
	got, err := engine.BuildSnapshot(Input{
		Positions: []PositionInput{
			{Symbol: "AAPL", Quantity: decimal.NewFromInt(10)},
			{Symbol: "MSFT", Quantity: decimal.NewFromInt(5)},
		},
		Prices: map[string]decimal.Decimal{
			"AAPL": decimal.NewFromInt(100),
			"MSFT": decimal.NewFromInt(200),
		},
		Sigma1D: map[string]decimal.Decimal{
			"AAPL": decimal.RequireFromString("0.02"),
			"MSFT": decimal.RequireFromString("0.03"),
		},
		TopN: 2,
	})
	if err != nil {
		t.Fatalf("BuildSnapshot error: %v", err)
	}

	if !got.TotalMarketValue.Equal(decimal.NewFromInt(2000)) {
		t.Fatalf("total_market_value = %s want 2000", got.TotalMarketValue)
	}
	// Equal weights 0.5 each => sigma = sqrt(0.5^2*0.02^2 + 0.5^2*0.03^2) = 0.018027756...
	wantSigma := decimal.RequireFromString("0.01802775637731995")
	if got.Sigma1DPortfolio.Sub(wantSigma).Abs().GreaterThan(decimal.RequireFromString("0.0000000001")) {
		t.Fatalf("sigma_1d_portfolio = %s want approx %s", got.Sigma1DPortfolio, wantSigma)
	}
	// VaR = 2000 * sigma * 1.645 = 59.311318...
	wantVaR := decimal.RequireFromString("59.31131848138261905")
	if got.VaR95_1d.Sub(wantVaR).Abs().GreaterThan(decimal.RequireFromString("0.0000000001")) {
		t.Fatalf("var_95_1d = %s want approx %s", got.VaR95_1d, wantVaR)
	}

	if !got.Assumptions.Confidence.Equal(decimal.RequireFromString("0.95")) {
		t.Fatalf("assumptions.confidence = %s", got.Assumptions.Confidence)
	}
	if !got.Assumptions.HorizonDays.Equal(decimal.NewFromInt(1)) {
		t.Fatalf("assumptions.horizon_days = %s", got.Assumptions.HorizonDays)
	}
	if !got.Assumptions.ZScore.Equal(decimal.RequireFromString("1.645")) {
		t.Fatalf("assumptions.z_score = %s", got.Assumptions.ZScore)
	}
	if got.Assumptions.VolatilityConvention != "annual_to_daily_sqrt_252" {
		t.Fatalf("assumptions.volatility_convention = %q", got.Assumptions.VolatilityConvention)
	}
	if got.Assumptions.Model != "parametric_normal" {
		t.Fatalf("assumptions.model = %q", got.Assumptions.Model)
	}
}

func TestBuildSnapshot_ExcludesUnpricedSymbols(t *testing.T) {
	t.Parallel()

	engine := NewEngine()
	got, err := engine.BuildSnapshot(Input{
		Positions: []PositionInput{
			{Symbol: "AAPL", Quantity: decimal.NewFromInt(10)},
			{Symbol: "TSLA", Quantity: decimal.NewFromInt(1)},
		},
		Prices: map[string]decimal.Decimal{
			"AAPL": decimal.NewFromInt(100),
		},
		Sigma1D: map[string]decimal.Decimal{
			"AAPL": decimal.RequireFromString("0.02"),
		},
		TopN: 5,
	})
	if err != nil {
		t.Fatalf("BuildSnapshot error: %v", err)
	}

	if !got.TotalMarketValue.Equal(decimal.NewFromInt(1000)) {
		t.Fatalf("total_market_value = %s want 1000", got.TotalMarketValue)
	}
	if len(got.ExposureBySymbol) != 1 || got.ExposureBySymbol[0].Symbol != "AAPL" {
		t.Fatalf("exposure symbols = %+v want only AAPL", got.ExposureBySymbol)
	}
	if len(got.UnpricedSymbols) != 1 || got.UnpricedSymbols[0] != "TSLA" {
		t.Fatalf("unpriced_symbols = %+v want [TSLA]", got.UnpricedSymbols)
	}
}

