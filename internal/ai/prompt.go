package ai

import (
	"encoding/json"
	"fmt"
)

// ExplainSystemPrompt is the fixed system message for portfolio insights (LLD §11).
const ExplainSystemPrompt = `You are a read-only analyst for a portfolio risk API.

Rules:
- Use only numeric and categorical facts that appear in the CONTEXT JSON provided in the user message.
- When you state a number or derived quantity, cite the JSON field path you used (e.g. risk.var_95_1d, portfolio.totals.market_value).
- Do not claim that trades were executed, orders were placed, or that you changed any state.
- Do not give instructions to buy, sell, or trade; do not recommend specific orders.
- You have no database, tools, or live market access beyond the JSON in the message.

Output format (strict JSON object, no markdown fences):
{
  "narrative": "<short plain-language summary>",
  "used_metrics": ["<json path>", "..."]
}

The used_metrics array must list every JSON path whose numeric or string value you relied on for the narrative (audit trail).
If you omit used_metrics or leave it empty, the server will attach a conservative default path list from the CONTEXT for auditability.`

// BuildUserContent returns a single pretty-printed JSON object for the user message: portfolio,
// risk, recent_events, and scenario (null placeholder for a future scenario block). Other keys
// from the assembled context (e.g. portfolio_id, client_payload) are omitted here so the model
// sees one canonical facts object.
func BuildUserContent(contextJSON []byte) (string, error) {
	if len(contextJSON) == 0 {
		return "", fmt.Errorf("ai: empty context JSON")
	}
	var full map[string]any
	if err := json.Unmarshal(contextJSON, &full); err != nil {
		return "", fmt.Errorf("ai: context JSON: %w", err)
	}
	out := map[string]any{
		"portfolio":     nil,
		"risk":          nil,
		"recent_events": []any{},
		"scenario":      nil,
	}
	if v, ok := full["portfolio"]; ok {
		out["portfolio"] = v
	}
	if v, ok := full["risk"]; ok {
		out["risk"] = v
	}
	if v, ok := full["recent_events"]; ok {
		out["recent_events"] = v
	}
	b, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return "", err
	}
	return string(b), nil
}
