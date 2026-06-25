package runtime

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"go.uber.org/zap"

	"github.com/KevinMReardon/realtime-portfolio-risk/internal/connectors/alpaca"
	"github.com/KevinMReardon/realtime-portfolio-risk/internal/events"
)

// EquityAnchorStore loads and writes portfolio_equity_anchor rows.
type EquityAnchorStore interface {
	EquityAnchorWriter
	LoadEquityAnchorForPortfolioDate(ctx context.Context, portfolioID uuid.UUID, anchorDate time.Time) (equity decimal.Decimal, ok bool, err error)
	InsertEquityAnchorIfMissing(ctx context.Context, portfolioID uuid.UUID, anchorDate time.Time, equity decimal.Decimal) (inserted bool, err error)
}

const defaultEquityAnchorEnsureInterval = 15 * time.Minute

// EquityAnchorEnsureRunner periodically ensures today's NY anchor exists for each sync target.
type EquityAnchorEnsureRunner struct {
	Job      *EquityAnchorJob
	Interval time.Duration
	Log      *zap.Logger
}

// Run blocks until ctx is cancelled, ticking on Interval.
func (r *EquityAnchorEnsureRunner) Run(ctx context.Context) {
	if r == nil || r.Job == nil {
		return
	}
	log := r.Log
	if log == nil {
		log = zap.NewNop()
	}
	interval := r.Interval
	if interval <= 0 {
		interval = defaultEquityAnchorEnsureInterval
	}
	log.Info("equity_anchor_ensure_runner_started", zap.Duration("interval", interval))
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			log.Info("equity_anchor_ensure_runner_stopped")
			return
		case <-ticker.C:
			runCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
			r.Job.EnsureTodayAllMissing(runCtx)
			cancel()
		}
	}
}

// NYLocation returns America/New_York or a fixed-offset fallback.
func NYLocation() *time.Location {
	loc, err := time.LoadLocation(defaultEquityAnchorTZ)
	if err != nil {
		return time.FixedZone("America/New_York", -5*3600)
	}
	return loc
}

// TodayAnchorDateUTC is the stored anchor_date for the NY calendar day containing now.
func TodayAnchorDateUTC(now time.Time) time.Time {
	loc := NYLocation()
	nowNY := now.In(loc)
	return time.Date(nowNY.Year(), nowNY.Month(), nowNY.Day(), 0, 0, 0, 0, time.UTC)
}

func alpacaRESTBaseURL(accountMode, baseURL string) string {
	if v := strings.TrimSpace(baseURL); v != "" {
		return v
	}
	if strings.EqualFold(accountMode, "live") {
		return alpaca.DefaultRESTBaseURLLive
	}
	return alpaca.DefaultRESTBaseURLPaper
}

// EnsureTodayAllMissing ensures today's anchor for each Alpaca sync target that lacks a row.
func (j *EquityAnchorJob) EnsureTodayAllMissing(ctx context.Context) {
	if j == nil {
		return
	}
	log := j.Log
	if log == nil {
		log = zap.NewNop()
	}
	if j.Targets == nil || j.anchorStore() == nil || j.NewREST == nil {
		log.Warn("equity_anchor_ensure_not_configured")
		return
	}
	targets, err := j.Targets.ListAlpacaSyncTargets(ctx)
	if err != nil {
		log.Warn("equity_anchor_ensure_list_targets_failed", zap.Error(err))
		return
	}
	for _, t := range targets {
		if ctx.Err() != nil {
			return
		}
		j.EnsureTodayForTarget(ctx, t)
	}
}

// EnsureTodayForTarget ensures today's anchor for one sync target when missing.
func (j *EquityAnchorJob) EnsureTodayForTarget(ctx context.Context, t events.AlpacaSyncTarget) {
	if j == nil {
		return
	}
	_ = j.ensureToday(ctx, t.PortfolioID, t.AlpacaKeyID, t.AlpacaSecretKey, alpacaRESTBaseURL(t.AlpacaAccountMode, t.AlpacaBaseURL), t.AlpacaAccountMode)
}

// EnsureTodayForPortfolioKeys ensures today's anchor using portfolio key material when missing.
func (j *EquityAnchorJob) EnsureTodayForPortfolioKeys(ctx context.Context, portfolioID uuid.UUID, keys events.PortfolioAlpacaKeyMaterial) {
	if j == nil {
		return
	}
	_ = j.ensureToday(ctx, portfolioID, keys.KeyID, keys.SecretKey, alpacaRESTBaseURL(keys.AccountMode, keys.BaseURL), keys.AccountMode)
}

func (j *EquityAnchorJob) anchorStore() EquityAnchorStore {
	if j == nil {
		return nil
	}
	s, ok := j.Anchor.(EquityAnchorStore)
	if !ok {
		return nil
	}
	return s
}

func (j *EquityAnchorJob) ensureToday(ctx context.Context, portfolioID uuid.UUID, keyID, secret, baseURL, mode string) error {
	log := j.Log
	if log == nil {
		log = zap.NewNop()
	}
	store := j.anchorStore()
	if store == nil || j.NewREST == nil {
		log.Warn("equity_anchor_ensure_not_configured", zap.String("portfolio_id", portfolioID.String()))
		return nil
	}
	if strings.TrimSpace(keyID) == "" || strings.TrimSpace(secret) == "" {
		return nil
	}
	anchorDate := TodayAnchorDateUTC(time.Now())
	if _, ok, err := store.LoadEquityAnchorForPortfolioDate(ctx, portfolioID, anchorDate); err != nil {
		log.Warn("equity_anchor_ensure_load_failed",
			zap.String("portfolio_id", portfolioID.String()),
			zap.Error(err))
		return err
	} else if ok {
		return nil
	}
	rest, err := j.NewREST(alpaca.RESTConfig{
		KeyID:     keyID,
		SecretKey: secret,
		BaseURL:   baseURL,
	})
	if err != nil {
		log.Warn("equity_anchor_ensure_rest_init_failed",
			zap.String("portfolio_id", portfolioID.String()),
			zap.Error(err))
		return err
	}
	acct, err := rest.GetAccount(ctx)
	if err != nil {
		log.Warn("equity_anchor_ensure_get_account_failed",
			zap.String("portfolio_id", portfolioID.String()),
			zap.Error(err))
		return err
	}
	if !acct.Equity.IsPositive() {
		log.Info("equity_anchor_ensure_skip_non_positive_equity",
			zap.String("portfolio_id", portfolioID.String()),
			zap.String("equity", acct.Equity.String()))
		return nil
	}
	inserted, err := store.InsertEquityAnchorIfMissing(ctx, portfolioID, anchorDate, acct.Equity)
	if err != nil {
		log.Warn("equity_anchor_ensure_insert_failed",
			zap.String("portfolio_id", portfolioID.String()),
			zap.Error(err))
		return err
	}
	if inserted {
		log.Info("equity_anchor_ensured",
			zap.String("portfolio_id", portfolioID.String()),
			zap.String("equity", acct.Equity.String()),
			zap.String("anchor_date", anchorDate.Format("2006-01-02")),
			zap.String("mode", mode),
		)
	}
	return nil
}
