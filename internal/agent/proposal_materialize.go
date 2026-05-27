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
	"github.com/KevinMReardon/realtime-portfolio-risk/internal/events"
	"github.com/KevinMReardon/realtime-portfolio-risk/internal/policy"
	"github.com/KevinMReardon/realtime-portfolio-risk/internal/config"
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

// MaterializeAlpacaKeyLoader loads per-portfolio Alpaca credentials so the materializer can query
// the broker for live equity and account flags (optional; nil is tolerated).
type MaterializeAlpacaKeyLoader interface {
	LoadPortfolioAlpacaKeyMaterial(ctx context.Context, portfolioID uuid.UUID) (events.PortfolioAlpacaKeyMaterial, bool, error)
}

// MaterializeDailyUsageLoader reads today's submitted notional + recent order count for snapshot
// daily-budget and rate-limit rules (optional; nil is tolerated).
type MaterializeDailyUsageLoader interface {
	LoadDailyUsage(ctx context.Context, portfolioID uuid.UUID, nowNY time.Time) (decimal.Decimal, int, error)
}

// MaterializeRESTFactory builds an alpaca.REST from key material (same signature as submit.RESTFactory).
type MaterializeRESTFactory func(cfg alpaca.RESTConfig) (alpaca.REST, error)

// BriefingProposalMaterializer turns validated briefing trade ideas into proposed_trades rows.
type BriefingProposalMaterializer struct {
	Store       ProposalStore
	Loader      PortfolioAssemblerLoader
	Keys        MaterializeAlpacaKeyLoader
	Usage       MaterializeDailyUsageLoader
	NewREST     MaterializeRESTFactory
	Policy      policy.Config
	TradingHalt bool
	// Runtime when set supplies Policy and TradingHalt on each Materialize call.
	Runtime *config.ConfigHolder
	Log     *zap.Logger
}

var _ ProposalMaterializer = (*BriefingProposalMaterializer)(nil)

// Materialize inserts one proposal per materializable trade idea (idempotent on session index).
func (m *BriefingProposalMaterializer) Materialize(ctx context.Context, portfolioID, sessionID uuid.UUID, out BriefingOutput) ([]uuid.UUID, error) {
	if m == nil || m.Store == nil || m.Loader == nil {
		return nil, nil
	}
	if m.Log == nil {
		m.Log = zap.NewNop()
	}
	in, found, err := m.Loader.LoadPortfolioAssemblerInput(ctx, portfolioID)
	if err != nil {
		return nil, fmt.Errorf("proposal materialize: load assembler input: %w", err)
	}
	if !found {
		m.Log.Warn("proposal_materialize_skipped", zap.String("reason", "portfolio_not_found"), zap.String("portfolio_id", portfolioID.String()))
		return nil, nil
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
	pol := m.Policy
	tradingHalt := m.TradingHalt
	if m.Runtime != nil {
		c := m.Runtime.Get()
		pol = c.PolicyConfig()
		tradingHalt = c.TradingHalt
	}

	dbKillActive, dbKillPresent, err := m.Store.KillSwitchLatestActive(ctx)
	if err != nil {
		return nil, fmt.Errorf("proposal materialize: kill switch: %w", err)
	}
	killEnv, killDB := proposals.KillSwitchInputs(tradingHalt, dbKillActive, dbKillPresent)
	snap := policy.BuildSnapshot(in, equityAnchor, nowNY, killEnv, killDB)

	// Overlay live broker account (cash-aware equity + PDT/blocks) so proposal-time policy matches submit-time.
	if m.Keys != nil && m.NewREST != nil {
		if keys, linked, err := m.Keys.LoadPortfolioAlpacaKeyMaterial(ctx, portfolioID); err != nil {
			m.Log.Warn("proposal_materialize_alpaca_keys_failed", zap.Error(err), zap.String("portfolio_id", portfolioID.String()))
		} else if linked {
			baseURL := strings.TrimSpace(keys.BaseURL)
			if baseURL == "" {
				if strings.EqualFold(keys.AccountMode, "live") {
					baseURL = alpaca.DefaultRESTBaseURLLive
				} else {
					baseURL = alpaca.DefaultRESTBaseURLPaper
				}
			}
			if rest, err := m.NewREST(alpaca.RESTConfig{KeyID: keys.KeyID, SecretKey: keys.SecretKey, BaseURL: baseURL}); err != nil {
				m.Log.Warn("proposal_materialize_alpaca_client_failed", zap.Error(err), zap.String("portfolio_id", portfolioID.String()))
			} else if acct, err := rest.GetAccount(ctx); err != nil {
				m.Log.Warn("proposal_materialize_alpaca_account_failed", zap.Error(err), zap.String("portfolio_id", portfolioID.String()))
			} else {
				policy.ApplyBrokerAccount(&snap, policy.BrokerAccountSnapshot{
					PatternDayTrader: acct.PatternDayTrader,
					TradingBlocked:   acct.TradingBlocked,
					AccountBlocked:   acct.AccountBlocked,
					Equity:           acct.Equity,
				})
				m.Log.Info("proposal_materialize_broker_snapshot",
					zap.String("portfolio_id", portfolioID.String()),
					zap.String("broker_equity", acct.Equity.String()),
					zap.Bool("trading_blocked", acct.TradingBlocked),
					zap.Bool("account_blocked", acct.AccountBlocked),
					zap.Bool("pdt", acct.PatternDayTrader),
				)
			}
		}
	}

	// Overlay today's submitted notional + recent order count so daily-budget and rate-limit rules are real.
	if m.Usage != nil {
		if dailyNotional, ordersLastMin, err := m.Usage.LoadDailyUsage(ctx, portfolioID, nowNY); err != nil {
			m.Log.Warn("proposal_materialize_daily_usage_failed", zap.Error(err), zap.String("portfolio_id", portfolioID.String()))
		} else {
			policy.ApplyDailyUsage(&snap, dailyNotional, ordersLastMin)
		}
	}

	var inserted []uuid.UUID
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
		decision := policy.Evaluate(intent, snap, pol)
		idx := i
		rat := strings.TrimSpace(idea.Rationale)
		var ratPtr *string
		if rat != "" {
			ratPtr = &rat
		}
		sess := sessionID
		prop, err := m.Store.InsertProposal(ctx, proposals.InsertParams{
			PortfolioID:       portfolioID,
			AgentSessionID:    &sess,
			TradeIdeaIndex:    &idx,
			RationaleSnapshot: ratPtr,
			Intent:            intent,
			Decision:          decision,
			Mode:              pol.Mode,
		})
		if err == nil {
			inserted = append(inserted, prop.ProposalID)
			continue
		}
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			m.Log.Debug("proposal_materialize_idempotent_skip", zap.Int("trade_idea_index", i), zap.String("session_id", sessionID.String()))
			continue
		}
		return nil, fmt.Errorf("proposal materialize: insert index %d: %w", i, err)
	}
	return inserted, nil
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

// BuildPolicySnapshot delegates to policy.BuildSnapshot for callers that still use the agent name.
func BuildPolicySnapshot(in portfolio.PortfolioAssemblerInput, equityAnchor decimal.Decimal, nowNY time.Time, killEnv, killDB bool) policy.Snapshot {
	return policy.BuildSnapshot(in, equityAnchor, nowNY, killEnv, killDB)
}
