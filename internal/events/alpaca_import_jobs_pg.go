package events

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

// PortfolioExists reports whether a row exists in portfolios.
func (s *PostgresStore) PortfolioExists(ctx context.Context, portfolioID uuid.UUID) (bool, error) {
	var ok bool
	err := s.pool.QueryRow(ctx, `
		SELECT EXISTS(SELECT 1 FROM portfolios WHERE portfolio_id = $1)
	`, portfolioID).Scan(&ok)
	if err != nil {
		return false, fmt.Errorf("portfolio exists: %w", err)
	}
	return ok, nil
}

// CreateAlpacaImportJob inserts a queued job. ErrAlpacaImportJobConflict when another active job exists.
func (s *PostgresStore) CreateAlpacaImportJob(ctx context.Context, portfolioID uuid.UUID, requestedBy *uuid.UUID, since, until *time.Time, fullHistory bool) (uuid.UUID, error) {
	var jobID uuid.UUID
	err := s.pool.QueryRow(ctx, `
		INSERT INTO alpaca_import_jobs (
			job_id, portfolio_id, requested_by_user_id, status,
			since_ts, until_ts, full_history
		)
		VALUES (gen_random_uuid(), $1, $2, 'queued', $3, $4, $5)
		RETURNING job_id
	`, portfolioID, requestedBy, since, until, fullHistory).Scan(&jobID)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return uuid.Nil, ErrAlpacaImportJobConflict
		}
		return uuid.Nil, fmt.Errorf("insert alpaca_import_jobs: %w", err)
	}
	return jobID, nil
}

// GetAlpacaImportJob loads one job by id.
func (s *PostgresStore) GetAlpacaImportJob(ctx context.Context, jobID uuid.UUID) (AlpacaImportJob, bool, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT
			job_id, portfolio_id, requested_by_user_id, status,
			since_ts, until_ts, full_history,
			inserted_count, duplicate_count, skipped_invalid_count,
			pages_fetched, activities_seen, fills_considered,
			error_message,
			created_at, updated_at, started_at, finished_at
		FROM alpaca_import_jobs
		WHERE job_id = $1
	`, jobID)
	job, err := scanAlpacaImportJob(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return AlpacaImportJob{}, false, nil
	}
	if err != nil {
		return AlpacaImportJob{}, false, err
	}
	return job, true, nil
}

func scanAlpacaImportJob(row pgx.Row) (AlpacaImportJob, error) {
	var (
		job                                               AlpacaImportJob
		reqBy                                             pgtype.UUID
		sinceTS, untilTS                                  pgtype.Timestamptz
		errMsg                                            pgtype.Text
		startedAt, finishedAt                             pgtype.Timestamptz
	)
	err := row.Scan(
		&job.JobID,
		&job.PortfolioID,
		&reqBy,
		&job.Status,
		&sinceTS,
		&untilTS,
		&job.FullHistory,
		&job.InsertedCount,
		&job.DuplicateCount,
		&job.SkippedInvalidCount,
		&job.PagesFetched,
		&job.ActivitiesSeen,
		&job.FillsConsidered,
		&errMsg,
		&job.CreatedAt,
		&job.UpdatedAt,
		&startedAt,
		&finishedAt,
	)
	if err != nil {
		return AlpacaImportJob{}, err
	}
	if reqBy.Valid {
		u := uuid.UUID(reqBy.Bytes)
		job.RequestedByUserID = &u
	}
	if sinceTS.Valid {
		t := sinceTS.Time.UTC()
		job.Since = &t
	}
	if untilTS.Valid {
		t := untilTS.Time.UTC()
		job.Until = &t
	}
	if errMsg.Valid {
		s := errMsg.String
		job.ErrorMessage = &s
	}
	if startedAt.Valid {
		t := startedAt.Time.UTC()
		job.StartedAt = &t
	}
	if finishedAt.Valid {
		t := finishedAt.Time.UTC()
		job.FinishedAt = &t
	}
	return job, nil
}

// ClaimNextQueuedAlpacaImportJob claims at most one queued job using SKIP LOCKED.
func (s *PostgresStore) ClaimNextQueuedAlpacaImportJob(ctx context.Context) (*AlpacaImportJob, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin claim job: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	row := tx.QueryRow(ctx, `
		WITH c AS (
			SELECT job_id FROM alpaca_import_jobs
			WHERE status = 'queued'
			ORDER BY created_at ASC
			LIMIT 1
			FOR UPDATE SKIP LOCKED
		)
		UPDATE alpaca_import_jobs j
		SET status = 'running',
		    started_at = COALESCE(j.started_at, NOW()),
		    updated_at = NOW()
		FROM c
		WHERE j.job_id = c.job_id
		RETURNING
			j.job_id, j.portfolio_id, j.requested_by_user_id, j.status,
			j.since_ts, j.until_ts, j.full_history,
			j.inserted_count, j.duplicate_count, j.skipped_invalid_count,
			j.pages_fetched, j.activities_seen, j.fills_considered,
			j.error_message,
			j.created_at, j.updated_at, j.started_at, j.finished_at
	`)
	job, err := scanAlpacaImportJob(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("claim alpaca_import_jobs: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit claim job: %w", err)
	}
	return &job, nil
}

// CompleteAlpacaImportJobSuccess marks running job succeeded with stats.
func (s *PostgresStore) CompleteAlpacaImportJobSuccess(ctx context.Context, jobID uuid.UUID,
	inserted, duplicate, skippedInvalid, pagesFetched, activitiesSeen, fillsConsidered int,
) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE alpaca_import_jobs
		SET status = 'succeeded',
		    inserted_count = $2,
		    duplicate_count = $3,
		    skipped_invalid_count = $4,
		    pages_fetched = $5,
		    activities_seen = $6,
		    fills_considered = $7,
		    finished_at = NOW(),
		    updated_at = NOW(),
		    error_message = NULL
		WHERE job_id = $1 AND status = 'running'
	`, jobID,
		inserted,
		duplicate,
		skippedInvalid,
		pagesFetched,
		activitiesSeen,
		fillsConsidered,
	)
	if err != nil {
		return fmt.Errorf("complete alpaca import success: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("complete alpaca import success: job %s not running", jobID)
	}
	return nil
}

// CompleteAlpacaImportJobFailure marks running job failed.
func (s *PostgresStore) CompleteAlpacaImportJobFailure(ctx context.Context, jobID uuid.UUID, message string) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE alpaca_import_jobs
		SET status = 'failed',
		    error_message = $2,
		    finished_at = NOW(),
		    updated_at = NOW()
		WHERE job_id = $1 AND status = 'running'
	`, jobID, message)
	if err != nil {
		return fmt.Errorf("complete alpaca import failure: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("complete alpaca import failure: job %s not running", jobID)
	}
	return nil
}
