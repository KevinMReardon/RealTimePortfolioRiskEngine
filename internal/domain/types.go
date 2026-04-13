package domain

import "regexp"

// Side is the trade direction in a TradeExecuted payload. LLD §4.2 JSON values are
// exactly "BUY" and "SELL" (uppercase).
type Side string

const (
	// SideBuy adds quantity on apply (see Positions.ApplyTrade).
	SideBuy Side = "BUY"
	// SideSell subtracts quantity when sufficient long inventory exists; v1 forbids
	// resulting negative quantities (no shorting).
	SideSell Side = "SELL"
)

var (
	// symbolRegexp approximates LLD “symbol regex and universe policy” for v1 ingest.
	symbolRegexp = regexp.MustCompile(`^[A-Z0-9._-]{1,32}$`)
)

// IsValidSide reports whether s is BUY or SELL per LLD §4.2.
func IsValidSide(s Side) bool {
	return s == SideBuy || s == SideSell
}

// IsValidSymbol reports whether symbol satisfies the v1 domain ticker pattern
// (subset of universe policy until a configurable allowlist exists).
func IsValidSymbol(symbol string) bool {
	return symbolRegexp.MatchString(symbol)
}


