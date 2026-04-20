package alpaca

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/KevinMReardon/realtime-portfolio-risk/internal/domain"
	"github.com/KevinMReardon/realtime-portfolio-risk/internal/ingestion"
	"github.com/google/uuid"
)

// FillActivityType is the Alpaca account activity type for executions.
const FillActivityType = "FILL"

// FillIngestOutcome classifies a single fill ingest attempt.
type FillIngestOutcome int

const (
	OutcomeAppended FillIngestOutcome = iota
	OutcomeDuplicate
	OutcomeSkippedInvalid
)

// TryIngestFillActivity maps one activity row to TradeExecuted and ingests. FILL-only;
// non-FILL rows return OutcomeSkippedInvalid with nil error.
func TryIngestFillActivity(ctx context.Context, ingest ingestion.Service, portfolioID uuid.UUID, act ActivityRow) (FillIngestOutcome, error) {
	if !strings.EqualFold(act.ActivityType, FillActivityType) {
		return OutcomeSkippedInvalid, nil
	}
	tp, err := ActivityToTradePayload(act)
	if err != nil {
		return OutcomeSkippedInvalid, nil
	}
	payloadJSON, err := json.Marshal(tp)
	if err != nil {
		return OutcomeSkippedInvalid, nil
	}
	ev := domain.EventEnvelope{
		EventID:        uuid.New(),
		EventType:      domain.EventTypeTradeExecuted,
		EventTime:      act.TransactionTime.UTC(),
		ProcessingTime: time.Now().UTC(),
		Source:         "alpaca",
		PortfolioID:    portfolioID.String(),
		IdempotencyKey: act.ID,
		Payload:        payloadJSON,
	}
	res, err := ingest.Ingest(ctx, ev)
	if err != nil {
		return OutcomeSkippedInvalid, fmt.Errorf("ingest fill %s: %w", act.ID, err)
	}
	if res.Inserted {
		return OutcomeAppended, nil
	}
	return OutcomeDuplicate, nil
}

// ActivityToTradePayload maps an Alpaca account activity to domain trade fields (USD).
func ActivityToTradePayload(act ActivityRow) (domain.TradePayload, error) {
	return activityToTradePayload(act)
}

func activityToTradePayload(act ActivityRow) (domain.TradePayload, error) {
	sym := strings.ToUpper(strings.TrimSpace(act.Symbol))
	if sym == "" {
		return domain.TradePayload{}, errors.New("empty symbol")
	}
	qty := act.Qty
	if qty.IsNegative() {
		qty = qty.Abs()
	}
	if !qty.IsPositive() {
		return domain.TradePayload{}, fmt.Errorf("non-positive qty")
	}
	if !act.Price.IsPositive() {
		return domain.TradePayload{}, fmt.Errorf("non-positive price")
	}
	side, err := domainSideFromAlpaca(act.Side)
	if err != nil {
		return domain.TradePayload{}, err
	}
	tid := strings.TrimSpace(act.ID)
	if tid == "" {
		return domain.TradePayload{}, errors.New("empty activity id")
	}
	return domain.TradePayload{
		TradeID:  tid,
		Symbol:   sym,
		Side:     side,
		Quantity: qty,
		Price:    act.Price,
		Currency: "USD",
	}, nil
}

func domainSideFromAlpaca(side string) (domain.Side, error) {
	switch strings.ToUpper(strings.TrimSpace(side)) {
	case "BUY":
		return domain.SideBuy, nil
	case "SELL":
		return domain.SideSell, nil
	default:
		return "", fmt.Errorf("unknown side %q", side)
	}
}

// FillBackfillStats aggregates backfill outcomes.
type FillBackfillStats struct {
	PagesFetched    int
	ActivitiesSeen  int
	FillsConsidered int
	Inserted        int
	Duplicate       int
	SkippedInvalid  int
}

// BackfillOptions constrains which activities are pulled from Alpaca.
// Since: exclusive lower bound (maps to REST After). Nil with FullHistory uses a distant past anchor.
// Until: inclusive upper bound enforced client-side on transaction_time as well as REST Until when set.
type BackfillOptions struct {
	Since       *time.Time
	Until       *time.Time
	FullHistory bool
}

// BackfillFills pages all FILL activities for the window and ingests each. Returns error on REST
// failure or on first ingest failure (OutcomeSkippedInvalid does not fail the run).
func BackfillFills(ctx context.Context, rest REST, ingest ingestion.Service, portfolioID uuid.UUID, opts BackfillOptions) (FillBackfillStats, error) {
	var stats FillBackfillStats
	if rest == nil || ingest == nil {
		return stats, fmt.Errorf("alpaca backfill: nil dependency")
	}
	if portfolioID == uuid.Nil {
		return stats, fmt.Errorf("alpaca backfill: portfolio_id required")
	}

	var since time.Time
	switch {
	case opts.FullHistory:
		since = time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC)
	case opts.Since != nil:
		since = opts.Since.UTC()
	default:
		since = time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC)
	}

	var pageToken string
	for {
		req := ListActivitiesRequest{
			ActivityTypes: []string{FillActivityType},
			Direction:     "asc",
			PageSize:      100,
			After:         since,
		}
		if opts.Until != nil {
			req.Until = opts.Until.UTC()
		}
		if pageToken != "" {
			req.PageToken = pageToken
			req.After = time.Time{}
		}

		page, err := rest.ListActivities(ctx, req)
		if err != nil {
			return stats, fmt.Errorf("list activities: %w", err)
		}
		stats.PagesFetched++

		for _, act := range page.Activities {
			stats.ActivitiesSeen++
			if opts.Until != nil && act.TransactionTime.UTC().After(opts.Until.UTC()) {
				continue
			}
			if !strings.EqualFold(act.ActivityType, FillActivityType) {
				continue
			}
			stats.FillsConsidered++

			out, err := TryIngestFillActivity(ctx, ingest, portfolioID, act)
			if err != nil {
				return stats, err
			}
			switch out {
			case OutcomeAppended:
				stats.Inserted++
			case OutcomeDuplicate:
				stats.Duplicate++
			case OutcomeSkippedInvalid:
				stats.SkippedInvalid++
			}
		}

		nextTok := strings.TrimSpace(page.NextPageToken)
		if nextTok == "" {
			break
		}
		pageToken = nextTok
	}

	return stats, nil
}
