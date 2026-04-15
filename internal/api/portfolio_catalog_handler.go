package api

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/KevinMReardon/realtime-portfolio-risk/internal/events"
)

// PortfolioCatalogStore supports portfolio directory create/list APIs.
type PortfolioCatalogStore interface {
	ListPortfolios(ctx context.Context) ([]events.PortfolioCatalogEntry, error)
	CreatePortfolio(ctx context.Context, portfolioID uuid.UUID, name, baseCurrency string) (events.PortfolioCatalogEntry, error)
}

type createPortfolioRequest struct {
	Name         string `json:"name" binding:"required"`
	BaseCurrency string `json:"base_currency"`
}

type portfolioCatalogResponse struct {
	PortfolioID  string    `json:"portfolio_id"`
	Name         string    `json:"name"`
	BaseCurrency string    `json:"base_currency"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

func toPortfolioCatalogResponse(in events.PortfolioCatalogEntry) portfolioCatalogResponse {
	return portfolioCatalogResponse{
		PortfolioID:  in.PortfolioID.String(),
		Name:         in.Name,
		BaseCurrency: in.BaseCurrency,
		CreatedAt:    in.CreatedAt,
		UpdatedAt:    in.UpdatedAt,
	}
}

func listPortfoliosHandler(store PortfolioCatalogStore, log *zap.Logger, priceStreamPartitions []uuid.UUID) gin.HandlerFunc {
	return func(c *gin.Context) {
		rows, err := store.ListPortfolios(c.Request.Context())
		if err != nil {
			log.Warn("list_portfolios_failed", zap.Error(err))
			respondAPIError(c, http.StatusInternalServerError, ErrCodeInternal, "internal error", nil)
			return
		}
		out := make([]portfolioCatalogResponse, 0, len(rows))
		reserved := make(map[uuid.UUID]struct{}, len(priceStreamPartitions))
		for _, p := range priceStreamPartitions {
			reserved[p] = struct{}{}
		}
		for _, row := range rows {
			if _, isReserved := reserved[row.PortfolioID]; isReserved {
				continue
			}
			out = append(out, toPortfolioCatalogResponse(row))
		}
		c.JSON(http.StatusOK, gin.H{"portfolios": out})
	}
}

func createPortfolioHandler(store PortfolioCatalogStore, log *zap.Logger, priceStreamPartitions []uuid.UUID) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req createPortfolioRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			respondAPIError(c, http.StatusBadRequest, ErrCodeValidation, "invalid request body including JSON shape", nil)
			return
		}

		name := strings.TrimSpace(req.Name)
		if name == "" {
			respondAPIError(c, http.StatusBadRequest, ErrCodeValidation, "name is required", nil)
			return
		}
		baseCurrency := strings.ToUpper(strings.TrimSpace(req.BaseCurrency))
		if baseCurrency == "" {
			baseCurrency = "USD"
		}

		portfolioID := uuid.New()
		for {
			collision := false
			for _, reserved := range priceStreamPartitions {
				if portfolioID == reserved {
					collision = true
					portfolioID = uuid.New()
					break
				}
			}
			if !collision {
				break
			}
		}

		row, err := store.CreatePortfolio(c.Request.Context(), portfolioID, name, baseCurrency)
		if err != nil {
			log.Warn("create_portfolio_failed", zap.String("portfolio_id", portfolioID.String()), zap.Error(err))
			respondAPIError(c, http.StatusInternalServerError, ErrCodeInternal, "internal error", nil)
			return
		}

		c.JSON(http.StatusCreated, toPortfolioCatalogResponse(row))
	}
}
