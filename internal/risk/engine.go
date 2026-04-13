package risk

import (
	"fmt"
	"math"
	"sort"

	"github.com/shopspring/decimal"
)

const (
	// ZScore95OneTailed is the LLD v1 z-score constant for 95% 1-day parametric VaR.
	ZScore95OneTailed = 1.645
)

var (
	confidence95      = decimal.RequireFromString("0.95")
	horizonDays1      = decimal.NewFromInt(1)
	zScore95          = decimal.RequireFromString("1.645")
	zero              = decimal.Zero
	defaultTopN       = 5
	maxTopN           = 50
	sqrtDomainEpsilon = 1e-12
)

// PositionInput contains a position quantity by symbol.
type PositionInput struct {
	Symbol   string
	Quantity decimal.Decimal
}

// Input is the pure internal/risk calculator contract.
// Prices and Sigma1D are keyed by symbol.
type Input struct {
	Positions []PositionInput
	Prices    map[string]decimal.Decimal
	Sigma1D   map[string]decimal.Decimal
	TopN      int
}

// Assumptions is the fixed LLD §8.4 assumptions payload.
type Assumptions struct {
	Confidence           decimal.Decimal `json:"confidence"`
	HorizonDays          decimal.Decimal `json:"horizon_days"`
	ZScore               decimal.Decimal `json:"z_score"`
	VolatilityConvention string          `json:"volatility_convention"`
	Model                string          `json:"model"`
}

// SymbolRisk stores per-symbol exposure/weight/sigma outputs.
type SymbolRisk struct {
	Symbol   string          `json:"symbol"`
	Exposure decimal.Decimal `json:"exposure"`
	Weight   decimal.Decimal `json:"weight"`
	Sigma1D  decimal.Decimal `json:"sigma_1d"`
}

// Concentration contains optional concentration metrics per LLD §8.2.
type Concentration struct {
	TopN []SymbolRisk    `json:"top_n"`
	HHI  decimal.Decimal `json:"hhi"`
}

// Snapshot is the pure output of risk math; no HTTP/worker concerns.
type Snapshot struct {
	ExposureBySymbol []SymbolRisk    `json:"exposure_by_symbol"`
	TotalMarketValue decimal.Decimal `json:"total_market_value"`
	Sigma1DPortfolio decimal.Decimal `json:"sigma_1d_portfolio"`
	VaR95_1d         decimal.Decimal `json:"var_95_1d"`
	Concentration    Concentration   `json:"concentration"`
	Assumptions      Assumptions     `json:"assumptions"`
	UnpricedSymbols  []string        `json:"unpriced_symbols"`
}

// Engine computes pure risk snapshots from already-materialized inputs.
type Engine struct{}

// NewEngine constructs a pure risk engine.
func NewEngine() Engine {
	return Engine{}
}

// BuildSnapshot computes v1 exposure/weights/concentration, portfolio sigma, and VaR.
func (Engine) BuildSnapshot(in Input) (Snapshot, error) {
	exposureBySymbol := make([]SymbolRisk, 0, len(in.Positions))
	unpriced := make([]string, 0)
	topN := normalizeTopN(in.TopN)
	totalMarketValue := zero

	for _, pos := range in.Positions {
		price, ok := in.Prices[pos.Symbol]
		if !ok {
			unpriced = append(unpriced, pos.Symbol)
			continue
		}
		sigma, ok := in.Sigma1D[pos.Symbol]
		if !ok {
			return Snapshot{}, fmt.Errorf("missing sigma_1d for priced symbol %q", pos.Symbol)
		}
		// LLD §8.2 allows abs(quantity * last_price); we lock to absolute exposure in v1.
		exposure := pos.Quantity.Mul(price).Abs()
		totalMarketValue = totalMarketValue.Add(exposure)
		exposureBySymbol = append(exposureBySymbol, SymbolRisk{
			Symbol:   pos.Symbol,
			Exposure: exposure,
			Sigma1D:  sigma,
		})
	}

	sort.Slice(exposureBySymbol, func(i, j int) bool {
		return exposureBySymbol[i].Symbol < exposureBySymbol[j].Symbol
	})
	sort.Strings(unpriced)

	if totalMarketValue.GreaterThan(zero) {
		for i := range exposureBySymbol {
			exposureBySymbol[i].Weight = exposureBySymbol[i].Exposure.Div(totalMarketValue)
		}
	}

	sigmaPortfolio, err := sigmaPortfolioUncorrelated(exposureBySymbol)
	if err != nil {
		return Snapshot{}, err
	}
	var95 := totalMarketValue.Mul(sigmaPortfolio).Mul(zScore95)

	return Snapshot{
		ExposureBySymbol: exposureBySymbol,
		TotalMarketValue: totalMarketValue,
		Sigma1DPortfolio: sigmaPortfolio,
		VaR95_1d:         var95,
		Concentration: Concentration{
			TopN: topByWeight(exposureBySymbol, topN),
			HHI:  hhi(exposureBySymbol),
		},
		Assumptions: Assumptions{
			Confidence:           confidence95,
			HorizonDays:          horizonDays1,
			ZScore:               zScore95,
			VolatilityConvention: "annual_to_daily_sqrt_252",
			Model:                "parametric_normal",
		},
		UnpricedSymbols: unpriced,
	}, nil
}

func normalizeTopN(n int) int {
	if n <= 0 {
		return defaultTopN
	}
	if n > maxTopN {
		return maxTopN
	}
	return n
}

func sigmaPortfolioUncorrelated(symbols []SymbolRisk) (decimal.Decimal, error) {
	if len(symbols) == 0 {
		return zero, nil
	}
	sum := 0.0
	for _, s := range symbols {
		w, _ := s.Weight.Float64()
		sigma, _ := s.Sigma1D.Float64()
		sum += (w * w) * (sigma * sigma)
	}
	if sum < 0 && math.Abs(sum) <= sqrtDomainEpsilon {
		sum = 0
	}
	if sum < 0 {
		return zero, fmt.Errorf("invalid weighted variance sum: %f", sum)
	}
	return decimal.NewFromFloat(math.Sqrt(sum)), nil
}

func hhi(symbols []SymbolRisk) decimal.Decimal {
	acc := zero
	for _, s := range symbols {
		acc = acc.Add(s.Weight.Mul(s.Weight))
	}
	return acc
}

func topByWeight(symbols []SymbolRisk, n int) []SymbolRisk {
	if len(symbols) == 0 {
		return nil
	}
	cpy := make([]SymbolRisk, len(symbols))
	copy(cpy, symbols)
	sort.Slice(cpy, func(i, j int) bool {
		if cpy[i].Weight.Equal(cpy[j].Weight) {
			return cpy[i].Symbol < cpy[j].Symbol
		}
		return cpy[i].Weight.GreaterThan(cpy[j].Weight)
	})
	if n > len(cpy) {
		n = len(cpy)
	}
	return cpy[:n]
}
