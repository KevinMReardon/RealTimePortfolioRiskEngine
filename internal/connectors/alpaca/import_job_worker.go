package alpaca

import (
	"context"
	"strings"
	"time"

	"github.com/KevinMReardon/realtime-portfolio-risk/internal/events"
	"github.com/KevinMReardon/realtime-portfolio-risk/internal/ingestion"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

// AlpacaImportJobExecutor runs queued alpaca_import_jobs rows (claim + BackfillFills).
type AlpacaImportJobExecutor interface {
	ClaimNextQueuedAlpacaImportJob(ctx context.Context) (*events.AlpacaImportJob, error)
	CompleteAlpacaImportJobSuccess(ctx context.Context, jobID uuid.UUID,
		inserted, duplicate, skippedInvalid, pagesFetched, activitiesSeen, fillsConsidered int,
	) error
	CompleteAlpacaImportJobFailure(ctx context.Context, jobID uuid.UUID, message string) error
}

// ImportJobWorkerConfig wires the polling worker that drains import jobs.
type ImportJobWorkerConfig struct {
	Jobs       AlpacaImportJobExecutor
	Ingest     ingestion.Service
	REST       REST
	Logger     *zap.Logger
	PollEvery  time.Duration
	JobTimeout time.Duration
}

// RunImportJobWorker polls for queued jobs and executes them one at a time until ctx is done.
func RunImportJobWorker(ctx context.Context, cfg ImportJobWorkerConfig) {
	log := cfg.Logger
	if log == nil {
		log = zap.NewNop()
	}
	poll := cfg.PollEvery
	if poll <= 0 {
		poll = 2 * time.Second
	}
	jobTimeout := cfg.JobTimeout
	if jobTimeout <= 0 {
		jobTimeout = 2 * time.Hour
	}
	ticker := time.NewTicker(poll)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Info("alpaca_import_worker_stopped")
			return
		case <-ticker.C:
			if cfg.REST == nil || cfg.Ingest == nil || cfg.Jobs == nil {
				continue
			}
			job, err := cfg.Jobs.ClaimNextQueuedAlpacaImportJob(ctx)
			if err != nil {
				log.Warn("alpaca_import_claim_failed", zap.Error(err))
				continue
			}
			if job == nil {
				continue
			}
			runOneImportJob(ctx, cfg, job, jobTimeout, log)
		}
	}
}

func runOneImportJob(ctx context.Context, cfg ImportJobWorkerConfig, job *events.AlpacaImportJob, jobTimeout time.Duration, log *zap.Logger) {
	jctx, cancel := context.WithTimeout(ctx, jobTimeout)
	defer cancel()

	opts := BackfillOptions{FullHistory: job.FullHistory}
	if job.Since != nil {
		t := job.Since.UTC()
		opts.Since = &t
	}
	if job.Until != nil {
		t := job.Until.UTC()
		opts.Until = &t
	}

	log.Info("alpaca_import_job_started",
		zap.String("job_id", job.JobID.String()),
		zap.String("portfolio_id", job.PortfolioID.String()),
		zap.Bool("full_history", job.FullHistory),
	)

	stats, err := BackfillFills(jctx, cfg.REST, cfg.Ingest, job.PortfolioID, opts)
	if err != nil {
		msg := trimErrMsg(err, 8000)
		if cerr := cfg.Jobs.CompleteAlpacaImportJobFailure(ctx, job.JobID, msg); cerr != nil {
			log.Error("alpaca_import_job_finish_failed",
				zap.String("job_id", job.JobID.String()),
				zap.Error(cerr),
			)
		}
		log.Warn("alpaca_import_job_failed",
			zap.String("job_id", job.JobID.String()),
			zap.Error(err),
		)
		return
	}

	if cerr := cfg.Jobs.CompleteAlpacaImportJobSuccess(ctx, job.JobID,
		stats.Inserted, stats.Duplicate, stats.SkippedInvalid,
		stats.PagesFetched, stats.ActivitiesSeen, stats.FillsConsidered,
	); cerr != nil {
		log.Error("alpaca_import_job_finish_failed",
			zap.String("job_id", job.JobID.String()),
			zap.Error(cerr),
		)
		return
	}

	log.Info("alpaca_import_job_succeeded",
		zap.String("job_id", job.JobID.String()),
		zap.Int("inserted", stats.Inserted),
		zap.Int("duplicate", stats.Duplicate),
		zap.Int("skipped_invalid", stats.SkippedInvalid),
	)
}

func trimErrMsg(err error, max int) string {
	if err == nil {
		return ""
	}
	s := err.Error()
	if len(s) <= max {
		return s
	}
	return strings.TrimSpace(s[:max])
}