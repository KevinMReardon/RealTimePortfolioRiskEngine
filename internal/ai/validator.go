package ai

import (
	"fmt"
	"regexp"
	"strings"
)

// Validation failure reason codes (stable for tests and API details).
const (
	ValReasonBannedPhrase  = "BANNED_PHRASE"
	ValReasonUnknownSymbol = "UNKNOWN_SYMBOL"
	ValReasonSuspiciousSQL = "SUSPICIOUS_SQL"
	ValReasonBadUsedMetric = "BAD_USED_METRIC"
)

// ValidationError is returned when model output fails policy checks.
type ValidationError struct {
	Code   string
	Detail string
}

func (e *ValidationError) Error() string {
	if e == nil {
		return ""
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Detail)
}

var (
	tickerTokenRE = regexp.MustCompile(`\b[A-Z]{1,5}\b`)
	sqlProbeRE    = regexp.MustCompile(`(?is)(union\s+all\s+select|union\s+select|;\s*drop\s+table|;\s*delete\s+from|into\s+outfile|exec\s*\(|xp_cmdshell)`)
	usedMetricRE  = regexp.MustCompile(`^(portfolio|risk|recent_events)(\.|\[)`)
)

var defaultBannedPhrases = []string{
	"i updated your portfolio",
	"i placed",
	"i executed",
	"executed your trade",
	"placed your order",
	"i bought",
	"i sold",
	"i shorted",
	"you should buy",
	"you should sell",
	"go long",
	"go short",
	"wire transfer",
	"send me your",
	"your api key",
	"your password",
}

var englishTickerStoplist = map[string]struct{}{
	"THE": {}, "AND": {}, "FOR": {}, "ARE": {}, "BUT": {}, "NOT": {}, "YOU": {}, "ALL": {}, "CAN": {},
	"HER": {}, "WAS": {}, "ONE": {}, "OUR": {}, "OUT": {}, "DAY": {}, "GET": {}, "HAS": {},
	"HIM": {}, "HIS": {}, "HOW": {}, "ITS": {}, "LET": {}, "MAY": {}, "NEW": {}, "NOW": {},
	"OLD": {}, "SEE": {}, "TWO": {}, "WHO": {}, "BOY": {}, "DID": {}, "OWN": {}, "SAY": {},
	"SHE": {}, "TOO": {}, "ANY": {}, "HAD": {}, "WAY": {}, "USE": {},
	"IT": {}, "OR": {}, "IN": {}, "ON": {}, "AT": {}, "BE": {}, "DO": {}, "IF": {}, "IS": {},
	"NO": {}, "OF": {}, "TO": {}, "UP": {}, "AS": {}, "AN": {}, "AM": {}, "WE": {}, "ME": {},
	"US": {}, "GO": {}, "AI": {}, "OK": {}, "VS": {}, "TV": {}, "PC": {}, "UK": {}, "EU": {},
	"IM": {}, "PM": {},
}

var financeTickerStoplist = map[string]struct{}{
	"CASH": {}, "USD": {}, "VAR": {}, "MTM": {}, "PNL": {}, "ROI": {}, "ETF": {}, "IPO": {},
	"EPS": {}, "YTD": {}, "QTD": {}, "MTD": {}, "LLM": {}, "API": {},
}

var defaultBannedLowered []string

func init() {
	defaultBannedLowered = make([]string, len(defaultBannedPhrases))
	for i, p := range defaultBannedPhrases {
		defaultBannedLowered[i] = strings.ToLower(p)
	}
}

// ValidateInsightOutput checks full model text (and optional used_metrics) before returning to clients.
// allowSymbols keys must be upper-case trimmed symbols from portfolio + risk exposure.
func ValidateInsightOutput(allowSymbols map[string]struct{}, fullModelText string, usedMetrics []string) error {
	if fullModelText == "" {
		return nil
	}
	lower := strings.ToLower(fullModelText)
	for _, phrase := range defaultBannedLowered {
		if strings.Contains(lower, phrase) {
			return &ValidationError{Code: ValReasonBannedPhrase, Detail: "output contains disallowed phrasing"}
		}
	}
	if sqlProbeRE.MatchString(fullModelText) {
		return &ValidationError{Code: ValReasonSuspiciousSQL, Detail: "output contains disallowed SQL-like patterns"}
	}
	for _, m := range usedMetrics {
		m = strings.TrimSpace(m)
		if m == "" {
			continue
		}
		if !usedMetricRE.MatchString(m) {
			return &ValidationError{Code: ValReasonBadUsedMetric, Detail: fmt.Sprintf("used_metrics path not allowed: %q", m)}
		}
	}
	for _, tok := range tickerTokenRE.FindAllString(fullModelText, -1) {
		if !isKnownTickerToken(tok, allowSymbols) {
			return &ValidationError{Code: ValReasonUnknownSymbol, Detail: fmt.Sprintf("unknown ticker-like token %q", tok)}
		}
	}
	return nil
}

func isKnownTickerToken(tok string, allowSymbols map[string]struct{}) bool {
	if _, skip := englishTickerStoplist[tok]; skip {
		return true
	}
	if _, skip := financeTickerStoplist[tok]; skip {
		return true
	}
	_, ok := allowSymbols[tok]
	return ok
}

// SanitizeUnknownTickerTokens rewrites unknown ticker-like tokens to "asset".
// This keeps the narrative usable when the model hallucinates an out-of-context symbol.
func SanitizeUnknownTickerTokens(fullModelText string, allowSymbols map[string]struct{}) string {
	return tickerTokenRE.ReplaceAllStringFunc(fullModelText, func(tok string) string {
		if isKnownTickerToken(tok, allowSymbols) {
			return tok
		}
		return "asset"
	})
}
