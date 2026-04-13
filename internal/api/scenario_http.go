package api

import (
	"errors"
	"net/http"

	"github.com/KevinMReardon/realtime-portfolio-risk/internal/portfolio"
	"github.com/KevinMReardon/realtime-portfolio-risk/internal/scenario"
)

// MapScenarioRunError maps domain, scenario-core, and portfolio-assembler errors to HTTP status
// and LLD §12 error_code / message for the scenario route envelope.
func MapScenarioRunError(err error) (status int, code string, message string) {
	if s, c, m, ok := mapDomainErrors(err); ok {
		return s, c, m
	}
	switch {
	case errors.Is(err, scenario.ErrEmptyShocks):
		return http.StatusBadRequest, ErrCodeValidation, "shocks must include at least one item"
	case errors.Is(err, scenario.ErrEmptyShockSymbol):
		return http.StatusBadRequest, ErrCodeValidation, "shock symbol is required"
	case errors.Is(err, scenario.ErrInvalidShockType):
		return http.StatusBadRequest, ErrCodeValidation, "shock type must be PCT"
	case errors.Is(err, scenario.ErrDuplicateShockSym):
		return http.StatusBadRequest, ErrCodeValidation, "duplicate shock symbol"
	case errors.Is(err, scenario.ErrNilPortfolioID):
		return http.StatusInternalServerError, ErrCodeInternal, "internal error"
	case errors.Is(err, portfolio.ErrAssemblerPortfolioIDRequired),
		errors.Is(err, portfolio.ErrAssemblerDuplicatePositionSymbol):
		return http.StatusInternalServerError, ErrCodeInternal, "internal error"
	default:
		return http.StatusInternalServerError, ErrCodeInternal, "internal error"
	}
}
