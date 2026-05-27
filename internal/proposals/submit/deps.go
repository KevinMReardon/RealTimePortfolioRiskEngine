package submit

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/KevinMReardon/realtime-portfolio-risk/internal/connectors/alpaca"
	"github.com/KevinMReardon/realtime-portfolio-risk/internal/events"
	"github.com/KevinMReardon/realtime-portfolio-risk/internal/policy"
	"github.com/KevinMReardon/realtime-portfolio-risk/internal/portfolio"
	"github.com/KevinMReardon/realtime-portfolio-risk/internal/config"
	"github.com/KevinMReardon/realtime-portfolio-risk/internal/proposals"
	"go.uber.org/zap"
)

// ProposalStore is the subset of proposals.Store required for broker submit.
type ProposalStore interface {
	GetByIDForPortfolio(ctx context.Context, portfolioID, proposalID uuid.UUID) (proposals.Proposal, error)
	MarkProposalSubmitted(ctx context.Context, p proposals.SubmitSuccessParams) error
	MarkProposalBrokerError(ctx context.Context, portfolioID, proposalID uuid.UUID, msg string) error
	LoadEquityAnchorForPortfolioDate(ctx context.Context, portfolioID uuid.UUID, anchorDate time.Time) (equity decimal.Decimal, ok bool, err error)
	KillSwitchLatestActive(ctx context.Context) (active bool, ok bool, err error)
}

// AssemblerInputLoader loads portfolio assembler input for policy snapshots.
type AssemblerInputLoader interface {
	LoadPortfolioAssemblerInput(ctx context.Context, portfolioID uuid.UUID) (portfolio.PortfolioAssemblerInput, bool, error)
}

// AlpacaKeyLoader returns stored per-portfolio Alpaca REST credentials.
type AlpacaKeyLoader interface {
	LoadPortfolioAlpacaKeyMaterial(ctx context.Context, portfolioID uuid.UUID) (events.PortfolioAlpacaKeyMaterial, bool, error)
}

// DailyUsageLoader returns today's submitted notional and recent order count for policy snapshot
// daily-budget and rate-limit rules. Implementations typically wrap proposals.Store.LoadDailyUsage.
type DailyUsageLoader interface {
	LoadDailyUsage(ctx context.Context, portfolioID uuid.UUID, nowNY time.Time) (decimal.Decimal, int, error)
}

// RESTFactory builds an alpaca.REST client (production uses alpaca.NewREST).
type RESTFactory func(cfg alpaca.RESTConfig) (alpaca.REST, error)

// Deps bundles dependencies for broker submit (HTTP, automation, tools).
type Deps struct {
	Store          ProposalStore
	Read           AssemblerInputLoader
	Keys           AlpacaKeyLoader
	Usage          DailyUsageLoader
	Policy         policy.Config
	TradingHaltEnv bool
	// RuntimeConfig when set supplies Policy and TradingHaltEnv on each submit.
	RuntimeConfig *config.ConfigHolder
	Log           *zap.Logger
	NewREST        RESTFactory
	// Now optionally overrides the clock used for NY policy time (tests); nil uses time.Now.
	Now func() time.Time
}

func (d Deps) effectivePolicyAndHalt() (policy.Config, bool) {
	if d.RuntimeConfig != nil {
		c := d.RuntimeConfig.Get()
		return c.PolicyConfig(), c.TradingHalt
	}
	return d.Policy, d.TradingHaltEnv
}
