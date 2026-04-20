package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/KevinMReardon/realtime-portfolio-risk/internal/portfolio"
)

// loadPortfolioAssemblerInputOnly validates :id, optional ownership, and loads projection input without assembling the view.
func loadPortfolioAssemblerInputOnly(
	c *gin.Context,
	readStore PortfolioReadStore,
	log *zap.Logger,
	priceStreamPartitions []uuid.UUID,
	opName string,
) (uuid.UUID, portfolio.PortfolioAssemblerInput, bool) {
	pid, ok := validatePortfolioPathID(c, priceStreamPartitions)
	if !ok {
		return uuid.Nil, portfolio.PortfolioAssemblerInput{}, false
	}
	if user, hasUser := authUserFromContext(c); hasUser {
		if checker, ok := readStore.(portfolioOwnerChecker); ok {
			owned, err := checker.PortfolioOwnedByUser(c.Request.Context(), pid, user.UserID)
			if err != nil {
				log.Warn(opName+"_ownership_failed", zap.String("portfolio_id", pid.String()), zap.Error(err))
				respondAPIError(c, http.StatusInternalServerError, ErrCodeInternal, "internal error", nil)
				return uuid.Nil, portfolio.PortfolioAssemblerInput{}, false
			}
			if !owned {
				respondAPIError(c, http.StatusForbidden, ErrCodeForbidden, "forbidden", nil)
				return uuid.Nil, portfolio.PortfolioAssemblerInput{}, false
			}
		}
	}

	input, found, err := readStore.LoadPortfolioAssemblerInput(c.Request.Context(), pid)
	if err != nil {
		log.Warn(opName+"_query_failed", zap.String("portfolio_id", pid.String()), zap.Error(err))
		respondAPIError(c, http.StatusInternalServerError, ErrCodeInternal, "internal error", nil)
		return uuid.Nil, portfolio.PortfolioAssemblerInput{}, false
	}
	if !found {
		respondAPIError(c, http.StatusNotFound, ErrCodeNotFound, "portfolio not found", nil)
		return uuid.Nil, portfolio.PortfolioAssemblerInput{}, false
	}

	return pid, input, true
}
