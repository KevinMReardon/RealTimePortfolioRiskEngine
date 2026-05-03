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
	"For every numeric claim, ground it in provided sources and include references in used_sources and used_fields."

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
		"BRIEFING_REQUEST:\n%s\n\nPORTFOLIO_CONTEXT:\n%s\n\nRISK_CONTEXT:\n%s\n\nTOOL_CONTEXT:\n%s\n\nINSTRUCTIONS:\n- Propose only; do not claim execution.\n- Use unknown when data is missing.\n- Include used_sources and used_fields for factual/numeric statements.\n- For actionable trades only, include structured fields together: symbol, side (BUY or SELL), quantity or notional_usd, order_type, time_in_force, and limit_price for limit-style orders. Do not send order_type or time_in_force alone without side and quantity/notional; omit those keys for narrative-only ideas.",
		input,
		portfolio,
		risk,
		tools,
	)
}

func BriefingSystemPrompt() string {
	return briefingSystemPrompt
}
