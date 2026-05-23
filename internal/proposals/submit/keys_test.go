package submit

import (
	"testing"

	"github.com/KevinMReardon/realtime-portfolio-risk/internal/events"
)

func TestIsPaperLinked(t *testing.T) {
	t.Parallel()
	if !IsPaperLinked(events.PortfolioAlpacaKeyMaterial{AccountMode: "paper"}) {
		t.Fatal("paper mode")
	}
	if IsPaperLinked(events.PortfolioAlpacaKeyMaterial{AccountMode: "live"}) {
		t.Fatal("live mode")
	}
	if !IsPaperLinked(events.PortfolioAlpacaKeyMaterial{BaseURL: "https://paper-api.alpaca.markets"}) {
		t.Fatal("paper url")
	}
	if IsPaperLinked(events.PortfolioAlpacaKeyMaterial{BaseURL: "https://api.alpaca.markets", AccountMode: "live"}) {
		t.Fatal("live url")
	}
}
