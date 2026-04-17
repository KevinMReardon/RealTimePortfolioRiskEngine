package pricesource

import (
	"fmt"
	"strings"

	"github.com/KevinMReardon/realtime-portfolio-risk/internal/domain"
)

// NormalizeToInternalSymbol maps provider symbol variants to the internal
// PricePayload symbol format.
func NormalizeToInternalSymbol(raw string) (string, error) {
	s := strings.ToUpper(strings.TrimSpace(raw))
	if s == "" {
		return "", fmt.Errorf("symbol is required")
	}
	replacer := strings.NewReplacer("/", "-", ":", "-", " ", "", "\t", "")
	s = replacer.Replace(s)
	if err := domain.ValidateSymbol(s); err != nil {
		return "", fmt.Errorf("normalize symbol %q: %w", raw, err)
	}
	return s, nil
}
