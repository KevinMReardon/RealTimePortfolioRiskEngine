package proposals

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"
	"go.uber.org/zap"

	"github.com/KevinMReardon/realtime-portfolio-risk/internal/observability"
	"github.com/KevinMReardon/realtime-portfolio-risk/internal/policy"
)

// Store is a Postgres-backed proposal + kill-switch + equity anchor helper.
type Store struct {
	pool *pgxpool.Pool
	log  *zap.Logger
}

// SetLogger sets optional structured logging for proposal lifecycle (nil disables).
func (s *Store) SetLogger(l *zap.Logger) {
	if s == nil {
		return
	}
	s.log = l
}

// NewStore returns a Store backed by pool.
func NewStore(pool *pgxpool.Pool) *Store {
	if pool == nil {
		return nil
	}
	return &Store{pool: pool}
}

// InsertProposal inserts a row in status proposed with hashes from policy evaluation.
func (s *Store) InsertProposal(ctx context.Context, p InsertParams) (Proposal, error) {
	if s == nil || s.pool == nil {
		return Proposal{}, fmt.Errorf("proposals: nil store")
	}
	payloadHash := policy.OrderPayloadHash(p.Intent)
	rec := PolicyResultRecord{
		StrictOutcome:    p.Decision.StrictOutcome,
		EffectiveOutcome: p.Decision.EffectiveOutcome,
		PolicyMode:       p.Mode,
		Violations:       p.Decision.Violations,
	}
	policyJSON, err := json.Marshal(rec)
	if err != nil {
		return Proposal{}, fmt.Errorf("proposals: marshal policy_result: %w", err)
	}

	var proposalID uuid.UUID
	row := s.pool.QueryRow(ctx, `
		INSERT INTO proposed_trades (
			portfolio_id, agent_session_id, trade_idea_index, idea_fingerprint,
			symbol, side, quantity, notional_usd, order_type, limit_price, time_in_force, client_order_id,
			rationale_snapshot, policy_result, policy_inputs_hash, policy_config_hash, payload_hash,
			status, row_version
		) VALUES (
			$1, $2, $3, $4,
			$5, $6, $7, $8, $9, $10, $11, $12,
			$13, $14, $15, $16, $17,
			'proposed', 1
		)
		RETURNING proposal_id
	`,
		p.PortfolioID,
		p.AgentSessionID,
		p.TradeIdeaIndex,
		p.IdeaFingerprint,
		policy.NormalizeSymbol(p.Intent.Symbol),
		string(p.Intent.Side),
		nullableDecimalString(p.Intent.Quantity),
		nullableDecimalString(p.Intent.NotionalUSD),
		nullableStringPtr(p.Intent.OrderType),
		nullableDecimalString(p.Intent.LimitPrice),
		nullableStringPtr(p.Intent.TimeInForce),
		p.ClientOrderID,
		p.RationaleSnapshot,
		policyJSON,
		p.Decision.InputsHash,
		p.Decision.PolicyConfigHash,
		payloadHash,
	)
	if err := row.Scan(&proposalID); err != nil {
		return Proposal{}, fmt.Errorf("proposals: insert proposed_trades: %w", err)
	}
	prop, err := s.GetByIDForPortfolio(ctx, p.PortfolioID, proposalID)
	if err != nil {
		return Proposal{}, err
	}
	observability.IncProposedTradeTransition("none", "proposed")
	if s.log != nil {
		fields := []zap.Field{
			zap.String("proposal_id", prop.ProposalID.String()),
			zap.String("portfolio_id", p.PortfolioID.String()),
			zap.String("policy_config_hash", prop.PolicyConfigHash),
			zap.String("inputs_hash", prop.PolicyInputsHash),
			zap.String("payload_hash", prop.PayloadHash),
			zap.String("effective_outcome", string(p.Decision.EffectiveOutcome)),
			zap.String("strict_outcome", string(p.Decision.StrictOutcome)),
			zap.String("policy_mode", string(p.Mode)),
		}
		if p.AgentSessionID != nil {
			fields = append(fields, zap.String("agent_session_id", p.AgentSessionID.String()))
		}
		s.log.Info("proposal_inserted", fields...)
	}
	return prop, nil
}

func nullableDecimalString(d *decimal.Decimal) interface{} {
	if d == nil {
		return nil
	}
	return d.String()
}

func nullableStringPtr(s string) interface{} {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	return s
}

// GetByIDForPortfolio loads a proposal scoped to portfolio (tenant safety).
func (s *Store) GetByIDForPortfolio(ctx context.Context, portfolioID, proposalID uuid.UUID) (Proposal, error) {
	if s == nil || s.pool == nil {
		return Proposal{}, fmt.Errorf("proposals: nil store")
	}
	row := s.pool.QueryRow(ctx, selectProposalSQL+`
		WHERE proposal_id = $1 AND portfolio_id = $2
	`, proposalID, portfolioID)
	prop, err := scanProposal(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Proposal{}, ErrProposalNotFound
		}
		return Proposal{}, err
	}
	return prop, nil
}

const selectProposalSQL = `
		SELECT
			proposal_id, portfolio_id,
			agent_session_id::text, trade_idea_index, idea_fingerprint,
			symbol, side,
			quantity::text, notional_usd::text, order_type, limit_price::text, time_in_force, client_order_id,
			rationale_snapshot, policy_result, policy_inputs_hash, policy_config_hash, payload_hash,
			status, row_version,
			created_at, updated_at,
			approved_by_user_id::text, approved_at, denied_by_user_id::text, deny_reason,
			submitted_at, broker_order_id, last_error
		FROM proposed_trades
`

// ListByPortfolio returns proposals newest first.
func (s *Store) ListByPortfolio(ctx context.Context, portfolioID uuid.UUID, filter ListFilter) ([]Proposal, error) {
	if s == nil || s.pool == nil {
		return nil, fmt.Errorf("proposals: nil store")
	}
	statusArg := interface{}(nil)
	if filter.Status != nil && strings.TrimSpace(*filter.Status) != "" {
		statusArg = strings.TrimSpace(*filter.Status)
	}
	symbolArg := interface{}(nil)
	if filter.Symbol != nil && strings.TrimSpace(*filter.Symbol) != "" {
		symbolArg = policy.NormalizeSymbol(*filter.Symbol)
	}

	rows, err := s.pool.Query(ctx, selectProposalSQL+`
		WHERE portfolio_id = $1
		  AND ($2::text IS NULL OR status = $2::text)
		  AND ($3::text IS NULL OR symbol = $3::text)
		ORDER BY created_at DESC
	`, portfolioID, statusArg, symbolArg)
	if err != nil {
		return nil, fmt.Errorf("proposals: list: %w", err)
	}
	defer rows.Close()

	var out []Proposal
	for rows.Next() {
		p, err := scanProposal(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// ApproveProposal transitions proposed → approved with optimistic lock + payload binding.
func (s *Store) ApproveProposal(ctx context.Context, p ApproveParams) error {
	if s == nil || s.pool == nil {
		return fmt.Errorf("proposals: nil store")
	}
	tag, err := s.pool.Exec(ctx, `
		UPDATE proposed_trades SET
			status = 'approved',
			approved_by_user_id = $1,
			approved_at = NOW(),
			row_version = row_version + 1,
			updated_at = NOW()
		WHERE proposal_id = $2 AND portfolio_id = $3
		  AND status = 'proposed'
		  AND payload_hash = $4
		  AND row_version = $5
	`, p.UserID, p.ProposalID, p.PortfolioID, p.PayloadHash, p.RowVersion)
	if err != nil {
		return fmt.Errorf("proposals: approve update: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrApproveConflict
	}
	observability.IncProposedTradeTransition("proposed", "approved")
	if s.log != nil {
		s.log.Info("proposal_approved",
			zap.String("proposal_id", p.ProposalID.String()),
			zap.String("portfolio_id", p.PortfolioID.String()),
			zap.String("approved_by_user_id", p.UserID.String()),
		)
	}
	return nil
}

// DenyProposal transitions proposed → rejected.
func (s *Store) DenyProposal(ctx context.Context, p DenyParams) error {
	if s == nil || s.pool == nil {
		return fmt.Errorf("proposals: nil store")
	}
	tag, err := s.pool.Exec(ctx, `
		UPDATE proposed_trades SET
			status = 'rejected',
			denied_by_user_id = $1,
			deny_reason = $2,
			row_version = row_version + 1,
			updated_at = NOW()
		WHERE proposal_id = $3 AND portfolio_id = $4
		  AND status = 'proposed'
		  AND payload_hash = $5
		  AND row_version = $6
	`, p.UserID, strings.TrimSpace(p.DenyReason), p.ProposalID, p.PortfolioID, p.PayloadHash, p.RowVersion)
	if err != nil {
		return fmt.Errorf("proposals: deny update: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrDenyConflict
	}
	observability.IncProposedTradeTransition("proposed", "rejected")
	if s.log != nil {
		s.log.Info("proposal_rejected",
			zap.String("proposal_id", p.ProposalID.String()),
			zap.String("portfolio_id", p.PortfolioID.String()),
			zap.String("denied_by_user_id", p.UserID.String()),
		)
	}
	return nil
}

// LoadEquityAnchorForPortfolioDate returns stored anchor equity for portfolio on anchorDate (calendar date).
// ok is false when no row exists (caller may use zero equity in policy snapshot).
func (s *Store) LoadEquityAnchorForPortfolioDate(ctx context.Context, portfolioID uuid.UUID, anchorDate time.Time) (equity decimal.Decimal, ok bool, err error) {
	if s == nil || s.pool == nil {
		return decimal.Zero, false, fmt.Errorf("proposals: nil store")
	}
	d := time.Date(anchorDate.Year(), anchorDate.Month(), anchorDate.Day(), 0, 0, 0, 0, time.UTC)
	var raw sql.NullString
	q := s.pool.QueryRow(ctx, `
		SELECT equity::text FROM portfolio_equity_anchor
		WHERE portfolio_id = $1 AND anchor_date = $2::date
	`, portfolioID, d)
	if err := q.Scan(&raw); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return decimal.Zero, false, nil
		}
		return decimal.Zero, false, fmt.Errorf("proposals: load equity anchor: %w", err)
	}
	if !raw.Valid || strings.TrimSpace(raw.String) == "" {
		return decimal.Zero, false, nil
	}
	dec, err := decimal.NewFromString(strings.TrimSpace(raw.String))
	if err != nil {
		return decimal.Zero, false, fmt.Errorf("proposals: parse equity anchor: %w", err)
	}
	return dec, true, nil
}

// UpsertEquityAnchor sets equity for portfolio on anchorDate (calendar date; use date parts in UTC or NY-local date).
func (s *Store) UpsertEquityAnchor(ctx context.Context, portfolioID uuid.UUID, anchorDate time.Time, equity decimal.Decimal) error {
	if s == nil || s.pool == nil {
		return fmt.Errorf("proposals: nil store")
	}
	if !equity.IsPositive() {
		return fmt.Errorf("proposals: equity must be positive")
	}
	d := time.Date(anchorDate.Year(), anchorDate.Month(), anchorDate.Day(), 0, 0, 0, 0, time.UTC)
	_, err := s.pool.Exec(ctx, `
		INSERT INTO portfolio_equity_anchor (portfolio_id, anchor_date, equity, captured_at)
		VALUES ($1, $2::date, $3, NOW())
		ON CONFLICT (portfolio_id, anchor_date) DO UPDATE SET
			equity = EXCLUDED.equity,
			captured_at = EXCLUDED.captured_at
	`, portfolioID, d, equity.String())
	if err != nil {
		return fmt.Errorf("proposals: upsert equity anchor: %w", err)
	}
	return nil
}

// AppendKillSwitchEvent records an append-only kill-switch toggle.
func (s *Store) AppendKillSwitchEvent(ctx context.Context, active bool, reason string, toggledByUserID *uuid.UUID) error {
	if s == nil || s.pool == nil {
		return fmt.Errorf("proposals: nil store")
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "(no reason)"
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO kill_switch_events (active, reason, toggled_by_user_id)
		VALUES ($1, $2, $3)
	`, active, reason, toggledByUserID)
	if err != nil {
		return fmt.Errorf("proposals: insert kill_switch_events: %w", err)
	}
	return nil
}

// KillSwitchLatestActive returns the active flag from the newest kill_switch_events row.
// If there are no rows, ok is false and active is false.
func (s *Store) KillSwitchLatestActive(ctx context.Context) (active bool, ok bool, err error) {
	if s == nil || s.pool == nil {
		return false, false, fmt.Errorf("proposals: nil store")
	}
	var a bool
	q := s.pool.QueryRow(ctx, `
		SELECT active FROM kill_switch_events
		ORDER BY event_id DESC
		LIMIT 1
	`)
	if err := q.Scan(&a); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, false, nil
		}
		return false, false, fmt.Errorf("proposals: kill switch latest: %w", err)
	}
	return a, true, nil
}

// KillSwitchInputs combines DB latest row with env kill flag (OR semantics for policy Snapshot).
func KillSwitchInputs(envKill bool, dbActive bool, dbRowPresent bool) (killSwitchEnv bool, killSwitchDB bool) {
	killSwitchEnv = envKill
	killSwitchDB = dbRowPresent && dbActive
	return killSwitchEnv, killSwitchDB
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanProposal(sc rowScanner) (Proposal, error) {
	var p Proposal
	var agentSession sql.NullString
	var tradeIdx sql.NullInt64
	var ideaFp, rationale, orderType, tif, clientID sql.NullString
	var qty, notional, lim sql.NullString
	var approvedBy, deniedBy sql.NullString
	var approvedAt, submittedAt sql.NullTime
	var brokerID, denyReason, lastErr sql.NullString
	var policyRaw []byte

	err := sc.Scan(
		&p.ProposalID, &p.PortfolioID,
		&agentSession, &tradeIdx, &ideaFp,
		&p.Symbol, &p.Side,
		&qty, &notional, &orderType, &lim, &tif, &clientID,
		&rationale, &policyRaw, &p.PolicyInputsHash, &p.PolicyConfigHash, &p.PayloadHash,
		&p.Status, &p.RowVersion,
		&p.CreatedAt, &p.UpdatedAt,
		&approvedBy, &approvedAt, &deniedBy, &denyReason,
		&submittedAt, &brokerID, &lastErr,
	)
	if err != nil {
		return Proposal{}, err
	}

	p.PolicyResult = json.RawMessage(append(json.RawMessage(nil), policyRaw...))

	aid, err := nullableUUIDString(agentSession)
	if err != nil {
		return Proposal{}, fmt.Errorf("agent_session_id: %w", err)
	}
	p.AgentSessionID = aid

	if tradeIdx.Valid {
		i := int(tradeIdx.Int64)
		p.TradeIdeaIndex = &i
	}
	p.IdeaFingerprint = nullableStringFromSQL(ideaFp)
	p.Quantity, err = nullableDecimalFromSQL(qty)
	if err != nil {
		return Proposal{}, err
	}
	p.NotionalUSD, err = nullableDecimalFromSQL(notional)
	if err != nil {
		return Proposal{}, err
	}
	p.OrderType = nullableStringFromSQL(orderType)
	p.LimitPrice, err = nullableDecimalFromSQL(lim)
	if err != nil {
		return Proposal{}, err
	}
	p.TimeInForce = nullableStringFromSQL(tif)
	p.ClientOrderID = nullableStringFromSQL(clientID)
	p.RationaleSnapshot = nullableStringFromSQL(rationale)

	p.ApprovedByUserID, err = nullableUUIDString(approvedBy)
	if err != nil {
		return Proposal{}, err
	}
	if approvedAt.Valid {
		t := approvedAt.Time.UTC()
		p.ApprovedAt = &t
	}
	p.DeniedByUserID, err = nullableUUIDString(deniedBy)
	if err != nil {
		return Proposal{}, err
	}
	p.DenyReason = nullableStringFromSQL(denyReason)
	if submittedAt.Valid {
		t := submittedAt.Time.UTC()
		p.SubmittedAt = &t
	}
	p.BrokerOrderID = nullableStringFromSQL(brokerID)
	p.LastError = nullableStringFromSQL(lastErr)

	p.CreatedAt = p.CreatedAt.UTC()
	p.UpdatedAt = p.UpdatedAt.UTC()
	return p, nil
}

func nullableUUIDString(v sql.NullString) (*uuid.UUID, error) {
	if !v.Valid || strings.TrimSpace(v.String) == "" {
		return nil, nil
	}
	id, err := uuid.Parse(strings.TrimSpace(v.String))
	if err != nil {
		return nil, err
	}
	return &id, nil
}

func nullableStringFromSQL(v sql.NullString) *string {
	if !v.Valid {
		return nil
	}
	s := strings.TrimSpace(v.String)
	if s == "" {
		return nil
	}
	return &s
}

func nullableDecimalFromSQL(v sql.NullString) (*decimal.Decimal, error) {
	if !v.Valid || strings.TrimSpace(v.String) == "" {
		return nil, nil
	}
	d, err := decimal.NewFromString(strings.TrimSpace(v.String))
	if err != nil {
		return nil, err
	}
	return &d, nil
}
