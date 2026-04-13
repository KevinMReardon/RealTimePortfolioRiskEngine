package portfolio

// Projector applies domain events to portfolio state.
//
// Implementations must update position quantities only via
// domain.Positions.ApplyTrade (or a thin wrapper that calls it) so the
// no-shorting rule (ErrPositionUnderflow at apply time) cannot be bypassed.
// HTTP validation alone is insufficient because concurrent events may change
// positions before apply.
type Projector interface {
	Project() error
}
