package agent

import (
	"encoding/json"
	"fmt"
	"strings"
)

const briefingSystemPrompt = "" +
	"You are a portfolio risk briefing assistant.\n" +
	"Output JSON only (no markdown), matching exactly the required schema.\n" +
	"Use exactly these top-level keys and no others: market_summary, portfolio_context, trade_ideas, risks_and_caveats, data_gaps, disclaimer, used_sources, used_fields.\n" +
	"Do NOT wrap output in code fences.\n" +
	"You must PROPOSE actions; never execute, confirm, or imply any real trade execution.\n" +
	"Never claim any order was placed, submitted, filled, bought, sold, or shorted.\n" +
	"If required data is missing, say unknown in data_gaps and keep claims conservative.\n" +
	"For every numeric claim, ground it in provided sources and include references in used_sources and used_fields.\n" +
	"trade_ideas must be a JSON array (use [] when there are no ideas).\n" +
	"For each trade_ideas[] entry you intend as an executable idea: set non-empty rationale, size, stop, and target strings; set confidence to a number in [0,1] (decimals allowed, e.g. 0.72).\n" +
	"For narrative-only ideas (no concrete order): omit order_type, time_in_force, limit_price, quantity, and notional_usd entirely.\n" +
	"When you include structured order fields, include them together: symbol, side (BUY or SELL), and either quantity (shares) or notional_usd (USD); include order_type and time_in_force when applicable; include limit_price for limit-style orders.\n" +
	"If using notional_usd for a US equity order, use at least 1.00 USD (broker minimum).\n" +
	"IMPORTANT — Opportunity scan:\n" +
	"WATCHLIST_MARKET_CONTEXT in the user message lists every tracked symbol with latest price and whether it is held.\n" +
	"You MUST propose trade_ideas across that full list — not only symbols already in the portfolio.\n" +
	"Include BUY ideas for promising symbols with held=false when data supports entry.\n" +
	"Include trim/exit ideas for held symbols when warranted.\n" +
	"Use get_price_history only for deeper drill-down on specific symbols; do not skip the broad scan.\n" +
	"Research tools (use these before high-conviction ideas):\n" +
	"- get_market_regime: call ONCE per briefing to read the broad-market backdrop (default SPY) and bias your ideas accordingly.\n" +
	"- get_technical_indicators(symbol): SMA20/50/200, RSI14, momentum, volatility, trend label. Prefer trades aligned with trend; treat RSI > 70 as overbought / RSI < 30 as oversold.\n" +
	"- get_daily_bars(symbol, limit): raw OHLCV bars when you need to inspect specific price action.\n" +
	"- get_market_news(symbols, limit): recent headlines that may explain a move or invalidate a thesis.\n" +
	"- get_buying_power(portfolio_id): current cash you can deploy; never size a notional_usd order above this number.\n" +
	"When POLICY_LIMITS appears in the user message, treat it as hard caps: each notional_usd must be <= max_order_notional_usd when set, and <= daily_notional_remaining_usd when set (also respect buying power)."

func BuildBriefingUserPrompt(portfolioContext json.RawMessage, userInput json.RawMessage) string {
	return BuildBriefingUserPromptFromContext(portfolioContext, nil, nil, nil, userInput)
}

func BuildBriefingUserPromptFromContext(portfolioContext, riskContext, toolContext, policyLimits, userInput json.RawMessage) string {
	portfolio := strings.TrimSpace(string(portfolioContext))
	if portfolio == "" {
		portfolio = "{}"
	}
	risk := strings.TrimSpace(string(riskContext))
	if risk == "" {
		risk = "{}"
	}
	tools := strings.TrimSpace(string(toolContext))
	if tools == "" {
		tools = "{}"
	}
	policyLim := strings.TrimSpace(string(policyLimits))
	if policyLim == "" {
		policyLim = "{}"
	}
	input := strings.TrimSpace(string(userInput))
	if input == "" {
		input = "{}"
	}
	return fmt.Sprintf(
		"BRIEFING_REQUEST:\n%s\n\nPORTFOLIO_CONTEXT:\n%s\n\nWATCHLIST_MARKET_CONTEXT:\n%s\n\nRISK_CONTEXT:\n%s\n\nPOLICY_LIMITS:\n%s\n\nINSTRUCTIONS:\n"+
			"- Propose only; do not claim execution.\n"+
			"- WATCHLIST_MARKET_CONTEXT lists the full tracked universe with prices; scan ALL symbols, not only held positions.\n"+
			"- Include trade ideas for new symbols (held=false) when supported by data; include holds, trims, and exits for held symbols.\n"+
			"- Use unknown when data is missing.\n"+
			"- Include used_sources and used_fields for factual/numeric statements.\n"+
			"- trade_ideas: always an array; each actionable idea needs rationale, size, stop, target, and confidence in [0,1].\n"+
			"- Narrative-only ideas: omit quantity, notional_usd, order_type, time_in_force, limit_price.\n"+
			"- Executable ideas: symbol, side (BUY or SELL), quantity XOR notional_usd, plus order_type/time_in_force as needed; limit_price when required for the order type.\n"+
			"- If using notional_usd, use at least 1.00 USD (broker minimum).\n"+
			"- When POLICY_LIMITS sets caps, keep every executable idea's notional_usd within those caps.",
		input,
		portfolio,
		tools,
		risk,
		policyLim,
	)
}

func BriefingSystemPrompt() string {
	return briefingSystemPrompt
}

func BuildValidationRepairPrompt(invalidOutput string, issues []ValidationIssue) string {
	invalid := strings.TrimSpace(invalidOutput)
	if invalid == "" {
		invalid = "{}"
	}
	issuesJSON, err := json.Marshal(issues)
	if err != nil {
		issuesJSON = []byte("[]")
	}
	return fmt.Sprintf(
		"You are fixing a prior invalid briefing JSON payload.\n"+
			"Return ONLY valid JSON for the briefing schema with these exact top-level keys:\n"+
			"market_summary, portfolio_context, trade_ideas, risks_and_caveats, data_gaps, disclaimer, used_sources, used_fields.\n"+
			"Do not include markdown or code fences.\n"+
			"Keep existing content where possible, but fix every validation issue listed.\n"+
			"Actionable trade ideas must include symbol, side (BUY or SELL), and quantity or notional_usd when structured order fields are present.\n"+
			"Narrative-only ideas must omit structured order fields entirely.\n\n"+
			"VALIDATION_ISSUES:\n%s\n\n"+
			"INVALID_OUTPUT:\n%s",
		string(issuesJSON),
		invalid,
	)
}
