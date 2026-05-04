package proposals

import (
	"fmt"
	"strings"

	"github.com/KevinMReardon/realtime-portfolio-risk/internal/domain"
	"github.com/KevinMReardon/realtime-portfolio-risk/internal/policy"
	"github.com/shopspring/decimal"
)

// IntentFromProposal maps stored proposal columns to a policy intent.
func IntentFromProposal(p Proposal) (policy.Intent, error) {
	sym := policy.NormalizeSymbol(strings.TrimSpace(p.Symbol))
	side := domain.Side(strings.ToUpper(strings.TrimSpace(p.Side)))
	if !domain.IsValidSide(side) {
		return policy.Intent{}, fmt.Errorf("invalid side %q", p.Side)
	}
	var qty *decimal.Decimal
	if p.Quantity != nil && !p.Quantity.IsZero() {
		q := *p.Quantity
		qty = &q
	}
	var notional *decimal.Decimal
	if p.NotionalUSD != nil && !p.NotionalUSD.IsZero() {
		n := *p.NotionalUSD
		notional = &n
	}
	var lim *decimal.Decimal
	if p.LimitPrice != nil && !p.LimitPrice.IsZero() {
		l := *p.LimitPrice
		lim = &l
	}
	ot := ""
	if p.OrderType != nil {
		ot = strings.TrimSpace(*p.OrderType)
	}
	tif := ""
	if p.TimeInForce != nil {
		tif = strings.TrimSpace(*p.TimeInForce)
	}
	return policy.Intent{
		Symbol:      sym,
		Side:        side,
		Quantity:    qty,
		NotionalUSD: notional,
		OrderType:   ot,
		TimeInForce: tif,
		LimitPrice:  lim,
	}, nil
}
