package api

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/KevinMReardon/realtime-portfolio-risk/internal/config"
	"github.com/KevinMReardon/realtime-portfolio-risk/internal/domain"
	"github.com/KevinMReardon/realtime-portfolio-risk/internal/ingestion"
	"github.com/KevinMReardon/realtime-portfolio-risk/internal/portfolio"
)

// --- POST /v1/trades ---

type postTradeRequest struct {
	PortfolioID    string          `json:"portfolio_id" binding:"required"`
	IdempotencyKey string          `json:"idempotency_key" binding:"required"`
	Source         string          `json:"source" binding:"required"`
	EventTime      *time.Time      `json:"event_time"`
	Trade          json.RawMessage `json:"trade" binding:"required"`
}

type PortfolioOwnershipChecker interface {
	PortfolioOwnedByUser(ctx context.Context, portfolioID, ownerUserID uuid.UUID) (bool, error)
}

func postTradeHandler(svc ingestion.Service, log *zap.Logger, priceStreamPartitions []uuid.UUID, ownership PortfolioOwnershipChecker) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		requestID := RequestIDFromContext(c)
		var req postTradeRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			respondAPIError(c, http.StatusBadRequest, ErrCodeValidation, "invalid request body including JSON shape", nil)
			return
		}

		pid, err := uuid.Parse(req.PortfolioID)
		if err != nil {
			respondAPIError(c, http.StatusBadRequest, ErrCodeValidation, "portfolio_id must be a UUID", nil)
			return
		}
		for _, reserved := range priceStreamPartitions {
			if pid == reserved {
				respondAPIError(c, http.StatusBadRequest, ErrCodeValidation, "portfolio_id is reserved for the market price stream", nil)
				return
			}
		}
		if user, ok := authUserFromContext(c); ok && ownership != nil {
			owned, err := ownership.PortfolioOwnedByUser(c.Request.Context(), pid, user.UserID)
			if err != nil {
				respondAPIError(c, http.StatusInternalServerError, ErrCodeInternal, "internal error", nil)
				return
			}
			if !owned {
				respondAPIError(c, http.StatusForbidden, ErrCodeForbidden, "forbidden", nil)
				return
			}
		}

		var trade domain.TradePayload
		if err := json.Unmarshal(req.Trade, &trade); err != nil {
			respondAPIError(c, http.StatusBadRequest, ErrCodeValidation, "invalid trade object", nil)
			return
		}

		payloadJSON, err := json.Marshal(trade)
		if err != nil {
			respondAPIError(c, http.StatusInternalServerError, ErrCodeInternal, "internal error", nil)
			return
		}

		evTime := time.Now().UTC()
		if req.EventTime != nil {
			evTime = req.EventTime.UTC()
		}

		ev := domain.EventEnvelope{
			EventID:        uuid.New(),
			EventType:      domain.EventTypeTradeExecuted,
			EventTime:      evTime,
			ProcessingTime: time.Now().UTC(),
			Source:         req.Source,
			PortfolioID:    req.PortfolioID,
			IdempotencyKey: req.IdempotencyKey,
			Payload:        payloadJSON,
		}

		ctx := c.Request.Context()
		res, err := svc.Ingest(ctx, ev)
		if err != nil {
			status, errCode, msg := MapDomainIngestError(err)
			log.Warn("trade_ingest_failed",
				zap.String("portfolio_id", req.PortfolioID),
				zap.String("request_id", requestID),
				zap.Int64("latency_ms", time.Since(start).Milliseconds()),
				zap.Error(err),
			)
			respondAPIError(c, status, errCode, msg, nil)
			return
		}
		log.Info("trade_event_appended",
			zap.String("portfolio_id", req.PortfolioID),
			zap.String("event_id", res.EventID.String()),
			zap.String("request_id", requestID),
			zap.Int64("latency_ms", time.Since(start).Milliseconds()),
		)

		status := http.StatusCreated
		body := gin.H{
			"event_id": res.EventID.String(),
			"status":   "created",
		}
		if !res.Inserted {
			status = http.StatusOK
			body["status"] = "duplicate"
		}
		c.JSON(status, body)
	}
}

// --- POST /v1/prices ---

type postPriceRequest struct {
	IdempotencyKey string          `json:"idempotency_key" binding:"required"`
	Source         string          `json:"source" binding:"required"`
	EventTime      *time.Time      `json:"event_time"`
	Price          json.RawMessage `json:"price" binding:"required"`
}

func postPriceHandler(svc ingestion.Service, log *zap.Logger, priceStreamPartitions []uuid.UUID) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		requestID := RequestIDFromContext(c)
		var req postPriceRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			respondAPIError(c, http.StatusBadRequest, ErrCodeValidation, "invalid request body including JSON shape", nil)
			return
		}

		var price domain.PricePayload
		if err := json.Unmarshal(req.Price, &price); err != nil {
			respondAPIError(c, http.StatusBadRequest, ErrCodeValidation, "invalid price object", nil)
			return
		}

		partition, err := config.PricePartitionForSymbol(priceStreamPartitions, price.Symbol)
		if err != nil {
			log.Error("price_partition_config", zap.Error(err))
			respondAPIError(c, http.StatusInternalServerError, ErrCodeInternal, "internal error", nil)
			return
		}

		payloadJSON, err := json.Marshal(price)
		if err != nil {
			respondAPIError(c, http.StatusInternalServerError, ErrCodeInternal, "internal error", nil)
			return
		}

		evTime := time.Now().UTC()
		if req.EventTime != nil {
			evTime = req.EventTime.UTC()
		}

		ev := domain.EventEnvelope{
			EventID:        uuid.New(),
			EventType:      domain.EventTypePriceUpdated,
			EventTime:      evTime,
			ProcessingTime: time.Now().UTC(),
			Source:         req.Source,
			PortfolioID:    partition.String(),
			IdempotencyKey: req.IdempotencyKey,
			Payload:        payloadJSON,
		}

		ctx := c.Request.Context()
		res, err := svc.Ingest(ctx, ev)
		if err != nil {
			status, errCode, msg := MapDomainIngestError(err)
			log.Warn("price_ingest_failed",
				zap.String("portfolio_id", partition.String()),
				zap.String("request_id", requestID),
				zap.Int64("latency_ms", time.Since(start).Milliseconds()),
				zap.Error(err),
			)
			respondAPIError(c, status, errCode, msg, nil)
			return
		}
		log.Info("price_event_appended",
			zap.String("portfolio_id", partition.String()),
			zap.String("event_id", res.EventID.String()),
			zap.String("request_id", requestID),
			zap.Int64("latency_ms", time.Since(start).Milliseconds()),
		)

		status := http.StatusCreated
		body := gin.H{
			"event_id": res.EventID.String(),
			"status":   "created",
		}
		if !res.Inserted {
			status = http.StatusOK
			body["status"] = "duplicate"
		}
		c.JSON(status, body)
	}
}

// --- GET /v1/portfolios/:id ---

// PortfolioReadStore loads everything needed by the pure portfolio assembler.
type PortfolioReadStore interface {
	LoadPortfolioAssemblerInput(ctx context.Context, portfolioID uuid.UUID) (portfolio.PortfolioAssemblerInput, bool, error)
}

func getPortfolioHandler(readStore PortfolioReadStore, log *zap.Logger, priceStreamPartitions []uuid.UUID) gin.HandlerFunc {
	return func(c *gin.Context) {
		base, ok := loadPortfolioBaseState(c, readStore, log, priceStreamPartitions, "portfolio")
		if !ok {
			return
		}
		c.JSON(http.StatusOK, base.View)
	}
}
