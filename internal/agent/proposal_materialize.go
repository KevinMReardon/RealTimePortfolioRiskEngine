package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/shopspring/decimal"
	"go.uber.org/zap"

	"github.com/KevinMReardon/realtime-portfolio-risk/internal/connectors/alpaca"
	"github.com/KevinMReardon/realtime-portfolio-risk/internal/domain"
	"github.com/KevinMReardon/realtime-portfolio-risk/internal/policy"
	"github.com/KevinMReardon/realtime-portfolio-risk/internal/portfolio"
	"github.com/KevinMReardon/realtime-portfolio-risk/internal/proposals"
)

// PortfolioAssemblerLoader loads read-model input for policy snapshots (typically events.PostgresStore).
type PortfolioAssemblerLoader interface {
	LoadPortfolioAssemblerInput(ctx context.Context, portfolioID uuid.UUID) (portfolio.PortfolioAssemblerInput, bool, error)
}

// ProposalStore inserts evaluated proposals (typically *proposals.Store).
type ProposalStore interface {
	InsertProposal(ctx context.Context, p proposals.InsertParams) (proposals.Proposal, error)
	LoadEquityAnchorForPortfolioDate(ctx context.Context, portfolioID uuid.UUID, anchorDate time.Time) (decimal.Decimal, bool, error)
	KillSwitchLatestActive(ctx context.Context) (active bool, ok bool, err error)
}

// BriefingProposalMaterializer turns validated briefing trade ideas into proposed_trades rows.
type BriefingProposalMaterializer struct {
	Store         ProposalStore
	Loader        PortfolioAssemblerLoader
	Policy        policy.Config
	TradingHalt   bool
	Log           *zap.Logger
}

var _ ProposalMaterializer = (*BriefingProposalMaterializer)(nil)

// Materialize inserts one proposal per materializable trade idea (idempotent on session index).
func (m *BriefingProposalMaterializer) Materialize(ctx context.Context, portfolioID, sessionID uuid.UUID, out BriefingOutput) error {
	if m == nil || m.Store == nil || m.Loader == nil {
		return nil
	}
	if m.Log == nil {
		m.Log = zap.NewNop()
	}
	in, found, err := m.Loader.LoadPortfolioAssemblerInput(ctx, portfolioID)
	if err != nil {
		return fmt.Errorf("proposal materialize: load assembler input: %w", err)
	}
	if !found {
		m.Log.Warn("proposal_materialize_skipped", zap.String("reason", "portfolio_not_found"), zap.String("portfolio_id", portfolioID.String()))
		return nil
	}
	nyLoc, err := time.LoadLocation("America/New_York")
	if err != nil {
		nyLoc = time.FixedZone("America/New_York", -5*3600)
	}
	nowNY := time.Now().In(nyLoc)
	anchorDate := time.Date(nowNY.Year(), nowNY.Month(), nowNY.Day(), 0, 0, 0, 0, time.UTC)
	equityAnchor := decimal.Zero
	if eq, ok, err := m.Store.LoadEquityAnchorForPortfolioDate(ctx, portfolioID, anchorDate); err != nil {
		m.Log.Warn("proposal_materialize_equity_anchor", zap.Error(err))
	} else if ok {
		equityAnchor = eq
	}
	dbKillActive, dbKillPresent, err := m.Store.KillSwitchLatestActive(ctx)
	if err != nil {
		return fmt.Errorf("proposal materialize: kill switch: %w", err)
	}
	killEnv, killDB := proposals.KillSwitchInputs(m.TradingHalt, dbKillActive, dbKillPresent)
	snap := BuildPolicySnapshot(in, equityAnchor, nowNY, killEnv, killDB)

	for i := range out.TradeIdeas {
		idea := out.TradeIdeas[i]
		if !briefingIdeaMaterializable(idea) {
			continue
		}
		intent, err := intentFromBriefingIdea(idea)
		if err != nil {
			m.Log.Warn("proposal_materialize_skip_idea", zap.Int("trade_idea_index", i), zap.Error(err))
			continue
		}
		decision := policy.Evaluate(intent, snap, m.Policy)
		idx := i
		rat := strings.TrimSpace(idea.Rationale)
		var ratPtr *string
		if rat != "" {
			ratPtr = &rat
		}
		sess := sessionID
		_, err = m.Store.InsertProposal(ctx, proposals.InsertParams{
			PortfolioID:       portfolioID,
			AgentSessionID:    &sess,
			TradeIdeaIndex:    &idx,
			RationaleSnapshot: ratPtr,
			Intent:            intent,
			Decision:          decision,
			Mode:              m.Policy.Mode,
		})
		if err == nil {
			continue
		}
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			m.Log.Debug("proposal_materialize_idempotent_skip", zap.Int("trade_idea_index", i), zap.String("session_id", sessionID.String()))
			continue
		}
		return fmt.Errorf("proposal materialize: insert index %d: %w", i, err)
	}
	return nil
}

func briefingIdeaMaterializable(idea BriefingIdea) bool {
	sym := policy.NormalizeSymbol(strings.TrimSpace(idea.Symbol))
	if sym == "" || !domain.IsValidSymbol(sym) {
		return false
	}
	side := domain.Side(strings.ToUpper(strings.TrimSpace(idea.Side)))
	if !domain.IsValidSide(side) {
		return false
	}
	hasQty := strings.TrimSpace(idea.Quantity) != ""
	hasNotional := strings.TrimSpace(idea.NotionalUSD) != ""
	if !hasQty && !hasNotional {
		return false
	}
	if hasQty {
		q, err := decimal.NewFromString(strings.TrimSpace(idea.Quantity))
		if err != nil || !q.IsPositive() {
			return false
		}
	}
	if hasNotional {
		n, err := decimal.NewFromString(strings.TrimSpace(idea.NotionalUSD))
		if err != nil || !n.IsPositive() || n.LessThan(alpaca.MinNotionalStockOrderUSD) {
			return false
		}
	}
	if strings.TrimSpace(idea.OrderType) == "" || strings.TrimSpace(idea.TimeInForce) == "" {
		return false
	}
	if orderTypeImpliesLimit(idea.OrderType) {
		lp := strings.TrimSpace(idea.LimitPrice)
		if lp == "" {
			return false
		}
		if _, err := decimal.NewFromString(lp); err != nil {
			return false
		}
	}
	return true
}

func intentFromBriefingIdea(idea BriefingIdea) (policy.Intent, error) {
	sym := policy.NormalizeSymbol(strings.TrimSpace(idea.Symbol))
	side := domain.Side(strings.ToUpper(strings.TrimSpace(idea.Side)))
	var qty *decimal.Decimal
	if strings.TrimSpace(idea.Quantity) != "" {
		q, err := decimal.NewFromString(strings.TrimSpace(idea.Quantity))
		if err != nil {
			return policy.Intent{}, err
		}
		qty = &q
	}
	var notional *decimal.Decimal
	if strings.TrimSpace(idea.NotionalUSD) != "" {
		n, err := decimal.NewFromString(strings.TrimSpace(idea.NotionalUSD))
		if err != nil {
			return policy.Intent{}, err
		}
		notional = &n
	}
	var lim *decimal.Decimal
	if strings.TrimSpace(idea.LimitPrice) != "" {
		l, err := decimal.NewFromString(strings.TrimSpace(idea.LimitPrice))
		if err != nil {
			return policy.Intent{}, err
		}
		lim = &l
	}
	return policy.Intent{
		Symbol:      sym,
		Side:        side,
		Quantity:    qty,
		NotionalUSD: notional,
		OrderType:   strings.TrimSpace(idea.OrderType),
		TimeInForce: strings.TrimSpace(idea.TimeInForce),
		LimitPrice:  lim,
	}, nil
}

// BuildPolicySnapshot builds a policy evaluation snapshot from assembler input (materializer + proposal submit).
func BuildPolicySnapshot(in portfolio.PortfolioAssemblerInput, equityAnchor decimal.Decimal, nowNY time.Time, killEnv, killDB bool) policy.Snapshot {
	posQty := make(map[string]decimal.Decimal)
	marks := make(map[string]decimal.Decimal)
	totalMV := decimal.Zero

	for _, p := range in.Positions {
		sym := strings.TrimSpace(p.Symbol)
		if sym == "" {
			continue
		}
		posQty[sym] = p.Quantity
		if pm, ok := in.PriceBySymbol[sym]; ok && !pm.Price.IsZero() {
			marks[sym] = pm.Price
			if !p.Quantity.IsZero() {
				totalMV = totalMV.Add(p.Quantity.Abs().Mul(pm.Price))
			}
		}
	}

	return policy.Snapshot{
		PortfolioEquity:      totalMV,
		PositionQtyBySymbol:  posQty,
		MarkPriceBySymbol:    marks,
		NowNY:                nowNY,
		EquityAnchor:         equityAnchor,
		KillSwitchEnv:        killEnv,
		KillSwitchDB:         killDB,
		DailyNotionalUsedUSD: decimal.Zero,
		OrdersLastMinute:     0,
	}
}
