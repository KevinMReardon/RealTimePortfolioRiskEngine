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
	"If using notional_usd for a US equity order, use at least 1.00 USD (broker minimum)."

func BuildBriefingUserPrompt(portfolioContext json.RawMessage, userInput json.RawMessage) string {
	return BuildBriefingUserPromptFromContext(portfolioContext, nil, nil, userInput)
}

func BuildBriefingUserPromptFromContext(portfolioContext, riskContext, toolContext, userInput json.RawMessage) string {
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
	input := strings.TrimSpace(string(userInput))
	if input == "" {
		input = "{}"
	}
	return fmt.Sprintf(
		"BRIEFING_REQUEST:\n%s\n\nPORTFOLIO_CONTEXT:\n%s\n\nRISK_CONTEXT:\n%s\n\nTOOL_CONTEXT:\n%s\n\nINSTRUCTIONS:\n"+
			"- Propose only; do not claim execution.\n"+
			"- Use unknown when data is missing.\n"+
			"- Include used_sources and used_fields for factual/numeric statements.\n"+
			"- trade_ideas: always an array; each actionable idea needs rationale, size, stop, target, and confidence in [0,1].\n"+
			"- Narrative-only ideas: omit quantity, notional_usd, order_type, time_in_force, limit_price.\n"+
			"- Executable ideas: symbol, side (BUY or SELL), quantity XOR notional_usd, plus order_type/time_in_force as needed; limit_price when required for the order type.\n"+
			"- If using notional_usd, use at least 1.00 USD (broker minimum).",
		input,
		portfolio,
		risk,
		tools,
	)
}

func BriefingSystemPrompt() string {
	return briefingSystemPrompt
}
