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

func filterCatalogRowsMinusReserved(rows []events.PortfolioCatalogEntry, priceStreamPartitions []uuid.UUID) []events.PortfolioCatalogEntry {
	reserved := make(map[uuid.UUID]struct{}, len(priceStreamPartitions))
	for _, p := range priceStreamPartitions {
		reserved[p] = struct{}{}
	}
	out := make([]events.PortfolioCatalogEntry, 0, len(rows))
	for _, row := range rows {
		if _, skip := reserved[row.PortfolioID]; skip {
			continue
		}
		out = append(out, row)
	}
	return out
}

func catalogRowsVisibleToCaller(c *gin.Context, store PortfolioCatalogStore, priceStreamPartitions []uuid.UUID) ([]events.PortfolioCatalogEntry, error) {
	var rows []events.PortfolioCatalogEntry
	var err error
	if user, ok := authUserFromContext(c); ok {
		rows, err = store.ListPortfoliosByOwner(c.Request.Context(), user.UserID)
	} else {
		rows, err = store.ListPortfolios(c.Request.Context())
	}
	if err != nil {
		return nil, err
	}
	return filterCatalogRowsMinusReserved(rows, priceStreamPartitions), nil
}

// PortfolioCatalogStore supports portfolio directory create/list APIs.
type PortfolioCatalogStore interface {
	ListPortfolios(ctx context.Context) ([]events.PortfolioCatalogEntry, error)
	CreatePortfolio(ctx context.Context, portfolioID uuid.UUID, name, baseCurrency string) (events.PortfolioCatalogEntry, error)
	ListPortfoliosByOwner(ctx context.Context, ownerUserID uuid.UUID) ([]events.PortfolioCatalogEntry, error)
	CreatePortfolioForOwner(ctx context.Context, ownerUserID, portfolioID uuid.UUID, name, baseCurrency string) (events.PortfolioCatalogEntry, error)
	PortfolioOwnedByUser(ctx context.Context, portfolioID, ownerUserID uuid.UUID) (bool, error)
	UpsertPortfolioAlpacaLink(ctx context.Context, portfolioID uuid.UUID, link events.AlpacaPortfolioLinkInput) error
}

type createPortfolioRequest struct {
	Name         string `json:"name" binding:"required"`
	BaseCurrency string `json:"base_currency"`
	Alpaca       *struct {
		AccountMode string `json:"account_mode"`
		SyncEnabled *bool  `json:"sync_enabled"`
		KeyID       string `json:"key_id"`
		SecretKey   string `json:"secret_key"`
		BaseURL     string `json:"base_url"`
	} `json:"alpaca,omitempty"`
}

type portfolioCatalogResponse struct {
	PortfolioID       string    `json:"portfolio_id"`
	Name              string    `json:"name"`
	BaseCurrency      string    `json:"base_currency"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
	AlpacaAccountID   *string   `json:"alpaca_account_id,omitempty"`
	AlpacaAccountMode string    `json:"alpaca_account_mode"`
	AlpacaSyncEnabled bool      `json:"alpaca_sync_enabled"`
	AlpacaLinked      bool      `json:"alpaca_linked"`
}

func toPortfolioCatalogResponse(in events.PortfolioCatalogEntry) portfolioCatalogResponse {
	return portfolioCatalogResponse{
		PortfolioID:       in.PortfolioID.String(),
		Name:              in.Name,
		BaseCurrency:      in.BaseCurrency,
		CreatedAt:         in.CreatedAt,
		UpdatedAt:         in.UpdatedAt,
		AlpacaAccountID:   in.AlpacaAccountID,
		AlpacaAccountMode: in.AlpacaAccountMode,
		AlpacaSyncEnabled: in.AlpacaSyncEnabled,
		AlpacaLinked:      in.AlpacaLinked,
	}
}

func listPortfoliosHandler(store PortfolioCatalogStore, log *zap.Logger, priceStreamPartitions []uuid.UUID) gin.HandlerFunc {
	return func(c *gin.Context) {
		rows, err := catalogRowsVisibleToCaller(c, store, priceStreamPartitions)
		if err != nil {
			log.Warn("list_portfolios_failed", zap.Error(err))
			respondAPIError(c, http.StatusInternalServerError, ErrCodeInternal, "internal error", nil)
			return
		}
		out := make([]portfolioCatalogResponse, 0, len(rows))
		for _, row := range rows {
			out = append(out, toPortfolioCatalogResponse(row))
		}
		c.JSON(http.StatusOK, gin.H{"portfolios": out})
	}
}

func createPortfolioHandler(store PortfolioCatalogStore, log *zap.Logger, priceStreamPartitions []uuid.UUID, singleUser bool) gin.HandlerFunc {
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

		if singleUser {
			visible, err := catalogRowsVisibleToCaller(c, store, priceStreamPartitions)
			if err != nil {
				log.Warn("create_portfolio_precheck_failed", zap.Error(err))
				respondAPIError(c, http.StatusInternalServerError, ErrCodeInternal, "internal error", nil)
				return
			}
			if len(visible) >= 1 {
				respondAPIError(c, http.StatusConflict, ErrCodeConflict,
					"only one portfolio is allowed in single-user mode; set SINGLE_USER_APP=false for multiple portfolios", nil)
				return
			}
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

		var (
			row events.PortfolioCatalogEntry
			err error
		)
		if user, ok := authUserFromContext(c); ok {
			row, err = store.CreatePortfolioForOwner(c.Request.Context(), user.UserID, portfolioID, name, baseCurrency)
		} else {
			row, err = store.CreatePortfolio(c.Request.Context(), portfolioID, name, baseCurrency)
		}
		if err != nil {
			log.Warn("create_portfolio_failed", zap.String("portfolio_id", portfolioID.String()), zap.Error(err))
			respondAPIError(c, http.StatusInternalServerError, ErrCodeInternal, "internal error", nil)
			return
		}
		if req.Alpaca != nil {
			syncEnabled := true
			if req.Alpaca.SyncEnabled != nil {
				syncEnabled = *req.Alpaca.SyncEnabled
			}
			link := events.AlpacaPortfolioLinkInput{
				AlpacaAccountMode: req.Alpaca.AccountMode,
				AlpacaSyncEnabled: syncEnabled,
				AlpacaKeyID:       req.Alpaca.KeyID,
				AlpacaSecretKey:   req.Alpaca.SecretKey,
				AlpacaBaseURL:     req.Alpaca.BaseURL,
			}
			if err := store.UpsertPortfolioAlpacaLink(c.Request.Context(), portfolioID, link); err != nil {
				log.Warn("create_portfolio_alpaca_link_failed",
					zap.String("portfolio_id", portfolioID.String()),
					zap.Error(err),
				)
				respondAPIError(c, http.StatusBadRequest, ErrCodeValidation, err.Error(), nil)
				return
			}
			row.AlpacaAccountMode = strings.ToLower(strings.TrimSpace(req.Alpaca.AccountMode))
			if row.AlpacaAccountMode == "" {
				row.AlpacaAccountMode = "paper"
			}
			row.AlpacaSyncEnabled = syncEnabled
			row.AlpacaLinked = true
		}

		c.JSON(http.StatusCreated, toPortfolioCatalogResponse(row))
	}
}
