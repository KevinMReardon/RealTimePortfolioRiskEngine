package domain

import "sort"

// SymbolLotEntry is one symbol’s §7 lot for ordered iteration (e.g. snapshot serialization).
type SymbolLotEntry struct {
	Symbol string
	Lot    PositionLot
}

// SortedSymbolLots returns all lots in p sorted by symbol ascending. Nil or empty p yields nil.
func SortedSymbolLots(p *Positions) []SymbolLotEntry {
	if p == nil || len(p.bySymbol) == 0 {
		return nil
	}
	syms := make([]string, 0, len(p.bySymbol))
	for s := range p.bySymbol {
		syms = append(syms, s)
	}
	sort.Strings(syms)
	out := make([]SymbolLotEntry, 0, len(syms))
	for _, s := range syms {
		out = append(out, SymbolLotEntry{Symbol: s, Lot: p.bySymbol[s]})
	}
	return out
}
