package api

import (
	"errors"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/KevinMReardon/realtime-portfolio-risk/internal/ai"
)

func postInsightsExplainHandler(
	readStore PortfolioReadStore,
	riskStore RiskReadStore,
	svc InsightsService,
	log *zap.Logger,
	priceStreamPartitions []uuid.UUID,
	sigmaWindowN int,
) gin.HandlerFunc {
	return func(c *gin.Context) {
		raw, _ := io.ReadAll(c.Request.Body)
		if len(raw) == 0 {
			raw = []byte("{}")
		}

		base, ok := loadPortfolioBaseState(c, readStore, log, priceStreamPartitions, "insights_explain")
		if !ok {
			return
		}
		pid := base.PortfolioID

		if riskStore == nil {
			respondAPIError(c, http.StatusServiceUnavailable, ErrCodeInsufficientData,
				"insights explain requires risk read support",
				map[string]any{"reason": InsightsReasonRiskReadUnavailable})
			return
		}

		riskDTO, err := BuildRiskHTTPResponse(c.Request.Context(), riskStore, base, sigmaWindowN)
		if err != nil {
			var unpriced *ErrRiskUnpricedPositions
			var insuff *ErrRiskInsufficientReturnHistory
			switch {
			case errors.As(err, &unpriced):
				respondAPIError(c, http.StatusUnprocessableEntity, ErrCodeInsufficientData,
					"one or more open positions lack a price mark",
					map[string]any{
						"reason":            InsightsReasonUnpricedOpenPositions,
						"unpriced_symbols": unpriced.Symbols,
					})
				return
			case errors.As(err, &insuff):
				respondAPIError(c, http.StatusUnprocessableEntity, ErrCodeInsufficientData,
					"insufficient daily return history for volatility estimate",
					map[string]any{
						"reason":                       InsightsReasonInsufficientReturnHistory,
						"symbols":                      insuff.Symbols,
						"min_daily_returns_required": insuff.MinDailyReturnsRequired,
						"sigma_window_n":               insuff.SigmaWindowN,
					})
				return
			}
			log.Warn("insights_risk_build_failed", zap.String("portfolio_id", pid.String()), zap.Error(err))
			respondAPIError(c, http.StatusInternalServerError, ErrCodeInternal, "internal error", nil)
			return
		}

		recentEnvelopes, err := riskStore.ListRecentEventsForPortfolio(c.Request.Context(), pid, InsightsRecentEventsLimit)
		if err != nil {
			log.Warn("insights_recent_events_failed", zap.String("portfolio_id", pid.String()), zap.Error(err))
			respondAPIError(c, http.StatusInternalServerError, ErrCodeInternal, "internal error", nil)
			return
		}
		recentSummaries := make([]InsightsEventSummary, 0, len(recentEnvelopes))
		for _, ev := range recentEnvelopes {
			recentSummaries = append(recentSummaries, eventSummaryForInsights(ev))
		}

		ctxDTO := &InsightsExplainContext{
			PortfolioID:   pid.String(),
			Portfolio:     base.View,
			Risk:          riskDTO,
			RecentEvents:  recentSummaries,
			ClientPayload: raw,
		}

		if svc == nil {
			respondAPIError(c, http.StatusServiceUnavailable, ErrCodeInsufficientData,
				"AI insights are not configured for this deployment",
				map[string]any{"reason": InsightsReasonOpenAINotConfigured})
			return
		}

		out, err := svc.Explain(c.Request.Context(), InsightsExplainRequest{
			PortfolioID: pid,
			Payload:     raw,
			Context:     ctxDTO,
		})
		if err != nil {
			var val *ai.ValidationError
			if errors.As(err, &val) {
				log.Warn("insights_output_validation_failed",
					zap.String("portfolio_id", pid.String()),
					zap.String("code", val.Code),
				)
				respondAPIError(c, http.StatusBadRequest, ErrCodeValidation, "model output failed validation",
					map[string]any{"code": val.Code, "detail": val.Detail})
				return
			}
			log.Warn("insights_explain_failed", zap.String("portfolio_id", pid.String()), zap.Error(err))
			respondAPIError(c, http.StatusBadGateway, ErrCodeInternal, "upstream insights provider failed", nil)
			return
		}
		if out == nil {
			out = &InsightsExplainResponse{}
		}
		out.Context = ctxDTO
		if out.UsedMetrics == nil {
			out.UsedMetrics = []string{}
		}
		c.JSON(http.StatusOK, out)
	}
}
