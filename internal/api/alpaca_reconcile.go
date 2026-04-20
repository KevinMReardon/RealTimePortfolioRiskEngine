package api

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"

	"github.com/KevinMReardon/realtime-portfolio-risk/internal/connectors/alpaca"
	"github.com/KevinMReardon/realtime-portfolio-risk/internal/portfolio"
	"github.com/shopspring/decimal"
)

func normalizeSym(s string) string {
	return strings.ToUpper(strings.TrimSpace(s))
}

// ReconcileQtyDrift compares internal projection quantities to Alpaca open positions for the linked account.
// Symbols are normalized (uppercase). Missing side is treated as quantity zero.
func ReconcileQtyDrift(internal []portfolio.ProjectionRow, broker []alpaca.PositionRow) AlpacaReconciliationPayload {
	internalQty := map[string]decimal.Decimal{}
	for _, p := range internal {
		sym := normalizeSym(p.Symbol)
		if sym == "" {
			continue
		}
		internalQty[sym] = p.Quantity
	}

	brokerQty := map[string]decimal.Decimal{}
	for _, p := range broker {
		sym := normalizeSym(p.Symbol)
		if sym == "" {
			continue
		}
		brokerQty[sym] = p.Qty
	}

	all := map[string]struct{}{}
	for s := range internalQty {
		all[s] = struct{}{}
	}
	for s := range brokerQty {
		all[s] = struct{}{}
	}

	syms := make([]string, 0, len(all))
	for s := range all {
		syms = append(syms, s)
	}
	sort.Strings(syms)

	mismatches := make([]AlpacaQtyMismatchRow, 0)
	internalOnly := make([]string, 0)
	brokerOnly := make([]string, 0)

	for _, sym := range syms {
		iq, iok := internalQty[sym]
		if !iok {
			iq = decimal.Zero
		}
		bq, bok := brokerQty[sym]
		if !bok {
			bq = decimal.Zero
		}
		if iq.IsZero() && bq.IsZero() {
			continue
		}
		if iq.Equal(bq) {
			continue
		}

		mismatches = append(mismatches, AlpacaQtyMismatchRow{
			Symbol:      sym,
			InternalQty: iq.String(),
			BrokerQty:   bq.String(),
		})
		if !iq.IsZero() && bq.IsZero() {
			internalOnly = append(internalOnly, sym)
		}
		if !bq.IsZero() && iq.IsZero() {
			brokerOnly = append(brokerOnly, sym)
		}
	}

	sort.Strings(internalOnly)
	sort.Strings(brokerOnly)

	return AlpacaReconciliationPayload{
		Mismatches:          mismatches,
		InternalOnlySymbols: internalOnly,
		BrokerOnlySymbols:   brokerOnly,
		AggregateHash:       hashQtyMaps(syms, internalQty, brokerQty),
	}
}

// AlpacaQtyMismatchRow is one symbol where internal qty ≠ broker qty.
type AlpacaQtyMismatchRow struct {
	Symbol      string `json:"symbol"`
	InternalQty string `json:"internal_quantity"`
	BrokerQty   string `json:"broker_quantity"`
}

// AlpacaReconciliationPayload is the drift section returned by GET .../alpaca/reconciliation.
type AlpacaReconciliationPayload struct {
	Mismatches          []AlpacaQtyMismatchRow `json:"mismatches"`
	InternalOnlySymbols []string               `json:"internal_only_symbols"`
	BrokerOnlySymbols   []string               `json:"broker_only_symbols"`
	AggregateHash       string                 `json:"aggregate_hash"`
}

func hashQtyMaps(sortedSyms []string, internal, broker map[string]decimal.Decimal) string {
	var lines []string
	for _, sym := range sortedSyms {
		iq := decimal.Zero
		bq := decimal.Zero
		if q, ok := internal[sym]; ok {
			iq = q
		}
		if q, ok := broker[sym]; ok {
			bq = q
		}
		if iq.IsZero() && bq.IsZero() {
			continue
		}
		lines = append(lines, sym+"\t"+iq.String()+"\t"+bq.String())
	}
	sum := sha256.Sum256([]byte(strings.Join(lines, "\n")))
	return hex.EncodeToString(sum[:])
}
