package agent

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/KevinMReardon/realtime-portfolio-risk/internal/events"
	"github.com/KevinMReardon/realtime-portfolio-risk/internal/portfolio"
)

type fakeToolDataSource struct {
	loadAssemblerCalls int
	loadSigmaCalls     int
	priceDetailCalls   int

	assemblerInput portfolio.PortfolioAssemblerInput
	assemblerFound bool
	assemblerErr   error

	sigma map[string]decimal.Decimal

	priceDetail *events.PriceSymbolDetail
	priceFound  bool
	priceErr    error
}

func (f *fakeToolDataSource) LoadPortfolioAssemblerInput(context.Context, uuid.UUID) (portfolio.PortfolioAssemblerInput, bool, error) {
	f.loadAssemblerCalls++
	return f.assemblerInput, f.assemblerFound, f.assemblerErr
}

func (f *fakeToolDataSource) LoadSymbolSigma1D(context.Context, []string, int) (map[string]decimal.Decimal, error) {
	f.loadSigmaCalls++
	return f.sigma, nil
}

func (f *fakeToolDataSource) GetPriceSymbolDetail(context.Context, string, int) (*events.PriceSymbolDetail, bool, error) {
	f.priceDetailCalls++
	return f.priceDetail, f.priceFound, f.priceErr
}

type fakeBuyingPower struct {
	value      string
	configured bool
	err        error
	calls      int
}

func (f *fakeBuyingPower) GetBuyingPower(context.Context, uuid.UUID) (string, bool, error) {
	f.calls++
	return f.value, f.configured, f.err
}

type fakeNewsProvider struct {
	items []MarketNewsItem
	err   error
	calls int
}

func (f *fakeNewsProvider) GetMarketNews(context.Context, []string, int) ([]MarketNewsItem, error) {
	f.calls++
	return f.items, f.err
}

func TestTools_GetPortfolioState_SuccessAndMissing(t *testing.T) {
	t.Parallel()
	pid := uuid.New()
	ds := &fakeToolDataSource{
		assemblerFound: true,
		assemblerInput: portfolio.PortfolioAssemblerInput{
			PortfolioID: pid,
			Positions: []portfolio.ProjectionRow{
				{Symbol: "AAPL", Quantity: decimal.NewFromInt(10), AverageCost: decimal.NewFromInt(100)},
			},
		},
	}
	d := NewToolDispatcher(ds, nil, nil)
	in := json.RawMessage(`{"portfolio_id":"` + pid.String() + `"}`)

	res, err := d.Execute(context.Background(), ToolCallRequest{SessionID: "s1", ToolName: ToolGetPortfolioState, Input: in})
	if err != nil {
		t.Fatalf("get_portfolio_state success err: %v", err)
	}
	if !res.Success {
		t.Fatalf("expected success, got %+v", res)
	}

	ds.assemblerFound = false
	res, err = d.Execute(context.Background(), ToolCallRequest{SessionID: "s2", ToolName: ToolGetPortfolioState, Input: in})
	if err != nil {
		t.Fatalf("get_portfolio_state missing err: %v", err)
	}
	var body map[string]any
	_ = json.Unmarshal(res.Output, &body)
	if body["status"] != "missing_data" {
		t.Fatalf("expected missing_data, got %v", body["status"])
	}
}

func TestTools_GetRiskSnapshot_SuccessAndMissing(t *testing.T) {
	t.Parallel()
	pid := uuid.New()
	ds := &fakeToolDataSource{
		assemblerFound: true,
		assemblerInput: portfolio.PortfolioAssemblerInput{
			PortfolioID: pid,
			Positions: []portfolio.ProjectionRow{
				{Symbol: "AAPL", Quantity: decimal.NewFromInt(5), AverageCost: decimal.NewFromInt(100)},
			},
			PriceBySymbol: map[string]portfolio.PriceMarkInput{
				"AAPL": {Price: decimal.NewFromInt(110)},
			},
		},
		sigma: map[string]decimal.Decimal{"AAPL": decimal.RequireFromString("0.02")},
	}
	d := NewToolDispatcher(ds, nil, nil)
	in := json.RawMessage(`{"portfolio_id":"` + pid.String() + `"}`)
	res, err := d.Execute(context.Background(), ToolCallRequest{SessionID: "s1", ToolName: ToolGetRiskSnapshot, Input: in})
	if err != nil {
		t.Fatalf("get_risk_snapshot success err: %v", err)
	}
	if !res.Success {
		t.Fatalf("expected success, got %+v", res)
	}

	ds.assemblerFound = false
	res, err = d.Execute(context.Background(), ToolCallRequest{SessionID: "s2", ToolName: ToolGetRiskSnapshot, Input: in})
	if err != nil {
		t.Fatalf("get_risk_snapshot missing err: %v", err)
	}
	var body map[string]any
	_ = json.Unmarshal(res.Output, &body)
	if body["status"] != "missing_data" {
		t.Fatalf("expected missing_data, got %v", body["status"])
	}
}

func TestTools_GetPriceHistory_SuccessAndMissing(t *testing.T) {
	t.Parallel()
	ds := &fakeToolDataSource{
		priceFound: true,
		priceDetail: &events.PriceSymbolDetail{
			Symbol: "AAPL",
			Price:  "200",
			AsOf:   time.Now().UTC(),
			History: []events.PriceHistoryPoint{
				{ReturnDate: "2026-04-25", ClosePrice: "200"},
			},
		},
	}
	d := NewToolDispatcher(ds, nil, nil)
	res, err := d.Execute(context.Background(), ToolCallRequest{
		SessionID: "s1", ToolName: ToolGetPriceHistory, Input: json.RawMessage(`{"symbol":"AAPL","limit":5}`),
	})
	if err != nil {
		t.Fatalf("get_price_history success err: %v", err)
	}
	if !res.Success {
		t.Fatalf("expected success, got %+v", res)
	}
	ds.priceFound = false
	res, err = d.Execute(context.Background(), ToolCallRequest{
		SessionID: "s2", ToolName: ToolGetPriceHistory, Input: json.RawMessage(`{"symbol":"AAPL","limit":5}`),
	})
	if err != nil {
		t.Fatalf("get_price_history missing err: %v", err)
	}
	var body map[string]any
	_ = json.Unmarshal(res.Output, &body)
	if body["status"] != "missing_data" {
		t.Fatalf("expected missing_data, got %v", body["status"])
	}
}

func TestTools_GetMarketNews_SuccessAndUnavailable(t *testing.T) {
	t.Parallel()
	dUnavailable := NewToolDispatcher(nil, nil, nil)
	res, err := dUnavailable.Execute(context.Background(), ToolCallRequest{
		SessionID: "s1", ToolName: ToolGetMarketNews, Input: json.RawMessage(`{"symbols":["AAPL"],"limit":2}`),
	})
	if err != nil {
		t.Fatalf("get_market_news unavailable err: %v", err)
	}
	var body map[string]any
	_ = json.Unmarshal(res.Output, &body)
	if body["status"] != "unavailable" {
		t.Fatalf("expected unavailable, got %v", body["status"])
	}

	news := &fakeNewsProvider{
		items: []MarketNewsItem{{Title: "Headline", Summary: "Summary", Source: "wire", Published: "2026-04-26T00:00:00Z"}},
	}
	dConfigured := NewToolDispatcher(nil, nil, news)
	res, err = dConfigured.Execute(context.Background(), ToolCallRequest{
		SessionID: "s2", ToolName: ToolGetMarketNews, Input: json.RawMessage(`{"symbols":["AAPL"],"limit":2}`),
	})
	if err != nil {
		t.Fatalf("get_market_news configured err: %v", err)
	}
	_ = json.Unmarshal(res.Output, &body)
	if body["status"] != "ok" {
		t.Fatalf("expected ok, got %v", body["status"])
	}
}

func TestTools_GetPositions_SuccessAndMissing(t *testing.T) {
	t.Parallel()
	pid := uuid.New()
	ds := &fakeToolDataSource{
		assemblerFound: true,
		assemblerInput: portfolio.PortfolioAssemblerInput{
			PortfolioID: pid,
			Positions: []portfolio.ProjectionRow{
				{Symbol: "MSFT", Quantity: decimal.NewFromInt(2), AverageCost: decimal.NewFromInt(250)},
			},
		},
	}
	d := NewToolDispatcher(ds, nil, nil)
	in := json.RawMessage(`{"portfolio_id":"` + pid.String() + `"}`)
	res, err := d.Execute(context.Background(), ToolCallRequest{SessionID: "s1", ToolName: ToolGetPositions, Input: in})
	if err != nil {
		t.Fatalf("get_positions success err: %v", err)
	}
	if !res.Success {
		t.Fatalf("expected success, got %+v", res)
	}
	ds.assemblerFound = false
	res, err = d.Execute(context.Background(), ToolCallRequest{SessionID: "s2", ToolName: ToolGetPositions, Input: in})
	if err != nil {
		t.Fatalf("get_positions missing err: %v", err)
	}
	var body map[string]any
	_ = json.Unmarshal(res.Output, &body)
	if body["status"] != "missing_data" {
		t.Fatalf("expected missing_data, got %v", body["status"])
	}
}

func TestTools_GetBuyingPower_SuccessAndNotConfigured(t *testing.T) {
	t.Parallel()
	pid := uuid.New()
	dNotConfigured := NewToolDispatcher(nil, nil, nil)
	in := json.RawMessage(`{"portfolio_id":"` + pid.String() + `"}`)
	res, err := dNotConfigured.Execute(context.Background(), ToolCallRequest{SessionID: "s1", ToolName: ToolGetBuyingPower, Input: in})
	if err != nil {
		t.Fatalf("get_buying_power not_configured err: %v", err)
	}
	var body map[string]any
	_ = json.Unmarshal(res.Output, &body)
	if body["status"] != "not_configured" {
		t.Fatalf("expected not_configured, got %v", body["status"])
	}

	bp := &fakeBuyingPower{value: "10000.50", configured: true}
	dConfigured := NewToolDispatcher(nil, bp, nil)
	res, err = dConfigured.Execute(context.Background(), ToolCallRequest{SessionID: "s2", ToolName: ToolGetBuyingPower, Input: in})
	if err != nil {
		t.Fatalf("get_buying_power configured err: %v", err)
	}
	_ = json.Unmarshal(res.Output, &body)
	if body["status"] != "ok" {
		t.Fatalf("expected ok, got %v", body["status"])
	}
}

func TestTools_RequestScopedMemoization(t *testing.T) {
	t.Parallel()
	pid := uuid.New()
	ds := &fakeToolDataSource{
		assemblerFound: true,
		assemblerInput: portfolio.PortfolioAssemblerInput{
			PortfolioID: pid,
			Positions:   []portfolio.ProjectionRow{},
		},
	}
	d := NewToolDispatcher(ds, nil, nil)
	in := json.RawMessage(`{"portfolio_id":"` + pid.String() + `"}`)
	call := ToolCallRequest{SessionID: "memo-session", ToolName: ToolGetPositions, Input: in}
	_, err := d.Execute(context.Background(), call)
	if err != nil {
		t.Fatalf("first execute: %v", err)
	}
	_, err = d.Execute(context.Background(), call)
	if err != nil {
		t.Fatalf("second execute: %v", err)
	}
	if ds.loadAssemblerCalls != 1 {
		t.Fatalf("expected memoized single datasource call, got %d", ds.loadAssemblerCalls)
	}
}
