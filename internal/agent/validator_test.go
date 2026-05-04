package agent

import (
	"errors"
	"strings"
	"testing"
)

const validPayload = `{
  "market_summary": "Markets are mixed; volatility is elevated.",
  "portfolio_context": "Portfolio has concentrated single-name exposure.",
  "trade_ideas": [
    {
      "symbol": "AAPL",
      "rationale": "High concentration can be reduced gradually.",
      "confidence": 0.62,
      "size": "Trim 10% of current position",
      "stop": "If price closes above recent high, pause trimming",
      "target": "Reduce single-name weight below 25%"
    }
  ],
  "risks_and_caveats": "Signal quality is limited by short lookback.",
  "data_gaps": ["Missing intraday liquidity metrics"],
  "disclaimer": "Educational only; this is not investment advice.",
  "used_sources": ["portfolio_snapshot_v1", "risk_snapshot_v1"],
  "used_fields": ["portfolio.positions[0].quantity", "risk.var_1d"]
}`

func TestValidateBriefingOutput_ok(t *testing.T) {
	t.Parallel()
	out, err := ValidateBriefingOutput([]byte(validPayload))
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if out.MarketSummary == "" || len(out.TradeIdeas) != 1 {
		t.Fatalf("unexpected output parsed: %+v", out)
	}
}

func TestValidateBriefingOutput_canonicalizesAndIgnoresUnknownField(t *testing.T) {
	t.Parallel()
	payload := `{"market_summary":"x","portfolio_context":"x","trade_ideas":[],"risks_and_caveats":"x","data_gaps":[],"disclaimer":"x","used_sources":[],"used_fields":[],"extra":"boom"}`
	out, err := ValidateBriefingOutput([]byte(payload))
	if err != nil {
		t.Fatalf("unexpected err after canonicalization: %v", err)
	}
	if out.MarketSummary != "x" || out.PortfolioContext != "x" {
		t.Fatalf("unexpected normalized output: %+v", out)
	}
}

func TestValidateBriefingOutput_acceptsFencedJSON(t *testing.T) {
	t.Parallel()
	payload := "```json\n" + validPayload + "\n```"
	out, err := ValidateBriefingOutput([]byte(payload))
	if err != nil {
		t.Fatalf("unexpected err for fenced JSON: %v", err)
	}
	if out.MarketSummary == "" || len(out.TradeIdeas) != 1 {
		t.Fatalf("unexpected parsed output: %+v", out)
	}
}

func TestValidateBriefingOutput_acceptsPreambleBeforeJSON(t *testing.T) {
	t.Parallel()
	payload := "I now have all the data needed. Composing the full briefing:\n\n" + validPayload
	out, err := ValidateBriefingOutput([]byte(payload))
	if err != nil {
		t.Fatalf("unexpected err for prefixed JSON payload: %v", err)
	}
	if out.MarketSummary == "" || len(out.TradeIdeas) != 1 {
		t.Fatalf("unexpected parsed output: %+v", out)
	}
}

func TestValidateBriefingOutput_acceptsPlainTextFallback(t *testing.T) {
	t.Parallel()
	payload := "I analyzed the current portfolio and market context. Concentration risk appears elevated."
	out, err := ValidateBriefingOutput([]byte(payload))
	if err != nil {
		t.Fatalf("unexpected err for plain-text payload fallback: %v", err)
	}
	if strings.TrimSpace(out.MarketSummary) == "" {
		t.Fatal("expected non-empty market_summary")
	}
	if strings.TrimSpace(out.PortfolioContext) == "" {
		t.Fatal("expected non-empty portfolio_context")
	}
	if out.TradeIdeas == nil {
		t.Fatal("expected non-nil trade_ideas")
	}
	if strings.TrimSpace(out.Disclaimer) == "" {
		t.Fatal("expected non-empty disclaimer")
	}
}

func TestValidateBriefingOutput_orderTypeAndTIFAlone_ok(t *testing.T) {
	t.Parallel()
	payload := `{
	  "market_summary":"m",
	  "portfolio_context":"p",
	  "trade_ideas":[{"symbol":"AAPL","rationale":"r","confidence":0.5,"size":"s","stop":"st","target":"t","order_type":"market","time_in_force":"day"}],
	  "risks_and_caveats":"r",
	  "data_gaps":[],
	  "disclaimer":"d",
	  "used_sources":[],
	  "used_fields":[]
	}`
	_, err := ValidateBriefingOutput([]byte(payload))
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
}

func TestValidateBriefingOutput_limitOrderTypeRequiresLimitPrice(t *testing.T) {
	t.Parallel()
	payload := `{
	  "market_summary":"m",
	  "portfolio_context":"p",
	  "trade_ideas":[{"symbol":"AAPL","rationale":"r","confidence":0.5,"size":"s","stop":"st","target":"t","side":"BUY","quantity":"1","order_type":"limit"}],
	  "risks_and_caveats":"r",
	  "data_gaps":[],
	  "disclaimer":"d",
	  "used_sources":[],
	  "used_fields":[]
	}`
	_, err := ValidateBriefingOutput([]byte(payload))
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("expected ValidationError, got %v", err)
	}
	found := false
	for _, issue := range ve.Issues {
		if strings.Contains(issue.Field, "limit_price") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected limit_price issue, got %#v", ve.Issues)
	}
}

func TestValidateBriefingOutput_notionalBelowOneDollarFails(t *testing.T) {
	t.Parallel()
	payload := `{
	  "market_summary":"m",
	  "portfolio_context":"p",
	  "trade_ideas":[{"rationale":"r","confidence":0.5,"size":"s","stop":"st","target":"t","symbol":"AAPL","side":"BUY","quantity":"","notional_usd":"0.50","order_type":"market","time_in_force":"day"}],
	  "risks_and_caveats":"r",
	  "data_gaps":[],
	  "disclaimer":"d",
	  "used_sources":[],
	  "used_fields":[]
	}`
	_, err := ValidateBriefingOutput([]byte(payload))
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("expected ValidationError, got %v", err)
	}
	found := false
	for _, issue := range ve.Issues {
		if strings.Contains(issue.Field, "notional_usd") && issue.Code == ValidationCodeInvalidTradeIdea {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected notional_usd issue, got %#v", ve.Issues)
	}
}

func TestValidateBriefingOutput_structuredTrade_requiresSymbolSideQtyOrNotional(t *testing.T) {
	t.Parallel()
	payload := `{
	  "market_summary":"m",
	  "portfolio_context":"p",
	  "trade_ideas":[{"rationale":"r","confidence":0.5,"size":"s","stop":"st","target":"t","side":"BUY"}],
	  "risks_and_caveats":"r",
	  "data_gaps":[],
	  "disclaimer":"d",
	  "used_sources":[],
	  "used_fields":[]
	}`
	_, err := ValidateBriefingOutput([]byte(payload))
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("expected ValidationError, got %v", err)
	}
	found := false
	for _, issue := range ve.Issues {
		if strings.Contains(issue.Field, "trade_ideas[0]") && issue.Code == ValidationCodeInvalidTradeIdea {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected trade_ideas invalid issues, got %#v", ve.Issues)
	}
}

func TestValidateBriefingOutput_confidenceBounds(t *testing.T) {
	t.Parallel()
	payload := `{
	  "market_summary":"m",
	  "portfolio_context":"p",
	  "trade_ideas":[{"rationale":"r","confidence":1.2,"size":"s","stop":"st","target":"t"}],
	  "risks_and_caveats":"r",
	  "data_gaps":[],
	  "disclaimer":"d",
	  "used_sources":[],
	  "used_fields":[]
	}`
	_, err := ValidateBriefingOutput([]byte(payload))
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("expected ValidationError, got %v", err)
	}
	found := false
	for _, issue := range ve.Issues {
		if issue.Code == ValidationCodeInvalidConfidence {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected INVALID_CONFIDENCE issue, got %+v", ve.Issues)
	}
}

func TestValidateBriefingOutput_bannedExecutionPhraseIsSanitized(t *testing.T) {
	t.Parallel()
	payload := `{
	  "market_summary":"I executed your trade at open.",
	  "portfolio_context":"p",
	  "trade_ideas":[{"rationale":"r","confidence":0.5,"size":"s","stop":"st","target":"t"}],
	  "risks_and_caveats":"r",
	  "data_gaps":[],
	  "disclaimer":"d",
	  "used_sources":[],
	  "used_fields":[]
	}`
	out, err := ValidateBriefingOutput([]byte(payload))
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if strings.Contains(strings.ToLower(out.MarketSummary), "executed your trade") {
		t.Fatalf("expected execution phrase to be sanitized, got %q", out.MarketSummary)
	}
}

func TestValidateBriefingOutput_requiresRationale(t *testing.T) {
	t.Parallel()
	payload := `{
	  "market_summary":"m",
	  "portfolio_context":"p",
	  "trade_ideas":[{"rationale":" ","confidence":0.5,"size":"s","stop":"st","target":"t"}],
	  "risks_and_caveats":"r",
	  "data_gaps":[],
	  "disclaimer":"d",
	  "used_sources":[],
	  "used_fields":[]
	}`
	_, err := ValidateBriefingOutput([]byte(payload))
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("expected ValidationError, got %v", err)
	}
	found := false
	for _, issue := range ve.Issues {
		if issue.Code == ValidationCodeInvalidTradeIdea && issue.Field == "trade_ideas[0].rationale" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected rationale issue, got %+v", ve.Issues)
	}
}

func TestValidateBriefingOutput_canonicalizesObjectStyleModelResponse(t *testing.T) {
	t.Parallel()
	payload := `{
	  "market_summary": {"status":"unavailable","detail":"news unavailable"},
	  "portfolio_context": {"status":"unavailable","detail":"portfolio id missing"},
	  "trade_ideas": [
	    {
	      "id":"TI-1",
	      "action":"REDUCE CONCENTRATION",
	      "rationale":"generic proposal",
	      "confidence":"low",
	      "proposed_size":"unknown"
	    }
	  ],
	  "risks_and_caveats":["risk one","risk two"],
	  "data_gaps":["missing portfolio_id"],
	  "disclaimer":"educational only",
	  "used_sources":[{"tool":"get_market_news","status":"unavailable"}],
	  "used_fields":["get_market_news.status"]
	}`
	out, err := ValidateBriefingOutput([]byte(payload))
	if err != nil {
		t.Fatalf("unexpected err after canonicalization: %v", err)
	}
	if out.MarketSummary != "news unavailable" {
		t.Fatalf("market_summary = %q, want flattened detail", out.MarketSummary)
	}
	if out.PortfolioContext != "portfolio id missing" {
		t.Fatalf("portfolio_context = %q, want flattened detail", out.PortfolioContext)
	}
	if len(out.TradeIdeas) != 1 {
		t.Fatalf("trade_ideas len = %d, want 1", len(out.TradeIdeas))
	}
	if out.TradeIdeas[0].Confidence != 0.3 {
		t.Fatalf("confidence = %v, want 0.3 from \"low\"", out.TradeIdeas[0].Confidence)
	}
	if out.RisksAndCaveats == "" {
		t.Fatal("expected joined risks_and_caveats string")
	}
}
