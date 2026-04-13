// Package domain defines event contracts, payloads, and validation rules aligned with
// [docs/design/LLD.md] section 4 (event contracts and validation).
//
// v1 invariants:
//   - Canonical envelope and payload JSON field names match the LLD (e.g. event_type
//     TradeExecuted | PriceUpdated; trade side BUY | SELL; trade_id, quantity, price).
//   - Shape validation (IDs, times, symbol regex, positive quantity/price, required
//     currency) is enforced by Validate* helpers before persistence.
//   - Position quantities are updated only through Positions.ApplyTrade, which enforces
//     no shorting for v1: a SELL cannot reduce a symbol's quantity below zero
//     (ErrPositionUnderflow at apply time). HTTP validation cannot replace this.
//
// Future versions (shorting, margin, additional asset classes) may introduce alternate
// application strategies (e.g. a TradeApplicator interface) while keeping envelope
// contracts stable until the LLD is explicitly versioned.
package domain
