package submit

import (
	"strings"

	"github.com/KevinMReardon/realtime-portfolio-risk/internal/events"
)

// IsPaperLinked reports whether stored Alpaca credentials target a paper account (Phase 3 auto-submit gate).
func IsPaperLinked(keys events.PortfolioAlpacaKeyMaterial) bool {
	if strings.EqualFold(strings.TrimSpace(keys.AccountMode), "live") {
		return false
	}
	baseURL := strings.ToLower(strings.TrimSpace(keys.BaseURL))
	if baseURL == "" {
		return true
	}
	if strings.Contains(baseURL, "paper") {
		return true
	}
	if strings.Contains(baseURL, "api.alpaca.markets") {
		return false
	}
	return true
}
