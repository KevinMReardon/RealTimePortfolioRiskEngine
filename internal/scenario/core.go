package scenario

import (
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/KevinMReardon/realtime-portfolio-risk/internal/portfolio"
)

// ShockTypePCT is a fractional change to the mark: scenario_price = base_price * (1 + Value).
// Example: Value 0.10 means multiply the scenario price by 1.10 (+10%).
const ShockTypePCT = "PCT"

var (
	ErrEmptyShocks        = errors.New("scenario: shocks must include at least one item")
	ErrEmptyShockSymbol   = errors.New("scenario: shock symbol is required")
	ErrInvalidShockType   = errors.New("scenario: shock type must be PCT")
	ErrDuplicateShockSym  = errors.New("scenario: duplicate shock symbol")
	ErrNilPortfolioID = errors.New("scenario: portfolio_id is required for shocked assembly")
)

// Shock is one user-defined mark adjustment (quantities and costs stay on the base snapshot).
type Shock struct {
	Symbol string
	Type   string
	Value  decimal.Decimal
}

// ClonePriceBySymbol returns a copy of the price map; marks can be adjusted without mutating the base snapshot.
func ClonePriceBySymbol(in map[string]portfolio.PriceMarkInput) map[string]portfolio.PriceMarkInput {
	if in == nil {
		return map[string]portfolio.PriceMarkInput{}
	}
	out := make(map[string]portfolio.PriceMarkInput, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

// ValidateShocks checks non-empty list, symbols, types, and duplicate symbols.
func ValidateShocks(shocks []Shock) error {
	if len(shocks) == 0 {
		return ErrEmptyShocks
	}
	seen := make(map[string]struct{}, len(shocks))
	for i, sh := range shocks {
		if sh.Symbol == "" {
			return fmt.Errorf("%w at index %d", ErrEmptyShockSymbol, i)
		}
		if sh.Type != ShockTypePCT {
			return fmt.Errorf("%w: got %q at index %d", ErrInvalidShockType, sh.Type, i)
		}
		if _, dup := seen[sh.Symbol]; dup {
			return fmt.Errorf("%w %q", ErrDuplicateShockSym, sh.Symbol)
		}
		seen[sh.Symbol] = struct{}{}
	}
	return nil
}

// ShockedPortfolioInput returns a copy of base with scenario marks applied via §7 rules inputs
// (PriceBySymbol only). Positions and trade lineage are unchanged.
//
// If any shocked symbol is missing from the base price map or has a zero mark, those symbols are
// listed in missing (no error); the returned input must not be used in that case.
// ValidateShocks / nil portfolio_id failures return a non-nil err.
func ShockedPortfolioInput(base portfolio.PortfolioAssemblerInput, shocks []Shock) (out portfolio.PortfolioAssemblerInput, missing []string, err error) {
	if err := ValidateShocks(shocks); err != nil {
		return portfolio.PortfolioAssemblerInput{}, nil, err
	}
	if base.PortfolioID == uuid.Nil {
		return portfolio.PortfolioAssemblerInput{}, nil, ErrNilPortfolioID
	}

	out = portfolio.PortfolioAssemblerInput{
		PortfolioID:   base.PortfolioID,
		Positions:     append([]portfolio.ProjectionRow(nil), base.Positions...),
		PriceBySymbol: ClonePriceBySymbol(base.PriceBySymbol),
		TradeApply:    base.TradeApply,
	}

	for _, sh := range shocks {
		pm, ok := out.PriceBySymbol[sh.Symbol]
		if !ok || pm.Price.IsZero() {
			missing = append(missing, sh.Symbol)
			continue
		}
		pm.Price = pm.Price.Mul(decimal.NewFromInt(1).Add(sh.Value))
		out.PriceBySymbol[sh.Symbol] = pm
	}
	if len(missing) > 0 {
		return portfolio.PortfolioAssemblerInput{}, missing, nil
	}
	return out, nil, nil
}

// Eval runs ShockedPortfolioInput and AssemblePortfolioView so totals (market_value, unrealized_pnl, …)
// match the live portfolio read-model rules with scenario marks only.
func Eval(base portfolio.PortfolioAssemblerInput, shocks []Shock) (portfolio.PortfolioView, []string, error) {
	in, missing, err := ShockedPortfolioInput(base, shocks)
	if err != nil {
		return portfolio.PortfolioView{}, nil, err
	}
	if len(missing) > 0 {
		return portfolio.PortfolioView{}, missing, nil
	}
	v, err := portfolio.AssemblePortfolioView(in)
	if err != nil {
		return portfolio.PortfolioView{}, nil, err
	}
	return v, nil, nil
}
