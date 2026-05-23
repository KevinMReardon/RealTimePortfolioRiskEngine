package evaluation

import (
	"math"

	"github.com/shopspring/decimal"
)

// MaxDrawdown returns peak-to-trough decline as a positive fraction (e.g. 0.05 = 5% drawdown).
func MaxDrawdown(equity []decimal.Decimal) decimal.Decimal {
	if len(equity) == 0 {
		return decimal.Zero
	}
	peak := equity[0]
	maxDD := decimal.Zero
	for _, v := range equity {
		if v.GreaterThan(peak) {
			peak = v
		}
		if peak.IsZero() {
			continue
		}
		dd := peak.Sub(v).Div(peak)
		if dd.GreaterThan(maxDD) {
			maxDD = dd
		}
	}
	return maxDD
}

// SharpeRatio annualizes per-period returns using 252 trading days.
// riskFreePerPeriod is the per-period risk-free rate (often 0 in tests).
func SharpeRatio(returns []decimal.Decimal, riskFreePerPeriod decimal.Decimal) decimal.Decimal {
	n := len(returns)
	if n < 2 {
		return decimal.Zero
	}
	excess := make([]float64, 0, n)
	for _, r := range returns {
		ex, _ := r.Sub(riskFreePerPeriod).Float64()
		excess = append(excess, ex)
	}
	var sum float64
	for _, x := range excess {
		sum += x
	}
	mean := sum / float64(n)
	var varSum float64
	for _, x := range excess {
		d := x - mean
		varSum += d * d
	}
	std := math.Sqrt(varSum / float64(n-1))
	if std == 0 {
		return decimal.Zero
	}
	sharpe := (mean / std) * math.Sqrt(252)
	return decimal.NewFromFloat(sharpe)
}

// CumulativePnL sums period PnL values.
func CumulativePnL(periodPnL []decimal.Decimal) decimal.Decimal {
	total := decimal.Zero
	for _, p := range periodPnL {
		total = total.Add(p)
	}
	return total
}
