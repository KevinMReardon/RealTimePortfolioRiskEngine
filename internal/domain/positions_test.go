package domain

import (
	"errors"
	"math/rand"
	"testing"

	"github.com/shopspring/decimal"
)

func trade(sym string, side Side, qty decimal.Decimal) TradePayload {
	return TradePayload{
		TradeID:  "x",
		Symbol:   sym,
		Side:     side,
		Quantity: qty,
		Price:    decimal.NewFromInt(100),
		Currency: "USD",
	}
}

func TestPositions_ApplyTrade_table(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		seq       []TradePayload
		wantErrAt int // index in seq where first error expected; -1 if none
		wantErr   error
		assert    func(t *testing.T, p *Positions)
	}{
		{
			name: "buy_then_sell_ok",
			seq: []TradePayload{
				trade("AAPL", SideBuy, decimal.NewFromInt(10)),
				trade("AAPL", SideSell, decimal.NewFromInt(10)),
			},
			wantErrAt: -1,
			assert: func(t *testing.T, p *Positions) {
				t.Helper()
				if !p.Quantity("AAPL").IsZero() {
					t.Fatalf("AAPL qty = %s want 0", p.Quantity("AAPL"))
				}
			},
		},
		{
			name: "sell_exactly_current_qty_ok",
			seq: []TradePayload{
				trade("MSFT", SideBuy, decimal.NewFromInt(5)),
				trade("MSFT", SideSell, decimal.NewFromInt(5)),
			},
			wantErrAt: -1,
			assert: func(t *testing.T, p *Positions) {
				t.Helper()
				if !p.Quantity("MSFT").IsZero() {
					t.Fatalf("MSFT qty = %s want 0", p.Quantity("MSFT"))
				}
			},
		},
		{
			name: "sell_more_than_current_underflow",
			seq: []TradePayload{
				trade("AAPL", SideSell, decimal.NewFromInt(1)),
			},
			wantErrAt: 0,
			wantErr:   ErrPositionUnderflow,
		},
		{
			name: "sell_partial_then_oversell",
			seq: []TradePayload{
				trade("AAPL", SideBuy, decimal.NewFromInt(3)),
				trade("AAPL", SideSell, decimal.NewFromInt(2)),
				trade("AAPL", SideSell, decimal.NewFromInt(2)),
			},
			wantErrAt: 2,
			wantErr:   ErrPositionUnderflow,
			assert: func(t *testing.T, p *Positions) {
				t.Helper()
				if !p.Quantity("AAPL").Equal(decimal.NewFromInt(1)) {
					t.Fatalf("AAPL qty = %s want 1", p.Quantity("AAPL"))
				}
			},
		},
		{
			name: "invalid_side",
			seq: []TradePayload{
				{
					TradeID:  "1",
					Symbol:   "AAPL",
					Side:     "HOLD",
					Quantity: decimal.NewFromInt(1),
					Price:    decimal.NewFromInt(1),
					Currency: "USD",
				},
			},
			wantErrAt: 0,
			wantErr:   ErrValidation,
		},
		{
			name: "multi_symbol_independent",
			seq: []TradePayload{
				trade("AAPL", SideBuy, decimal.NewFromInt(10)),
				trade("MSFT", SideBuy, decimal.NewFromInt(5)),
				trade("AAPL", SideSell, decimal.NewFromInt(4)),
				trade("MSFT", SideSell, decimal.NewFromInt(5)),
			},
			wantErrAt: -1,
			assert: func(t *testing.T, p *Positions) {
				t.Helper()
				if !p.Quantity("AAPL").Equal(decimal.NewFromInt(6)) {
					t.Fatalf("AAPL qty = %s want 6", p.Quantity("AAPL"))
				}
				if !p.Quantity("MSFT").IsZero() {
					t.Fatalf("MSFT qty = %s want 0", p.Quantity("MSFT"))
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p := NewPositions()
			for i, tr := range tt.seq {
				err := p.ApplyTrade(tr)
				if i == tt.wantErrAt {
					if tt.wantErr == nil {
						t.Fatalf("wantErr at %d but wantErr is nil", i)
					}
					if !errors.Is(err, tt.wantErr) {
						t.Fatalf("step %d: want %v, got %v", i, tt.wantErr, err)
					}
					if tt.assert != nil {
						tt.assert(t, p)
					}
					return
				}
				if err != nil {
					t.Fatalf("step %d: unexpected error: %v", i, err)
				}
			}
			if tt.wantErrAt >= 0 {
				t.Fatalf("expected error at step %d", tt.wantErrAt)
			}
			if tt.assert != nil {
				tt.assert(t, p)
			}
		})
	}
}

func TestPositions_ApplyTrade_weightedAverageAndRealized(t *testing.T) {
	t.Parallel()
	p := NewPositions()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(p.ApplyTrade(TradePayload{TradeID: "1", Symbol: "AAPL", Side: SideBuy, Quantity: decimal.NewFromInt(100), Price: decimal.NewFromInt(10), Currency: "USD"}))
	lot := p.Lot("AAPL")
	if !lot.Quantity.Equal(decimal.NewFromInt(100)) || !lot.AverageCost.Equal(decimal.NewFromInt(10)) || !lot.RealizedPnL.IsZero() {
		t.Fatalf("after buy 100@10: %+v", lot)
	}
	must(p.ApplyTrade(TradePayload{TradeID: "2", Symbol: "AAPL", Side: SideBuy, Quantity: decimal.NewFromInt(50), Price: decimal.NewFromInt(12), Currency: "USD"}))
	lot = p.Lot("AAPL")
	wantAvg := decimal.NewFromInt(1600).Div(decimal.NewFromInt(150))
	if !lot.Quantity.Equal(decimal.NewFromInt(150)) || !lot.AverageCost.Equal(wantAvg) {
		t.Fatalf("after second buy: qty=%s avg=%s want avg 1600/150", lot.Quantity, lot.AverageCost)
	}
	must(p.ApplyTrade(TradePayload{TradeID: "3", Symbol: "AAPL", Side: SideSell, Quantity: decimal.NewFromInt(30), Price: decimal.NewFromInt(11), Currency: "USD"}))
	lot = p.Lot("AAPL")
	wantReal := decimal.NewFromInt(11).Sub(wantAvg).Mul(decimal.NewFromInt(30))
	if !lot.Quantity.Equal(decimal.NewFromInt(120)) || !lot.AverageCost.Equal(wantAvg) || !lot.RealizedPnL.Equal(wantReal) {
		t.Fatalf("after partial sell: %+v wantReal %s", lot, wantReal)
	}
}

func TestPositions_ApplyTrade_flatWithRealizedRetained(t *testing.T) {
	t.Parallel()
	p := NewPositions()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(p.ApplyTrade(TradePayload{Symbol: "AAPL", Side: SideBuy, Quantity: decimal.NewFromInt(10), Price: decimal.NewFromInt(10), Currency: "USD"}))
	must(p.ApplyTrade(TradePayload{Symbol: "AAPL", Side: SideSell, Quantity: decimal.NewFromInt(10), Price: decimal.NewFromInt(12), Currency: "USD"}))
	lot := p.Lot("AAPL")
	if !lot.Quantity.IsZero() || !lot.RealizedPnL.Equal(decimal.NewFromInt(20)) {
		t.Fatalf("flat with realized: %+v", lot)
	}
}

func TestApplyTrade_validRandomSequenceNonNegative(t *testing.T) {
	t.Parallel()
	const iterations = 300
	const steps = 40
	symbols := []string{"A", "MSFT", "BRK.B"}

	for it := 0; it < iterations; it++ {
		rng := rand.New(rand.NewSource(int64(it + 424242))) //nolint:gosec // deterministic test RNG
		state := map[string]int64{"A": 0, "MSFT": 0, "BRK.B": 0}
		p := NewPositions()

		for s := 0; s < steps; s++ {
			sym := symbols[rng.Intn(len(symbols))]
			cur := state[sym]

			if cur == 0 || rng.Intn(2) == 0 {
				q := int64(rng.Intn(25) + 1)
				state[sym] = cur + q
				if err := p.ApplyTrade(TradePayload{
					TradeID:  "t",
					Symbol:   sym,
					Side:     SideBuy,
					Quantity: decimal.NewFromInt(q),
					Price:    decimal.NewFromInt(1),
					Currency: "USD",
				}); err != nil {
					t.Fatalf("iter %d step %d buy: %v", it, s, err)
				}
			} else {
				sell := int64(rng.Intn(int(cur))) + 1
				state[sym] = cur - sell
				if err := p.ApplyTrade(TradePayload{
					TradeID:  "t",
					Symbol:   sym,
					Side:     SideSell,
					Quantity: decimal.NewFromInt(sell),
					Price:    decimal.NewFromInt(1),
					Currency: "USD",
				}); err != nil {
					t.Fatalf("iter %d step %d sell: %v", it, s, err)
				}
			}

			for _, checkSym := range symbols {
				q := p.Quantity(checkSym)
				if q.IsNegative() {
					t.Fatalf("iter %d step %d: negative qty %s=%s", it, s, checkSym, q)
				}
				if !q.Equal(decimal.NewFromInt(state[checkSym])) {
					t.Fatalf("iter %d step %d: drift %s p=%s state=%d", it, s, checkSym, q, state[checkSym])
				}
			}
		}
	}
}
