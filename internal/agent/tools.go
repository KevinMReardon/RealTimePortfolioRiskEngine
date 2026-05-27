package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/KevinMReardon/realtime-portfolio-risk/internal/events"
	"github.com/KevinMReardon/realtime-portfolio-risk/internal/portfolio"
	"github.com/KevinMReardon/realtime-portfolio-risk/internal/proposals/submit"
	"github.com/KevinMReardon/realtime-portfolio-risk/internal/risk"
)

// ToolExecutor runs one tool call requested by the model.
type ToolExecutor interface {
	Execute(ctx context.Context, call ToolCallRequest) (ToolCallResult, error)
}

type ToolCallRequest struct {
	SessionID string
	SeqNo     int
	ToolName  string
	Input     json.RawMessage
}

type ToolCallResult struct {
	Output    json.RawMessage
	LatencyMS int
	Success   bool
	Error     string
}

// ErrToolUnknown indicates a requested tool has no registered implementation.
var ErrToolUnknown = fmt.Errorf("agent tool: unknown tool")

type ToolDefinition struct {
	Name        string
	Description string
	InputSchema map[string]any
}

const (
	ToolGetPortfolioState = "get_portfolio_state"
	ToolGetRiskSnapshot   = "get_risk_snapshot"
	ToolGetPriceHistory   = "get_price_history"
	ToolGetMarketNews     = "get_market_news"
	ToolGetPositions      = "get_positions"
	ToolGetBuyingPower    = "get_buying_power"
	ToolGetWatchlist              = "get_watchlist"
	ToolGetWatchlistMarketSnapshot = "get_watchlist_market_snapshot"
	ToolSubmitProposal            = "submit_proposal"
)

func ToolDefinitions() []ToolDefinition {
	base := baseToolDefinitions()
	return append(base, researchToolDefinitions()...)
}

func baseToolDefinitions() []ToolDefinition {
	return []ToolDefinition{
		{
			Name: ToolGetPortfolioState,
			Description: "Return deterministic portfolio state (positions, totals, lineage) from internal projections only. " +
				"Read-only; never executes trades. Use when summarizing current holdings and exposure context.",
			InputSchema: map[string]any{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]any{
					"portfolio_id": map[string]any{"type": "string", "format": "uuid"},
				},
				"required": []string{"portfolio_id"},
			},
		},
		{
			Name: ToolGetRiskSnapshot,
			Description: "Compute read-only risk snapshot (VaR, volatility, concentration) from current projected positions, " +
				"latest marks, and historical symbol sigma inputs.",
			InputSchema: map[string]any{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]any{
					"portfolio_id":   map[string]any{"type": "string", "format": "uuid"},
					"sigma_window_n": map[string]any{"type": "integer", "minimum": 2, "maximum": 252},
				},
				"required": []string{"portfolio_id"},
			},
		},
		{
			Name: ToolGetPriceHistory,
			Description: "Return latest symbol price mark plus deterministic recent daily-return history for that symbol. " +
				"Read-only and sourced from internal price projections/history.",
			InputSchema: map[string]any{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]any{
					"symbol": map[string]any{"type": "string"},
					"limit":  map[string]any{"type": "integer", "minimum": 1, "maximum": 60},
				},
				"required": []string{"symbol"},
			},
		},
		{
			Name: ToolGetMarketNews,
			Description: "Return recent market-news headlines from configured provider adapter when available. " +
				"If provider is not configured in phase 1, returns deterministic unavailable payload.",
			InputSchema: map[string]any{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]any{
					"symbols": map[string]any{
						"type":  "array",
						"items": map[string]any{"type": "string"},
					},
					"limit": map[string]any{"type": "integer", "minimum": 1, "maximum": 20},
				},
			},
		},
		{
			Name: ToolGetPositions,
			Description: "Return normalized open and closed position rows from internal position projection for one portfolio. " +
				"Use this when the model needs raw position-level details.",
			InputSchema: map[string]any{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]any{
					"portfolio_id": map[string]any{"type": "string", "format": "uuid"},
				},
				"required": []string{"portfolio_id"},
			},
		},
		{
			Name: ToolGetBuyingPower,
			Description: "Return account buying power from Alpaca account status path for a linked portfolio. " +
				"When not configured/linked, returns deterministic not_configured payload.",
			InputSchema: map[string]any{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]any{
					"portfolio_id": map[string]any{"type": "string", "format": "uuid"},
				},
				"required": []string{"portfolio_id"},
			},
		},
		{
			Name: ToolGetWatchlist,
			Description: "Return symbol names on the price-feed watchlist (no prices). Prefer get_watchlist_market_snapshot for briefing scans.",
			InputSchema: map[string]any{
				"type":                 "object",
				"additionalProperties": false,
				"properties":          map[string]any{},
			},
		},
		{
			Name: ToolGetWatchlistMarketSnapshot,
			Description: "Return latest price and daily change for every watchlist symbol, with held flags vs portfolio. " +
				"Use this to compare opportunities across the full tracked universe, not only current holdings.",
			InputSchema: map[string]any{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]any{
					"portfolio_id": map[string]any{"type": "string", "format": "uuid"},
				},
				"required": []string{"portfolio_id"},
			},
		},
		{
			Name: ToolSubmitProposal,
			Description: "Submit an already human- or system-approved proposal to the broker via the server submit pipeline. " +
				"Requires proposal_id and portfolio_id. Does not place orders without an approved proposal row; never exposes broker credentials.",
			InputSchema: map[string]any{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]any{
					"portfolio_id": map[string]any{"type": "string", "format": "uuid"},
					"proposal_id":  map[string]any{"type": "string", "format": "uuid"},
				},
				"required": []string{"portfolio_id", "proposal_id"},
			},
		},
	}
}

func AnthropicToolCatalog() []AnthropicToolSpec {
	defs := ToolDefinitions()
	out := make([]AnthropicToolSpec, 0, len(defs))
	for _, d := range defs {
		out = append(out, AnthropicToolSpec{
			Name:        d.Name,
			Description: d.Description,
			InputSchema: d.InputSchema,
		})
	}
	return out
}

// WatchlistReader returns the live price-feed watchlist.
type WatchlistReader interface {
	Watchlist() []string
}

type ToolDataSource interface {
	LoadPortfolioAssemblerInput(ctx context.Context, portfolioID uuid.UUID) (portfolio.PortfolioAssemblerInput, bool, error)
	LoadSymbolSigma1D(ctx context.Context, symbols []string, windowN int) (map[string]decimal.Decimal, error)
	GetPriceSymbolDetail(ctx context.Context, symbol string, historyLimit int) (*events.PriceSymbolDetail, bool, error)
	ListPriceMarks(ctx context.Context, p events.ListPriceMarksParams) (events.ListPriceMarksResult, error)
}

type BuyingPowerProvider interface {
	GetBuyingPower(ctx context.Context, portfolioID uuid.UUID) (buyingPower string, configured bool, err error)
}

type MarketNewsItem struct {
	Title     string `json:"title"`
	Summary   string `json:"summary"`
	Source    string `json:"source"`
	Published string `json:"published"`
	URL       string `json:"url,omitempty"`
}

type MarketNewsProvider interface {
	GetMarketNews(ctx context.Context, symbols []string, limit int) ([]MarketNewsItem, error)
}

type ToolDispatcher struct {
	dataSource         ToolDataSource
	buyingPower        BuyingPowerProvider
	marketNewsProvider MarketNewsProvider
	proposalSubmitter  ProposalSubmitter
	watchlistReader    WatchlistReader
	barsProvider       DailyBarsProvider

	mu    sync.Mutex
	cache map[string]toolCacheEntry
}

type toolCacheEntry struct {
	result    ToolCallResult
	expiresAt time.Time
}

const toolCacheTTL = 30 * time.Minute

func NewToolDispatcher(dataSource ToolDataSource, buyingPower BuyingPowerProvider, marketNewsProvider MarketNewsProvider, proposalSubmitter ProposalSubmitter) *ToolDispatcher {
	return &ToolDispatcher{
		dataSource:         dataSource,
		buyingPower:        buyingPower,
		marketNewsProvider: marketNewsProvider,
		proposalSubmitter:  proposalSubmitter,
		cache:              make(map[string]toolCacheEntry),
	}
}

func (d *ToolDispatcher) WithWatchlist(r WatchlistReader) *ToolDispatcher {
	d.watchlistReader = r
	return d
}

func (d *ToolDispatcher) Execute(ctx context.Context, call ToolCallRequest) (ToolCallResult, error) {
	if d == nil {
		return ToolCallResult{}, fmt.Errorf("tool dispatcher: nil receiver")
	}
	cacheKey := d.memoKey(call)
	d.mu.Lock()
	d.cleanupExpiredLocked(time.Now().UTC())
	if cached, ok := d.cache[cacheKey]; ok {
		d.mu.Unlock()
		return cached.result, nil
	}
	d.mu.Unlock()

	start := time.Now()
	result, err := d.executeUncached(ctx, call)
	if result.LatencyMS <= 0 {
		result.LatencyMS = int(time.Since(start).Milliseconds())
	}
	if result.LatencyMS < 0 {
		result.LatencyMS = 0
	}
	if err != nil {
		if len(result.Output) == 0 {
			result.Output = json.RawMessage(`{"status":"error","error_code":"dispatch_failed"}`)
		}
		if strings.TrimSpace(result.Error) == "" {
			result.Error = err.Error()
		}
		result.Success = false
	}
	d.mu.Lock()
	d.cache[cacheKey] = toolCacheEntry{
		result:    result,
		expiresAt: time.Now().UTC().Add(toolCacheTTL),
	}
	d.mu.Unlock()
	return result, err
}

// ClearSessionCache removes all memoized tool outputs for one agent session.
func (d *ToolDispatcher) ClearSessionCache(sessionID string) {
	if d == nil {
		return
	}
	prefix := strings.TrimSpace(sessionID) + "|"
	d.mu.Lock()
	defer d.mu.Unlock()
	for key := range d.cache {
		if strings.HasPrefix(key, prefix) {
			delete(d.cache, key)
		}
	}
}

func (d *ToolDispatcher) cleanupExpiredLocked(now time.Time) {
	for key, entry := range d.cache {
		if !entry.expiresAt.IsZero() && now.After(entry.expiresAt) {
			delete(d.cache, key)
		}
	}
}

func (d *ToolDispatcher) memoKey(call ToolCallRequest) string {
	return strings.TrimSpace(call.SessionID) + "|" + strings.TrimSpace(call.ToolName) + "|" + strings.TrimSpace(string(call.Input))
}

func (d *ToolDispatcher) executeUncached(ctx context.Context, call ToolCallRequest) (ToolCallResult, error) {
	switch strings.TrimSpace(call.ToolName) {
	case ToolGetPortfolioState:
		return d.getPortfolioState(ctx, call.Input)
	case ToolGetRiskSnapshot:
		return d.getRiskSnapshot(ctx, call.Input)
	case ToolGetPriceHistory:
		return d.getPriceHistory(ctx, call.Input)
	case ToolGetMarketNews:
		return d.getMarketNews(ctx, call.Input)
	case ToolGetPositions:
		return d.getPositions(ctx, call.Input)
	case ToolGetBuyingPower:
		return d.getBuyingPower(ctx, call.Input)
	case ToolGetWatchlist:
		return d.getWatchlist(ctx, call.Input)
	case ToolGetWatchlistMarketSnapshot:
		return d.getWatchlistMarketSnapshot(ctx, call.Input)
	case ToolSubmitProposal:
		return d.submitProposal(ctx, call.Input)
	case ToolGetDailyBars:
		return d.getDailyBars(ctx, call.Input)
	case ToolGetTechnicalIndicators:
		return d.getTechnicalIndicators(ctx, call.Input)
	case ToolGetMarketRegime:
		return d.getMarketRegime(ctx, call.Input)
	default:
		return ToolCallResult{
			Output:  json.RawMessage(`{"status":"error","error_code":"unknown_tool"}`),
			Success: false,
			Error:   ErrToolUnknown.Error(),
		}, ErrToolUnknown
	}
}

type portfolioIDInput struct {
	PortfolioID string `json:"portfolio_id"`
}

func parsePortfolioIDInput(raw json.RawMessage) (uuid.UUID, error) {
	var in portfolioIDInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return uuid.Nil, fmt.Errorf("decode input: %w", err)
	}
	id, err := uuid.Parse(strings.TrimSpace(in.PortfolioID))
	if err != nil {
		return uuid.Nil, fmt.Errorf("portfolio_id must be uuid: %w", err)
	}
	return id, nil
}

func encodeToolOutput(v any) (ToolCallResult, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return ToolCallResult{}, err
	}
	return ToolCallResult{Output: b, Success: true}, nil
}

func (d *ToolDispatcher) getPortfolioState(ctx context.Context, raw json.RawMessage) (ToolCallResult, error) {
	pid, err := parsePortfolioIDInput(raw)
	if err != nil {
		return ToolCallResult{Output: json.RawMessage(`{"status":"error","error_code":"invalid_input"}`), Error: err.Error()}, err
	}
	if d.dataSource == nil {
		out := map[string]any{"status": "unavailable", "reason": "data_source_not_configured", "portfolio_id": pid.String()}
		return encodeToolOutput(out)
	}
	in, found, err := d.dataSource.LoadPortfolioAssemblerInput(ctx, pid)
	if err != nil {
		return ToolCallResult{Output: json.RawMessage(`{"status":"error","error_code":"load_failed"}`), Error: err.Error()}, err
	}
	if !found {
		out := map[string]any{"status": "missing_data", "reason": "portfolio_not_found", "portfolio_id": pid.String()}
		return encodeToolOutput(out)
	}
	view, err := portfolio.AssemblePortfolioView(in)
	if err != nil {
		return ToolCallResult{Output: json.RawMessage(`{"status":"error","error_code":"assemble_failed"}`), Error: err.Error()}, err
	}
	out := map[string]any{
		"status":       "ok",
		"portfolio_id": pid.String(),
		"snapshot":     view,
	}
	return encodeToolOutput(out)
}

type riskInput struct {
	PortfolioID  string `json:"portfolio_id"`
	SigmaWindowN int    `json:"sigma_window_n,omitempty"`
}

func (d *ToolDispatcher) getRiskSnapshot(ctx context.Context, raw json.RawMessage) (ToolCallResult, error) {
	var in riskInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return ToolCallResult{Output: json.RawMessage(`{"status":"error","error_code":"invalid_input"}`), Error: err.Error()}, err
	}
	pid, err := uuid.Parse(strings.TrimSpace(in.PortfolioID))
	if err != nil {
		return ToolCallResult{Output: json.RawMessage(`{"status":"error","error_code":"invalid_portfolio_id"}`), Error: err.Error()}, err
	}
	if in.SigmaWindowN <= 0 {
		in.SigmaWindowN = 60
	}
	if d.dataSource == nil {
		return encodeToolOutput(map[string]any{"status": "unavailable", "reason": "data_source_not_configured", "portfolio_id": pid.String()})
	}
	asm, found, err := d.dataSource.LoadPortfolioAssemblerInput(ctx, pid)
	if err != nil {
		return ToolCallResult{Output: json.RawMessage(`{"status":"error","error_code":"load_failed"}`), Error: err.Error()}, err
	}
	if !found {
		return encodeToolOutput(map[string]any{"status": "missing_data", "reason": "portfolio_not_found", "portfolio_id": pid.String()})
	}
	riskIn := risk.Input{
		Positions: make([]risk.PositionInput, 0, len(asm.Positions)),
		Prices:    map[string]decimal.Decimal{},
		Sigma1D:   map[string]decimal.Decimal{},
	}
	symbols := make([]string, 0, len(asm.Positions))
	for _, p := range asm.Positions {
		if p.Quantity.IsZero() {
			continue
		}
		riskIn.Positions = append(riskIn.Positions, risk.PositionInput{Symbol: p.Symbol, Quantity: p.Quantity})
		symbols = append(symbols, p.Symbol)
		if pm, ok := asm.PriceBySymbol[p.Symbol]; ok && !pm.Price.IsZero() {
			riskIn.Prices[p.Symbol] = pm.Price
		}
	}
	sigmaBySymbol, err := d.dataSource.LoadSymbolSigma1D(ctx, symbols, in.SigmaWindowN)
	if err != nil {
		return ToolCallResult{Output: json.RawMessage(`{"status":"error","error_code":"sigma_load_failed"}`), Error: err.Error()}, err
	}
	for sym := range riskIn.Prices {
		if s, ok := sigmaBySymbol[sym]; ok {
			riskIn.Sigma1D[sym] = s
			continue
		}
		riskIn.Sigma1D[sym] = decimal.Zero
	}
	snapshot, err := risk.NewEngine().BuildSnapshot(riskIn)
	if err != nil {
		return ToolCallResult{Output: json.RawMessage(`{"status":"error","error_code":"risk_compute_failed"}`), Error: err.Error()}, err
	}
	return encodeToolOutput(map[string]any{
		"status":       "ok",
		"portfolio_id": pid.String(),
		"risk":         snapshot,
	})
}

type priceHistoryInput struct {
	Symbol string `json:"symbol"`
	Limit  int    `json:"limit,omitempty"`
}

func (d *ToolDispatcher) getPriceHistory(ctx context.Context, raw json.RawMessage) (ToolCallResult, error) {
	var in priceHistoryInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return ToolCallResult{Output: json.RawMessage(`{"status":"error","error_code":"invalid_input"}`), Error: err.Error()}, err
	}
	in.Symbol = strings.ToUpper(strings.TrimSpace(in.Symbol))
	if in.Symbol == "" {
		return ToolCallResult{Output: json.RawMessage(`{"status":"error","error_code":"symbol_required"}`), Error: "symbol required"}, fmt.Errorf("symbol required")
	}
	if in.Limit <= 0 {
		in.Limit = 10
	}
	if d.dataSource == nil {
		return encodeToolOutput(map[string]any{"status": "unavailable", "reason": "data_source_not_configured", "symbol": in.Symbol})
	}
	detail, found, err := d.dataSource.GetPriceSymbolDetail(ctx, in.Symbol, in.Limit)
	if err != nil {
		return ToolCallResult{Output: json.RawMessage(`{"status":"error","error_code":"price_history_load_failed"}`), Error: err.Error()}, err
	}
	if !found || detail == nil {
		return encodeToolOutput(map[string]any{"status": "missing_data", "reason": "symbol_not_found", "symbol": in.Symbol, "history": []any{}})
	}
	return encodeToolOutput(map[string]any{
		"status": "ok",
		"symbol": in.Symbol,
		"price":  detail,
	})
}

type newsInput struct {
	Symbols []string `json:"symbols,omitempty"`
	Limit   int      `json:"limit,omitempty"`
}

func (d *ToolDispatcher) getMarketNews(ctx context.Context, raw json.RawMessage) (ToolCallResult, error) {
	var in newsInput
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &in); err != nil {
			return ToolCallResult{Output: json.RawMessage(`{"status":"error","error_code":"invalid_input"}`), Error: err.Error()}, err
		}
	}
	if in.Limit <= 0 {
		in.Limit = 5
	}
	symbols := make([]string, 0, len(in.Symbols))
	for _, s := range in.Symbols {
		s = strings.ToUpper(strings.TrimSpace(s))
		if s != "" {
			symbols = append(symbols, s)
		}
	}
	sort.Strings(symbols)
	if d.marketNewsProvider == nil {
		return encodeToolOutput(map[string]any{
			"status":  "unavailable",
			"reason":  "not_configured",
			"provider": "phase1_news_adapter",
			"items":   []MarketNewsItem{},
			"symbols": symbols,
		})
	}
	items, err := d.marketNewsProvider.GetMarketNews(ctx, symbols, in.Limit)
	if err != nil {
		return ToolCallResult{Output: json.RawMessage(`{"status":"error","error_code":"market_news_failed"}`), Error: err.Error()}, err
	}
	return encodeToolOutput(map[string]any{
		"status":   "ok",
		"provider": "configured",
		"symbols":  symbols,
		"items":    items,
	})
}

func (d *ToolDispatcher) getPositions(ctx context.Context, raw json.RawMessage) (ToolCallResult, error) {
	pid, err := parsePortfolioIDInput(raw)
	if err != nil {
		return ToolCallResult{Output: json.RawMessage(`{"status":"error","error_code":"invalid_input"}`), Error: err.Error()}, err
	}
	if d.dataSource == nil {
		return encodeToolOutput(map[string]any{"status": "unavailable", "reason": "data_source_not_configured", "portfolio_id": pid.String()})
	}
	asm, found, err := d.dataSource.LoadPortfolioAssemblerInput(ctx, pid)
	if err != nil {
		return ToolCallResult{Output: json.RawMessage(`{"status":"error","error_code":"load_failed"}`), Error: err.Error()}, err
	}
	if !found {
		return encodeToolOutput(map[string]any{"status": "missing_data", "reason": "portfolio_not_found", "portfolio_id": pid.String(), "positions": []any{}})
	}
	type row struct {
		Symbol      string `json:"symbol"`
		Quantity    string `json:"quantity"`
		AverageCost string `json:"average_cost"`
		RealizedPnL string `json:"realized_pnl"`
	}
	outRows := make([]row, 0, len(asm.Positions))
	for _, p := range asm.Positions {
		outRows = append(outRows, row{
			Symbol:      p.Symbol,
			Quantity:    p.Quantity.String(),
			AverageCost: p.AverageCost.String(),
			RealizedPnL: p.RealizedPnL.String(),
		})
	}
	return encodeToolOutput(map[string]any{
		"status":       "ok",
		"portfolio_id": pid.String(),
		"positions":    outRows,
	})
}

func (d *ToolDispatcher) getBuyingPower(ctx context.Context, raw json.RawMessage) (ToolCallResult, error) {
	pid, err := parsePortfolioIDInput(raw)
	if err != nil {
		return ToolCallResult{Output: json.RawMessage(`{"status":"error","error_code":"invalid_input"}`), Error: err.Error()}, err
	}
	if d.buyingPower == nil {
		return encodeToolOutput(map[string]any{
			"status":       "not_configured",
			"portfolio_id": pid.String(),
			"buying_power": "",
		})
	}
	value, configured, err := d.buyingPower.GetBuyingPower(ctx, pid)
	if err != nil {
		return ToolCallResult{Output: json.RawMessage(`{"status":"error","error_code":"buying_power_failed"}`), Error: err.Error()}, err
	}
	if !configured {
		return encodeToolOutput(map[string]any{
			"status":       "not_configured",
			"portfolio_id": pid.String(),
			"buying_power": "",
		})
	}
	return encodeToolOutput(map[string]any{
		"status":       "ok",
		"portfolio_id": pid.String(),
		"buying_power": strings.TrimSpace(value),
	})
}

type submitProposalInput struct {
	PortfolioID string `json:"portfolio_id"`
	ProposalID  string `json:"proposal_id"`
}

func (d *ToolDispatcher) getWatchlistMarketSnapshot(ctx context.Context, raw json.RawMessage) (ToolCallResult, error) {
	pid, err := parsePortfolioIDInput(raw)
	if err != nil {
		return ToolCallResult{Output: json.RawMessage(`{"status":"error","error_code":"invalid_input"}`), Error: err.Error()}, err
	}
	if d.dataSource == nil {
		return encodeToolOutput(map[string]any{"status": "unavailable", "reason": "data_source_not_configured"})
	}
	snap, err := d.MarketSnapshotJSON(ctx, pid)
	if err != nil {
		return ToolCallResult{Output: json.RawMessage(`{"status":"error","error_code":"snapshot_failed"}`), Error: err.Error()}, err
	}
	var payload any
	_ = json.Unmarshal(snap, &payload)
	return encodeToolOutput(map[string]any{"status": "ok", "portfolio_id": pid.String(), "snapshot": payload})
}

func (d *ToolDispatcher) getWatchlist(_ context.Context, _ json.RawMessage) (ToolCallResult, error) {
	if d.watchlistReader == nil {
		return encodeToolOutput(map[string]any{
			"status":  "unavailable",
			"reason":  "not_configured",
			"symbols": []string{},
			"count":   0,
		})
	}
	symbols := d.watchlistReader.Watchlist()
	if symbols == nil {
		symbols = []string{}
	}
	return encodeToolOutput(map[string]any{
		"status":  "ok",
		"symbols": symbols,
		"count":   len(symbols),
	})
}

func (d *ToolDispatcher) submitProposal(ctx context.Context, raw json.RawMessage) (ToolCallResult, error) {
	if d.proposalSubmitter == nil {
		return encodeToolOutput(map[string]any{
			"status":     "not_configured",
			"error_code": "submit_not_configured",
		})
	}
	var in submitProposalInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return ToolCallResult{Output: json.RawMessage(`{"status":"error","error_code":"invalid_input"}`), Error: err.Error()}, err
	}
	pid, err := uuid.Parse(strings.TrimSpace(in.PortfolioID))
	if err != nil {
		return ToolCallResult{Output: json.RawMessage(`{"status":"error","error_code":"invalid_portfolio_id"}`), Error: err.Error()}, err
	}
	propID, err := uuid.Parse(strings.TrimSpace(in.ProposalID))
	if err != nil {
		return ToolCallResult{Output: json.RawMessage(`{"status":"error","error_code":"invalid_proposal_id"}`), Error: err.Error()}, err
	}
	res := d.proposalSubmitter.SubmitApproved(ctx, pid, propID)
	out := map[string]any{
		"status":    "error",
		"outcome":   string(res.Outcome),
		"proposal_id": propID.String(),
	}
	switch res.Outcome {
	case submit.OutcomeSuccess:
		out["status"] = "submitted"
		out["broker_order_id"] = res.BrokerOrderID
	case submit.OutcomeBadStatus:
		out["error_code"] = "not_approved"
		out["proposal_status"] = res.ProposalStatus
	case submit.OutcomePolicyDenied:
		out["error_code"] = "policy_denied"
	default:
		out["error_code"] = string(res.Outcome)
		if res.BrokerDetail != "" {
			out["detail"] = res.BrokerDetail
		}
	}
	return encodeToolOutput(out)
}
