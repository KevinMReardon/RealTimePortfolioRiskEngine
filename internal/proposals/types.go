package proposals

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/KevinMReardon/realtime-portfolio-risk/internal/policy"
)

// Approval source for approved rows. NULL in DB = legacy or not set (see migration comments).
const (
	ApprovalSourceHuman     = "human"
	ApprovalSourcePaperAuto = "paper_auto"
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

	// CriticVerdict is optional JSON from the self-critic pass (NULL when not run).
	CriticVerdict json.RawMessage
	// CriticCompletedAt is set when critic_verdict is written.
	CriticCompletedAt *time.Time
	// CriticModel is the model id used for the critic call (NULL if unknown / not set).
	CriticModel *string
	// ApprovalSource is human vs paper auto-approve; NULL for legacy rows or pre-approval.
	ApprovalSource *string

	// PaperAutoRetryCount is incremented on each failed paper-auto retry pass (not materialize-first).
	PaperAutoRetryCount int
}

// Proposal status values for proposed_trades.status.
const (
	StatusProposed       = "proposed"
	StatusApproved       = "approved"
	StatusSubmitted      = "submitted"
	StatusFilled         = "filled"
	StatusRejected       = "rejected"
	StatusCancelled      = "cancelled"
	StatusAutoAbandoned  = "auto_abandoned"
)

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

// ListByAgentSessionFilter narrows ListByAgentSession.
type ListByAgentSessionFilter struct {
	Statuses []string // empty = proposed and approved only
}

// ApproveParams binds human approval to payload_hash and row_version.
type ApproveParams struct {
	PortfolioID uuid.UUID
	ProposalID  uuid.UUID
	UserID      uuid.UUID
	PayloadHash string
	RowVersion  int
}

// AutoApproveParams binds system paper-auto approval to payload_hash and row_version.
type AutoApproveParams struct {
	PortfolioID uuid.UUID
	ProposalID  uuid.UUID
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

// SaveCriticVerdictParams updates critic columns for a proposal (tenant-scoped).
type SaveCriticVerdictParams struct {
	PortfolioID uuid.UUID
	ProposalID  uuid.UUID
	// Verdict is JSON stored in critic_verdict (e.g. structured pass/fail from critic).
	Verdict json.RawMessage
	// CompletedAt is stored as critic_completed_at (UTC).
	CompletedAt time.Time
	// Model is optional critic model name; empty is stored as NULL.
	Model string
}
