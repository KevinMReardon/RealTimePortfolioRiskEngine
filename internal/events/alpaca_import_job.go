package events

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

// ErrAlpacaImportJobConflict indicates an active import already exists for this portfolio.
var ErrAlpacaImportJobConflict = errors.New("alpaca import job already queued or running for portfolio")

// AlpacaImportJob is a durable row for async Alpaca fill history imports.
type AlpacaImportJob struct {
	JobID               uuid.UUID
	PortfolioID         uuid.UUID
	RequestedByUserID   *uuid.UUID
	Status              string
	Since               *time.Time
	Until               *time.Time
	FullHistory         bool
	InsertedCount       int
	DuplicateCount      int
	SkippedInvalidCount int
	PagesFetched        int
	ActivitiesSeen      int
	FillsConsidered     int
	ErrorMessage        *string
	CreatedAt           time.Time
	UpdatedAt           time.Time
	StartedAt           *time.Time
	FinishedAt          *time.Time
}
