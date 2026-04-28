package events

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
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"

	"github.com/KevinMReardon/realtime-portfolio-risk/internal/domain"
	"github.com/KevinMReardon/realtime-portfolio-risk/internal/observability"
	"github.com/KevinMReardon/realtime-portfolio-risk/internal/portfolio"
)

// PostgresStore implements Repository against the v1 schema.
type PostgresStore struct {
	pool *pgxpool.Pool
}

// NewPostgresStore returns a repository backed by pool.
func NewPostgresStore(pool *pgxpool.Pool) *PostgresStore {
	return &PostgresStore{pool: pool}
}

func catalogAlpacaPtr(ns sql.NullString) *string {
	if !ns.Valid {
		return nil
	}
	s := ns.String
	return &s
}

func hasAlpacaCreds(keyID, secret sql.NullString) bool {
	return keyID.Valid && strings.TrimSpace(keyID.String) != "" &&
		secret.Valid && strings.TrimSpace(secret.String) != ""
}

func normalizeAlpacaMode(raw string) string {
	v := strings.ToLower(strings.TrimSpace(raw))
	if v == "live" {
		return "live"
	}
	return "paper"
}

// ListPortfolios returns catalog rows ordered by newest create first.
func (s *PostgresStore) ListPortfolios(ctx context.Context) ([]PortfolioCatalogEntry, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT portfolio_id, owner_user_id, name, base_currency, created_at, updated_at, alpaca_account_id, alpaca_account_mode, alpaca_sync_enabled, alpaca_key_id, alpaca_secret_key
		FROM portfolios
		ORDER BY created_at DESC, portfolio_id ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("list portfolios catalog: %w", err)
	}
	defer rows.Close()

	out := make([]PortfolioCatalogEntry, 0)
	for rows.Next() {
		var e PortfolioCatalogEntry
		var alpaca sql.NullString
		var mode string
		var keyID, secret sql.NullString
		if err := rows.Scan(&e.PortfolioID, &e.OwnerUserID, &e.Name, &e.BaseCurrency, &e.CreatedAt, &e.UpdatedAt, &alpaca, &mode, &e.AlpacaSyncEnabled, &keyID, &secret); err != nil {
			return nil, fmt.Errorf("scan portfolio catalog row: %w", err)
		}
		e.AlpacaAccountID = catalogAlpacaPtr(alpaca)
		e.AlpacaAccountMode = normalizeAlpacaMode(mode)
		e.AlpacaLinked = hasAlpacaCreds(keyID, secret)
		e.CreatedAt = e.CreatedAt.UTC()
		e.UpdatedAt = e.UpdatedAt.UTC()
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *PostgresStore) ListPortfoliosByOwner(ctx context.Context, ownerUserID uuid.UUID) ([]PortfolioCatalogEntry, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT portfolio_id, owner_user_id, name, base_currency, created_at, updated_at, alpaca_account_id, alpaca_account_mode, alpaca_sync_enabled, alpaca_key_id, alpaca_secret_key
		FROM portfolios
		WHERE owner_user_id = $1
		ORDER BY created_at DESC, portfolio_id ASC
	`, ownerUserID)
	if err != nil {
		return nil, fmt.Errorf("list portfolios by owner: %w", err)
	}
	defer rows.Close()

	out := make([]PortfolioCatalogEntry, 0)
	for rows.Next() {
		var e PortfolioCatalogEntry
		var alpaca sql.NullString
		var mode string
		var keyID, secret sql.NullString
		if err := rows.Scan(&e.PortfolioID, &e.OwnerUserID, &e.Name, &e.BaseCurrency, &e.CreatedAt, &e.UpdatedAt, &alpaca, &mode, &e.AlpacaSyncEnabled, &keyID, &secret); err != nil {
			return nil, fmt.Errorf("scan owner portfolio row: %w", err)
		}
		e.AlpacaAccountID = catalogAlpacaPtr(alpaca)
		e.AlpacaAccountMode = normalizeAlpacaMode(mode)
		e.AlpacaLinked = hasAlpacaCreds(keyID, secret)
		e.CreatedAt = e.CreatedAt.UTC()
		e.UpdatedAt = e.UpdatedAt.UTC()
		out = append(out, e)
	}
	return out, rows.Err()
}

// ListAgentBriefingEligiblePortfolios returns portfolios with a bound owner (auth-aligned scope).
func (s *PostgresStore) ListAgentBriefingEligiblePortfolios(ctx context.Context) ([]AgentBriefingEligiblePortfolio, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT portfolio_id, owner_user_id
		FROM portfolios
		WHERE owner_user_id IS NOT NULL
		ORDER BY created_at DESC, portfolio_id ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("list agent briefing eligible portfolios: %w", err)
	}
	defer rows.Close()

	out := make([]AgentBriefingEligiblePortfolio, 0)
	for rows.Next() {
		var e AgentBriefingEligiblePortfolio
		if err := rows.Scan(&e.PortfolioID, &e.OwnerUserID); err != nil {
			return nil, fmt.Errorf("scan agent briefing eligible row: %w", err)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// CreatePortfolio inserts one catalog row and returns the stored values.
func (s *PostgresStore) CreatePortfolio(ctx context.Context, portfolioID uuid.UUID, name, baseCurrency string) (PortfolioCatalogEntry, error) {
	var out PortfolioCatalogEntry
	var alpacaInsert sql.NullString
	err := s.pool.QueryRow(ctx, `
		INSERT INTO portfolios (portfolio_id, owner_user_id, name, base_currency, created_at, updated_at, alpaca_account_mode, alpaca_sync_enabled)
		VALUES ($1, NULL, $2, $3, NOW(), NOW(), 'paper', true)
		RETURNING portfolio_id, owner_user_id, name, base_currency, created_at, updated_at, alpaca_account_id, alpaca_account_mode, alpaca_sync_enabled
	`, portfolioID, name, baseCurrency).Scan(
		&out.PortfolioID,
		&out.OwnerUserID,
		&out.Name,
		&out.BaseCurrency,
		&out.CreatedAt,
		&out.UpdatedAt,
		&alpacaInsert,
		&out.AlpacaAccountMode,
		&out.AlpacaSyncEnabled,
	)
	if err != nil {
		return PortfolioCatalogEntry{}, fmt.Errorf("create portfolio catalog row: %w", err)
	}
	out.AlpacaAccountID = catalogAlpacaPtr(alpacaInsert)
	out.AlpacaAccountMode = normalizeAlpacaMode(out.AlpacaAccountMode)
	out.AlpacaLinked = false
	out.CreatedAt = out.CreatedAt.UTC()
	out.UpdatedAt = out.UpdatedAt.UTC()
	return out, nil
}

func (s *PostgresStore) CreatePortfolioForOwner(ctx context.Context, ownerUserID, portfolioID uuid.UUID, name, baseCurrency string) (PortfolioCatalogEntry, error) {
	var out PortfolioCatalogEntry
	var alpacaOwnerInsert sql.NullString
	err := s.pool.QueryRow(ctx, `
		INSERT INTO portfolios (portfolio_id, owner_user_id, name, base_currency, created_at, updated_at, alpaca_account_mode, alpaca_sync_enabled)
		VALUES ($1, $2, $3, $4, NOW(), NOW(), 'paper', true)
		RETURNING portfolio_id, owner_user_id, name, base_currency, created_at, updated_at, alpaca_account_id, alpaca_account_mode, alpaca_sync_enabled
	`, portfolioID, ownerUserID, name, baseCurrency).Scan(
		&out.PortfolioID,
		&out.OwnerUserID,
		&out.Name,
		&out.BaseCurrency,
		&out.CreatedAt,
		&out.UpdatedAt,
		&alpacaOwnerInsert,
		&out.AlpacaAccountMode,
		&out.AlpacaSyncEnabled,
	)
	if err != nil {
		return PortfolioCatalogEntry{}, fmt.Errorf("create owner portfolio row: %w", err)
	}
	out.AlpacaAccountID = catalogAlpacaPtr(alpacaOwnerInsert)
	out.AlpacaAccountMode = normalizeAlpacaMode(out.AlpacaAccountMode)
	out.AlpacaLinked = false
	out.CreatedAt = out.CreatedAt.UTC()
	out.UpdatedAt = out.UpdatedAt.UTC()
	return out, nil
}

// ListAlpacaSyncTargets returns portfolios configured for Alpaca fill polling.
func (s *PostgresStore) ListAlpacaSyncTargets(ctx context.Context) ([]AlpacaSyncTarget, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT portfolio_id, alpaca_account_mode, alpaca_account_id, alpaca_sync_enabled, alpaca_key_id, alpaca_secret_key, alpaca_base_url
		FROM portfolios
		WHERE alpaca_sync_enabled = true
		  AND COALESCE(NULLIF(TRIM(alpaca_key_id), ''), '') <> ''
		  AND COALESCE(NULLIF(TRIM(alpaca_secret_key), ''), '') <> ''
		ORDER BY created_at ASC, portfolio_id ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("list alpaca sync targets: %w", err)
	}
	defer rows.Close()

	out := make([]AlpacaSyncTarget, 0)
	for rows.Next() {
		var row AlpacaSyncTarget
		var aid, keyID, secret, baseURL sql.NullString
		if err := rows.Scan(&row.PortfolioID, &row.AlpacaAccountMode, &aid, &row.AlpacaSyncEnabled, &keyID, &secret, &baseURL); err != nil {
			return nil, fmt.Errorf("scan alpaca sync target: %w", err)
		}
		row.AlpacaAccountMode = normalizeAlpacaMode(row.AlpacaAccountMode)
		row.AlpacaAccountID = catalogAlpacaPtr(aid)
		row.AlpacaKeyID = strings.TrimSpace(keyID.String)
		row.AlpacaSecretKey = strings.TrimSpace(secret.String)
		row.AlpacaBaseURL = strings.TrimSpace(baseURL.String)
		out = append(out, row)
	}
	return out, rows.Err()
}

func (s *PostgresStore) UpsertPortfolioAlpacaLink(ctx context.Context, portfolioID uuid.UUID, link AlpacaPortfolioLinkInput) error {
	mode := normalizeAlpacaMode(link.AlpacaAccountMode)
	keyID := strings.TrimSpace(link.AlpacaKeyID)
	secret := strings.TrimSpace(link.AlpacaSecretKey)
	baseURL := strings.TrimSpace(link.AlpacaBaseURL)
	if keyID == "" || secret == "" {
		return fmt.Errorf("alpaca credentials are required")
	}
	tag, err := s.pool.Exec(ctx, `
		UPDATE portfolios
		SET alpaca_account_mode = $2,
		    alpaca_sync_enabled = $3,
		    alpaca_key_id = $4,
		    alpaca_secret_key = $5,
		    alpaca_base_url = NULLIF($6, ''),
		    updated_at = NOW()
		WHERE portfolio_id = $1
	`, portfolioID, mode, link.AlpacaSyncEnabled, keyID, secret, baseURL)
	if err != nil {
		return fmt.Errorf("upsert portfolio alpaca link: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("portfolio not found for alpaca link")
	}
	return nil
}

// SetPortfolioAlpacaAccountID records the Alpaca brokerage account id (GET /v2/account) for this catalog row.
func (s *PostgresStore) SetPortfolioAlpacaAccountID(ctx context.Context, portfolioID uuid.UUID, alpacaAccountID string) error {
	id := strings.TrimSpace(alpacaAccountID)
	if id == "" {
		return fmt.Errorf("empty alpaca account id")
	}
	tag, err := s.pool.Exec(ctx, `
		UPDATE portfolios SET alpaca_account_id = $2, updated_at = NOW() WHERE portfolio_id = $1
	`, portfolioID, id)
	if err != nil {
		return fmt.Errorf("set portfolio alpaca_account_id: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("portfolio not found for alpaca link")
	}
	return nil
}

func (s *PostgresStore) PortfolioOwnedByUser(ctx context.Context, portfolioID, ownerUserID uuid.UUID) (bool, error) {
	var ok bool
	err := s.pool.QueryRow(ctx, `
		SELECT EXISTS(
			SELECT 1
			FROM portfolios
			WHERE portfolio_id = $1 AND owner_user_id = $2
		)
	`, portfolioID, ownerUserID).Scan(&ok)
	if err != nil {
		return false, fmt.Errorf("check portfolio owner: %w", err)
	}
	return ok, nil
}

func (s *PostgresStore) CreateUser(ctx context.Context, user UserAccount) (UserAccount, error) {
	var out UserAccount
	err := s.pool.QueryRow(ctx, `
		INSERT INTO users (user_id, display_name, work_email, password_hash, created_at, updated_at)
		VALUES ($1, $2, $3, $4, NOW(), NOW())
		RETURNING user_id, display_name, work_email, password_hash, created_at, updated_at
	`, user.UserID, user.DisplayName, strings.ToLower(strings.TrimSpace(user.WorkEmail)), user.PasswordHash).Scan(
		&out.UserID,
		&out.DisplayName,
		&out.WorkEmail,
		&out.PasswordHash,
		&out.CreatedAt,
		&out.UpdatedAt,
	)
	if err != nil {
		return UserAccount{}, fmt.Errorf("create user: %w", err)
	}
	out.CreatedAt = out.CreatedAt.UTC()
	out.UpdatedAt = out.UpdatedAt.UTC()
	return out, nil
}

func (s *PostgresStore) GetUserByEmail(ctx context.Context, workEmail string) (UserAccount, bool, error) {
	var out UserAccount
	err := s.pool.QueryRow(ctx, `
		SELECT user_id, display_name, work_email, password_hash, created_at, updated_at
		FROM users
		WHERE LOWER(work_email) = LOWER($1)
	`, strings.ToLower(strings.TrimSpace(workEmail))).Scan(
		&out.UserID,
		&out.DisplayName,
		&out.WorkEmail,
		&out.PasswordHash,
		&out.CreatedAt,
		&out.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return UserAccount{}, false, nil
	}
	if err != nil {
		return UserAccount{}, false, fmt.Errorf("get user by email: %w", err)
	}
	out.CreatedAt = out.CreatedAt.UTC()
	out.UpdatedAt = out.UpdatedAt.UTC()
	return out, true, nil
}

func (s *PostgresStore) GetUserByID(ctx context.Context, userID uuid.UUID) (UserAccount, bool, error) {
	var out UserAccount
	err := s.pool.QueryRow(ctx, `
		SELECT user_id, display_name, work_email, password_hash, created_at, updated_at
		FROM users
		WHERE user_id = $1
	`, userID).Scan(
		&out.UserID,
		&out.DisplayName,
		&out.WorkEmail,
		&out.PasswordHash,
		&out.CreatedAt,
		&out.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return UserAccount{}, false, nil
	}
	if err != nil {
		return UserAccount{}, false, fmt.Errorf("get user by id: %w", err)
	}
	out.CreatedAt = out.CreatedAt.UTC()
	out.UpdatedAt = out.UpdatedAt.UTC()
	return out, true, nil
}

func (s *PostgresStore) CreateSession(ctx context.Context, session UserSession) (UserSession, error) {
	var out UserSession
	err := s.pool.QueryRow(ctx, `
		INSERT INTO user_sessions (session_id, user_id, expires_at, revoked_at, created_at)
		VALUES ($1, $2, $3, NULL, NOW())
		RETURNING session_id, user_id, expires_at, revoked_at, created_at
	`, session.SessionID, session.UserID, session.ExpiresAt.UTC()).Scan(
		&out.SessionID,
		&out.UserID,
		&out.ExpiresAt,
		&out.RevokedAt,
		&out.CreatedAt,
	)
	if err != nil {
		return UserSession{}, fmt.Errorf("create session: %w", err)
	}
	out.ExpiresAt = out.ExpiresAt.UTC()
	out.CreatedAt = out.CreatedAt.UTC()
	return out, nil
}

func (s *PostgresStore) GetSessionByID(ctx context.Context, sessionID uuid.UUID) (UserSession, bool, error) {
	var out UserSession
	err := s.pool.QueryRow(ctx, `
		SELECT session_id, user_id, expires_at, revoked_at, created_at
		FROM user_sessions
		WHERE session_id = $1
	`, sessionID).Scan(
		&out.SessionID,
		&out.UserID,
		&out.ExpiresAt,
		&out.RevokedAt,
		&out.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return UserSession{}, false, nil
	}
	if err != nil {
		return UserSession{}, false, fmt.Errorf("get session: %w", err)
	}
	out.ExpiresAt = out.ExpiresAt.UTC()
	out.CreatedAt = out.CreatedAt.UTC()
	if out.RevokedAt != nil {
		v := out.RevokedAt.UTC()
		out.RevokedAt = &v
	}
	return out, true, nil
}

func (s *PostgresStore) RevokeSession(ctx context.Context, sessionID uuid.UUID) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE user_sessions
		SET revoked_at = NOW()
		WHERE session_id = $1
	`, sessionID)
	if err != nil {
		return fmt.Errorf("revoke session: %w", err)
	}
	return nil
}

func (s *PostgresStore) CreateAgentSession(ctx context.Context, session AgentSession) (AgentSession, error) {
	var requestedByUserID any
	if session.RequestedByUserID != nil {
		requestedByUserID = *session.RequestedByUserID
	}
	var temperature any
	if session.Temperature != nil {
		temperature = session.Temperature.String()
	}
	var maxTokens any
	if session.MaxTokens != nil {
		maxTokens = *session.MaxTokens
	}
	var inputTokens any
	if session.InputTokens != nil {
		inputTokens = *session.InputTokens
	}
	var outputTokens any
	if session.OutputTokens != nil {
		outputTokens = *session.OutputTokens
	}
	var estimatedCostUSD any
	if session.EstimatedCostUSD != nil {
		estimatedCostUSD = session.EstimatedCostUSD.String()
	}
	var errorCode any
	if session.ErrorCode != nil {
		errorCode = *session.ErrorCode
	}
	var errorMessage any
	if session.ErrorMessage != nil {
		errorMessage = *session.ErrorMessage
	}
	var startedAt any
	if session.StartedAt != nil {
		startedAt = session.StartedAt.UTC()
	}
	var completedAt any
	if session.CompletedAt != nil {
		completedAt = session.CompletedAt.UTC()
	}
	out, err := s.scanAgentSession(s.pool.QueryRow(ctx, `
		INSERT INTO agent_sessions (
			session_id, portfolio_id, requested_by_user_id, trigger_source, run_date, status,
			provider, model, temperature, max_tokens, system_prompt, user_prompt, response_raw,
			response_validated, validation_errors, input_tokens, output_tokens, tool_call_count,
			estimated_cost_usd, error_code, error_message, started_at, completed_at, created_at, updated_at
		)
		VALUES (
			$1, $2, $3, $4, $5::date, $6,
			$7, $8, $9::numeric, $10, $11, $12::jsonb, $13::jsonb,
			$14::jsonb, $15::jsonb, $16, $17, $18,
			$19::numeric, $20, $21, $22, $23, NOW(), NOW()
		)
		RETURNING
			session_id, portfolio_id, requested_by_user_id, trigger_source, run_date, status,
			provider, model, temperature::text, max_tokens, system_prompt, user_prompt, response_raw,
			response_validated, validation_errors, input_tokens, output_tokens, tool_call_count,
			estimated_cost_usd::text, error_code, error_message, started_at, completed_at, created_at, updated_at
	`,
		session.SessionID,
		session.PortfolioID,
		requestedByUserID,
		session.TriggerSource,
		session.RunDate.UTC(),
		session.Status,
		session.Provider,
		session.Model,
		temperature,
		maxTokens,
		session.SystemPrompt,
		[]byte(session.UserPrompt),
		[]byte(session.ResponseRaw),
		[]byte(session.ResponseValidated),
		[]byte(session.ValidationErrors),
		inputTokens,
		outputTokens,
		session.ToolCallCount,
		estimatedCostUSD,
		errorCode,
		errorMessage,
		startedAt,
		completedAt,
	))
	if err != nil {
		return AgentSession{}, fmt.Errorf("create agent session: %w", err)
	}
	return out, nil
}

func (s *PostgresStore) MarkAgentSessionRunning(ctx context.Context, sessionID uuid.UUID, startedAt time.Time) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE agent_sessions
		SET status = 'running',
		    started_at = $2,
		    updated_at = NOW()
		WHERE session_id = $1
	`, sessionID, startedAt.UTC())
	if err != nil {
		return fmt.Errorf("mark agent session running: %w", err)
	}
	return nil
}

func (s *PostgresStore) AppendAgentSessionToolCall(ctx context.Context, call AgentSessionToolCall) (AgentSessionToolCall, error) {
	var latencyMS any
	if call.LatencyMS != nil {
		latencyMS = *call.LatencyMS
	}
	var errorMessage any
	if call.ErrorMessage != nil {
		errorMessage = *call.ErrorMessage
	}
	out, err := s.scanAgentSessionToolCall(s.pool.QueryRow(ctx, `
		INSERT INTO agent_session_tool_calls (
			session_id, seq_no, tool_name, tool_input, tool_output, latency_ms, success, error_message, created_at
		)
		VALUES ($1, $2, $3, $4::jsonb, $5::jsonb, $6, $7, $8, NOW())
		RETURNING id, session_id, seq_no, tool_name, tool_input, tool_output, latency_ms, success, error_message, created_at
	`,
		call.SessionID,
		call.SeqNo,
		call.ToolName,
		[]byte(call.ToolInput),
		[]byte(call.ToolOutput),
		latencyMS,
		call.Success,
		errorMessage,
	))
	if err != nil {
		return AgentSessionToolCall{}, fmt.Errorf("append agent session tool call: %w", err)
	}
	return out, nil
}

func (s *PostgresStore) CompleteAgentSessionSuccess(ctx context.Context, session AgentSession) error {
	var responseRaw any = []byte(session.ResponseRaw)
	var responseValidated any = []byte(session.ResponseValidated)
	var validationErrors any = []byte(session.ValidationErrors)
	var inputTokens any
	if session.InputTokens != nil {
		inputTokens = *session.InputTokens
	}
	var outputTokens any
	if session.OutputTokens != nil {
		outputTokens = *session.OutputTokens
	}
	var estimatedCostUSD any
	if session.EstimatedCostUSD != nil {
		estimatedCostUSD = session.EstimatedCostUSD.String()
	}
	var completedAt any = time.Now().UTC()
	if session.CompletedAt != nil {
		completedAt = session.CompletedAt.UTC()
	}
	_, err := s.pool.Exec(ctx, `
		UPDATE agent_sessions
		SET status = 'succeeded',
		    response_raw = $2::jsonb,
		    response_validated = $3::jsonb,
		    validation_errors = $4::jsonb,
		    input_tokens = $5,
		    output_tokens = $6,
		    tool_call_count = $7,
		    estimated_cost_usd = $8::numeric,
		    error_code = NULL,
		    error_message = NULL,
		    completed_at = $9,
		    updated_at = NOW()
		WHERE session_id = $1
	`,
		session.SessionID,
		responseRaw,
		responseValidated,
		validationErrors,
		inputTokens,
		outputTokens,
		session.ToolCallCount,
		estimatedCostUSD,
		completedAt,
	)
	if err != nil {
		return fmt.Errorf("complete agent session success: %w", err)
	}
	return nil
}

func (s *PostgresStore) CompleteAgentSessionFailure(ctx context.Context, session AgentSession) error {
	var responseRaw any = []byte(session.ResponseRaw)
	var responseValidated any = []byte(session.ResponseValidated)
	var validationErrors any = []byte(session.ValidationErrors)
	var inputTokens any
	if session.InputTokens != nil {
		inputTokens = *session.InputTokens
	}
	var outputTokens any
	if session.OutputTokens != nil {
		outputTokens = *session.OutputTokens
	}
	var estimatedCostUSD any
	if session.EstimatedCostUSD != nil {
		estimatedCostUSD = session.EstimatedCostUSD.String()
	}
	var errorCode any
	if session.ErrorCode != nil {
		errorCode = *session.ErrorCode
	}
	var errorMessage any
	if session.ErrorMessage != nil {
		errorMessage = *session.ErrorMessage
	}
	status := strings.TrimSpace(session.Status)
	if status == "" || status == "succeeded" || status == "running" || status == "queued" {
		status = "failed"
	}
	var completedAt any = time.Now().UTC()
	if session.CompletedAt != nil {
		completedAt = session.CompletedAt.UTC()
	}
	_, err := s.pool.Exec(ctx, `
		UPDATE agent_sessions
		SET status = $2,
		    response_raw = $3::jsonb,
		    response_validated = $4::jsonb,
		    validation_errors = $5::jsonb,
		    input_tokens = $6,
		    output_tokens = $7,
		    tool_call_count = $8,
		    estimated_cost_usd = $9::numeric,
		    error_code = $10,
		    error_message = $11,
		    completed_at = $12,
		    updated_at = NOW()
		WHERE session_id = $1
	`,
		session.SessionID,
		status,
		responseRaw,
		responseValidated,
		validationErrors,
		inputTokens,
		outputTokens,
		session.ToolCallCount,
		estimatedCostUSD,
		errorCode,
		errorMessage,
		completedAt,
	)
	if err != nil {
		return fmt.Errorf("complete agent session failure: %w", err)
	}
	return nil
}

func (s *PostgresStore) GetLatestAgentSessionForPortfolio(ctx context.Context, portfolioID uuid.UUID) (AgentSession, bool, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT
			session_id, portfolio_id, requested_by_user_id, trigger_source, run_date, status,
			provider, model, temperature::text, max_tokens, system_prompt, user_prompt, response_raw,
			response_validated, validation_errors, input_tokens, output_tokens, tool_call_count,
			estimated_cost_usd::text, error_code, error_message, started_at, completed_at, created_at, updated_at
		FROM agent_sessions
		WHERE portfolio_id = $1
		ORDER BY created_at DESC, session_id DESC
		LIMIT 1
	`, portfolioID)
	out, err := s.scanAgentSession(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return AgentSession{}, false, nil
	}
	if err != nil {
		return AgentSession{}, false, fmt.Errorf("get latest agent session for portfolio: %w", err)
	}
	return out, true, nil
}

func (s *PostgresStore) ListAgentSessionsForPortfolio(ctx context.Context, portfolioID uuid.UUID, filter AgentSessionListFilter) ([]AgentSession, error) {
	limit := filter.Limit
	if limit <= 0 {
		limit = 50
	}
	offset := filter.Offset
	if offset < 0 {
		offset = 0
	}
	statuses := make([]string, 0, len(filter.Statuses))
	for _, status := range filter.Statuses {
		s := strings.TrimSpace(status)
		if s == "" {
			continue
		}
		statuses = append(statuses, s)
	}
	rows, err := s.pool.Query(ctx, `
		SELECT
			session_id, portfolio_id, requested_by_user_id, trigger_source, run_date, status,
			provider, model, temperature::text, max_tokens, system_prompt, user_prompt, response_raw,
			response_validated, validation_errors, input_tokens, output_tokens, tool_call_count,
			estimated_cost_usd::text, error_code, error_message, started_at, completed_at, created_at, updated_at
		FROM agent_sessions
		WHERE portfolio_id = $1
		  AND (COALESCE(array_length($2::text[], 1), 0) = 0 OR status = ANY($2::text[]))
		ORDER BY created_at DESC, session_id DESC
		LIMIT $3 OFFSET $4
	`, portfolioID, statuses, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("list agent sessions for portfolio: %w", err)
	}
	defer rows.Close()
	out := make([]AgentSession, 0)
	for rows.Next() {
		row, err := s.scanAgentSession(rows)
		if err != nil {
			return nil, fmt.Errorf("scan agent session list row: %w", err)
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate agent sessions list: %w", err)
	}
	return out, nil
}

func (s *PostgresStore) GetAgentSessionReplayByID(ctx context.Context, sessionID uuid.UUID) (AgentSessionReplay, bool, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT
			session_id, portfolio_id, requested_by_user_id, trigger_source, run_date, status,
			provider, model, temperature::text, max_tokens, system_prompt, user_prompt, response_raw,
			response_validated, validation_errors, input_tokens, output_tokens, tool_call_count,
			estimated_cost_usd::text, error_code, error_message, started_at, completed_at, created_at, updated_at
		FROM agent_sessions
		WHERE session_id = $1
	`, sessionID)
	session, err := s.scanAgentSession(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return AgentSessionReplay{}, false, nil
	}
	if err != nil {
		return AgentSessionReplay{}, false, fmt.Errorf("get agent session replay session: %w", err)
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id, session_id, seq_no, tool_name, tool_input, tool_output, latency_ms, success, error_message, created_at
		FROM agent_session_tool_calls
		WHERE session_id = $1
		ORDER BY seq_no ASC, id ASC
	`, sessionID)
	if err != nil {
		return AgentSessionReplay{}, false, fmt.Errorf("get agent session replay tool calls: %w", err)
	}
	defer rows.Close()
	toolCalls := make([]AgentSessionToolCall, 0)
	for rows.Next() {
		call, err := s.scanAgentSessionToolCall(rows)
		if err != nil {
			return AgentSessionReplay{}, false, fmt.Errorf("scan agent session replay tool call: %w", err)
		}
		toolCalls = append(toolCalls, call)
	}
	if err := rows.Err(); err != nil {
		return AgentSessionReplay{}, false, fmt.Errorf("iterate agent session replay tool calls: %w", err)
	}
	return AgentSessionReplay{
		Session:   session,
		ToolCalls: toolCalls,
	}, true, nil
}

func (s *PostgresStore) LoadPriceFeedWatchlist(ctx context.Context) ([]string, bool, error) {
	var raw []byte
	err := s.pool.QueryRow(ctx, `
		SELECT setting_value
		FROM app_settings
		WHERE setting_key = 'price_feed_watchlist'
	`).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("load price feed watchlist: %w", err)
	}
	var watchlist []string
	if err := json.Unmarshal(raw, &watchlist); err != nil {
		return nil, false, fmt.Errorf("decode price feed watchlist: %w", err)
	}
	return normalizeWatchlistSymbols(watchlist), true, nil
}

func (s *PostgresStore) UpsertPriceFeedWatchlist(ctx context.Context, watchlist []string) error {
	body, err := json.Marshal(normalizeWatchlistSymbols(watchlist))
	if err != nil {
		return fmt.Errorf("encode price feed watchlist: %w", err)
	}
	_, err = s.pool.Exec(ctx, `
		INSERT INTO app_settings (setting_key, setting_value, updated_at)
		VALUES ('price_feed_watchlist', $1::jsonb, NOW())
		ON CONFLICT (setting_key) DO UPDATE SET
			setting_value = EXCLUDED.setting_value,
			updated_at = NOW()
	`, body)
	if err != nil {
		return fmt.Errorf("upsert price feed watchlist: %w", err)
	}
	return nil
}

func normalizeWatchlistSymbols(symbols []string) []string {
	out := make([]string, 0, len(symbols))
	seen := make(map[string]struct{}, len(symbols))
	for _, s := range symbols {
		v := strings.ToUpper(strings.TrimSpace(s))
		if v == "" {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}

func nullableInt64PtrToInt(v sql.NullInt64) *int {
	if !v.Valid {
		return nil
	}
	n := int(v.Int64)
	return &n
}

func nullableStringPtr(v sql.NullString) *string {
	if !v.Valid {
		return nil
	}
	s := v.String
	return &s
}

func nullableUUIDPtr(v sql.NullString) (*uuid.UUID, error) {
	if !v.Valid || strings.TrimSpace(v.String) == "" {
		return nil, nil
	}
	id, err := uuid.Parse(v.String)
	if err != nil {
		return nil, err
	}
	return &id, nil
}

func nullableDecimalPtr(v sql.NullString) (*decimal.Decimal, error) {
	if !v.Valid || strings.TrimSpace(v.String) == "" {
		return nil, nil
	}
	d, err := decimal.NewFromString(v.String)
	if err != nil {
		return nil, err
	}
	return &d, nil
}

func rawJSONPtr(v []byte) json.RawMessage {
	if len(v) == 0 {
		return nil
	}
	return append(json.RawMessage(nil), v...)
}

func (s *PostgresStore) scanAgentSession(scannable interface{ Scan(dest ...any) error }) (AgentSession, error) {
	var out AgentSession
	var requestedByUserID sql.NullString
	var runDate time.Time
	var temperature sql.NullString
	var maxTokens sql.NullInt64
	var responseRaw []byte
	var responseValidated []byte
	var validationErrors []byte
	var inputTokens sql.NullInt64
	var outputTokens sql.NullInt64
	var estimatedCostUSD sql.NullString
	var errorCode sql.NullString
	var errorMessage sql.NullString
	var startedAt sql.NullTime
	var completedAt sql.NullTime
	if err := scannable.Scan(
		&out.SessionID,
		&out.PortfolioID,
		&requestedByUserID,
		&out.TriggerSource,
		&runDate,
		&out.Status,
		&out.Provider,
		&out.Model,
		&temperature,
		&maxTokens,
		&out.SystemPrompt,
		&out.UserPrompt,
		&responseRaw,
		&responseValidated,
		&validationErrors,
		&inputTokens,
		&outputTokens,
		&out.ToolCallCount,
		&estimatedCostUSD,
		&errorCode,
		&errorMessage,
		&startedAt,
		&completedAt,
		&out.CreatedAt,
		&out.UpdatedAt,
	); err != nil {
		return AgentSession{}, err
	}
	requestedBy, err := nullableUUIDPtr(requestedByUserID)
	if err != nil {
		return AgentSession{}, fmt.Errorf("parse requested_by_user_id: %w", err)
	}
	out.RequestedByUserID = requestedBy
	out.RunDate = runDate.UTC()
	out.RunDate = time.Date(out.RunDate.Year(), out.RunDate.Month(), out.RunDate.Day(), 0, 0, 0, 0, time.UTC)
	out.CreatedAt = out.CreatedAt.UTC()
	out.UpdatedAt = out.UpdatedAt.UTC()
	if startedAt.Valid {
		t := startedAt.Time.UTC()
		out.StartedAt = &t
	}
	if completedAt.Valid {
		t := completedAt.Time.UTC()
		out.CompletedAt = &t
	}
	out.Temperature, err = nullableDecimalPtr(temperature)
	if err != nil {
		return AgentSession{}, fmt.Errorf("parse temperature: %w", err)
	}
	out.MaxTokens = nullableInt64PtrToInt(maxTokens)
	out.ResponseRaw = rawJSONPtr(responseRaw)
	out.ResponseValidated = rawJSONPtr(responseValidated)
	out.ValidationErrors = rawJSONPtr(validationErrors)
	out.InputTokens = nullableInt64PtrToInt(inputTokens)
	out.OutputTokens = nullableInt64PtrToInt(outputTokens)
	out.EstimatedCostUSD, err = nullableDecimalPtr(estimatedCostUSD)
	if err != nil {
		return AgentSession{}, fmt.Errorf("parse estimated_cost_usd: %w", err)
	}
	out.ErrorCode = nullableStringPtr(errorCode)
	out.ErrorMessage = nullableStringPtr(errorMessage)
	return out, nil
}

func (s *PostgresStore) scanAgentSessionToolCall(scannable interface{ Scan(dest ...any) error }) (AgentSessionToolCall, error) {
	var out AgentSessionToolCall
	var toolOutput []byte
	var latencyMS sql.NullInt64
	var errorMessage sql.NullString
	if err := scannable.Scan(
		&out.ID,
		&out.SessionID,
		&out.SeqNo,
		&out.ToolName,
		&out.ToolInput,
		&toolOutput,
		&latencyMS,
		&out.Success,
		&errorMessage,
		&out.CreatedAt,
	); err != nil {
		return AgentSessionToolCall{}, err
	}
	out.ToolOutput = rawJSONPtr(toolOutput)
	out.LatencyMS = nullableInt64PtrToInt(latencyMS)
	out.ErrorMessage = nullableStringPtr(errorMessage)
	out.CreatedAt = out.CreatedAt.UTC()
	return out, nil
}

// Append inserts a canonical event in one transaction. On idempotency conflict, returns the stored event_id.
func (s *PostgresStore) Append(ctx context.Context, event domain.EventEnvelope) (AppendResult, error) {
	portfolioUUID, err := uuid.Parse(event.PortfolioID)
	if err != nil {
		return AppendResult{}, fmt.Errorf("%w: portfolio_id must be UUID: %v", domain.ErrValidation, err)
	}

	payload := []byte(event.Payload)
	if len(payload) == 0 {
		payload = []byte("{}")
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return AppendResult{}, fmt.Errorf("append begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var inserted uuid.UUID
	err = tx.QueryRow(ctx, `
		INSERT INTO events (event_id, portfolio_id, event_time, event_type, idempotency_key, source, payload)
		VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb)
		ON CONFLICT (portfolio_id, idempotency_key) DO NOTHING
		RETURNING event_id
	`,
		event.EventID,
		portfolioUUID,
		event.EventTime,
		string(event.EventType),
		event.IdempotencyKey,
		event.Source,
		payload,
	).Scan(&inserted)

	if errors.Is(err, pgx.ErrNoRows) {
		err = tx.QueryRow(ctx, `
			SELECT event_id FROM events WHERE portfolio_id = $1 AND idempotency_key = $2
		`, portfolioUUID, event.IdempotencyKey).Scan(&inserted)
		if err != nil {
			return AppendResult{}, fmt.Errorf("append idempotent lookup: %w", err)
		}
		if err := tx.Commit(ctx); err != nil {
			return AppendResult{}, fmt.Errorf("append commit: %w", err)
		}
		return AppendResult{EventID: inserted, Inserted: false}, nil
	}
	if err != nil {
		return AppendResult{}, fmt.Errorf("append event: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return AppendResult{}, fmt.Errorf("append commit: %w", err)
	}
	observability.IncEventsAppended()
	return AppendResult{EventID: inserted, Inserted: true}, nil
}

// ListPortfolioIDs returns distinct portfolio keys present in the event stream.
func (s *PostgresStore) ListPortfolioIDs(ctx context.Context) ([]uuid.UUID, error) {
	rows, err := s.pool.Query(ctx, `SELECT DISTINCT portfolio_id FROM events`)
	if err != nil {
		return nil, fmt.Errorf("list portfolios: %w", err)
	}
	defer rows.Close()

	var out []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan portfolio_id: %w", err)
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// ListPortfolioIDsNotIn returns distinct portfolio_id values not in exclude (empty exclude = no filter).
func (s *PostgresStore) ListPortfolioIDsNotIn(ctx context.Context, exclude []uuid.UUID) ([]uuid.UUID, error) {
	if len(exclude) == 0 {
		return s.ListPortfolioIDs(ctx)
	}
	rows, err := s.pool.Query(ctx, `
		SELECT DISTINCT portfolio_id FROM events
		WHERE NOT (portfolio_id = ANY($1::uuid[]))
	`, exclude)
	if err != nil {
		return nil, fmt.Errorf("list portfolios not in exclude set: %w", err)
	}
	defer rows.Close()

	var out []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan portfolio_id: %w", err)
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// FetchAllForPortfolio returns all events for a partition in apply order (full replay; no watermark).
func (s *PostgresStore) FetchAllForPortfolio(ctx context.Context, portfolioID uuid.UUID) ([]domain.EventEnvelope, error) {
	return s.queryEvents(ctx, `
		SELECT event_id, portfolio_id, event_time, event_type, idempotency_key, source, payload
		FROM events
		WHERE portfolio_id = $1
		ORDER BY event_time ASC, event_id ASC
	`, portfolioID)
}

// FetchAfter returns up to limit events strictly after the cursor, in apply order (unbounded by watermark).
func (s *PostgresStore) FetchAfter(ctx context.Context, portfolioID uuid.UUID, after Cursor, limit int) ([]domain.EventEnvelope, error) {
	if limit <= 0 {
		limit = 100
	}
	if after.IsZero() {
		return s.queryEventsWithLimit(ctx, `
			SELECT event_id, portfolio_id, event_time, event_type, idempotency_key, source, payload
			FROM events
			WHERE portfolio_id = $1
			ORDER BY event_time ASC, event_id ASC
			LIMIT $2
		`, portfolioID, limit)
	}
	return s.queryEventsWithLimit(ctx, `
		SELECT event_id, portfolio_id, event_time, event_type, idempotency_key, source, payload
		FROM events
		WHERE portfolio_id = $1
		  AND (event_time > $2 OR (event_time = $2 AND event_id > $3))
		ORDER BY event_time ASC, event_id ASC
		LIMIT $4
	`, portfolioID, after.Time, after.ID, limit)
}

// ListRecentEventsForPortfolio returns the last limit events for the partition in chronological order
// (oldest first): newest rows are selected with DESC ordering, then reversed for stable summaries.
func (s *PostgresStore) ListRecentEventsForPortfolio(ctx context.Context, portfolioID uuid.UUID, limit int) ([]domain.EventEnvelope, error) {
	if limit <= 0 {
		limit = 20
	}
	return s.queryEventsWithLimit(ctx, `
		SELECT event_id, portfolio_id, event_time, event_type, idempotency_key, source, payload
		FROM (
			SELECT event_id, portfolio_id, event_time, event_type, idempotency_key, source, payload
			FROM events
			WHERE portfolio_id = $1
			ORDER BY event_time DESC, event_id DESC
			LIMIT $2
		) t
		ORDER BY event_time ASC, event_id ASC
	`, portfolioID, limit)
}

// MaxEventTime returns MAX(event_time) for the partition, or zero time if there are no events.
// (PostgreSQL -infinity cannot be scanned into Go time.Time; empty partitions are common for price shards.)
func (s *PostgresStore) MaxEventTime(ctx context.Context, portfolioID uuid.UUID) (time.Time, error) {
	var nt sql.NullTime
	err := s.pool.QueryRow(ctx, `
		SELECT MAX(event_time) FROM events WHERE portfolio_id = $1
	`, portfolioID).Scan(&nt)
	if err != nil {
		return time.Time{}, fmt.Errorf("max event time: %w", err)
	}
	if !nt.Valid {
		return time.Time{}, nil
	}
	return nt.Time.UTC(), nil
}

// LoadProjectionCursor returns the last applied ordering key. No row means never applied:
// returns Cursor{} (IsZero), matching migration 000003_projection_cursor (no sentinel row).
func (s *PostgresStore) LoadProjectionCursor(ctx context.Context, portfolioID uuid.UUID) (Cursor, error) {
	var t Cursor
	err := s.pool.QueryRow(ctx, `
		SELECT last_event_time, last_event_id FROM projection_cursor WHERE portfolio_id = $1
	`, portfolioID).Scan(&t.Time, &t.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Cursor{}, nil
	}
	if err != nil {
		return Cursor{}, fmt.Errorf("load projection cursor: %w", err)
	}
	return t, nil
}

// LoadPortfolioAssemblerInput reads projection rows plus marks for open symbols and returns
// the pure assembler input for GET /v1/portfolios/{id}. found=false means unknown portfolio id.
func (s *PostgresStore) LoadPortfolioAssemblerInput(ctx context.Context, portfolioID uuid.UUID) (portfolio.PortfolioAssemblerInput, bool, error) {
	var exists bool
	if err := s.pool.QueryRow(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM portfolios WHERE portfolio_id = $1
		) OR EXISTS(
			SELECT 1 FROM events WHERE portfolio_id = $1
		)
	`, portfolioID).Scan(&exists); err != nil {
		return portfolio.PortfolioAssemblerInput{}, false, fmt.Errorf("check portfolio exists in catalog/events: %w", err)
	}
	if !exists {
		return portfolio.PortfolioAssemblerInput{}, false, nil
	}

	posRows, openSymbols, err := s.loadPositionsForPortfolio(ctx, portfolioID)
	if err != nil {
		return portfolio.PortfolioAssemblerInput{}, true, err
	}

	priceBySymbol := map[string]portfolio.PriceMarkInput{}
	if len(openSymbols) > 0 {
		priceBySymbol, err = s.loadPriceMarksBySymbol(ctx, openSymbols)
		if err != nil {
			return portfolio.PortfolioAssemblerInput{}, true, err
		}
	}

	cur, err := s.LoadProjectionCursor(ctx, portfolioID)
	if err != nil {
		return portfolio.PortfolioAssemblerInput{}, true, err
	}
	var tradeApply *portfolio.ApplyCursorMeta
	if !cur.IsZero() {
		meta := portfolio.ApplyCursorMeta{EventID: cur.ID, EventTime: cur.Time}
		var createdAt time.Time
		err = s.pool.QueryRow(ctx, `SELECT created_at FROM events WHERE event_id = $1`, cur.ID).Scan(&createdAt)
		if err == nil {
			meta.ProcessingTime = createdAt.UTC()
		}
		tradeApply = &meta
	}

	return portfolio.PortfolioAssemblerInput{
		PortfolioID:   portfolioID,
		Positions:     posRows,
		PriceBySymbol: priceBySymbol,
		TradeApply:    tradeApply,
	}, true, nil
}

// ListPortfolioIDsByOpenSymbols returns portfolios with non-zero quantity for any symbol in symbols.
func (s *PostgresStore) ListPortfolioIDsByOpenSymbols(ctx context.Context, symbols []string) ([]uuid.UUID, error) {
	if len(symbols) == 0 {
		return nil, nil
	}
	rows, err := s.pool.Query(ctx, `
		SELECT DISTINCT portfolio_id
		FROM positions_projection
		WHERE symbol = ANY($1::text[])
		  AND quantity <> 0::numeric
		ORDER BY portfolio_id
	`, symbols)
	if err != nil {
		return nil, fmt.Errorf("list portfolio ids by open symbols: %w", err)
	}
	defer rows.Close()
	out := make([]uuid.UUID, 0)
	for rows.Next() {
		var pid uuid.UUID
		if err := rows.Scan(&pid); err != nil {
			return nil, fmt.Errorf("scan portfolio_id by symbol: %w", err)
		}
		out = append(out, pid)
	}
	return out, rows.Err()
}

// LoadSymbolSigma1D computes 1-day sigma from latest windowN daily returns per symbol.
func (s *PostgresStore) LoadSymbolSigma1D(ctx context.Context, symbols []string, windowN int) (map[string]decimal.Decimal, error) {
	if len(symbols) == 0 {
		return map[string]decimal.Decimal{}, nil
	}
	if windowN <= 0 {
		windowN = 60
	}
	rows, err := s.pool.Query(ctx, `
		WITH ranked AS (
			SELECT
				symbol,
				daily_return,
				ROW_NUMBER() OVER (PARTITION BY symbol ORDER BY return_date DESC) AS rn
			FROM symbol_returns
			WHERE symbol = ANY($1::text[])
			  AND daily_return IS NOT NULL
		)
		SELECT symbol, COALESCE(STDDEV_SAMP(daily_return), 0)::text AS sigma
		FROM ranked
		WHERE rn <= $2
		GROUP BY symbol
	`, symbols, windowN)
	if err != nil {
		return nil, fmt.Errorf("load symbol sigma_1d: %w", err)
	}
	defer rows.Close()

	out := make(map[string]decimal.Decimal, len(symbols))
	for rows.Next() {
		var sym, sigmaStr string
		if err := rows.Scan(&sym, &sigmaStr); err != nil {
			return nil, fmt.Errorf("scan sigma row: %w", err)
		}
		sig, err := decimal.NewFromString(sigmaStr)
		if err != nil {
			return nil, fmt.Errorf("parse sigma %q: %w", sigmaStr, err)
		}
		out[sym] = sig
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// LoadSymbolReturnSampleCounts returns how many non-null daily_return samples exist per symbol in the latest windowN rows.
func (s *PostgresStore) LoadSymbolReturnSampleCounts(ctx context.Context, symbols []string, windowN int) (map[string]int, error) {
	if len(symbols) == 0 {
		return map[string]int{}, nil
	}
	if windowN <= 0 {
		windowN = 60
	}
	rows, err := s.pool.Query(ctx, `
		WITH ranked AS (
			SELECT
				symbol,
				daily_return,
				ROW_NUMBER() OVER (PARTITION BY symbol ORDER BY return_date DESC) AS rn
			FROM symbol_returns
			WHERE symbol = ANY($1::text[])
			  AND daily_return IS NOT NULL
		)
		SELECT symbol, COUNT(*)::int
		FROM ranked
		WHERE rn <= $2
		GROUP BY symbol
	`, symbols, windowN)
	if err != nil {
		return nil, fmt.Errorf("load symbol return sample counts: %w", err)
	}
	defer rows.Close()
	out := make(map[string]int, len(symbols))
	for rows.Next() {
		var sym string
		var n int
		if err := rows.Scan(&sym, &n); err != nil {
			return nil, fmt.Errorf("scan return count: %w", err)
		}
		out[sym] = n
	}
	return out, rows.Err()
}

// InsertPortfolioSnapshot appends one checkpoint row. Retries for the same apply cursor are
// coalesced (ON CONFLICT DO NOTHING) per unique index uq_portfolio_snapshots_checkpoint.
func (s *PostgresStore) InsertPortfolioSnapshot(ctx context.Context, portfolioID uuid.UUID, asOfEventTime time.Time, asOfEventID uuid.UUID, snapshot json.RawMessage) (PortfolioSnapshotInsertResult, error) {
	if asOfEventID == uuid.Nil || asOfEventTime.IsZero() {
		return PortfolioSnapshotInsertResult{}, fmt.Errorf("as_of_event_id/time required")
	}
	if len(snapshot) == 0 {
		snapshot = []byte(`{}`)
	}
	var id int64
	err := s.pool.QueryRow(ctx, `
		INSERT INTO portfolio_snapshots (portfolio_id, as_of_event_time, as_of_event_id, snapshot, created_at)
		VALUES ($1, $2, $3, $4::jsonb, NOW())
		ON CONFLICT (portfolio_id, as_of_event_time, as_of_event_id) DO NOTHING
		RETURNING id
	`, portfolioID, asOfEventTime.UTC(), asOfEventID, []byte(snapshot)).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return PortfolioSnapshotInsertResult{Inserted: false}, nil
	}
	if err != nil {
		return PortfolioSnapshotInsertResult{}, fmt.Errorf("insert portfolio_snapshot: %w", err)
	}
	return PortfolioSnapshotInsertResult{Inserted: true}, nil
}

// LoadLatestPortfolioSnapshot returns the newest checkpoint by apply ordering and hydrates
// rtpre_trade_positions_v1 into a single-partition Aggregate.
func (s *PostgresStore) LoadLatestPortfolioSnapshot(ctx context.Context, portfolioID uuid.UUID) (LatestPortfolioSnapshot, error) {
	var asOfTime time.Time
	var asOfID uuid.UUID
	var snapBytes []byte
	var createdAt time.Time
	err := s.pool.QueryRow(ctx, `
		SELECT as_of_event_time, as_of_event_id, snapshot, created_at
		FROM portfolio_snapshots
		WHERE portfolio_id = $1
		ORDER BY as_of_event_time DESC, as_of_event_id DESC
		LIMIT 1
	`, portfolioID).Scan(&asOfTime, &asOfID, &snapBytes, &createdAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return LatestPortfolioSnapshot{Found: false}, nil
	}
	if err != nil {
		return LatestPortfolioSnapshot{}, fmt.Errorf("load latest portfolio_snapshot: %w", err)
	}
	raw := append(json.RawMessage(nil), snapBytes...)
	pos, err := portfolio.ParseTradePositionsSnapshotV1(raw)
	if err != nil {
		return LatestPortfolioSnapshot{}, fmt.Errorf("parse portfolio_snapshot: %w", err)
	}
	agg := portfolio.NewAggregate()
	agg.SetPortfolioPositions(portfolioID.String(), pos)
	return LatestPortfolioSnapshot{
		Found:        true,
		AsOf:         Cursor{Time: asOfTime.UTC(), ID: asOfID},
		CreatedAt:    createdAt.UTC(),
		SnapshotJSON: raw,
		Aggregate:    agg,
	}, nil
}

// UpsertRiskSnapshot inserts one materialized risk snapshot row.
func (s *PostgresStore) UpsertRiskSnapshot(ctx context.Context, portfolioID uuid.UUID, asOfEventTime time.Time, asOfEventID uuid.UUID, snapshot json.RawMessage) error {
	if asOfEventID == uuid.Nil || asOfEventTime.IsZero() {
		return fmt.Errorf("as_of_event_id/time required")
	}
	if len(snapshot) == 0 {
		snapshot = []byte(`{}`)
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO risk_snapshots (portfolio_id, as_of_event_time, as_of_event_id, snapshot, created_at)
		VALUES ($1, $2, $3, $4::jsonb, NOW())
	`, portfolioID, asOfEventTime.UTC(), asOfEventID, []byte(snapshot))
	if err != nil {
		return fmt.Errorf("insert risk_snapshot: %w", err)
	}
	return nil
}

func (s *PostgresStore) loadPositionsForPortfolio(ctx context.Context, portfolioID uuid.UUID) ([]portfolio.ProjectionRow, []string, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT symbol, quantity::text, cost_basis::text, realized_pnl::text
		FROM positions_projection
		WHERE portfolio_id = $1
		ORDER BY symbol ASC
	`, portfolioID)
	if err != nil {
		return nil, nil, fmt.Errorf("query positions_projection: %w", err)
	}
	defer rows.Close()

	out := make([]portfolio.ProjectionRow, 0)
	openSymbols := make([]string, 0)
	for rows.Next() {
		var symbol, qtyStr, avgStr, realStr string
		if err := rows.Scan(&symbol, &qtyStr, &avgStr, &realStr); err != nil {
			return nil, nil, fmt.Errorf("scan positions row: %w", err)
		}
		qty, err := decimal.NewFromString(qtyStr)
		if err != nil {
			return nil, nil, fmt.Errorf("parse quantity %q: %w", symbol, err)
		}
		avg, err := decimal.NewFromString(avgStr)
		if err != nil {
			return nil, nil, fmt.Errorf("parse cost_basis %q: %w", symbol, err)
		}
		real, err := decimal.NewFromString(realStr)
		if err != nil {
			return nil, nil, fmt.Errorf("parse realized_pnl %q: %w", symbol, err)
		}
		out = append(out, portfolio.ProjectionRow{
			Symbol:      symbol,
			Quantity:    qty,
			AverageCost: avg,
			RealizedPnL: real,
		})
		if !qty.IsZero() {
			openSymbols = append(openSymbols, symbol)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("iterate positions rows: %w", err)
	}
	return out, openSymbols, nil
}

func (s *PostgresStore) loadPriceMarksBySymbol(ctx context.Context, symbols []string) (map[string]portfolio.PriceMarkInput, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT pr.symbol, pr.price::text, pr.as_of, pr.as_of_event_id, e.created_at
		FROM prices_projection pr
		LEFT JOIN events e ON e.event_id = pr.as_of_event_id
		WHERE pr.symbol = ANY($1::text[])
	`, symbols)
	if err != nil {
		return nil, fmt.Errorf("query prices_projection: %w", err)
	}
	defer rows.Close()

	out := make(map[string]portfolio.PriceMarkInput, len(symbols))
	for rows.Next() {
		var symbol, priceStr string
		var asOf time.Time
		var asOfEID pgtype.UUID
		var evtCreated pgtype.Timestamptz
		if err := rows.Scan(&symbol, &priceStr, &asOf, &asOfEID, &evtCreated); err != nil {
			return nil, fmt.Errorf("scan prices row: %w", err)
		}
		price, err := decimal.NewFromString(priceStr)
		if err != nil {
			return nil, fmt.Errorf("parse price %q: %w", symbol, err)
		}
		pm := portfolio.PriceMarkInput{Price: price, AsOfEventTime: asOf}
		if asOfEID.Valid {
			pm.AsOfEventID = uuid.UUID(asOfEID.Bytes)
		}
		if evtCreated.Valid {
			t := evtCreated.Time.UTC()
			pm.ProcessingTime = t
		}
		out[symbol] = pm
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate price rows: %w", err)
	}
	return out, nil
}

// LoadPortfolioPositionsIntoAggregate hydrates positions_projection into the aggregate.
func (s *PostgresStore) LoadPortfolioPositionsIntoAggregate(ctx context.Context, portfolioID uuid.UUID, portfolioKey string, agg *portfolio.Aggregate) error {
	rows, err := s.pool.Query(ctx, `
		SELECT symbol, quantity::text, cost_basis::text, realized_pnl::text
		FROM positions_projection WHERE portfolio_id = $1
	`, portfolioID)
	if err != nil {
		return fmt.Errorf("load positions: %w", err)
	}
	defer rows.Close()

	lots := make(map[string]domain.PositionLot)
	for rows.Next() {
		var sym, qstr, cbstr, rstr string
		if err := rows.Scan(&sym, &qstr, &cbstr, &rstr); err != nil {
			return fmt.Errorf("scan position row: %w", err)
		}
		qty, err := decimal.NewFromString(qstr)
		if err != nil {
			return fmt.Errorf("parse quantity for %s: %w", sym, err)
		}
		avg, err := decimal.NewFromString(cbstr)
		if err != nil {
			return fmt.Errorf("parse cost_basis for %s: %w", sym, err)
		}
		real, err := decimal.NewFromString(rstr)
		if err != nil {
			return fmt.Errorf("parse realized_pnl for %s: %w", sym, err)
		}
		lots[sym] = domain.PositionLot{Quantity: qty, AverageCost: avg, RealizedPnL: real}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	agg.SetPortfolioPositions(portfolioKey, domain.PositionsFromLots(lots))
	return nil
}

// ApplyBatch writes positions for symbols touched by trade events in applied, advances
// projection_cursor to the last event. Caller must have already applied each envelope
// to agg in order. Price marks are updated only by ApplyPriceBatch (price worker path).
func (s *PostgresStore) ApplyBatch(ctx context.Context, portfolioID uuid.UUID, portfolioKey string, applied []domain.EventEnvelope, agg *portfolio.Aggregate) error {
	if len(applied) == 0 {
		return nil
	}
	last := applied[len(applied)-1]

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin apply batch: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	touched := make(map[string]struct{})
	for _, ev := range applied {
		if ev.EventType != domain.EventTypeTradeExecuted {
			continue
		}
		var tr domain.TradePayload
		if err := json.Unmarshal(ev.Payload, &tr); err != nil {
			return fmt.Errorf("%w: trade payload: %v", domain.ErrInvalidPayload, err)
		}
		touched[tr.Symbol] = struct{}{}
	}

	for sym := range touched {
		lot := agg.Lot(portfolioKey, sym)
		if lot.Quantity.IsZero() {
			if lot.RealizedPnL.IsZero() {
				_, err = tx.Exec(ctx, `
					DELETE FROM positions_projection WHERE portfolio_id = $1 AND symbol = $2
				`, portfolioID, sym)
				if err != nil {
					return fmt.Errorf("delete positions_projection %s: %w", sym, err)
				}
				continue
			}
			_, err = tx.Exec(ctx, `
				INSERT INTO positions_projection (portfolio_id, symbol, quantity, cost_basis, realized_pnl, updated_at)
				VALUES ($1, $2, ROUND(0::numeric, 8), ROUND(0::numeric, 8), ROUND($3::numeric, 8), NOW())
				ON CONFLICT (portfolio_id, symbol) DO UPDATE SET
					quantity = EXCLUDED.quantity,
					cost_basis = EXCLUDED.cost_basis,
					realized_pnl = EXCLUDED.realized_pnl,
					updated_at = NOW()
			`, portfolioID, sym, lot.RealizedPnL.String())
			if err != nil {
				return fmt.Errorf("upsert positions_projection %s: %w", sym, err)
			}
			continue
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO positions_projection (portfolio_id, symbol, quantity, cost_basis, realized_pnl, updated_at)
			VALUES ($1, $2, ROUND($3::numeric, 8), ROUND($4::numeric, 8), ROUND($5::numeric, 8), NOW())
			ON CONFLICT (portfolio_id, symbol) DO UPDATE SET
				quantity = EXCLUDED.quantity,
				cost_basis = EXCLUDED.cost_basis,
				realized_pnl = EXCLUDED.realized_pnl,
				updated_at = NOW()
		`, portfolioID, sym, lot.Quantity.String(), lot.AverageCost.String(), lot.RealizedPnL.String())
		if err != nil {
			return fmt.Errorf("upsert positions_projection %s: %w", sym, err)
		}
	}

	if err := upsertProjectionCursor(ctx, tx, portfolioID, CursorFromEvent(last)); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit apply batch: %w", err)
	}
	return nil
}

// ApplyPriceBatch writes prices_projection for each PriceUpdated in order and advances projection_cursor.
func (s *PostgresStore) ApplyPriceBatch(ctx context.Context, streamPortfolioID uuid.UUID, applied []domain.EventEnvelope) error {
	if len(applied) == 0 {
		return nil
	}
	last := applied[len(applied)-1]

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin price batch: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	for _, ev := range applied {
		if ev.EventType != domain.EventTypePriceUpdated {
			continue
		}
		var p domain.PricePayload
		if err := json.Unmarshal(ev.Payload, &p); err != nil {
			return fmt.Errorf("%w: price payload: %v", domain.ErrInvalidPayload, err)
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO prices_projection (symbol, price, as_of, updated_at, as_of_event_id)
			VALUES ($1, $2::numeric, $3, NOW(), $4)
			ON CONFLICT (symbol) DO UPDATE SET
				price = EXCLUDED.price,
				as_of = EXCLUDED.as_of,
				updated_at = NOW(),
				as_of_event_id = EXCLUDED.as_of_event_id
			WHERE EXCLUDED.as_of > prices_projection.as_of
		`, p.Symbol, p.Price.String(), ev.EventTime, ev.EventID)
		if err != nil {
			return fmt.Errorf("upsert prices_projection %s: %w", p.Symbol, err)
		}
		if err := upsertSymbolReturn(ctx, tx, p.Symbol, p.Price, ev.EventTime, ev.EventID); err != nil {
			return fmt.Errorf("upsert symbol_returns %s: %w", p.Symbol, err)
		}
		if err := trimSymbolReturnsWindow(ctx, tx, p.Symbol, symbolReturnsWindowN); err != nil {
			return fmt.Errorf("trim symbol_returns %s: %w", p.Symbol, err)
		}
	}

	if err := upsertProjectionCursor(ctx, tx, streamPortfolioID, CursorFromEvent(last)); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit price batch: %w", err)
	}
	return nil
}

func upsertSymbolReturn(ctx context.Context, tx pgx.Tx, symbol string, close decimal.Decimal, eventTime time.Time, eventID uuid.UUID) error {
	d := eventUTCDate(eventTime)

	var prevCloseStr string
	err := tx.QueryRow(ctx, `
		SELECT close_price::text
		FROM symbol_returns
		WHERE symbol = $1 AND return_date < $2::date
		ORDER BY return_date DESC
		LIMIT 1
	`, symbol, d).Scan(&prevCloseStr)
	var daily any
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		// First known day for symbol: no return yet.
	case err != nil:
		return fmt.Errorf("select previous close: %w", err)
	default:
		prevClose, err := decimal.NewFromString(prevCloseStr)
		if err != nil {
			return fmt.Errorf("parse previous close %q: %w", prevCloseStr, err)
		}
		r, err := computeDailyReturn(prevClose, close)
		if err != nil {
			return err
		}
		daily = r.String()
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO symbol_returns (symbol, return_date, close_price, daily_return, as_of_event_time, as_of_event_id, created_at, updated_at)
		VALUES ($1, $2::date, ROUND($3::numeric, 8), ROUND($4::numeric, 10), $5, $6, NOW(), NOW())
		ON CONFLICT (symbol, return_date) DO UPDATE SET
			close_price = EXCLUDED.close_price,
			daily_return = EXCLUDED.daily_return,
			as_of_event_time = EXCLUDED.as_of_event_time,
			as_of_event_id = EXCLUDED.as_of_event_id,
			updated_at = NOW()
	`, symbol, d, close.String(), daily, eventTime.UTC(), eventID)
	if err != nil {
		return err
	}
	return nil
}

func trimSymbolReturnsWindow(ctx context.Context, tx pgx.Tx, symbol string, keepN int) error {
	if keepN <= 0 {
		return nil
	}
	_, err := tx.Exec(ctx, `
		DELETE FROM symbol_returns
		WHERE symbol = $1
		  AND return_date NOT IN (
			SELECT return_date
			FROM symbol_returns
			WHERE symbol = $1
			ORDER BY return_date DESC
			LIMIT $2
		  )
	`, symbol, keepN)
	return err
}

// PersistApplyDLQ records DLQ and advances projection_cursor past ev in one transaction.
func (s *PostgresStore) PersistApplyDLQ(ctx context.Context, portfolioID uuid.UUID, portfolioKey string, ev domain.EventEnvelope, reason string, cause error) error {
	_ = portfolioKey
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin dlq tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := insertDLQ(ctx, tx, ev, portfolioID, reason, cause); err != nil {
		return err
	}
	if err := upsertProjectionCursor(ctx, tx, portfolioID, CursorFromEvent(ev)); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit dlq: %w", err)
	}
	return nil
}

func upsertProjectionCursor(ctx context.Context, tx pgx.Tx, portfolioID uuid.UUID, cur Cursor) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO projection_cursor (portfolio_id, last_event_time, last_event_id, updated_at)
		VALUES ($1, $2, $3, NOW())
		ON CONFLICT (portfolio_id) DO UPDATE SET
			last_event_time = EXCLUDED.last_event_time,
			last_event_id = EXCLUDED.last_event_id,
			updated_at = NOW()
	`, portfolioID, cur.Time, cur.ID)
	if err != nil {
		return fmt.Errorf("upsert projection_cursor: %w", err)
	}
	return nil
}

func insertDLQ(ctx context.Context, tx pgx.Tx, ev domain.EventEnvelope, portfolioID uuid.UUID, reason string, cause error) error {
	envelopeJSON, err := json.Marshal(ev)
	if err != nil {
		return fmt.Errorf("marshal envelope for dlq: %w", err)
	}
	meta := map[string]string{"reason": reason}
	if cause != nil {
		meta["cause"] = cause.Error()
	}
	metaJSON, err := json.Marshal(meta)
	if err != nil {
		return fmt.Errorf("marshal dlq metadata: %w", err)
	}
	msg := reason
	if cause != nil {
		msg = cause.Error()
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO dlq_events (original_event_id, portfolio_id, error_message, payload, metadata)
		VALUES ($1, $2, $3, $4::jsonb, $5::jsonb)
	`, ev.EventID, portfolioID, msg, envelopeJSON, metaJSON)
	if err != nil {
		return fmt.Errorf("insert dlq: %w", err)
	}
	observability.IncDLQEvents()
	return nil
}

func (s *PostgresStore) queryEvents(ctx context.Context, query string, portfolioID uuid.UUID) ([]domain.EventEnvelope, error) {
	rows, err := s.pool.Query(ctx, query, portfolioID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanEventRows(rows)
}

func (s *PostgresStore) queryEventsWithLimit(ctx context.Context, query string, args ...any) ([]domain.EventEnvelope, error) {
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanEventRows(rows)
}

func scanEventRows(rows pgx.Rows) ([]domain.EventEnvelope, error) {
	var out []domain.EventEnvelope
	for rows.Next() {
		var (
			eventID        uuid.UUID
			portfolioID    uuid.UUID
			eventTime      time.Time
			eventType      string
			idempotencyKey string
			source         string
			payload        []byte
		)
		if err := rows.Scan(&eventID, &portfolioID, &eventTime, &eventType, &idempotencyKey, &source, &payload); err != nil {
			return nil, fmt.Errorf("scan event: %w", err)
		}
		out = append(out, domain.EventEnvelope{
			EventID:        eventID,
			EventType:      domain.EventType(eventType),
			EventTime:      eventTime,
			ProcessingTime: eventTime,
			Source:         source,
			PortfolioID:    portfolioID.String(),
			IdempotencyKey: idempotencyKey,
			Payload:        json.RawMessage(payload),
		})
	}
	return out, rows.Err()
}
