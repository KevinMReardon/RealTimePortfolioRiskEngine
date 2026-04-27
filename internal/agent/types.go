package agent

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"

	"github.com/KevinMReardon/realtime-portfolio-risk/internal/events"
)

// AgentService orchestrates one briefing run and replay/list operations.
type AgentService interface {
	RunBriefing(ctx context.Context, req RunBriefingRequest) (RunBriefingResult, error)
	CreateBriefingOnDemand(ctx context.Context, req RunBriefingRequest) (RunBriefingResult, error)
	CreateBriefingScheduled(ctx context.Context, req RunBriefingRequest) (RunBriefingResult, error)
	GetLatestBriefing(ctx context.Context, portfolioID uuid.UUID) (events.AgentSession, bool, error)
	ListBriefings(ctx context.Context, portfolioID uuid.UUID, filter events.AgentSessionListFilter) ([]events.AgentSession, error)
	GetSessionReplay(ctx context.Context, sessionID uuid.UUID) (events.AgentSessionReplay, bool, error)
	GetReplay(ctx context.Context, sessionID uuid.UUID) (events.AgentSessionReplay, bool, error)
	ListPortfolioSessions(ctx context.Context, portfolioID uuid.UUID, filter events.AgentSessionListFilter) ([]events.AgentSession, error)
}

// AgentStore is the persistence adapter used by the agent package.
type AgentStore interface {
	CreateAgentSession(ctx context.Context, session events.AgentSession) (events.AgentSession, error)
	MarkAgentSessionRunning(ctx context.Context, sessionID uuid.UUID, startedAt time.Time) error
	AppendAgentSessionToolCall(ctx context.Context, call events.AgentSessionToolCall) (events.AgentSessionToolCall, error)
	CompleteAgentSessionSuccess(ctx context.Context, session events.AgentSession) error
	CompleteAgentSessionFailure(ctx context.Context, session events.AgentSession) error
	GetLatestAgentSessionForPortfolio(ctx context.Context, portfolioID uuid.UUID) (events.AgentSession, bool, error)
	ListAgentSessionsForPortfolio(ctx context.Context, portfolioID uuid.UUID, filter events.AgentSessionListFilter) ([]events.AgentSession, error)
	GetAgentSessionReplayByID(ctx context.Context, sessionID uuid.UUID) (events.AgentSessionReplay, bool, error)
}

// BriefingOutput is the validated output contract persisted/replayed by the system.
type BriefingOutput struct {
	MarketSummary  string         `json:"market_summary"`
	PortfolioContext string       `json:"portfolio_context"`
	TradeIdeas     []BriefingIdea `json:"trade_ideas"`
	RisksAndCaveats string        `json:"risks_and_caveats"`
	DataGaps       []string       `json:"data_gaps"`
	Disclaimer     string         `json:"disclaimer"`
	UsedSources    []string       `json:"used_sources"`
	UsedFields     []string       `json:"used_fields"`
}

type BriefingIdea struct {
	Symbol     string  `json:"symbol,omitempty"`
	Rationale  string  `json:"rationale"`
	Confidence float64 `json:"confidence"`
	Size       string  `json:"size"`
	Stop       string  `json:"stop"`
	Target     string  `json:"target"`
}

type RunBriefingRequest struct {
	PortfolioID       uuid.UUID
	RequestedByUserID *uuid.UUID
	TriggerSource     string
	RunDate           time.Time
	Model             string
	Temperature       *float64
	MaxTokens         *int
	UserInput         json.RawMessage
}

type RunBriefingResult struct {
	Session events.AgentSession
	Output  BriefingOutput
}
