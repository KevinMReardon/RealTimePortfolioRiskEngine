package api

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/KevinMReardon/realtime-portfolio-risk/internal/portfolio"
)

type portfolioBaseState struct {
	PortfolioID uuid.UUID
	Input       portfolio.PortfolioAssemblerInput
	View        portfolio.PortfolioView
}

type portfolioOwnerChecker interface {
	PortfolioOwnedByUser(ctx context.Context, portfolioID, ownerUserID uuid.UUID) (bool, error)
}

// loadPortfolioBaseState reuses the canonical projection->assembler read path with no writes.
func loadPortfolioBaseState(
	c *gin.Context,
	readStore PortfolioReadStore,
	log *zap.Logger,
	priceStreamPartitions []uuid.UUID,
	opName string,
) (portfolioBaseState, bool) {
	pid, ok := validatePortfolioPathID(c, priceStreamPartitions)
	if !ok {
		return portfolioBaseState{}, false
	}
	if user, hasUser := authUserFromContext(c); hasUser {
		if checker, ok := readStore.(portfolioOwnerChecker); ok {
			owned, err := checker.PortfolioOwnedByUser(c.Request.Context(), pid, user.UserID)
			if err != nil {
				log.Warn(opName+"_ownership_failed", zap.String("portfolio_id", pid.String()), zap.Error(err))
				respondAPIError(c, http.StatusInternalServerError, ErrCodeInternal, "internal error", nil)
				return portfolioBaseState{}, false
			}
			if !owned {
				respondAPIError(c, http.StatusForbidden, ErrCodeForbidden, "forbidden", nil)
				return portfolioBaseState{}, false
			}
		}
	}

	input, found, err := readStore.LoadPortfolioAssemblerInput(c.Request.Context(), pid)
	if err != nil {
		log.Warn(opName+"_query_failed", zap.String("portfolio_id", pid.String()), zap.Error(err))
		respondAPIError(c, http.StatusInternalServerError, ErrCodeInternal, "internal error", nil)
		return portfolioBaseState{}, false
	}
	if !found {
		respondAPIError(c, http.StatusNotFound, ErrCodeNotFound, "portfolio not found", nil)
		return portfolioBaseState{}, false
	}

	view, err := portfolio.AssemblePortfolioView(input)
	if err != nil {
		log.Warn(opName+"_assemble_failed", zap.String("portfolio_id", pid.String()), zap.Error(err))
		respondAPIError(c, http.StatusInternalServerError, ErrCodeInternal, "internal error", nil)
		return portfolioBaseState{}, false
	}

	return portfolioBaseState{
		PortfolioID: pid,
		Input:       input,
		View:        view,
	}, true
}

func validatePortfolioPathID(c *gin.Context, priceStreamPartitions []uuid.UUID) (uuid.UUID, bool) {
	rawID := c.Param("id")
	pid, err := uuid.Parse(rawID)
	if err != nil {
		respondAPIError(c, http.StatusBadRequest, ErrCodeValidation, "portfolio id must be a UUID", nil)
		return uuid.Nil, false
	}
	for _, reserved := range priceStreamPartitions {
		if pid == reserved {
			respondAPIError(c, http.StatusBadRequest, ErrCodeValidation, "portfolio id is reserved for market price stream partitions", nil)
			return uuid.Nil, false
		}
	}
	return pid, true
}
