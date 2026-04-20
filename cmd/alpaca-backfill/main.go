package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/KevinMReardon/realtime-portfolio-risk/internal/config"
	"github.com/KevinMReardon/realtime-portfolio-risk/internal/connectors/alpaca"
	"github.com/KevinMReardon/realtime-portfolio-risk/internal/events"
	"github.com/KevinMReardon/realtime-portfolio-risk/internal/ingestion"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	exitCode := run()
	os.Exit(exitCode)
}

func run() int {
	portfolioFlag := flag.String("portfolio-id", "", "portfolio UUID (required when multiple catalog portfolios exist)")
	modeFlag := flag.String("alpaca-mode", "paper", "alpaca account mode for credentials: paper|live")
	sinceFlag := flag.String("since", "", "exclusive lower bound (RFC3339), maps to Alpaca activities `after`")
	untilFlag := flag.String("until", "", "inclusive upper bound (RFC3339); also sent as Alpaca `until`")
	fullHistory := flag.Bool("full-history", false, "fetch from distant past (same default window as omitting --since)")
	timeout := flag.Duration("timeout", 2*time.Hour, "overall backfill deadline")
	flag.Parse()

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
		return 1
	}

	if *fullHistory && *sinceFlag != "" {
		fmt.Fprintf(os.Stderr, "do not combine --full-history with --since\n")
		return 1
	}

	opts := alpaca.BackfillOptions{FullHistory: *fullHistory}
	if *sinceFlag != "" {
		t, err := time.Parse(time.RFC3339, *sinceFlag)
		if err != nil {
			fmt.Fprintf(os.Stderr, "--since: %v\n", err)
			return 1
		}
		utc := t.UTC()
		opts.Since = &utc
	}
	if *untilFlag != "" {
		t, err := time.Parse(time.RFC3339, *untilFlag)
		if err != nil {
			fmt.Fprintf(os.Stderr, "--until: %v\n", err)
			return 1
		}
		utc := t.UTC()
		opts.Until = &utc
	}

	mode := strings.ToLower(strings.TrimSpace(*modeFlag))
	if mode != "paper" && mode != "live" {
		fmt.Fprintf(os.Stderr, "--alpaca-mode must be paper or live\n")
		return 1
	}
	keyID := cfg.AlpacaPaperKeyID
	secret := cfg.AlpacaPaperSecretKey
	base := cfg.AlpacaPaperBaseURL
	if mode == "live" {
		keyID = cfg.AlpacaLiveKeyID
		secret = cfg.AlpacaLiveSecretKey
		base = cfg.AlpacaLiveBaseURL
	}
	if strings.TrimSpace(keyID) == "" || strings.TrimSpace(secret) == "" {
		fmt.Fprintf(os.Stderr, "alpaca credentials missing for mode %s\n", mode)
		return 1
	}
	rest, err := alpaca.NewREST(alpaca.RESTConfig{
		KeyID:     keyID,
		SecretKey: secret,
		BaseURL:   base,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "alpaca client: %v\n", err)
		return 1
	}

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "database: %v\n", err)
		return 1
	}
	defer pool.Close()

	pid := uuid.Nil
	if *portfolioFlag != "" {
		parsed, err := uuid.Parse(strings.TrimSpace(*portfolioFlag))
		if err != nil {
			fmt.Fprintf(os.Stderr, "--portfolio-id: %v\n", err)
			return 1
		}
		pid = parsed
	}
	if pid == uuid.Nil && cfg.SingleUserApp {
		resolved, err := events.ResolveAlpacaPortfolioForSingleUser(ctx, pool)
		if err != nil {
			fmt.Fprintf(os.Stderr, "portfolio id: %v (pass --portfolio-id, or keep exactly one catalog portfolio in single-user mode)\n", err)
			return 1
		}
		pid = resolved
	}
	if pid == uuid.Nil {
		fmt.Fprintf(os.Stderr, "portfolio id required: pass --portfolio-id (with SINGLE_USER_APP=false you must set an explicit id)\n")
		return 1
	}
	for _, reserved := range cfg.PriceStreamPartitions {
		if pid == reserved {
			fmt.Fprintf(os.Stderr, "portfolio id is reserved for the market price stream\n")
			return 1
		}
	}

	repo := events.NewPostgresStore(pool)
	ingestSvc := ingestion.NewService(repo)

	stats, err := alpaca.BackfillFills(ctx, rest, ingestSvc, pid, opts)
	fmt.Printf("pages_fetched=%d activities_seen=%d fills_considered=%d inserted=%d duplicate=%d skipped_invalid=%d\n",
		stats.PagesFetched,
		stats.ActivitiesSeen,
		stats.FillsConsidered,
		stats.Inserted,
		stats.Duplicate,
		stats.SkippedInvalid,
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "backfill: %v\n", err)
		return 1
	}
	return 0
}
