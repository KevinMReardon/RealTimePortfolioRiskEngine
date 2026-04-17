package api

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/KevinMReardon/realtime-portfolio-risk/internal/domain"
	"github.com/KevinMReardon/realtime-portfolio-risk/internal/events"
	"github.com/KevinMReardon/realtime-portfolio-risk/internal/ingestion/pricefeed"
)

// PriceMarksReader loads projection-backed price marks for read/query UIs.
type PriceMarksReader interface {
	ListPriceMarks(ctx context.Context, p events.ListPriceMarksParams) (events.ListPriceMarksResult, error)
	GetPriceSymbolDetail(ctx context.Context, symbol string, historyLimit int) (*events.PriceSymbolDetail, bool, error)
}

type PriceFeedWatchlistManager interface {
	Watchlist() []string
	SetWatchlist(symbols []string)
}

type PriceFeedWatchlistPersistence interface {
	UpsertPriceFeedWatchlist(ctx context.Context, watchlist []string) error
}

type priceListItemJSON struct {
	Symbol             string  `json:"symbol"`
	Price              string  `json:"price"`
	ChangePct          *string `json:"change_pct,omitempty"`
	AsOf               string  `json:"as_of"`
	UpdatedAt          string  `json:"updated_at"`
	Source             string  `json:"source"`
	StalenessSeconds   float64 `json:"staleness_seconds"`
	ProviderDataStatus string  `json:"provider_data_status"`
}

type listPricesResponse struct {
	Items  []priceListItemJSON `json:"items"`
	Total  int64               `json:"total"`
	Limit  int                 `json:"limit"`
	Offset int                 `json:"offset"`
}

type priceHistoryJSON struct {
	ReturnDate    string  `json:"return_date"`
	ClosePrice    string  `json:"close_price"`
	DailyReturn   *string `json:"daily_return,omitempty"`
	AsOfEventTime string  `json:"as_of_event_time"`
}

type priceDetailResponse struct {
	Symbol             string             `json:"symbol"`
	Price              string             `json:"price"`
	AsOf               string             `json:"as_of"`
	UpdatedAt          string             `json:"updated_at"`
	Source             string             `json:"source"`
	History            []priceHistoryJSON `json:"history"`
	HistorySummary     string             `json:"history_summary"`
	StalenessSeconds   float64            `json:"staleness_seconds"`
	ProviderDataStatus string             `json:"provider_data_status"`
}

type priceFeedStatusResponse struct {
	FeedEnabled bool `json:"feed_enabled"`

	ConfiguredProvider string   `json:"configured_provider"`
	PollIntervalMs     int64    `json:"poll_interval_ms"`
	WatchlistCount     int      `json:"watchlist_count"`
	WatchlistPreview   []string `json:"watchlist_preview,omitempty"`

	StaleAfterSeconds float64 `json:"staleness_threshold_seconds"`

	LastTickStartedAt     *string `json:"last_tick_started_at,omitempty"`
	LastTickFinishedAt    *string `json:"last_tick_finished_at,omitempty"`
	LastSuccessfulFetchAt *string `json:"last_successful_fetch_at,omitempty"`
	ActiveProvider        string  `json:"active_provider,omitempty"`
	LastTickUsedFailover  bool    `json:"last_tick_used_failover"`
	LastTickIngested      int     `json:"last_tick_ingested_count,omitempty"`
	LastError             string  `json:"last_error,omitempty"`
}

type priceFeedWatchlistResponse struct {
	Watchlist []string `json:"watchlist"`
}

type updatePriceFeedWatchlistRequest struct {
	Watchlist []string `json:"watchlist"`
}

func ptrTimeRFC3339(t *time.Time) *string {
	if t == nil {
		return nil
	}
	v := t.UTC().Format(time.RFC3339Nano)
	return &v
}

func dataStatusFromStaleness(staleAfter, staleness float64) string {
	if staleAfter <= 0 {
		return "unknown"
	}
	if staleness <= staleAfter {
		return "fresh"
	}
	return "stale"
}

func listPricesHandler(store PriceMarksReader, log *zap.Logger, staleAfter time.Duration) gin.HandlerFunc {
	staleSec := staleAfter.Seconds()
	return func(c *gin.Context) {
		q := strings.TrimSpace(c.Query("q"))
		sort := c.DefaultQuery("sort", "symbol")
		order := c.DefaultQuery("order", "asc")
		limit := clampIntQuery(c, "limit", 50, 1, 500)
		offset := clampIntQuery(c, "offset", 0, 0, 1_000_000_000)

		res, err := store.ListPriceMarks(c.Request.Context(), events.ListPriceMarksParams{
			Query:  q,
			Sort:   sort,
			Order:  order,
			Limit:  limit,
			Offset: offset,
		})
		if err != nil {
			if errors.Is(err, domain.ErrValidation) {
				respondAPIError(c, http.StatusBadRequest, ErrCodeValidation, err.Error(), nil)
				return
			}
			log.Warn("list_prices_failed", zap.Error(err))
			respondAPIError(c, http.StatusInternalServerError, ErrCodeInternal, "internal error", nil)
			return
		}
		now := time.Now()
		items := make([]priceListItemJSON, 0, len(res.Items))
		for _, row := range res.Items {
			st := now.Sub(row.UpdatedAt).Seconds()
			items = append(items, priceListItemJSON{
				Symbol:             row.Symbol,
				Price:              row.Price,
				ChangePct:          row.ChangePct,
				AsOf:               row.AsOf.UTC().Format(time.RFC3339Nano),
				UpdatedAt:          row.UpdatedAt.UTC().Format(time.RFC3339Nano),
				Source:             row.Source,
				StalenessSeconds:   st,
				ProviderDataStatus: dataStatusFromStaleness(staleSec, st),
			})
		}
		c.JSON(http.StatusOK, listPricesResponse{
			Items:  items,
			Total:  res.Total,
			Limit:  limit,
			Offset: offset,
		})
	}
}

func getPriceSymbolHandler(store PriceMarksReader, log *zap.Logger, staleAfter time.Duration) gin.HandlerFunc {
	staleSec := staleAfter.Seconds()
	return func(c *gin.Context) {
		symbol := strings.TrimSpace(c.Param("symbol"))
		histN := clampIntQuery(c, "history", 10, 1, 60)
		detail, ok, err := store.GetPriceSymbolDetail(c.Request.Context(), symbol, histN)
		if err != nil {
			if errors.Is(err, domain.ErrValidation) {
				respondAPIError(c, http.StatusBadRequest, ErrCodeValidation, err.Error(), nil)
				return
			}
			log.Warn("get_price_symbol_failed", zap.String("symbol", symbol), zap.Error(err))
			respondAPIError(c, http.StatusInternalServerError, ErrCodeInternal, "internal error", nil)
			return
		}
		if !ok {
			respondAPIError(c, http.StatusNotFound, ErrCodeNotFound, "symbol not found", map[string]any{"symbol": symbol})
			return
		}
		now := time.Now()
		st := now.Sub(detail.UpdatedAt).Seconds()
		hist := make([]priceHistoryJSON, 0, len(detail.History))
		for _, h := range detail.History {
			hist = append(hist, priceHistoryJSON{
				ReturnDate:    h.ReturnDate,
				ClosePrice:    h.ClosePrice,
				DailyReturn:   h.DailyReturn,
				AsOfEventTime: h.AsOfEventTime.UTC().Format(time.RFC3339Nano),
			})
		}
		c.JSON(http.StatusOK, priceDetailResponse{
			Symbol:             detail.Symbol,
			Price:              detail.Price,
			AsOf:               detail.AsOf.UTC().Format(time.RFC3339Nano),
			UpdatedAt:          detail.UpdatedAt.UTC().Format(time.RFC3339Nano),
			Source:             detail.Source,
			History:            hist,
			HistorySummary:     summarizeHistory(detail.History),
			StalenessSeconds:   st,
			ProviderDataStatus: dataStatusFromStaleness(staleSec, st),
		})
	}
}

func summarizeHistory(points []events.PriceHistoryPoint) string {
	if len(points) == 0 {
		return "No stored daily returns yet for this symbol."
	}
	n := len(points)
	if n > 5 {
		n = 5
	}
	var parts []string
	for i := 0; i < n; i++ {
		p := points[i]
		ch := "—"
		if p.DailyReturn != nil {
			ch = *p.DailyReturn
		}
		parts = append(parts, p.ReturnDate+": "+ch)
	}
	return strings.Join(parts, " · ")
}

func getPriceFeedStatusHandler(
	rt *pricefeed.RuntimeTracker,
	feedEnabled bool,
	provider string,
	poll time.Duration,
	watchlistMgr PriceFeedWatchlistManager,
) gin.HandlerFunc {
	staleAfter := 3 * poll
	if poll <= 0 {
		staleAfter = 5 * time.Minute
	}
	return func(c *gin.Context) {
		watchlist := []string(nil)
		if watchlistMgr != nil {
			watchlist = watchlistMgr.Watchlist()
		}
		preview := watchlist
		if len(preview) > 12 {
			preview = append([]string(nil), preview[:12]...)
		}
		var snap pricefeed.RuntimeSnapshot
		if rt != nil {
			snap = rt.Snapshot()
		}
		c.JSON(http.StatusOK, priceFeedStatusResponse{
			FeedEnabled:           feedEnabled,
			ConfiguredProvider:    provider,
			PollIntervalMs:        poll.Milliseconds(),
			WatchlistCount:        len(watchlist),
			WatchlistPreview:      preview,
			StaleAfterSeconds:     staleAfter.Seconds(),
			LastTickStartedAt:     ptrTimeRFC3339(snap.LastTickStartedAt),
			LastTickFinishedAt:    ptrTimeRFC3339(snap.LastTickFinishedAt),
			LastSuccessfulFetchAt: ptrTimeRFC3339(snap.LastSuccessAt),
			ActiveProvider:        snap.ActiveProvider,
			LastTickUsedFailover:  snap.LastTickUsedFailover,
			LastTickIngested:      snap.LastTickIngested,
			LastError:             snap.LastError,
		})
	}
}

func getPriceFeedWatchlistHandler(watchlistMgr PriceFeedWatchlistManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		if watchlistMgr == nil {
			c.JSON(http.StatusOK, priceFeedWatchlistResponse{Watchlist: nil})
			return
		}
		c.JSON(http.StatusOK, priceFeedWatchlistResponse{
			Watchlist: watchlistMgr.Watchlist(),
		})
	}
}

func putPriceFeedWatchlistHandler(watchlistMgr PriceFeedWatchlistManager) gin.HandlerFunc {
	return putPriceFeedWatchlistHandlerWithPersistence(watchlistMgr, nil)
}

func putPriceFeedWatchlistHandlerWithPersistence(
	watchlistMgr PriceFeedWatchlistManager,
	persist PriceFeedWatchlistPersistence,
) gin.HandlerFunc {
	return func(c *gin.Context) {
		if watchlistMgr == nil {
			respondAPIError(c, http.StatusServiceUnavailable, ErrCodeInsufficientData, "price feed watchlist is unavailable", nil)
			return
		}
		var req updatePriceFeedWatchlistRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			respondAPIError(c, http.StatusBadRequest, ErrCodeValidation, "invalid request body including JSON shape", nil)
			return
		}
		watchlistMgr.SetWatchlist(req.Watchlist)
		watchlist := watchlistMgr.Watchlist()
		if persist != nil {
			if err := persist.UpsertPriceFeedWatchlist(c.Request.Context(), watchlist); err != nil {
				respondAPIError(c, http.StatusInternalServerError, ErrCodeInternal, "failed to persist watchlist", nil)
				return
			}
		}
		c.JSON(http.StatusOK, priceFeedWatchlistResponse{
			Watchlist: watchlist,
		})
	}
}

func clampIntQuery(c *gin.Context, name string, def, min, max int) int {
	raw := strings.TrimSpace(c.Query(name))
	if raw == "" {
		return def
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		return def
	}
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}
