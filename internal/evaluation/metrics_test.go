package evaluation

import (
	"testing"

	"github.com/shopspring/decimal"
)

func TestMaxDrawdown_and_Sharpe(t *testing.T) {
	t.Parallel()
	eq := []decimal.Decimal{
		decimal.NewFromInt(100),
		decimal.NewFromInt(110),
		decimal.NewFromInt(105),
		decimal.NewFromInt(120),
	}
	dd := MaxDrawdown(eq)
	if dd.LessThanOrEqual(decimal.Zero) {
		t.Fatalf("dd=%s", dd)
	}
	rets := []decimal.Decimal{
		decimal.NewFromFloat(0.01),
		decimal.NewFromFloat(-0.005),
		decimal.NewFromFloat(0.02),
	}
	s := SharpeRatio(rets, decimal.Zero)
	if s.IsZero() {
		t.Fatal("expected non-zero sharpe")
	}
}

func TestCumulativePnL(t *testing.T) {
	t.Parallel()
	got := CumulativePnL([]decimal.Decimal{
		decimal.NewFromInt(10),
		decimal.NewFromInt(-3),
	})
	if !got.Equal(decimal.NewFromInt(7)) {
		t.Fatalf("got %s", got)
	}
}
