package agent

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/shopspring/decimal"

	"github.com/KevinMReardon/realtime-portfolio-risk/internal/connectors/alpaca"
	"github.com/KevinMReardon/realtime-portfolio-risk/internal/domain"
	"github.com/KevinMReardon/realtime-portfolio-risk/internal/policy"
)

const (
	ValidationCodeMalformedJSON      = "MALFORMED_JSON"
	ValidationCodeMissingField       = "MISSING_REQUIRED_FIELD"
	ValidationCodeInvalidConfidence  = "INVALID_CONFIDENCE"
	ValidationCodeBannedExecLanguage = "BANNED_EXECUTION_LANGUAGE"
	ValidationCodeInvalidTradeIdea   = "INVALID_TRADE_IDEA"
)

type ValidationIssue struct {
	Code   string `json:"code"`
	Field  string `json:"field,omitempty"`
	Detail string `json:"detail"`
}

type ValidationError struct {
	Issues []ValidationIssue `json:"issues"`
}

func (e *ValidationError) Error() string {
	if e == nil || len(e.Issues) == 0 {
		return "agent output validation failed"
	}
	parts := make([]string, 0, len(e.Issues))
	for _, issue := range e.Issues {
		if strings.TrimSpace(issue.Field) == "" {
			parts = append(parts, fmt.Sprintf("%s: %s", issue.Code, issue.Detail))
			continue
		}
		parts = append(parts, fmt.Sprintf("%s(%s): %s", issue.Code, issue.Field, issue.Detail))
	}
	return "agent output validation failed: " + strings.Join(parts, "; ")
}

var bannedExecutionPhrases = []string{
	"i executed",
	"executed your trade",
	"placed your order",
	"i placed",
	"order has been submitted",
	"we bought",
	"we sold",
	"i bought",
	"i sold",
	"position opened",
	"position closed",
	"trade completed",
	"fill confirmed",
}

func ValidateBriefingOutput(raw json.RawMessage) (BriefingOutput, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return BriefingOutput{}, &ValidationError{
			Issues: []ValidationIssue{{Code: ValidationCodeMalformedJSON, Detail: "empty payload"}},
		}
	}
	raw = normalizeJSONPayload(raw)
	raw = canonicalizeBriefingContract(raw)
	var out BriefingOutput
	dec := json.NewDecoder(bytes.NewReader(raw))
	// Canonical JSON already strips unknown keys; allow unknowns here so minor decoder/schema drift
	// does not reject otherwise-valid briefings.
	if err := dec.Decode(&out); err != nil {
		return BriefingOutput{}, &ValidationError{
			Issues: []ValidationIssue{{Code: ValidationCodeMalformedJSON, Detail: err.Error()}},
		}
	}
	out = sanitizeExecutionLanguage(out)
	issues := make([]ValidationIssue, 0)
	required := map[string]string{
		"market_summary":    out.MarketSummary,
		"portfolio_context": out.PortfolioContext,
		"risks_and_caveats": out.RisksAndCaveats,
		"disclaimer":        out.Disclaimer,
	}
	for field, value := range required {
		if strings.TrimSpace(value) == "" {
			issues = append(issues, ValidationIssue{
				Code:   ValidationCodeMissingField,
				Field:  field,
				Detail: "must be non-empty",
			})
		}
	}
	if out.TradeIdeas == nil {
		issues = append(issues, ValidationIssue{Code: ValidationCodeMissingField, Field: "trade_ideas", Detail: "field is required"})
	}
	if out.DataGaps == nil {
		issues = append(issues, ValidationIssue{Code: ValidationCodeMissingField, Field: "data_gaps", Detail: "field is required"})
	}
	if out.UsedSources == nil {
		issues = append(issues, ValidationIssue{Code: ValidationCodeMissingField, Field: "used_sources", Detail: "field is required"})
	}
	if out.UsedFields == nil {
		issues = append(issues, ValidationIssue{Code: ValidationCodeMissingField, Field: "used_fields", Detail: "field is required"})
	}
	for i, idea := range out.TradeIdeas {
		prefix := fmt.Sprintf("trade_ideas[%d]", i)
		if strings.TrimSpace(idea.Rationale) == "" {
			issues = append(issues, ValidationIssue{Code: ValidationCodeInvalidTradeIdea, Field: prefix + ".rationale", Detail: "must be non-empty"})
		}
		if strings.TrimSpace(idea.Size) == "" {
			issues = append(issues, ValidationIssue{Code: ValidationCodeInvalidTradeIdea, Field: prefix + ".size", Detail: "must be non-empty"})
		}
		if strings.TrimSpace(idea.Stop) == "" {
			issues = append(issues, ValidationIssue{Code: ValidationCodeInvalidTradeIdea, Field: prefix + ".stop", Detail: "must be non-empty"})
		}
		if strings.TrimSpace(idea.Target) == "" {
			issues = append(issues, ValidationIssue{Code: ValidationCodeInvalidTradeIdea, Field: prefix + ".target", Detail: "must be non-empty"})
		}
		if idea.Confidence < 0 || idea.Confidence > 1 {
			issues = append(issues, ValidationIssue{Code: ValidationCodeInvalidConfidence, Field: prefix + ".confidence", Detail: "must be between 0 and 1 inclusive"})
		}
		if tradeIdeaHasStructuredOrderIntent(idea) {
			sym := policy.NormalizeSymbol(strings.TrimSpace(idea.Symbol))
			if sym == "" {
				issues = append(issues, ValidationIssue{Code: ValidationCodeInvalidTradeIdea, Field: prefix + ".symbol", Detail: "required when structured order fields are set"})
			} else if !domain.IsValidSymbol(sym) {
				issues = append(issues, ValidationIssue{Code: ValidationCodeInvalidTradeIdea, Field: prefix + ".symbol", Detail: "must be a valid US equity symbol"})
			}
			side := domain.Side(strings.ToUpper(strings.TrimSpace(idea.Side)))
			if strings.TrimSpace(idea.Side) == "" {
				issues = append(issues, ValidationIssue{Code: ValidationCodeInvalidTradeIdea, Field: prefix + ".side", Detail: "required when structured order fields are set (BUY or SELL)"})
			} else if !domain.IsValidSide(side) {
				issues = append(issues, ValidationIssue{Code: ValidationCodeInvalidTradeIdea, Field: prefix + ".side", Detail: "must be BUY or SELL"})
			}
			hasQty := strings.TrimSpace(idea.Quantity) != ""
			hasNotional := strings.TrimSpace(idea.NotionalUSD) != ""
			if !hasQty && !hasNotional {
				issues = append(issues, ValidationIssue{Code: ValidationCodeInvalidTradeIdea, Field: prefix + ".quantity", Detail: "quantity or notional_usd is required when structured order fields are set"})
			}
			if hasQty {
				if _, err := decimal.NewFromString(strings.TrimSpace(idea.Quantity)); err != nil {
					issues = append(issues, ValidationIssue{Code: ValidationCodeInvalidTradeIdea, Field: prefix + ".quantity", Detail: "must be a decimal number"})
				}
			}
			if hasNotional {
				n, err := decimal.NewFromString(strings.TrimSpace(idea.NotionalUSD))
				if err != nil {
					issues = append(issues, ValidationIssue{Code: ValidationCodeInvalidTradeIdea, Field: prefix + ".notional_usd", Detail: "must be a decimal number"})
				} else if n.LessThan(alpaca.MinNotionalStockOrderUSD) {
					issues = append(issues, ValidationIssue{Code: ValidationCodeInvalidTradeIdea, Field: prefix + ".notional_usd", Detail: "must be at least 1.00 USD (broker minimum for notional orders)"})
				}
			}
			if orderTypeImpliesLimit(idea.OrderType) {
				if strings.TrimSpace(idea.LimitPrice) == "" {
					issues = append(issues, ValidationIssue{Code: ValidationCodeInvalidTradeIdea, Field: prefix + ".limit_price", Detail: "required when order_type is a limit order"})
				} else if _, err := decimal.NewFromString(strings.TrimSpace(idea.LimitPrice)); err != nil {
					issues = append(issues, ValidationIssue{Code: ValidationCodeInvalidTradeIdea, Field: prefix + ".limit_price", Detail: "must be a decimal number"})
				}
			}
		}
	}
	if len(issues) > 0 {
		return BriefingOutput{}, &ValidationError{Issues: issues}
	}
	return out, nil
}

func sanitizeExecutionLanguage(out BriefingOutput) BriefingOutput {
	out.MarketSummary = rewriteExecutionPhrases(out.MarketSummary)
	out.PortfolioContext = rewriteExecutionPhrases(out.PortfolioContext)
	out.RisksAndCaveats = rewriteExecutionPhrases(out.RisksAndCaveats)
	out.Disclaimer = rewriteExecutionPhrases(out.Disclaimer)
	for i := range out.TradeIdeas {
		out.TradeIdeas[i].Rationale = rewriteExecutionPhrases(out.TradeIdeas[i].Rationale)
	}
	return out
}

func rewriteExecutionPhrases(s string) string {
	v := s
	replacements := map[string]string{
		"i executed":             "this is a proposal",
		"executed your trade":    "proposed a trade",
		"placed your order":      "proposed an order",
		"i placed":               "this is a proposal for",
		"order has been submitted": "order has not been submitted",
		"we bought":              "this proposal suggests buying",
		"we sold":                "this proposal suggests selling",
		"i bought":               "this proposal suggests buying",
		"i sold":                 "this proposal suggests selling",
		"position opened":        "position could be opened",
		"position closed":        "position could be closed",
		"trade completed":        "trade is proposed",
		"fill confirmed":         "fill not confirmed",
	}
	for _, phrase := range bannedExecutionPhrases {
		replacement := replacements[phrase]
		if strings.TrimSpace(replacement) == "" {
			replacement = "proposal provided"
		}
		re := regexp.MustCompile(`(?i)` + regexp.QuoteMeta(phrase))
		v = re.ReplaceAllString(v, replacement)
	}
	return v
}

func normalizeJSONPayload(raw json.RawMessage) json.RawMessage {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" {
		return raw
	}
	if strings.HasPrefix(trimmed, "```") {
		lines := strings.Split(trimmed, "\n")
		if len(lines) >= 2 {
			first := strings.TrimSpace(lines[0])
			last := strings.TrimSpace(lines[len(lines)-1])
			if strings.HasPrefix(first, "```") && last == "```" {
				body := strings.Join(lines[1:len(lines)-1], "\n")
				return json.RawMessage(strings.TrimSpace(body))
			}
		}
	}
	if candidate, ok := extractJSONObject(trimmed); ok {
		return json.RawMessage(candidate)
	}
	if fallback, ok := synthesizePayloadFromPlainText(trimmed); ok {
		return fallback
	}
	return raw
}

func synthesizePayloadFromPlainText(text string) (json.RawMessage, bool) {
	clean := strings.TrimSpace(text)
	if clean == "" {
		return nil, false
	}
	// If JSON-like markers exist but extraction failed, prefer strict failure over guesswork.
	if strings.Contains(clean, "{") || strings.Contains(clean, "}") || strings.Contains(clean, "[") || strings.Contains(clean, "]") {
		return nil, false
	}
	const genericDisclaimer = "Educational only; this is not investment advice."
	const genericRisk = "Model returned non-JSON output; verify details before acting."
	payload := map[string]any{
		"market_summary":    clean,
		"portfolio_context": clean,
		"trade_ideas":       []map[string]any{},
		"risks_and_caveats": genericRisk,
		"data_gaps": []string{
			"Structured JSON response unavailable",
		},
		"disclaimer":   genericDisclaimer,
		"used_sources": []string{},
		"used_fields":  []string{},
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return nil, false
	}
	return b, true
}

func extractJSONObject(s string) (string, bool) {
	start := strings.Index(s, "{")
	if start < 0 {
		return "", false
	}
	depth := 0
	inString := false
	escaped := false
	for i := start; i < len(s); i++ {
		ch := s[i]
		if inString {
			if escaped {
				escaped = false
				continue
			}
			if ch == '\\' {
				escaped = true
				continue
			}
			if ch == '"' {
				inString = false
			}
			continue
		}
		if ch == '"' {
			inString = true
			continue
		}
		if ch == '{' {
			depth++
			continue
		}
		if ch == '}' {
			depth--
			if depth == 0 {
				return strings.TrimSpace(s[start : i+1]), true
			}
		}
	}
	return "", false
}

func canonicalizeBriefingContract(raw json.RawMessage) json.RawMessage {
	var src map[string]any
	if err := json.Unmarshal(raw, &src); err != nil {
		return raw
	}
	normalized := map[string]any{
		"market_summary":    anyToString(src["market_summary"]),
		"portfolio_context": anyToString(src["portfolio_context"]),
		"trade_ideas":       normalizeTradeIdeas(src["trade_ideas"]),
		"risks_and_caveats": normalizeRisksAndCaveats(src["risks_and_caveats"]),
		"data_gaps":         anyToStringSlice(src["data_gaps"]),
		"disclaimer":        anyToString(src["disclaimer"]),
		"used_sources":      anyToStringSlice(src["used_sources"]),
		"used_fields":       anyToStringSlice(src["used_fields"]),
	}
	out, err := json.Marshal(normalized)
	if err != nil {
		return raw
	}
	return out
}

func normalizeTradeIdeas(v any) []map[string]any {
	items, ok := v.([]any)
	if !ok {
		return []map[string]any{}
	}
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		ti := map[string]any{
			"symbol":     policy.NormalizeSymbol(anyToString(m["symbol"])),
			"rationale":  coalesceString(anyToString(m["rationale"]), anyToString(m["action"])),
			"confidence": normalizeConfidence(m["confidence"]),
			"size":       coalesceString(anyToString(m["size"]), anyToString(m["proposed_size"]), "unknown"),
			"stop":       coalesceString(anyToString(m["stop"]), "unknown"),
			"target":     coalesceString(anyToString(m["target"]), "unknown"),
		}
		for _, key := range []string{"side", "quantity", "notional_usd", "order_type", "limit_price", "time_in_force"} {
			if s := strings.TrimSpace(anyToString(m[key])); s != "" {
				ti[key] = s
			}
		}
		out = append(out, ti)
	}
	return out
}

// tradeIdeaHasStructuredOrderIntent is true when the model signalled executable intent beyond
// narrative-only hints. order_type / time_in_force alone do not trigger (models often emit
// "market"/"day" without symbol/side/qty, which should not fail the whole briefing).
func tradeIdeaHasStructuredOrderIntent(idea BriefingIdea) bool {
	if strings.TrimSpace(idea.Side) != "" ||
		strings.TrimSpace(idea.Quantity) != "" ||
		strings.TrimSpace(idea.NotionalUSD) != "" ||
		strings.TrimSpace(idea.LimitPrice) != "" {
		return true
	}
	return orderTypeImpliesLimit(idea.OrderType)
}

func orderTypeImpliesLimit(orderType string) bool {
	s := strings.ToLower(strings.TrimSpace(orderType))
	if s == "" {
		return false
	}
	// Tokenize on non-alphanumeric so "stop_limit" matches, "unlimited" does not.
	for _, tok := range strings.FieldsFunc(s, func(r rune) bool {
		return r < '0' || r > '9' && (r < 'a' || r > 'z')
	}) {
		if tok == "limit" {
			return true
		}
	}
	return false
}

func normalizeRisksAndCaveats(v any) string {
	switch vv := v.(type) {
	case string:
		return vv
	case []any:
		parts := make([]string, 0, len(vv))
		for _, item := range vv {
			s := strings.TrimSpace(anyToString(item))
			if s != "" {
				parts = append(parts, s)
			}
		}
		return strings.Join(parts, " ")
	default:
		return anyToString(v)
	}
}

func normalizeConfidence(v any) float64 {
	switch vv := v.(type) {
	case float64:
		return vv
	case string:
		s := strings.TrimSpace(strings.ToLower(vv))
		switch s {
		case "low":
			return 0.3
		case "medium":
			return 0.6
		case "high":
			return 0.8
		}
		if parsed, err := strconv.ParseFloat(s, 64); err == nil {
			return parsed
		}
	}
	return 0.5
}

func anyToStringSlice(v any) []string {
	items, ok := v.([]any)
	if !ok {
		return []string{}
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		s := strings.TrimSpace(anyToString(item))
		if s == "" {
			continue
		}
		out = append(out, s)
	}
	return out
}

func anyToString(v any) string {
	switch vv := v.(type) {
	case nil:
		return ""
	case string:
		return vv
	case float64:
		return strconv.FormatFloat(vv, 'f', -1, 64)
	case bool:
		if vv {
			return "true"
		}
		return "false"
	case map[string]any:
		if d := strings.TrimSpace(anyToString(vv["detail"])); d != "" {
			return d
		}
		if t := strings.TrimSpace(anyToString(vv["text"])); t != "" {
			return t
		}
		b, _ := json.Marshal(vv)
		return string(b)
	default:
		b, _ := json.Marshal(v)
		return string(b)
	}
}

func coalesceString(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
