package proposals

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/KevinMReardon/realtime-portfolio-risk/internal/policy"
)

// Proposal is a row from proposed_trades.
type Proposal struct {
	ProposalID        uuid.UUID
	PortfolioID       uuid.UUID
	AgentSessionID    *uuid.UUID
	TradeIdeaIndex    *int
	IdeaFingerprint   *string
	Symbol            string
	Side              string
	Quantity          *decimal.Decimal
	NotionalUSD       *decimal.Decimal
	OrderType         *string
	LimitPrice        *decimal.Decimal
	TimeInForce       *string
	ClientOrderID     *string
	RationaleSnapshot *string

	PolicyResult     json.RawMessage
	PolicyInputsHash string
	PolicyConfigHash string
	PayloadHash      string

	Status     string
	RowVersion int

	CreatedAt time.Time
	UpdatedAt time.Time

	ApprovedByUserID *uuid.UUID
	ApprovedAt       *time.Time
	DeniedByUserID   *uuid.UUID
	DenyReason       *string
	SubmittedAt      *time.Time
	BrokerOrderID    *string
	LastError        *string
}

// InsertParams builds a proposed_trades row after policy.Evaluate.
type InsertParams struct {
	PortfolioID       uuid.UUID
	AgentSessionID    *uuid.UUID
	TradeIdeaIndex    *int
	IdeaFingerprint   *string
	ClientOrderID     *string
	RationaleSnapshot *string

	Intent   policy.Intent
	Decision policy.Decision
	Mode     policy.Mode
}

// PolicyResultRecord is stored in policy_result JSONB.
type PolicyResultRecord struct {
	StrictOutcome    policy.Outcome     `json:"strict_outcome"`
	EffectiveOutcome policy.Outcome     `json:"effective_outcome"`
	PolicyMode       policy.Mode        `json:"policy_mode"`
	Violations       []policy.Violation `json:"violations"`
}

// ListFilter narrows ListByPortfolio.
type ListFilter struct {
	Status *string // exact status match; nil = all
	Symbol *string // normalized match; nil = all
}

// ApproveParams binds human approval to payload_hash and row_version.
type ApproveParams struct {
	PortfolioID uuid.UUID
	ProposalID  uuid.UUID
	UserID      uuid.UUID
	PayloadHash string
	RowVersion  int
}

// DenyParams rejects a proposal in proposed status.
type DenyParams struct {
	PortfolioID uuid.UUID
	ProposalID  uuid.UUID
	UserID      uuid.UUID
	PayloadHash string
	RowVersion  int
	DenyReason  string
}
