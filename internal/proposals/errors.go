package proposals

import "errors"

var (
	// ErrProposalNotFound is returned when no row matches scoped Get or unexpected zero rows.
	ErrProposalNotFound = errors.New("proposals: not found")

	// ErrApproveConflict means status != proposed, wrong portfolio, payload_hash mismatch, or row_version mismatch.
	ErrApproveConflict = errors.New("proposals: approve conflict (status, payload_hash, or row_version)")

	// ErrDenyConflict is ErrApproveConflict for deny.
	ErrDenyConflict = errors.New("proposals: deny conflict (status, payload_hash, or row_version)")
)
